package couchbase

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/xo/dbimp"
)

// The forms of a signature.
const (
	// modeObject is a signature that names each column and its kind, and each
	// row is an object.
	modeObject = iota
	// modeStar is the signature {"*":"*"} of SELECT *, whose columns are the
	// keys of the first row (D18).
	modeStar
	// modeRaw is the signature of SELECT RAW, a string that names one kind,
	// and each row is a bare value.
	modeRaw
	// modeNull is the null signature of a statement such as CREATE INDEX or
	// INFER. The rows are read as with a star when the first row is an
	// object, and as with RAW when it is not.
	modeNull
)

// rows reads a response of the query service one token at a time (D36).
type rows struct {
	s    *dbimp.Stream
	dec  *jsontext.Decoder
	mode int

	cols  []string
	kinds []string
	obj   *dbimp.ObjectRows
	vals  []jsontext.Value
	done  bool
	// read is true once a row has reached the caller, so that an error after
	// it wraps dbimp.ErrIncomplete (D107).
	read bool

	httpStatus int
	status     string
	errs       []Error
	mutations  int64
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
)

// readResponse reads a response up to its rows. It returns the error of the
// server when the server sent one before the member results, or when the
// status of the response is not 2xx, and then it reads the whole response,
// which is short. A failure before any row, such as a syntax error, does not
// wrap dbimp.ErrIncomplete, which is for a failure after a row (D107).
func readResponse(res *http.Response) (*rows, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder(), httpStatus: res.StatusCode, cols: []string{}}
	if err := r.readHead(); err != nil {
		_ = s.Close()
		return nil, err
	}
	if r.failed() || r.httpStatus >= http.StatusMultipleChoices {
		err := r.drain()
		_ = s.Close()
		if err == nil {
			err = r.error()
		}
		return nil, err
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36).
func (r *rows) Close() error {
	return r.s.Close()
}

// NextRow satisfies driver.RowsColumnScanner.
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	var err error
	if r.mode == modeRaw {
		err = r.nextRaw()
	} else {
		err = r.obj.Next(r.vals)
	}
	if isEOF(err) {
		return r.finish()
	}
	if err == nil {
		r.read = true
	}
	return err
}

// Next satisfies driver.Rows, for a caller that does not use
// RowsColumnScanner.
func (r *rows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	for i := range dest {
		v, err := r.value(i)
		if err != nil {
			return err
		}
		dest[i] = v
	}
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner. A byte slice gets a string
// decoded as base64, or the string as it is if it is not base64 (D44), and
// the JSON text of any other value. A *jsontext.Value gets the JSON text.
// Every other destination gets the value that value returns.
func (r *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	v := r.vals[i]
	switch d := dest.(type) {
	case *[]byte:
		*d = bytesOf(v)
		return nil
	case *sql.RawBytes:
		*d = bytesOf(v)
		return nil
	case *sql.Null[[]byte]:
		d.V, d.Valid = bytesOf(v), !dbimp.IsNull(v)
		return nil
	case *jsontext.Value:
		if len(v) == 0 {
			*d = nil
		} else {
			*d = v.Clone()
		}
		return nil
	}
	val, err := r.value(i)
	if err != nil {
		return err
	}
	return dbimp.Assign(scanCtx, dest, val)
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the kind that the signature names, in upper case.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	if i >= len(r.kinds) || r.kinds[i] == "*" || r.kinds[i] == "" {
		return "JSON"
	}
	return strings.ToUpper(r.kinds[i])
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType. A number can be
// an int64, a float64 or an *apd.Decimal, and a column of the kind json can
// hold any value, so both scan into any.
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	if i < len(r.kinds) {
		if t, ok := scanTypes[r.kinds[i]]; ok {
			return t
		}
	}
	return reflect.TypeFor[any]()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. Every column can
// be NULL or MISSING, because a document has no schema.
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// readHead reads the members of the response before its rows.
func (r *rows) readHead() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the response: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the response: %w", err)
		}
		name := tok.String()
		switch name {
		case "signature":
			if err := r.readSignature(); err != nil {
				return err
			}
		case "results":
			return r.startRows()
		default:
			if err := r.readMember(name); err != nil {
				return err
			}
		}
	}
	// A response with no results, such as a syntax error or a statement that
	// returns nothing. A success of that kind is an empty result.
	if err := r.finish(); !isEOF(err) {
		return err
	}
	return nil
}

// readSignature reads the columns and their kinds.
func (r *rows) readSignature() error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading the signature: %w", err)
	}
	switch v.Kind() {
	case '"':
		kind, err := dbimp.String(v)
		if err != nil {
			return err
		}
		r.mode, r.cols, r.kinds = modeRaw, []string{""}, []string{kind}
	case '{':
		dec := jsontext.NewDecoder(bytes.NewReader(v))
		if _, err := dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the signature: %w", err)
		}
		for dec.PeekKind() != '}' {
			tok, err := dec.ReadToken()
			if err != nil {
				return fmt.Errorf("reading the signature: %w", err)
			}
			col := tok.String()
			kv, err := dec.ReadValue()
			if err != nil {
				return fmt.Errorf("reading the signature: %w", err)
			}
			kind, _ := dbimp.String(kv)
			r.cols, r.kinds = append(r.cols, col), append(r.kinds, kind)
		}
		if len(r.cols) == 1 && r.cols[0] == "*" {
			r.mode, r.cols, r.kinds = modeStar, nil, nil
		}
	default:
		r.mode = modeNull
	}
	return nil
}

