package hive

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/xo/dbimp"
)

// The calls of TCLIService that the driver sends, as Thrift knows them.
const (
	callOpenSession        = "OpenSession"
	callCloseSession       = "CloseSession"
	callGetInfo            = "GetInfo"
	callExecuteStatement   = "ExecuteStatement"
	callGetOperationStatus = "GetOperationStatus"
	callCancelOperation    = "CancelOperation"
	callCloseOperation     = "CloseOperation"
	callGetResultSetMeta   = "GetResultSetMetadata"
	callFetchResults       = "FetchResults"
)

// protocolVersion is HIVE_CLI_SERVICE_PROTOCOL_V10 (D235). Hive answers with it
// and Impala answers with V6, which still returns columns (docs/HIVE.md, Requests).
const protocolVersion = 9

// The codes of a TStatus.
const (
	statusSuccess         = 0
	statusSuccessWithInfo = 1
	statusStillExecuting  = 2
	statusError           = 3
	statusInvalidHandle   = 4
)

// The states of an operation.
const (
	stateInitialized = 0
	stateRunning     = 1
	stateFinished    = 2
	stateCanceled    = 3
	stateClosed      = 4
	stateError       = 5
	stateUnknown     = 6
	statePending     = 7
	stateTimedOut    = 8
)

// The kinds of info that GetInfo answers on both flavors (docs/HIVE.md, Flavors).
const (
	infoServerName = 13
	infoDBMSVer    = 18
)

// handleID is a THandleIdentifier. The driver sends it back as it came.
type handleID struct {
	guid, secret []byte
}

// opHandle is a TOperationHandle.
type opHandle struct {
	id           handleID
	opType       int32
	hasResultSet bool
}

// sessionHandle is a TSessionHandle.
type sessionHandle struct {
	id handleID
}

// status is a TStatus.
type status struct {
	code     int32
	info     []string
	sqlState string
	errCode  int32
	message  string
}

// err returns the error that the status holds, or nil for a success.
func (s *status) err() error {
	switch s.code {
	case statusSuccess, statusSuccessWithInfo, statusStillExecuting:
		return nil
	}
	return s.toError()
}

// toError returns the Error of the status.
func (s *status) toError() *Error {
	msg := s.message
	if msg == "" {
		msg = fmt.Sprintf("the server answered the status %d", s.code)
	}
	return &Error{SQLState: s.sqlState, Code: s.errCode, Message: msg, Info: s.info}
}

// reply is the answer to one call, read up to the fields of its result.
type reply struct {
	body io.ReadCloser
	d    *dbimp.ThriftDecoder
}

// Close reads what is left of the answer, and closes the body. A body that does
// not close cannot fail a call whose result was read.
func (r *reply) Close() {
	_, _ = io.Copy(io.Discard, io.LimitReader(r.body, 1<<10))
	_ = r.body.Close()
}

// result reads the fields of the result struct, and calls field for each. A
// field that field does not take is skipped.
func (r *reply) result(field func(id int16, t dbimp.ThriftType) (bool, error)) error {
	for {
		t, id, err := r.d.ReadField()
		if err != nil {
			return fmt.Errorf("reading the reply: %w", err)
		}
		if t == dbimp.ThriftStop {
			return nil
		}
		if id != 0 {
			if err := r.d.Skip(t); err != nil {
				return fmt.Errorf("reading the reply: %w", err)
			}
			continue
		}
		return readStruct(r.d, field)
	}
}

// readStruct reads the fields of a struct, and calls field for each. A field
// that field does not take is skipped.
func readStruct(d *dbimp.ThriftDecoder, field func(id int16, t dbimp.ThriftType) (bool, error)) error {
	for {
		t, id, err := d.ReadField()
		if err != nil {
			return fmt.Errorf("reading a struct: %w", err)
		}
		if t == dbimp.ThriftStop {
			return nil
		}
		ok, err := field(id, t)
		if err != nil {
			return err
		}
		if !ok {
			if err := d.Skip(t); err != nil {
				return fmt.Errorf("skipping the field %d: %w", id, err)
			}
		}
	}
}

