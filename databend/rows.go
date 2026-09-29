package databend

import (
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The states of a query (measured).
const (
	stateSucceeded = "Succeeded"
	stateFailed    = "Failed"
)

// rows reads a result one page at a time, and each page one token at a time
// (D25 and D123).
type rows struct {
	c *conn
	// ctx is the context of QueryContext, which each fetch of a page uses
	// (D123). database/sql closes the rows when it ends.
	ctx context.Context //nolint:containedctx // D123 keeps the context of QueryContext for the fetch of each page.
	// id is the query id that the driver chose, and cancel says how it stops
	// the query (D123).
	id     string
	cancel string
	// header holds the headers of each request of the query.
	header http.Header
	// prev is the session before the statement, and database and settings
	// what the statement set for itself, which the connection does not keep
	// (D122).
	prev     session
	database string
	settings map[string]string
	// sent is the session that the request sent, and ran the session that
	// the statement ran in: the settings of sent, and over them those of the
	// last response. ran says how the values are written.
	sent session
	ran  session

	p    *page
	rd   pageReader
	body io.Closer
	// read is true once a row reached the caller, and ended once the query
	// needs nothing more from the server.
	read  bool
	ended bool
	// cur is the current row.
	cur []driver.Value
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*rows)(nil)
)

// hasTimestamp reports whether a type of types holds a Timestamp.
func hasTimestamp(types []*colType) bool {
	for _, t := range types {
		if t.kind == kindTimestamp || hasTimestamp(t.elems) {
			return true
		}
	}
	return false
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	if r.p.cols == nil {
		return []string{}
	}
	return r.p.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the type that schema names, in upper case, such as DECIMAL(38, 10),
// without Nullable.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return strings.ToUpper(r.p.types[i].name)
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D118).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return r.p.types[i].scanType()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable, by the Nullable
// of schema.
func (r *rows) ColumnTypeNullable(i int) (bool, bool) {
	return r.p.types[i].nullable, true
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale, for
// a Decimal.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	t := r.p.types[i]
	if t.kind != kindDecimal || len(t.args) != 2 {
		return 0, 0, false
	}
	return int64(t.args[0]), int64(t.args[1]), true
}

