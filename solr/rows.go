package solr

import (
	"database/sql/driver"
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

// rows reads the answer of one statement, one tuple at a time (D25 and D166).
// The answer is {"result-set":{"docs":[...]}}. With includeMetadata, the
// first tuple holds isMetadata, the fields of the columns in order, and the
// alias of each field. Each tuple after it is a row, an object whose keys are
// the aliases, in the order of the SELECT. The last tuple holds EOF, and
// EXCEPTION when the statement failed (measured).
type rows struct {
	s   *dbimp.Stream
	dec *jsontext.Decoder

	// load reads the types of the columns that the metadata names. It is
	// nil for a statement of the driver itself.
	load func(fields []string) ([]column, error)

	cols  []string
	types []column
	// index maps the name of a column to its place.
	index map[string]int
	// cur is the current row.
	cur []driver.Value
	// started is true once the first tuple was read. pending is true for a
	// row that readAnswer read and the caller did not take.
	started bool
	pending bool
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the answer is read to
	// its end.
	read bool
	done bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
)

// readAnswer reads an answer up to its first row, so that an error of the
// statement that comes before any row is returned here, and does not wrap
// dbimp.ErrIncomplete (D107). load reads the types of the columns, or is nil
// to leave them untyped.
func readAnswer(res *http.Response, load func([]string) ([]column, error)) (*rows, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder(), load: load}
	if err := r.open(); err != nil {
		_ = s.Close()
		return nil, err
	}
	switch err := r.advance(); {
	case err == nil:
		r.pending = true
	case errors.Is(err, io.EOF):
	default:
		_ = s.Close()
		return nil, err
	}
	return r, nil
}

