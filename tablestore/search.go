package tablestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/xo/dbimp"
)

// SearchFieldType is the type of a field of a search index.
type SearchFieldType uint8

// The types of a field. A Text field uses the analyzer that the server picks when
// the request names none, which is the single word analyzer (D226).
const (
	SearchLong SearchFieldType = iota + 1
	SearchDouble
	SearchBoolean
	SearchKeyword
	SearchText
)

// String satisfies fmt.Stringer.
func (t SearchFieldType) String() string {
	switch t {
	case SearchLong:
		return "SearchLong"
	case SearchDouble:
		return "SearchDouble"
	case SearchBoolean:
		return "SearchBoolean"
	case SearchKeyword:
		return "SearchKeyword"
	case SearchText:
		return "SearchText"
	}
	return fmt.Sprintf("SearchFieldType(%d)", uint8(t))
}

// SearchField is a field of a search index. The index of the field is always on.
// A true Sort lets a query sort and aggregate on the field, and a true Store keeps
// the value in the index so that a search can return it. The server turns Store
// off for a field that has Sort (D226). Name is a column of the table, but the
// server does not check that, and it makes an index for a name that no column has.
type SearchField struct {
	Name  string
	Type  SearchFieldType
	Sort  bool
	Store bool
}

// SearchIndex is a search index of a table. The server fills it from the rows of
// the table in the background, and the index is there at once, with the state
// "index is running" (D226).
type SearchIndex struct {
	Name   string
	Fields []SearchField
}

// SearchIndexOption sets an option of CreateSearchIndex (D109 and D226).
type SearchIndexOption = dbimp.Option[searchOptions]

// searchOptions are the options of CreateSearchIndex. A zero ttl asks for none.
type searchOptions struct {
	ttl time.Duration

	// err is the first option that has a value that the call refuses.
	err error
}

// WithSearchTimeToLive sets the time after which the server deletes a row from the
// index. The protocol counts whole seconds, so d must be a positive number of
// them. The server accepts a value of one day or more, and only for a table that
// refuses updates (WithAllowUpdate(false)). It refuses any other call with
// OTSParameterInvalid. With no option, a row of the index never expires.
func WithSearchTimeToLive(d time.Duration) SearchIndexOption {
	return func(o *searchOptions) {
		if d <= 0 || d%time.Second != 0 || d/time.Second > 1<<31-1 {
			if o.err == nil {
				o.err = fmt.Errorf("applying the option WithSearchTimeToLive: %v is not a positive number of whole seconds that fits 32 bits: %w", d, dbimp.ErrInvalidValue)
			}
			return
		}
		o.ttl = d
	}
}

// The numbers of the fields of the messages of the calls on search indexes, and
// of ListTable (otsprotocol/search.proto and table_store.proto of the SDK).
const (
	fieldSearchTable  = 1
	fieldSearchName   = 2
	fieldSearchSchema = 3
	fieldSearchTTL    = 5

	fieldSchemaFields = 1

	fieldFieldName  = 1
	fieldFieldType  = 2
	fieldFieldIndex = 5
	fieldFieldSort  = 6
	fieldFieldStore = 7

	fieldInfoTable = 1
	fieldInfoName  = 2

	fieldListTables  = 1
	fieldListIndexes = 1
)

// The numbers of the types of a field in the protocol.
const (
	searchTypeLong    = 1
	searchTypeDouble  = 2
	searchTypeBoolean = 3
	searchTypeKeyword = 4
	searchTypeText    = 5
)

// CreateSearchIndex makes the search index on a table that exists. It sends one
// request, which fails with an error that matches ErrExists when the index is
// there. A table that is not there gives OTSParameterInvalid, and no error that
// matches ErrNotFound. With no option, a row of the index never expires (D226).
func (c *Connector) CreateSearchIndex(ctx context.Context, table string, ix SearchIndex, opts ...SearchIndexOption) error {
	var o searchOptions
	for _, opt := range opts {
		opt(&o)
	}
	body, err := createSearchIndexBody(table, ix, o)
	if err != nil {
		return fmt.Errorf("creating search index %s on table %s: %w", ix.Name, table, err)
	}
	if err := c.send(ctx, "CreateSearchIndex", body); err != nil {
		return fmt.Errorf("creating search index %s on table %s: %w", ix.Name, table, err)
	}
	return nil
}

// DeleteSearchIndex deletes the search index of a table. It fails with an error
// that matches ErrNotFound when the table has no such index, unless the option
// WithIfExists is given. A table that is not there also gives an error that matches
// ErrNotFound, but WithIfExists does not ignore it, because only a missing index is
// ignored (D226). DeleteTable refuses a table that has a search
// index, so a caller deletes each search index first (D226).
func (c *Connector) DeleteSearchIndex(ctx context.Context, table, name string, opts ...DeleteOption) error {
	var o deleteOptions
	for _, opt := range opts {
		opt(&o)
	}
	if table == "" || name == "" {
		return fmt.Errorf("deleting a search index: the table name or the index name is empty: %w", dbimp.ErrInvalidValue)
	}
	body := appendString(nil, fieldSearchTable, table)
	body = appendString(body, fieldSearchName, name)
	err := c.send(ctx, "DeleteSearchIndex", body)
	switch {
	case err == nil, o.ifExists && indexMissing(err):
		return nil
	}
	return fmt.Errorf("deleting search index %s of table %s: %w", name, table, err)
}