// Close satisfies driver.Rows. Before the end of the result, it closes the
// body, reads nothing more (D36), and kills the query on the server, with
// cancel=kill (D123).
func (r *rows) Close() error {
	if r.ended {
		return nil
	}
	r.ended = true
	err := r.body.Close()
	if r.cancel == CancelKill {
		if kerr := r.c.c.stop(r.ctx, r.killURI(), r.header); kerr != nil {
			err = errors.Join(err, fmt.Errorf("killing the query: %w", kerr))
		}
	}
	return err
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row,
// which ScanColumn assigns.
func (r *rows) NextRow() error {
	if r.ended && r.rd == nil {
		return io.EOF
	}
	for {
		if len(r.cur) != len(r.p.cols) {
			r.cur = make([]driver.Value, len(r.p.cols))
		}
		err := r.rd.row(r.cur)
		if err == nil {
			r.read = true
			return nil
		}
		if !errors.Is(err, io.EOF) {
			return r.failure(err)
		}
		if err := r.rd.tail(r.p); err != nil {
			return r.failure(err)
		}
		if r.p.err != nil {
			r.finish()
			return r.failure(r.p.err)
		}
		if r.p.nextURI == "" || r.p.nextURI == r.p.finalURI {
			r.finish()
			r.rd = nil
			return io.EOF
		}
		if err := r.nextPage(); err != nil {
			return err
		}
	}
}

// Next satisfies driver.Rows, for a caller that does not scan through
// ScanColumn.
func (r *rows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	copy(dest, r.cur)
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner. It hands the value of the
// column to dbimp.Assign, which stores a decimal, a slice and a map as they
// are in a *any.
func (r *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	return dbimp.Assign(scanCtx, dest, r.cur[i])
}

// open reads the first response, and each page after it while the query
// starts and names no columns.
func (r *rows) open(res *http.Response) error {
	if err := r.start(res, nil); err != nil {
		return err
	}
	// next_uri follows the rows, so the driver reads the tail of a page of a
	// query that starts, which names no columns and holds no rows, to learn
	// it.
	for r.p.hasResultSet == nil && r.p.state != stateSucceeded && r.p.state != stateFailed {
		if err := r.rd.row(nil); !errors.Is(err, io.EOF) {
			_ = r.body.Close()
			return fmt.Errorf("reading a page of a query that starts: %w", errors.Join(err, dbimp.ErrInvalidValue))
		}
		if err := r.rd.tail(r.p); err != nil {
			return err
		}
		if r.p.nextURI == "" {
			r.rd = &jsonPage{done: true}
			return nil
		}
		if err := r.nextPage(); err != nil {
			return err
		}
	}
	return nil
}

// start reads the head of the response res, which follows the page prev, or
// is the first when prev is nil.
func (r *rows) start(res *http.Response, prev *page) error {
	p := &page{}
	rd := newJSONPage(res)
	r.p, r.rd, r.body = p, rd, res.Body
	if err := rd.head(p); err != nil {
		return err
	}
	if len(p.types) == 0 && prev != nil {
		p.cols, p.types = prev.cols, prev.types
	}
	if err := r.keep(p); err != nil {
		_ = r.body.Close()
		return err
	}
	if p.err != nil {
		// The server aborts a transaction on any error of a statement, even
		// one whose answer says Active, such as an unknown table (measured),
		// so the transaction ended (D122).
		if t := r.c.tx; t != nil && t.ended == nil {
			t.ended, t.failed = fmt.Errorf("%w: %w", errTxFailed, p.err), true
		}
		_ = rd.tail(p)
		r.finish()
		return r.failure(p.err)
	}
	if prev != nil && prev.fmt != nil && len(prev.types) > 0 {
		p.fmt = prev.fmt
		return nil
	}
	f, err := r.format(p)
	if err != nil {
		_ = r.body.Close()
		return err
	}
	p.fmt = f
	return nil
}

// keep keeps the session of p for the connection, without the settings of
// the statement (D122), and follows the state of a transaction (D121).
func (r *rows) keep(p *page) error {
	if p.session == nil {
		return nil
	}
	s, err := parseSession(p.session)
	if err != nil {
		return err
	}
	r.ran = maps.Clone(s)
	if r.sent != nil {
		m := r.sent.settings()
		maps.Copy(m, s.settings())
		r.ran.setSettings(m)
	}
	s.restore(r.prev, r.database, r.settings)
	r.c.sess, r.c.node = s, p.nodeID
	if t := r.c.tx; t != nil && t.ended == nil {
		switch s.str("txn_state") {
		case txActive:
		case txFail:
			t.ended, t.failed = errTxFailed, true
		default:
			t.ended = errTxCommitted
		}
	}
	return nil
}

// format returns how the values of p are written, by the session that the
// statement ran in. A Timestamp in a session that names no timezone reads in
// the timezone of the server.
func (r *rows) format(p *page) (*format, error) {
	s := r.ran
	if s == nil {
		s = r.sent
	}
	var zone *time.Location
	if s.settings()["timezone"] == "" && hasTimestamp(p.types) {
		var err error
		if zone, err = r.c.c.serverZone(r.ctx); err != nil {
			return nil, err
		}
	}
	return s.format(zone)
}

// nextPage reads the head of the page that next_uri names, after the tail of
// the current page.
func (r *rows) nextPage() error {
	uri := r.p.nextURI
	res, err := r.c.c.do(r.ctx, http.MethodGet, uri, nil, r.header)
	if err != nil {
		return r.failure(fmt.Errorf("reading the page %s: %w", uri, err))
	}
	return r.start(res, r.p)
}

// finish frees the query on the server, by its final URI, once the rows are
// read or the query failed (D123). It sends nothing more after that.
func (r *rows) finish() {
	if r.ended {
		return
	}
	r.ended = true
	if uri := r.p.finalURI; uri != "" {
		_ = r.c.c.stop(r.ctx, uri, r.header)
	}
}

// failure returns err, and wraps dbimp.ErrIncomplete once a row reached the
// caller (D107).
func (r *rows) failure(err error) error {
	if r.read {
		return fmt.Errorf("%w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// killURI returns the URI that kills the query. It needs no response,
// because the driver chose the id (D123).
func (r *rows) killURI() string {
	if r.p != nil && r.p.killURI != "" {
		return r.p.killURI
	}
	return "/v1/query/" + r.id + "/kill"
}

// serverZone returns the timezone of the server, which it reads once with
// SELECT timezone().
func (c *Connector) serverZone(ctx context.Context) (*time.Location, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.zone != nil {
		return c.zone, nil
	}
	body, err := json.Marshal(map[string]any{"sql": "SELECT timezone()", "session": newSession(&c.cfg)})
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	res, err := c.do(ctx, http.MethodPost, "/v1/query", body, nil)
	if err != nil {
		return nil, fmt.Errorf("reading the timezone of the server: %w", err)
	}
	defer res.Body.Close()
	var answer struct {
		Data     [][]string `json:"data"`
		FinalURI string     `json:"final_uri"`
	}
	if err := json.UnmarshalRead(res.Body, &answer); err != nil {
		return nil, fmt.Errorf("reading the timezone of the server: %w", err)
	}
	if len(answer.Data) != 1 || len(answer.Data[0]) != 1 {
		return nil, fmt.Errorf("reading the timezone of the server: no value: %w", dbimp.ErrInvalidValue)
	}
	loc, err := time.LoadLocation(answer.Data[0][0])
	if err != nil {
		return nil, fmt.Errorf("reading the timezone %q of the server: %w", answer.Data[0][0], err)
	}
	if answer.FinalURI != "" {
		_ = c.stop(ctx, answer.FinalURI, nil)
	}
	c.zone = loc
	return loc, nil
}