// call sends one call, and returns its reply with the decoder at the start of the
// result. build writes the fields of the request struct. A Thrift exception, and
// a response that is not 2xx, are errors. The call is sent once (D8).
func (c *Connector) call(ctx context.Context, name string, build func(*dbimp.ThriftEncoder)) (*reply, error) {
	var e dbimp.ThriftEncoder
	e.Message(name, dbimp.ThriftCall, 1)
	e.Field(dbimp.ThriftStruct, 1)
	build(&e)
	e.Stop()
	e.Stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(e.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-thrift")
	req.Header.Set("Accept", "application/x-thrift")
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.user(), c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	d := dbimp.NewThriftDecoder(res.Body)
	got, mt, _, err := d.ReadMessage()
	if err != nil {
		_ = res.Body.Close()
		return nil, fmt.Errorf("reading the reply to %s: %w", name, err)
	}
	r := &reply{body: res.Body, d: d}
	switch {
	case mt == dbimp.ThriftException:
		defer r.Close()
		return nil, readException(d, name)
	case mt != dbimp.ThriftReply || got != name:
		_ = res.Body.Close()
		return nil, fmt.Errorf("reading the reply to %s: a message %q of type %d: %w", name, got, mt, dbimp.ErrInvalidValue)
	}
	return r, nil
}

// readException reads a Thrift exception: a message, and a type.
func readException(d *dbimp.ThriftDecoder, name string) error {
	e := &Error{Message: "the server raised an exception for " + name}
	err := readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		if id != 1 || t != dbimp.ThriftString {
			return false, nil
		}
		msg, err := d.ReadString()
		if err != nil {
			return false, fmt.Errorf("reading an exception: %w", err)
		}
		e.Message = msg
		return true, nil
	})
	if err != nil {
		return err
	}
	return e
}

// readStatus reads a TStatus.
func readStatus(d *dbimp.ThriftDecoder) (*status, error) {
	s := &status{}
	err := readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		var err error
		switch {
		case id == 1 && t == dbimp.ThriftI32:
			s.code, err = d.ReadI32()
		case id == 2 && t == dbimp.ThriftList:
			s.info, err = readStrings(d)
		case id == 3 && t == dbimp.ThriftString:
			s.sqlState, err = d.ReadString()
		case id == 4 && t == dbimp.ThriftI32:
			s.errCode, err = d.ReadI32()
		case id == 5 && t == dbimp.ThriftString:
			s.message, err = d.ReadString()
		default:
			return false, nil
		}
		return true, err
	})
	if err != nil {
		return nil, fmt.Errorf("reading a status: %w", err)
	}
	return s, nil
}

// readStrings reads a list of strings.
func readStrings(d *dbimp.ThriftDecoder) ([]string, error) {
	et, n, err := d.ReadList()
	if err != nil {
		return nil, err
	}
	var out []string
	for range n {
		if et != dbimp.ThriftString {
			if err := d.Skip(et); err != nil {
				return nil, err
			}
			continue
		}
		s, err := d.ReadString()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// readID reads a THandleIdentifier.
func readID(d *dbimp.ThriftDecoder) (handleID, error) {
	var h handleID
	err := readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		if t != dbimp.ThriftString || (id != 1 && id != 2) {
			return false, nil
		}
		b, err := d.ReadBinary()
		if err != nil {
			return false, err
		}
		if id == 1 {
			h.guid = b
		} else {
			h.secret = b
		}
		return true, nil
	})
	return h, err
}

// readSession reads a TSessionHandle.
func readSession(d *dbimp.ThriftDecoder) (*sessionHandle, error) {
	h := &sessionHandle{}
	err := readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		if id != 1 || t != dbimp.ThriftStruct {
			return false, nil
		}
		var err error
		h.id, err = readID(d)
		return true, err
	})
	return h, err
}

// readOp reads a TOperationHandle.
func readOp(d *dbimp.ThriftDecoder) (*opHandle, error) {
	h := &opHandle{}
	err := readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		var err error
		switch {
		case id == 1 && t == dbimp.ThriftStruct:
			h.id, err = readID(d)
		case id == 2 && t == dbimp.ThriftI32:
			h.opType, err = d.ReadI32()
		case id == 3 && t == dbimp.ThriftBool:
			h.hasResultSet, err = d.ReadBool()
		default:
			return false, nil
		}
		return true, err
	})
	return h, err
}

// writeID writes the handle identifier as the field id of the struct that is
// open.
func (h handleID) write(e *dbimp.ThriftEncoder, id int16) {
	e.Field(dbimp.ThriftStruct, id)
	e.Field(dbimp.ThriftString, 1)
	e.Binary(h.guid)
	e.Field(dbimp.ThriftString, 2)
	e.Binary(h.secret)
	e.Stop()
}