// indexMissing reports whether err is the answer for an index that is not there on a
// table that is, which has the code OTSObjectNotExist. A table that is not there has
// another code.
func indexMissing(err error) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && e.Code == codeNotExist
}

// ListSearchIndexes returns the names of the search indexes of a table, in the order
// that the server gives, which is the order of their names. A table with none gives
// an empty list. A table that is not there gives an error that matches ErrNotFound (D226).
func (c *Connector) ListSearchIndexes(ctx context.Context, table string) ([]string, error) {
	if table == "" {
		return nil, fmt.Errorf("listing search indexes: the table name is empty: %w", dbimp.ErrInvalidValue)
	}
	var names []string
	err := c.list(ctx, "ListSearchIndex", appendString(nil, fieldSearchTable, table), func(field int, b []byte) error {
		if field != fieldListIndexes {
			return nil
		}
		return eachField(b, func(field int, b []byte) {
			if field == fieldInfoName {
				names = append(names, string(b))
			}
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing the search indexes of table %s: %w", table, err)
	}
	return names, nil
}

// ListTables returns the names of every table of the instance, in the order that
// the server gives. It includes a table that no mapping names, which SQL does not
// show (D226). The driver keeps the whole list in memory.
func (c *Connector) ListTables(ctx context.Context) ([]string, error) {
	var names []string
	err := c.list(ctx, "ListTable", nil, func(field int, b []byte) error {
		if field == fieldListTables {
			names = append(names, string(b))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	return names, nil
}

// list sends the action and reads the answer one field of bytes at a time, and
// calls visit for each. It skips every field of another wire type.
func (c *Connector) list(ctx context.Context, action string, body []byte, visit func(field int, b []byte) error) error {
	if c.cfg.User == "" || c.cfg.Password == "" {
		return ErrNoCredentials
	}
	res, err := c.call(ctx, action, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	p := newPBReader(res.Body)
	for {
		field, wire, err := p.tag()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return invalid("the answer to "+action, err)
		}
		if wire != wireBytes {
			if err := p.skip(wire); err != nil {
				return err
			}
			continue
		}
		n, err := p.length()
		if err != nil {
			return err
		}
		b, err := p.readBytes(n)
		if err != nil {
			return err
		}
		if err := visit(field, b); err != nil {
			return err
		}
	}
}

// eachField calls visit for each field of bytes of the message in b, and ignores
// the others. A message that is cut gives no error here, because the caller read
// b whole from a field of a message that was complete.
func eachField(b []byte, visit func(field int, b []byte)) error {
	p := newPBReader(bytes.NewReader(b))
	for {
		field, wire, err := p.tag()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return invalid("a message of the answer", err)
		}
		if wire != wireBytes {
			if err := p.skip(wire); err != nil {
				return err
			}
			continue
		}
		n, err := p.length()
		if err != nil {
			return err
		}
		v, err := p.readBytes(n)
		if err != nil {
			return err
		}
		visit(field, v)
	}
}

// createSearchIndexBody returns the body of CreateSearchIndex.
func createSearchIndexBody(table string, ix SearchIndex, o searchOptions) ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	switch {
	case table == "":
		return nil, fmt.Errorf("the table name is empty: %w", dbimp.ErrInvalidValue)
	case ix.Name == "":
		return nil, fmt.Errorf("the index name is empty: %w", dbimp.ErrInvalidValue)
	case len(ix.Fields) == 0:
		return nil, fmt.Errorf("the index has no field: %w", dbimp.ErrInvalidValue)
	}
	seen := make(map[string]bool, len(ix.Fields))
	var schema []byte
	for _, f := range ix.Fields {
		switch {
		case f.Name == "":
			return nil, fmt.Errorf("a field of search index %s has no name: %w", ix.Name, dbimp.ErrInvalidValue)
		case seen[f.Name]:
			return nil, fmt.Errorf("search index %s repeats field %s: %w", ix.Name, f.Name, dbimp.ErrInvalidValue)
		}
		seen[f.Name] = true
		typ, err := searchType(f)
		if err != nil {
			return nil, err
		}
		m := appendString(nil, fieldFieldName, f.Name)
		m = appendInt(m, fieldFieldType, typ)
		m = appendInt(m, fieldFieldIndex, 1)
		if f.Sort {
			m = appendInt(m, fieldFieldSort, 1)
		}
		if f.Store {
			m = appendInt(m, fieldFieldStore, 1)
		}
		schema = appendBytes(schema, fieldSchemaFields, m)
	}
	body := appendString(nil, fieldSearchTable, table)
	body = appendString(body, fieldSearchName, ix.Name)
	body = appendBytes(body, fieldSearchSchema, schema)
	if o.ttl > 0 {
		body = appendInt(body, fieldSearchTTL, int64(o.ttl/time.Second))
	}
	return body, nil
}

// searchType returns the number of the type of a field of a search index.
func searchType(f SearchField) (int64, error) {
	switch f.Type {
	case SearchLong:
		return searchTypeLong, nil
	case SearchDouble:
		return searchTypeDouble, nil
	case SearchBoolean:
		return searchTypeBoolean, nil
	case SearchKeyword:
		return searchTypeKeyword, nil
	case SearchText:
		return searchTypeText, nil
	}
	return 0, fmt.Errorf("field %s has the unknown type %s: %w", f.Name, f.Type, dbimp.ErrInvalidValue)
}
