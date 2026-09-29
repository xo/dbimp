package databend

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/xo/dbimp"
)

// page is what one response says about a query, apart from its rows. The
// server writes the members in this order: id, node_id, state, session,
// error, has_result_set, schema, data, then settings and the URIs (measured).
// So the head of a page, up to data, is known before its first row.
type page struct {
	id     string
	nodeID string
	state  string
	// session is the session that the statement left, as the server wrote
	// it.
	session jsontext.Value
	// err is the error of the query, or nil.
	err *Error
	// hasResultSet is nil while the query starts and knows no columns yet.
	hasResultSet *bool
	// cols are the names of the columns, and types their types, from schema.
	cols  []string
	types []*colType
	// fmt says how the values of the rows are written. The rows code sets it
	// after the head, before the first row.
	fmt *format
	// The URIs of the query.
	nextURI  string
	finalURI string
	killURI  string
}

// pageReader reads one response in one wire format. jsonPage reads JSON, the
// only format of the driver, and a reader of another format, such as Arrow,
// would stand beside it (D119, after D108).
type pageReader interface {
	// head reads the members of the response up to its rows into p.
	head(p *page) error
	// row decodes the next row into dest, by p.types and p.fmt. It returns
	// io.EOF after the last row of the page.
	row(dest []driver.Value) error
	// tail reads the members after the rows into p, and the end of the
	// response. It closes the body.
	tail(p *page) error
}

// jsonPage reads a response of JSON, one token at a time (D25).
type jsonPage struct {
	s    *dbimp.Stream
	p    *page
	arr  *dbimp.ArrayRows
	vals []jsontext.Value
	done bool
}

// ensure the interface.
var _ pageReader = (*jsonPage)(nil)

// newJSONPage returns a reader of the body of res.
func newJSONPage(res *http.Response) *jsonPage {
	return &jsonPage{s: dbimp.NewStream(res.Body)}
}

// head reads the members before data, and the start of data.
func (r *jsonPage) head(p *page) error {
	r.p = p
	dec := r.s.Decoder()
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return r.fail(fmt.Errorf("reading the start of the response: %w", errors.Join(err, dbimp.ErrInvalidValue)))
	}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return r.fail(fmt.Errorf("reading the response: %w", err))
		}
		name := tok.String()
		if name == "data" {
			if dec.PeekKind() == 'n' {
				if _, err := dec.ReadValue(); err != nil {
					return r.fail(fmt.Errorf("reading the rows: %w", err))
				}
				r.done = true
				continue
			}
			if r.arr, err = dbimp.NewArrayRows(dec, len(p.cols)); err != nil {
				return r.fail(err)
			}
			r.vals = make([]jsontext.Value, len(p.cols))
			return nil
		}
		if err := r.member(p, name); err != nil {
			return r.fail(err)
		}
	}
	// A response with no data, such as the answer of a page that is gone.
	r.done = true
	return nil
}

// member reads the value of the member name into p, or skips it.
func (r *jsonPage) member(p *page, name string) error {
	dec := r.s.Decoder()
	v, err := dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading the member %s of the response: %w", name, err)
	}
	var target any
	switch name {
	case "id":
		target = &p.id
	case "node_id":
		target = &p.nodeID
	case "state":
		target = &p.state
	case "next_uri":
		target = &p.nextURI
	case "final_uri":
		target = &p.finalURI
	case "kill_uri":
		target = &p.killURI
	case "has_result_set":
		target = &p.hasResultSet
	case "session":
		if v.Kind() == '{' {
			p.session = v.Clone()
		}
		return nil
	case "error":
		if v.Kind() != '{' {
			return nil
		}
		var e struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(v, &e); err != nil {
			return fmt.Errorf("reading the error of the response: %w", err)
		}
		p.err = &Error{HTTPStatus: http.StatusOK, Code: e.Code, Message: e.Message}
		return nil
	case "schema":
		return p.readSchema(v)
	default:
		return nil
	}
	if v.Kind() == 'n' {
		return nil
	}
	if err := json.Unmarshal(v, target); err != nil {
		return fmt.Errorf("reading the member %s of the response: %w", name, err)
	}
	return nil
}

// readSchema reads the names and the types of the columns.
func (p *page) readSchema(v jsontext.Value) error {
	var schema []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(v, &schema); err != nil {
		return fmt.Errorf("reading the schema of the response: %w", err)
	}
	p.cols, p.types = make([]string, len(schema)), make([]*colType, len(schema))
	for i, c := range schema {
		t, err := parseType(c.Type)
		if err != nil {
			return err
		}
		p.cols[i], p.types[i] = c.Name, t
	}
	return nil
}

// row reads and decodes the next row of data.
func (r *jsonPage) row(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	if err := r.arr.Next(r.vals); err != nil {
		if errors.Is(err, io.EOF) {
			r.done = true
		}
		return err
	}
	for i, v := range r.vals {
		switch v.Kind() {
		case 'n':
			dest[i] = nil
		case '"':
			text, err := dbimp.String(v)
			if err != nil {
				return err
			}
			if dest[i], err = r.p.fmt.decode(r.p.types[i], text); err != nil {
				return fmt.Errorf("reading the column %s: %w", r.p.cols[i], err)
			}
		default:
			return fmt.Errorf("reading the column %s: a %s where the server writes a string: %w", r.p.cols[i], v.Kind(), dbimp.ErrInvalidValue)
		}
	}
	return nil
}

// tail reads the members after data and the end of the response, and closes
// the body.
func (r *jsonPage) tail(p *page) error {
	dec := r.s.Decoder()
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return r.fail(fmt.Errorf("reading the response: %w", err))
		}
		if err := r.member(p, tok.String()); err != nil {
			return r.fail(err)
		}
	}
	if _, err := dec.ReadToken(); err != nil {
		return r.fail(fmt.Errorf("reading the end of the response: %w", err))
	}
	if err := r.s.End(); err != nil {
		return r.fail(err)
	}
	return r.s.Close()
}

// fail closes the body, and returns err.
func (r *jsonPage) fail(err error) error {
	return errors.Join(err, r.s.Close())
}