// writeSession writes the session handle as the field 1 of the request.
func (h *sessionHandle) write(e *dbimp.ThriftEncoder) {
	e.Field(dbimp.ThriftStruct, 1)
	h.id.write(e, 1)
	e.Stop()
}

// writeOp writes the operation handle as the field 1 of the request.
func (h *opHandle) write(e *dbimp.ThriftEncoder) {
	e.Field(dbimp.ThriftStruct, 1)
	h.id.write(e, 1)
	e.Field(dbimp.ThriftI32, 2)
	e.I32(h.opType)
	e.Field(dbimp.ThriftBool, 3)
	e.Bool(h.hasResultSet)
	e.Stop()
}

// openSession sends OpenSession, and returns the handle of the session.
func (c *Connector) openSession(ctx context.Context) (*sessionHandle, error) {
	r, err := c.call(ctx, callOpenSession, func(e *dbimp.ThriftEncoder) {
		e.Field(dbimp.ThriftI32, 1)
		e.I32(protocolVersion)
		e.Field(dbimp.ThriftString, 2)
		e.String(c.cfg.user())
		e.Field(dbimp.ThriftString, 3)
		e.String(c.cfg.Password)
	})
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var (
		st      *status
		session *sessionHandle
	)
	err = r.result(func(id int16, t dbimp.ThriftType) (bool, error) {
		var err error
		switch {
		case id == 1 && t == dbimp.ThriftStruct:
			st, err = readStatus(r.d)
		case id == 3 && t == dbimp.ThriftStruct:
			session, err = readSession(r.d)
		default:
			return false, nil
		}
		return true, err
	})
	if err != nil {
		return nil, err
	}
	if st == nil || session == nil {
		return nil, fmt.Errorf("reading OpenSession: no status or no session: %w", dbimp.ErrInvalidValue)
	}
	if err := st.err(); err != nil {
		return nil, err
	}
	return session, nil
}

// closeSession sends CloseSession.
func (c *Connector) closeSession(ctx context.Context, s *sessionHandle) error {
	return c.statusCall(ctx, callCloseSession, s.write)
}

// statusCall sends a call whose reply holds a TStatus and nothing else of use.
func (c *Connector) statusCall(ctx context.Context, name string, build func(*dbimp.ThriftEncoder)) error {
	r, err := c.call(ctx, name, build)
	if err != nil {
		return err
	}
	defer r.Close()
	var st *status
	if err := r.result(func(id int16, t dbimp.ThriftType) (bool, error) {
		if id != 1 || t != dbimp.ThriftStruct {
			return false, nil
		}
		var err error
		st, err = readStatus(r.d)
		return true, err
	}); err != nil {
		return err
	}
	if st == nil {
		return fmt.Errorf("reading %s: no status: %w", name, dbimp.ErrInvalidValue)
	}
	return st.err()
}

// getInfo sends GetInfo, and returns the string that it answers.
func (c *Connector) getInfo(ctx context.Context, s *sessionHandle, kind int32) (string, error) {
	r, err := c.call(ctx, callGetInfo, func(e *dbimp.ThriftEncoder) {
		s.write(e)
		e.Field(dbimp.ThriftI32, 2)
		e.I32(kind)
	})
	if err != nil {
		return "", err
	}
	defer r.Close()
	var (
		st  *status
		val string
	)
	err = r.result(func(id int16, t dbimp.ThriftType) (bool, error) {
		var err error
		switch {
		case id == 1 && t == dbimp.ThriftStruct:
			st, err = readStatus(r.d)
		case id == 2 && t == dbimp.ThriftStruct:
			err = readStruct(r.d, func(id int16, t dbimp.ThriftType) (bool, error) {
				if id != 1 || t != dbimp.ThriftString {
					return false, nil
				}
				var err error
				val, err = r.d.ReadString()
				return true, err
			})
		default:
			return false, nil
		}
		return true, err
	})
	if err != nil {
		return "", err
	}
	if st != nil {
		if err := st.err(); err != nil {
			return "", err
		}
	}
	return val, nil
}