// noEOF wraps ErrCut around io.EOF and io.ErrUnexpectedEOF, because an answer
// that ends where a value must follow is cut short.
func noEOF(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", ErrCut, io.ErrUnexpectedEOF)
	}
	return err
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the SQL type of the column from metadata.COLUMNS, such as BIGINT, and
// empty for a column that names none, such as an aggregate (D166).
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.types[i].sql
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and
// D166).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.types[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. Every column
// can be NULL, and the server names no other (D166).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). Solr has no way to stop a statement of /sql (D166).
func (r *rows) Close() error {
	return r.s.Close()
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it returns an error if the answer
// holds an exception or was cut short (D21 and D166).
func (r *rows) NextRow() error {
	if r.pending {
		r.pending = false
		r.read = true
		return nil
	}
	if r.done {
		return io.EOF
	}
	if err := r.advance(); err != nil {
		return r.fail(err)
	}
	r.read = true
	return nil
}

// Next satisfies driver.Rows, for a caller that does not use
// RowsColumnScanner.
func (r *rows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	copy(dest, r.cur)
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner.
func (r *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	return dbimp.Assign(scanCtx, dest, r.cur[i])
}

// open reads the start of the answer, up to the first tuple.
func (r *rows) open() error {
	tok, err := r.dec.ReadToken()
	switch {
	case err != nil:
		return fmt.Errorf("reading the answer: %w", noEOF(err))
	case tok.Kind() != '{':
		return fmt.Errorf("reading the answer: a %v where an object was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
	}
	if err := r.find("result-set", '{'); err != nil {
		return err
	}
	return r.find("docs", '[')
}

// find reads the keys of an object, and skips their values, until the key
// name. It reads the start of the value of name, which has the kind start.
func (r *rows) find(name string, start jsontext.Kind) error {
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the answer: %w", noEOF(err))
		}
		if tok.String() != name {
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading the answer: %w", noEOF(err))
			}
			continue
		}
		tok, err = r.dec.ReadToken()
		switch {
		case err != nil:
			return fmt.Errorf("reading the answer: %w", noEOF(err))
		case tok.Kind() != start:
			return fmt.Errorf("reading the answer: %q holds a %v and not a %v: %w", name, tok.Kind(), start, dbimp.ErrInvalidValue)
		}
		return nil
	}
	return fmt.Errorf("reading the answer: no %q: %w", name, dbimp.ErrInvalidValue)
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row
// reached the caller before err (D107).
func (r *rows) fail(err error) error {
	if errors.Is(err, io.EOF) {
		return err
	}
	r.done = true
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// tupleKind is the kind of a tuple.
type tupleKind int

const (
	tupleRow tupleKind = iota
	tupleMetadata
	tupleEOF
)

// advance reads tuples until the next row, which it decodes into cur. It
// reads the metadata on its way, and returns io.EOF at the tuple EOF, after
// it read the end of the answer. An exception in that tuple is the error.
func (r *rows) advance() error {
	for {
		clear(r.cur)
		if r.dec.PeekKind() == ']' {
			// The array ended with no tuple EOF.
			return fmt.Errorf("reading the answer: no tuple EOF: %w", ErrCut)
		}
		var (
			kind   tupleKind
			meta   metadata
			exc    string
			first  = true
			keys   []string
			values []jsontext.Value
		)
		fresh := !r.started
		err := r.object(func(key string, v jsontext.Value) error {
			if first {
				first = false
				switch key {
				case "isMetadata":
					kind = tupleMetadata
				case "EOF", "EXCEPTION":
					kind = tupleEOF
				}
			}
			switch kind {
			case tupleMetadata:
				return meta.set(key, v)
			case tupleEOF:
				if key == "EXCEPTION" {
					s, err := dbimp.String(v)
					if err != nil {
						return err
					}
					exc = s
				}
				return nil
			}
			if r.cols == nil {
				// The answer has no metadata, so the keys of the first row
				// name the columns.
				keys = append(keys, key)
				values = append(values, slices.Clone(v))
				return nil
			}
			return r.set(key, v)
		})
		if err != nil {
			return fmt.Errorf("reading a tuple: %w", noEOF(err))
		}
		r.started = true
		switch kind {
		case tupleMetadata:
			if !fresh {
				return fmt.Errorf("reading the answer: a second tuple of metadata: %w", dbimp.ErrInvalidValue)
			}
			if err := r.setColumns(meta); err != nil {
				return err
			}
			continue
		case tupleEOF:
			return r.finish(exc)
		}
		if r.cols == nil {
			if err := r.setFirst(keys, values); err != nil {
				return err
			}
		}
		return nil
	}
}

// metadata is the first tuple of an answer that asked for it.
type metadata struct {
	fields  []string
	aliases map[string]string
}

// set reads one key of the tuple of metadata.
func (m *metadata) set(key string, v jsontext.Value) error {
	switch key {
	case "fields":
		return json.Unmarshal(v, &m.fields)
	case "aliases":
		return json.Unmarshal(v, &m.aliases)
	}
	return nil
}

// object reads one tuple, an object, and calls fn for each key with its
// value. The value is void after fn returns.
func (r *rows) object(fn func(key string, v jsontext.Value) error) error {
	tok, err := r.dec.ReadToken()
	switch {
	case err != nil:
		return err
	case tok.Kind() != '{':
		return fmt.Errorf("a %v where a tuple was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return err
		}
		key := tok.String()
		v, err := r.dec.ReadValue()
		if err != nil {
			return err
		}
		if err := fn(key, v); err != nil {
			return err
		}
	}
	_, err = r.dec.ReadToken()
	return err
}

// setColumns makes the columns of the metadata, and refuses a statement
// whose columns share a name, because the server then gives wrong values for
// them (measured).
func (r *rows) setColumns(m metadata) error {
	names := make([]string, len(m.fields))
	index := make(map[string]int, len(m.fields))
	for i, f := range m.fields {
		name := f
		if a, ok := m.aliases[f]; ok {
			name = a
		}
		lower := strings.ToLower(name)
		if _, dup := index[lower]; dup {
			return fmt.Errorf("reading the columns: two columns are named %q, and the server gives wrong values for them: %w", name, dbimp.ErrNotSupported)
		}
		names[i], index[lower] = name, i
	}
	types := make([]column, len(names))
	if r.load != nil {
		var err error
		if types, err = r.load(m.fields); err != nil {
			return err
		}
	}
	r.cols, r.types, r.index = names, types, index
	r.cur = make([]driver.Value, len(names))
	return nil
}

// setFirst makes the columns from the keys of the first row of an answer with
// no metadata, and decodes the row.
func (r *rows) setFirst(keys []string, values []jsontext.Value) error {
	r.cols = keys
	r.index = make(map[string]int, len(keys))
	for i, k := range keys {
		r.index[strings.ToLower(k)] = i
	}
	r.types = make([]column, len(keys))
	if r.load != nil {
		var err error
		if r.types, err = r.load(keys); err != nil {
			return err
		}
	}
	r.cur = make([]driver.Value, len(keys))
	for i, v := range values {
		var err error
		if r.cur[i], err = decode(r.types[i], v); err != nil {
			return fmt.Errorf("reading the column %s: %w", r.cols[i], err)
		}
	}
	return nil
}

// set decodes the value of the key of a row into its column.
func (r *rows) set(key string, v jsontext.Value) error {
	i, ok := r.index[strings.ToLower(key)]
	if !ok {
		return fmt.Errorf("reading a row: the key %q names no column: %w", key, dbimp.ErrColumnCount)
	}
	// The value is decoded before the next call to the decoder, which reuses
	// its buffer.
	val, err := decode(r.types[i], v)
	if err != nil {
		return fmt.Errorf("reading the column %s: %w", r.cols[i], err)
	}
	r.cur[i] = val
	return nil
}

// finish reads the end of the answer after the tuple EOF, and returns the
// exception that the tuple held, or io.EOF.
func (r *rows) finish(exception string) error {
	r.done = true
	if exception != "" {
		return &Error{HTTPStatus: http.StatusOK, Message: exception}
	}
	for _, kind := range []jsontext.Kind{']', '}', '}'} {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the answer: %w", noEOF(err))
		}
		if tok.Kind() != kind {
			return fmt.Errorf("reading the end of the answer: a tuple after the tuple EOF: %w", dbimp.ErrInvalidValue)
		}
	}
	if err := r.s.End(); err != nil {
		return err
	}
	return io.EOF
}
