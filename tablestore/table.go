package tablestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/xo/dbimp"
)

// ColumnType is the type of a column of a table that CreateTable makes.
type ColumnType uint8

// The types of a column. A primary key column is an Integer, a String or a
// Binary. A defined column can be any of the five.
const (
	Integer ColumnType = iota + 1
	Double
	Boolean
	String
	Binary
)

// String satisfies fmt.Stringer.
func (t ColumnType) String() string {
	switch t {
	case Integer:
		return "Integer"
	case Double:
		return "Double"
	case Boolean:
		return "Boolean"
	case String:
		return "String"
	case Binary:
		return "Binary"
	}
	return fmt.Sprintf("ColumnType(%d)", uint8(t))
}

// Column is a column of a table.
type Column struct {
	Name string
	Type ColumnType
}

// Index is a global secondary index. Keys are the names of the columns that
// form the primary key of the index table, in order. Each is a defined column or
// a primary key column of the table. Columns are the names of the defined columns
// that the index table holds besides its key. The server fills the index table in
// the background, so a row can appear in it a moment after the write. A true Sync
// asks the server to update the index with each write, which the protocol has as
// the update mode of an index, and the server can refuse it for a global index
// (D225).
type Index struct {
	Name    string
	Keys    []string
	Columns []string
	Sync    bool
}

// Table describes a table that CreateTable makes. Keys are the primary key
// columns, in order, and Columns are the defined columns, which a mapping table
// and an index can name. The table keeps one version of each cell and never
// expires a row (D225).
type Table struct {
	Name    string
	Keys    []Column
	Columns []Column
	Indexes []Index
}

// The numbers of the fields of the messages of CreateTable, DeleteTable and
// CreateIndex (otsprotocol/table_store.proto of the SDK).
const (
	fieldTableMeta   = 1
	fieldThroughput  = 2
	fieldTableOpts   = 3
	fieldIndexMetas  = 7
	fieldMetaName    = 1
	fieldMetaKey     = 2
	fieldMetaColumn  = 3
	fieldSchemaName  = 1
	fieldSchemaType  = 2
	fieldCapacity    = 1
	fieldTimeToLive  = 1
	fieldMaxVersions = 2
	fieldDeviation   = 5
	fieldAllowUpdate = 6
	fieldStreamSpec  = 5
	fieldLocalTxn    = 8
	fieldStreamOn    = 1
	fieldStreamHours = 2
	fieldCapRead     = 1
	fieldCapWrite    = 2

	fieldIndexName  = 1
	fieldIndexKey   = 2
	fieldIndexCol   = 3
	fieldIndexMode  = 4
	fieldIndexType  = 5
	fieldIndexTable = 1
	fieldIndexMeta  = 2
	fieldIndexBase  = 3

	fieldDeleteName = 1
)

// The values of the enumerations of the API. A primary key column and a defined
// column number their types differently, and an index of the driver is global
// and updated in the background.
const (
	keyInteger = 1
	keyString  = 2
	keyBinary  = 3

	definedInteger = 1
	definedDouble  = 2
	definedBoolean = 3
	definedString  = 4
	definedBinary  = 7

	indexAsync  = 0
	indexSync   = 1
	indexGlobal = 0
)

// noExpiry is the time to live of a table whose rows never expire, in the unit of
// the protocol, which is the second.
const noExpiry = -1

// CreateTable makes the table. It sends one request, which fails with an error
// that matches ErrExists when the table is there. The options change what the
// table keeps: with none, a row never expires, the table keeps one version of a
// cell, and the reserved throughput is zero (D225).
func (c *Connector) CreateTable(ctx context.Context, t Table, opts ...TableOption) error {
	o := tableOptions{maxVersions: 1}
	for _, opt := range opts {
		opt(&o)
	}
	body, err := createTableBody(t, o)
	if err != nil {
		return fmt.Errorf("creating table %s: %w", t.Name, err)
	}
	if err := c.send(ctx, "CreateTable", body); err != nil {
		return fmt.Errorf("creating table %s: %w", t.Name, err)
	}
	return nil
}

// DeleteTable deletes the table and its data, with the indexes that it holds.
// It fails with an error that matches ErrNotFound when there is no such table,
// unless the option WithIfExists is given (D225).
func (c *Connector) DeleteTable(ctx context.Context, name string, opts ...DeleteOption) error {
	var o deleteOptions
	for _, opt := range opts {
		opt(&o)
	}
	if name == "" {
		return fmt.Errorf("deleting a table: the name is empty: %w", dbimp.ErrInvalidValue)
	}
	err := c.send(ctx, "DeleteTable", appendString(nil, fieldDeleteName, name))
	switch {
	case err == nil, o.ifExists && errors.Is(err, ErrNotFound):
		return nil
	}
	return fmt.Errorf("deleting table %s: %w", name, err)
}

// CreateIndex makes the index on a table that exists, and fills it with the
// rows that the table holds, unless the option WithBaseData asks for no rows. It
// fails with an error that matches ErrExists when the index is there (D225).
func (c *Connector) CreateIndex(ctx context.Context, table string, ix Index, opts ...IndexOption) error {
	o := indexOptions{baseData: true}
	for _, opt := range opts {
		opt(&o)
	}
	if table == "" {
		return fmt.Errorf("creating an index: the table name is empty: %w", dbimp.ErrInvalidValue)
	}
	meta, err := indexMeta(ix)
	if err != nil {
		return fmt.Errorf("creating index %s on table %s: %w", ix.Name, table, err)
	}
	body := appendString(nil, fieldIndexTable, table)
	body = appendBytes(body, fieldIndexMeta, meta)
	if o.baseData {
		body = appendInt(body, fieldIndexBase, 1)
	}
	if err := c.send(ctx, "CreateIndex", body); err != nil {
		return fmt.Errorf("creating index %s on table %s: %w", ix.Name, table, err)
	}
	return nil
}