// execute sends ExecuteStatement with runAsync, and returns the handle of the
// operation. conf is the confOverlay.
func (c *Connector) execute(ctx context.Context, s *sessionHandle, stmt string, conf map[string]string) (*opHandle, error) {
	r, err := c.call(ctx, callExecuteStatement, func(e *dbimp.ThriftEncoder) {
		s.write(e)
		e.Field(dbimp.ThriftString, 2)
		e.String(stmt)
		if len(conf) > 0 {
			e.Field(dbimp.ThriftMap, 3)
			e.Map(dbimp.ThriftString, dbimp.ThriftString, len(conf))
			for k, v := range conf {
				e.String(k)
				e.String(v)
			}
		}
		e.Field(dbimp.ThriftBool, 4)
		e.Bool(true)
	})
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var (
		st *status
		op *opHandle
	)
	err = r.result(func(id int16, t dbimp.ThriftType) (bool, error) {
		var err error
		switch {
		case id == 1 && t == dbimp.ThriftStruct:
			st, err = readStatus(r.d)
		case id == 2 && t == dbimp.ThriftStruct:
			op, err = readOp(r.d)
		default:
			return false, nil
		}
		return true, err
	})
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("reading ExecuteStatement: no status: %w", dbimp.ErrInvalidValue)
	}
	if err := st.err(); err != nil {
		return nil, err
	}
	if op == nil {
		return nil, fmt.Errorf("reading ExecuteStatement: no operation: %w", dbimp.ErrInvalidValue)
	}
	return op, nil
}

// opState is the answer of GetOperationStatus.
type opState struct {
	state int32
	st    status
	// modified is the number of rows that the statement changed, or -1 when the
	// server does not say (Hive does, Impala does not).
	modified int64
}

// operationStatus sends GetOperationStatus.
func (c *Connector) operationStatus(ctx context.Context, op *opHandle) (*opState, error) {
	r, err := c.call(ctx, callGetOperationStatus, op.write)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := &opState{modified: -1}
	var got bool
	err = r.result(func(id int16, t dbimp.ThriftType) (bool, error) {
		var err error
		switch {
		case id == 1 && t == dbimp.ThriftStruct:
			var st *status
			if st, err = readStatus(r.d); err == nil {
				out.st.code, out.st.info = st.code, st.info
				got = true
			}
		case id == 2 && t == dbimp.ThriftI32:
			out.state, err = r.d.ReadI32()
		case id == 3 && t == dbimp.ThriftString:
			out.st.sqlState, err = r.d.ReadString()
		case id == 4 && t == dbimp.ThriftI32:
			out.st.errCode, err = r.d.ReadI32()
		case id == 5 && t == dbimp.ThriftString:
			out.st.message, err = r.d.ReadString()
		case id == 11 && t == dbimp.ThriftI64:
			out.modified, err = r.d.ReadI64()
		default:
			return false, nil
		}
		return true, err
	})
	if err != nil {
		return nil, err
	}
	if !got {
		return nil, fmt.Errorf("reading GetOperationStatus: no status: %w", dbimp.ErrInvalidValue)
	}
	if err := out.st.err(); err != nil {
		return nil, err
	}
	return out, nil
}

// cancelOperation sends CancelOperation.
func (c *Connector) cancelOperation(ctx context.Context, op *opHandle) error {
	return c.statusCall(ctx, callCancelOperation, op.write)
}

// closeOperation sends CloseOperation.
func (c *Connector) closeOperation(ctx context.Context, op *opHandle) error {
	return c.statusCall(ctx, callCloseOperation, op.write)
}

// colDesc describes one column of a result.
type colDesc struct {
	name      string
	typeID    int32
	precision int32
	scale     int32
	length    int32
}

// resultSetMetadata sends GetResultSetMetadata.
func (c *Connector) resultSetMetadata(ctx context.Context, op *opHandle) ([]colDesc, error) {
	r, err := c.call(ctx, callGetResultSetMeta, op.write)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var (
		st   *status
		cols = []colDesc{}
	)
	err = r.result(func(id int16, t dbimp.ThriftType) (bool, error) {
		var err error
		switch {
		case id == 1 && t == dbimp.ThriftStruct:
			st, err = readStatus(r.d)
		case id == 2 && t == dbimp.ThriftStruct:
			cols, err = readSchema(r.d)
		default:
			return false, nil
		}
		return true, err
	})
	if err != nil {
		return nil, err
	}
	if st != nil {
		if err := st.err(); err != nil {
			return nil, err
		}
	}
	return cols, nil
}