// startRows reads the start of the rows.
func (r *rows) startRows() error {
	switch r.mode {
	case modeRaw:
		if err := expect(r.dec, '['); err != nil {
			return fmt.Errorf("reading the results: %w", err)
		}
	case modeStar:
		obj, err := dbimp.NewObjectRows(r.dec, nil)
		if err != nil {
			return err
		}
		r.obj, r.cols = obj, obj.Columns()
		r.kinds = slices.Repeat([]string{"json"}, len(r.cols))
	case modeNull:
		if err := expect(r.dec, '['); err != nil {
			return fmt.Errorf("reading the results: %w", err)
		}
		if r.dec.PeekKind() != '{' {
			r.mode, r.cols, r.kinds = modeRaw, []string{""}, []string{"json"}
			break
		}
		obj, err := dbimp.ContinueObjectRows(r.dec, nil)
		if err != nil {
			return err
		}
		r.mode, r.obj, r.cols = modeStar, obj, obj.Columns()
		r.kinds = slices.Repeat([]string{"json"}, len(r.cols))
	case modeObject:
		obj, err := dbimp.NewObjectRows(r.dec, r.cols)
		if err != nil {
			return err
		}
		r.obj = obj
	}
	r.vals = make([]jsontext.Value, len(r.cols))
	return nil
}

// readMember reads a member of the response that is not the rows.
func (r *rows) readMember(name string) error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading %q of the response: %w", name, err)
	}
	switch name {
	case "errors":
		var errs []Error
		if err := json.Unmarshal(v, &errs); err != nil {
			return fmt.Errorf("reading the errors of the response: %w", err)
		}
		r.errs = append(r.errs, errs...)
	case "status":
		r.status, _ = dbimp.String(v)
	case "metrics":
		var m struct {
			MutationCount int64 `json:"mutationCount"`
		}
		if err := json.Unmarshal(v, &m); err != nil {
			return fmt.Errorf("reading the metrics of the response: %w", err)
		}
		r.mutations = m.MutationCount
	}
	return nil
}

// finish reads the rest of the response after its rows, and the end of the
// body. It returns io.EOF for a result that the server completed, and the
// error of the server otherwise (D21 and D42).
func (r *rows) finish() error {
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		if err := r.readMember(tok.String()); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the response: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	r.done = true
	switch {
	case r.failed() && r.read:
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, r.error())
	case r.failed():
		return fmt.Errorf("reading the result: %w", r.error())
	}
	return io.EOF
}

// failed reports whether the server reported an error or a status that is
// not a success. A status of "stopped" is a failure, because the result was
// cut short. No recording holds "completed", and the driver takes it as a
// success.
func (r *rows) failed() bool {
	return len(r.errs) > 0 || r.status != "" && r.status != "success" && r.status != "completed"
}

func (r *rows) error() error {
	return &ResponseError{HTTPStatus: r.httpStatus, Status: r.status, Errs: r.errs}
}

// nextRaw reads the next bare value of SELECT RAW.
func (r *rows) nextRaw() error {
	if r.dec.PeekKind() == ']' {
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of the results: %w", err)
		}
		return io.EOF
	}
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	r.vals[0] = v.Clone()
	return nil
}

// drain reads every row that is left, and the rest of the response.
func (r *rows) drain() error {
	for {
		if err := r.NextRow(); err != nil {
			if isEOF(err) {
				return nil
			}
			return err
		}
	}
}

// column returns the index of a column, or -1.
func (r *rows) column(name string) int {
	return slices.Index(r.cols, name)
}

// value returns the value of column i as a Go value (D39).
func (r *rows) value(i int) (any, error) {
	v := r.vals[i]
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch v.Kind() {
	case '"':
		return dbimp.String(v)
	case 't', 'f':
		return dbimp.Bool(v)
	case '0':
		return dbimp.Number(v)
	}
	return dbimp.Any(v)
}

// bytesOf returns the bytes of a value for a byte slice (D44).
func bytesOf(v jsontext.Value) []byte {
	if dbimp.IsNull(v) {
		return nil
	}
	if v.Kind() != '"' {
		return bytes.Clone(v)
	}
	s, err := dbimp.String(v)
	if err != nil {
		return bytes.Clone(v)
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b
	}
	return []byte(s)
}

// kinds are the kinds that a signature names for a value, in the order of
// the type table, and the Go value of each (D39 and D44).
var kinds = []struct {
	name string
	goes string
}{
	{"missing", "nil"},
	{"null", "nil"},
	{"boolean", "bool"},
	{"number", "int64, float64, or *apd.Decimal for an integer too large for int64"},
	{"string", "string"},
	{"array", "[]any"},
	{"object", "map[string]any"},
}

// scanTypes are the scan types of the kinds of a signature.
var scanTypes = map[string]reflect.Type{
	"string":  reflect.TypeFor[string](),
	"boolean": reflect.TypeFor[bool](),
	"array":   reflect.TypeFor[[]any](),
	"object":  reflect.TypeFor[map[string]any](),
}

// expect reads one token, and returns an error if it is not kind.
func expect(dec *jsontext.Decoder, kind jsontext.Kind) error {
	tok, err := dec.ReadToken()
	switch {
	case err != nil:
		return err
	case tok.Kind() != kind:
		return fmt.Errorf("reading %v where %v was expected: %w", tok.Kind(), kind, dbimp.ErrInvalidValue)
	}
	return nil
}

func isEOF(err error) bool {
	return errors.Is(err, io.EOF)
}
