package dbimp

import (
	"encoding/json/jsontext"
	"fmt"
	"io"
)

// ObjectRows reads a JSON array of objects as rows, by rule 2 of D18.
//
// The columns are the keys of the first object, in the order that they
// arrive, or the columns that the server named. A key that a later object
// lacks is a nil value. A key that only a later object has is an error, and
// never a new column. A JSON null is the value "null", so a driver can tell
// a null from a missing key if its product has both.
type ObjectRows struct {
	dec     *jsontext.Decoder
	cols    []string
	index   map[string]int
	first   []jsontext.Value
	pending bool
	done    bool
}

// NewObjectRows reads the start of an array of objects from dec. If cols is
// nil, it reads the first object to learn the columns, and Next returns that
// object first.
func NewObjectRows(dec *jsontext.Decoder, cols []string) (*ObjectRows, error) {
	if err := readDelim(dec, '['); err != nil {
		return nil, fmt.Errorf("reading the rows: %w", err)
	}
	return ContinueObjectRows(dec, cols)
}

// ContinueObjectRows is NewObjectRows for a caller that has read the start
// of the array, such as to peek at the kind of the first row.
func ContinueObjectRows(dec *jsontext.Decoder, cols []string) (*ObjectRows, error) {
	r := &ObjectRows{dec: dec}
	if cols != nil {
		r.setColumns(cols)
		return r, nil
	}
	if dec.PeekKind() == ']' {
		r.setColumns([]string{})
		return r, nil
	}
	cols, vals, err := readObject(dec)
	if err != nil {
		return nil, fmt.Errorf("reading the first row: %w", err)
	}
	r.setColumns(cols)
	r.first, r.pending = vals, true
	return r, nil
}

// Columns returns the columns of the rows.
func (r *ObjectRows) Columns() []string {
	return r.cols
}

// Next reads the next row into vals, which has one entry for each column.
// It returns io.EOF after the last row, and reads the end of the array.
func (r *ObjectRows) Next(vals []jsontext.Value) error {
	if len(vals) != len(r.cols) {
		return fmt.Errorf("reading a row: %d values for %d columns: %w", len(vals), len(r.cols), ErrColumnCount)
	}
	switch {
	case r.pending:
		copy(vals, r.first)
		r.first, r.pending = nil, false
		return nil
	case r.done:
		return io.EOF
	}
	if r.dec.PeekKind() == ']' {
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of the rows: %w", err)
		}
		r.done = true
		return io.EOF
	}
	if err := readDelim(r.dec, '{'); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	clear(vals)
	for r.dec.PeekKind() != '}' {
		name, val, err := readMember(r.dec)
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		i, ok := r.index[name]
		if !ok {
			return fmt.Errorf("reading a row: %q: %w", name, ErrExtraColumn)
		}
		vals[i] = val
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a row: %w", err)
	}
	return nil
}

func (r *ObjectRows) setColumns(cols []string) {
	r.cols = cols
	r.index = make(map[string]int, len(cols))
	for i, col := range cols {
		r.index[col] = i
	}
}

// ArrayRows reads a JSON array of arrays as rows, by rule 1 of D18. The
// server names the columns elsewhere in the response, and each row is an
// array with one value for each column.
type ArrayRows struct {
	dec  *jsontext.Decoder
	n    int
	done bool
}

// NewArrayRows reads the start of an array of rows of n columns from dec.
func NewArrayRows(dec *jsontext.Decoder, n int) (*ArrayRows, error) {
	if err := readDelim(dec, '['); err != nil {
		return nil, fmt.Errorf("reading the rows: %w", err)
	}
	return &ArrayRows{dec: dec, n: n}, nil
}

// Next reads the next row into vals, which has one entry for each column.
// It returns io.EOF after the last row, and reads the end of the array.
func (r *ArrayRows) Next(vals []jsontext.Value) error {
	if len(vals) != r.n {
		return fmt.Errorf("reading a row: %d values for %d columns: %w", len(vals), r.n, ErrColumnCount)
	}
	if r.done {
		return io.EOF
	}
	if r.dec.PeekKind() == ']' {
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of the rows: %w", err)
		}
		r.done = true
		return io.EOF
	}
	if err := readDelim(r.dec, '['); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	i := 0
	for ; r.dec.PeekKind() != ']'; i++ {
		if i == r.n {
			return fmt.Errorf("reading a row: more than %d values: %w", r.n, ErrColumnCount)
		}
		val, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		vals[i] = val.Clone()
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a row: %w", err)
	}
	if i < r.n {
		return fmt.Errorf("reading a row: %d values for %d columns: %w", i, r.n, ErrColumnCount)
	}
	return nil
}

// readDelim reads one token, and returns an error if it is not the
// delimiter kind.
func readDelim(dec *jsontext.Decoder, kind jsontext.Kind) error {
	tok, err := dec.ReadToken()
	switch {
	case err != nil:
		return err
	case tok.Kind() != kind:
		return fmt.Errorf("%v where %v was expected: %w", tok.Kind(), kind, ErrInvalidValue)
	}
	return nil
}

// readMember reads the name and the value of one member of an object.
func readMember(dec *jsontext.Decoder) (string, jsontext.Value, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return "", nil, err
	}
	name := tok.String()
	val, err := dec.ReadValue()
	if err != nil {
		return "", nil, fmt.Errorf("reading %q: %w", name, err)
	}
	return name, val.Clone(), nil
}

// readObject reads one object, and returns its names and values in the
// order that they arrive.
func readObject(dec *jsontext.Decoder) ([]string, []jsontext.Value, error) {
	if err := readDelim(dec, '{'); err != nil {
		return nil, nil, err
	}
	var (
		names []string
		vals  []jsontext.Value
	)
	for dec.PeekKind() != '}' {
		name, val, err := readMember(dec)
		if err != nil {
			return nil, nil, err
		}
		names, vals = append(names, name), append(vals, val)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, nil, err
	}
	if names == nil {
		names = []string{}
	}
	return names, vals, nil
}