// readSchema reads a TTableSchema.
func readSchema(d *dbimp.ThriftDecoder) ([]colDesc, error) {
	cols := []colDesc{}
	err := readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		if id != 1 || t != dbimp.ThriftList {
			return false, nil
		}
		et, n, err := d.ReadList()
		if err != nil {
			return false, err
		}
		for range n {
			if et != dbimp.ThriftStruct {
				if err := d.Skip(et); err != nil {
					return false, err
				}
				continue
			}
			col, err := readColDesc(d)
			if err != nil {
				return false, err
			}
			cols = append(cols, col)
		}
		return true, nil
	})
	return cols, err
}

// readColDesc reads a TColumnDesc: the name, and the type with its qualifiers.
func readColDesc(d *dbimp.ThriftDecoder) (colDesc, error) {
	col := colDesc{typeID: -1}
	err := readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		var err error
		switch {
		case id == 1 && t == dbimp.ThriftString:
			col.name, err = d.ReadString()
		case id == 2 && t == dbimp.ThriftStruct:
			err = readTypeDesc(d, &col)
		default:
			return false, nil
		}
		return true, err
	})
	return col, err
}

// readTypeDesc reads a TTypeDesc, whose first entry is the type of the column.
func readTypeDesc(d *dbimp.ThriftDecoder, col *colDesc) error {
	return readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		if id != 1 || t != dbimp.ThriftList {
			return false, nil
		}
		et, n, err := d.ReadList()
		if err != nil {
			return false, err
		}
		for i := range n {
			if et != dbimp.ThriftStruct || i > 0 {
				if err := d.Skip(et); err != nil {
					return false, err
				}
				continue
			}
			if err := readTypeEntry(d, col); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}

// readTypeEntry reads a TTypeEntry, a union whose first member is the primitive
// entry: the type id and the qualifiers.
func readTypeEntry(d *dbimp.ThriftDecoder, col *colDesc) error {
	return readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		if id != 1 || t != dbimp.ThriftStruct {
			return false, nil
		}
		return true, readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
			var err error
			switch {
			case id == 1 && t == dbimp.ThriftI32:
				col.typeID, err = d.ReadI32()
			case id == 2 && t == dbimp.ThriftStruct:
				err = readQualifiers(d, col)
			default:
				return false, nil
			}
			return true, err
		})
	})
}

// readQualifiers reads TTypeQualifiers: a map from a name to a union with an
// i32 or a string.
func readQualifiers(d *dbimp.ThriftDecoder, col *colDesc) error {
	return readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		if id != 1 || t != dbimp.ThriftMap {
			return false, nil
		}
		kt, vt, n, err := d.ReadMap()
		if err != nil {
			return false, err
		}
		for range n {
			if kt != dbimp.ThriftString || vt != dbimp.ThriftStruct {
				return false, fmt.Errorf("reading the qualifiers: a map of types %d and %d: %w", kt, vt, dbimp.ErrInvalidValue)
			}
			name, err := d.ReadString()
			if err != nil {
				return false, err
			}
			var v int32
			if err := readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
				if id != 1 || t != dbimp.ThriftI32 {
					return false, nil
				}
				var err error
				v, err = d.ReadI32()
				return true, err
			}); err != nil {
				return false, err
			}
			switch name {
			case "precision":
				col.precision = v
			case "scale":
				col.scale = v
			case "characterMaximumLength":
				col.length = v
			}
		}
		return true, nil
	})
}

// column is one column of a page: the values of one kind, and the bitmap of the
// values that are NULL.
type column struct {
	// kind is the member of the union TColumn: 1 bool, 2 byte, 3 i16, 4 i32, 5
	// i64, 6 double, 7 string and 8 binary.
	kind    int16
	ints    []int64
	floats  []float64
	strs    [][]byte
	nulls   []byte
	nValues int
}

// null reports whether the value i is NULL.
func (c *column) null(i int) bool {
	b := i / 8
	return b < len(c.nulls) && c.nulls[b]&(1<<(uint(i)%8)) != 0
}

// page is one answer of FetchResults.
type page struct {
	rows    int
	cols    []column
	hasMore bool
}