// send sends the action with the body, and reads the empty answer to its end.
// A call of the API is no statement, so it uses the same signature, redirect and
// retry rules as one (D8, D222 and D225).
func (c *Connector) send(ctx context.Context, action string, body []byte) error {
	if c.cfg.User == "" || c.cfg.Password == "" {
		return ErrNoCredentials
	}
	res, err := c.call(ctx, action, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		return fmt.Errorf("reading the answer to %s: %w", action, err)
	}
	return nil
}

// createTableBody returns the body of CreateTable for t.
func createTableBody(t Table, o tableOptions) ([]byte, error) {
	if err := o.check(); err != nil {
		return nil, err
	}
	if t.Name == "" {
		return nil, fmt.Errorf("the table name is empty: %w", dbimp.ErrInvalidValue)
	}
	if len(t.Keys) == 0 {
		return nil, fmt.Errorf("the table has no primary key: %w", dbimp.ErrInvalidValue)
	}
	seen := make(map[string]bool, len(t.Keys)+len(t.Columns))
	meta := appendString(nil, fieldMetaName, t.Name)
	for _, k := range t.Keys {
		if err := t.claim(seen, k.Name); err != nil {
			return nil, err
		}
		typ, err := keyType(k)
		if err != nil {
			return nil, err
		}
		meta = appendBytes(meta, fieldMetaKey, schema(k.Name, typ))
	}
	for _, col := range t.Columns {
		if err := t.claim(seen, col.Name); err != nil {
			return nil, err
		}
		typ, err := definedType(col)
		if err != nil {
			return nil, err
		}
		meta = appendBytes(meta, fieldMetaColumn, schema(col.Name, typ))
	}
	body := appendBytes(nil, fieldTableMeta, meta)
	capacity := appendInt(appendInt(nil, fieldCapRead, o.read), fieldCapWrite, o.write)
	body = appendBytes(body, fieldThroughput, appendBytes(nil, fieldCapacity, capacity))
	body = appendBytes(body, fieldTableOpts, o.message())
	if o.stream > 0 {
		stream := appendInt(nil, fieldStreamOn, 1)
		stream = appendInt(stream, fieldStreamHours, int64(o.stream/time.Hour))
		body = appendBytes(body, fieldStreamSpec, stream)
	}
	for _, ix := range t.Indexes {
		for _, name := range append(append([]string(nil), ix.Keys...), ix.Columns...) {
			if !seen[name] {
				return nil, fmt.Errorf("index %s names column %s, which the table does not have: %w", ix.Name, name, dbimp.ErrInvalidValue)
			}
		}
		m, err := indexMeta(ix)
		if err != nil {
			return nil, err
		}
		body = appendBytes(body, fieldIndexMetas, m)
	}
	if o.localTxn {
		body = appendInt(body, fieldLocalTxn, 1)
	}
	return body, nil
}

// claim records the name of a column, and fails when the name is empty or
// repeats.
func (t Table) claim(seen map[string]bool, name string) error {
	switch {
	case name == "":
		return fmt.Errorf("a column of table %s has no name: %w", t.Name, dbimp.ErrInvalidValue)
	case seen[name]:
		return fmt.Errorf("table %s repeats column %s: %w", t.Name, name, dbimp.ErrInvalidValue)
	}
	seen[name] = true
	return nil
}

// keyType returns the number of the type of a primary key column.
func keyType(c Column) (int64, error) {
	switch c.Type {
	case Integer:
		return keyInteger, nil
	case String:
		return keyString, nil
	case Binary:
		return keyBinary, nil
	}
	return 0, fmt.Errorf("primary key column %s cannot be of type %s: %w", c.Name, c.Type, dbimp.ErrInvalidValue)
}

// definedType returns the number of the type of a defined column.
func definedType(c Column) (int64, error) {
	switch c.Type {
	case Integer:
		return definedInteger, nil
	case Double:
		return definedDouble, nil
	case Boolean:
		return definedBoolean, nil
	case String:
		return definedString, nil
	case Binary:
		return definedBinary, nil
	}
	return 0, fmt.Errorf("column %s has the unknown type %s: %w", c.Name, c.Type, dbimp.ErrInvalidValue)
}

// schema returns a PrimaryKeySchema or a DefinedColumnSchema message.
func schema(name string, typ int64) []byte {
	return appendInt(appendString(nil, fieldSchemaName, name), fieldSchemaType, typ)
}

// indexMeta returns the IndexMeta message of ix.
func indexMeta(ix Index) ([]byte, error) {
	if ix.Name == "" {
		return nil, fmt.Errorf("the index name is empty: %w", dbimp.ErrInvalidValue)
	}
	if len(ix.Keys) == 0 {
		return nil, fmt.Errorf("index %s has no key column: %w", ix.Name, dbimp.ErrInvalidValue)
	}
	m := appendString(nil, fieldIndexName, ix.Name)
	for _, k := range ix.Keys {
		m = appendString(m, fieldIndexKey, k)
	}
	for _, col := range ix.Columns {
		m = appendString(m, fieldIndexCol, col)
	}
	mode := int64(indexAsync)
	if ix.Sync {
		mode = indexSync
	}
	m = appendInt(m, fieldIndexMode, mode)
	return appendInt(m, fieldIndexType, indexGlobal), nil
}