// fetchResults sends FetchResults, and returns the page.
func (c *Connector) fetchResults(ctx context.Context, op *opHandle, n int) (*page, error) {
	r, err := c.call(ctx, callFetchResults, func(e *dbimp.ThriftEncoder) {
		op.write(e)
		e.Field(dbimp.ThriftI32, 2)
		e.I32(0)
		e.Field(dbimp.ThriftI64, 3)
		e.I64(int64(n))
		e.Field(dbimp.ThriftI16, 4)
		e.I16(0)
	})
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var (
		st *status
		pg = &page{}
	)
	err = r.result(func(id int16, t dbimp.ThriftType) (bool, error) {
		var err error
		switch {
		case id == 1 && t == dbimp.ThriftStruct:
			st, err = readStatus(r.d)
		case id == 2 && t == dbimp.ThriftBool:
			pg.hasMore, err = r.d.ReadBool()
		case id == 3 && t == dbimp.ThriftStruct:
			err = readRowSet(r.d, pg)
		default:
			return false, nil
		}
		return true, err
	})
	if err != nil {
		return nil, err
	}
	if st != nil {
		if err := st.err(); err != nil {
			return nil, err
		}
	}
	if len(pg.cols) > 0 {
		pg.rows = pg.cols[0].nValues
	}
	return pg, nil
}

// readRowSet reads a TRowSet, whose columns are what the driver takes.
func readRowSet(d *dbimp.ThriftDecoder, pg *page) error {
	return readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		if id != 3 || t != dbimp.ThriftList {
			return false, nil
		}
		et, n, err := d.ReadList()
		if err != nil {
			return false, err
		}
		for range n {
			if et != dbimp.ThriftStruct {
				if err := d.Skip(et); err != nil {
					return false, err
				}
				continue
			}
			col, err := readColumn(d)
			if err != nil {
				return false, err
			}
			pg.cols = append(pg.cols, col)
		}
		return true, nil
	})
}

// readColumn reads a TColumn.
func readColumn(d *dbimp.ThriftDecoder) (column, error) {
	var col column
	err := readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		if id < 1 || id > 8 || t != dbimp.ThriftStruct {
			return false, nil
		}
		col.kind = id
		return true, readValues(d, &col)
	})
	return col, err
}

// readValues reads the struct of a column of one kind: the list of values, and
// the bitmap of NULLs.
func readValues(d *dbimp.ThriftDecoder, col *column) error {
	return readStruct(d, func(id int16, t dbimp.ThriftType) (bool, error) {
		switch {
		case id == 1 && t == dbimp.ThriftList:
			return true, readList(d, col)
		case id == 2 && t == dbimp.ThriftString:
			var err error
			col.nulls, err = d.ReadBinary()
			return true, err
		}
		return false, nil
	})
}

// maxPrealloc bounds the room that a list asks for before its values arrive, so
// that a count that lies costs no memory.
const maxPrealloc = 1 << 16

// readList reads the list of values of a column, by its kind.
func readList(d *dbimp.ThriftDecoder, col *column) error {
	et, n, err := d.ReadList()
	if err != nil {
		return err
	}
	col.nValues = n
	switch col.kind {
	case 1, 2, 3, 4, 5:
		col.ints = make([]int64, 0, min(n, maxPrealloc))
	case 6:
		col.floats = make([]float64, 0, min(n, maxPrealloc))
	default:
		col.strs = make([][]byte, 0, min(n, maxPrealloc))
	}
	for range n {
		switch col.kind {
		case 1:
			var v bool
			if v, err = d.ReadBool(); v {
				col.ints = append(col.ints, 1)
			} else {
				col.ints = append(col.ints, 0)
			}
		case 2:
			var v int8
			v, err = d.ReadI8()
			col.ints = append(col.ints, int64(v))
		case 3:
			var v int16
			v, err = d.ReadI16()
			col.ints = append(col.ints, int64(v))
		case 4:
			var v int32
			v, err = d.ReadI32()
			col.ints = append(col.ints, int64(v))
		case 5:
			var v int64
			v, err = d.ReadI64()
			col.ints = append(col.ints, v)
		case 6:
			var v float64
			v, err = d.ReadDouble()
			col.floats = append(col.floats, v)
		default:
			var v []byte
			v, err = d.ReadBinary()
			col.strs = append(col.strs, v)
		}
		if err != nil {
			return fmt.Errorf("reading a value of a column of kind %d (%d): %w", col.kind, et, err)
		}
	}
	return nil
}
