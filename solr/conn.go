package solr

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds nothing on the server, because the SQL of
// Solr has no sessions and no transactions. It keeps the schema of each table
// that it read (D166).
type conn struct {
	c *Connector
	// schemas holds the schema of each table by its name. A table that the
	// server refuses has a nil schema.
	schemas map[string]*schema
}

// ensure the interfaces.
var (
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.NamedValueChecker  = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
)

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option,
// which the statement takes out (D109), a decimal, a dbimp.Date, a
// dbimp.LocalDateTime and a uuid.UUID, which the driver writes as a literal
// of their own (D166). A nil pointer that implements driver.Valuer becomes
// nil. It hands every other value to the default converter of database/sql.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch v := nv.Value.(type) {
	case *apd.Decimal:
		if v == nil {
			nv.Value = nil
		}
		return nil
	case apd.Decimal:
		nv.Value = &v
		return nil
	case dbimp.Date, dbimp.LocalDateTime, uuid.UUID:
		return nil
	}
	if v, ok := nv.Value.(driver.Valuer); ok {
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
			nv.Value = nil
			return nil
		}
	}
	return driver.ErrSkip
}

// QueryContext satisfies driver.QueryerContext. It answers SELECT version()
// itself (D181).
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if dbimp.IsVersionQuery(query) {
		// An option does not apply to this statement, and an argument that
		// is left over goes to the normal path, which refuses it.
		if _, rest := resolve(ctx, &c.c.cfg, args); len(rest) == 0 {
			return c.version(ctx)
		}
	}
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the result to its
// end. The SQL of Solr takes no write (D163), so a statement that writes
// fails with the error of the server.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for {
		if err := r.NextRow(); err != nil {
			if errors.Is(err, io.EOF) {
				return result{}, nil
			}
			return nil, err
		}
	}
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// when it runs, with its arguments.
func (c *conn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return &stmt{c: c, query: query}, nil
}

// Prepare satisfies driver.Conn.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{c: c, query: query}, nil
}

// Begin satisfies driver.Conn. database/sql calls BeginTx.
func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction without a context: %w", dbimp.ErrNotSupported)
}

// BeginTx satisfies driver.ConnBeginTx. Solr has no transactions (D20 and
// D166).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: Solr has none: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It counts the documents of the collection of
// the DSN, which checks the credentials and the handler of SQL. The ordinary
// user cannot read the version (measured), so Ping asks for no more.
func (c *conn) Ping(ctx context.Context) error {
	o, _ := resolve(ctx, &c.c.cfg, nil)
	if err := o.check(); err != nil {
		return err
	}
	r, err := c.run(ctx, o, "SELECT count(*) FROM "+"`"+strings.ReplaceAll(o.collection, "`", "``")+"`", false)
	if err != nil {
		return err
	}
	defer r.Close()
	for {
		if err := r.NextRow(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// query writes the arguments into the statement, sends it, and reads its
// answer up to its first row.
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the query: %w", err)
	}
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	text, err := syntax.Bind(query, args, literal)
	if err != nil {
		return nil, err
	}
	return c.run(ctx, o, text, true)
}

// run sends the text of a statement, and reads its answer up to its first
// row. If typed is true, it reads the type of each column from the schema of
// the tables that the statement names, when it has not read them yet (D166).
func (c *conn) run(ctx context.Context, o options, text string, typed bool) (*rows, error) {
	res, err := c.c.query(ctx, o.collection, o.form(text, c.c.cfg.Mode))
	if err != nil {
		return nil, err
	}
	var load func([]string) ([]column, error)
	if typed {
		load = func(fields []string) ([]column, error) {
			return c.types(ctx, o, text, fields)
		}
	}
	r, err := readAnswer(res, load)
	if err != nil {
		return nil, err
	}
	// The rows never read a type again.
	r.load = nil
	return r, nil
}

// types returns the type of each field of a result, from the schema of the
// tables that the statement reads. A field that no schema holds, such as an
// aggregate, has no type, and its value reads by its JSON token.
func (c *conn) types(ctx context.Context, o options, text string, fields []string) ([]column, error) {
	var schemas []*schema
	for _, table := range tables(text) {
		s, ok := c.schemas[table]
		if !ok {
			var err error
			if s, err = c.readSchema(ctx, o, table); err != nil {
				return nil, err
			}
			if c.schemas == nil {
				c.schemas = map[string]*schema{}
			}
			c.schemas[table] = s
		}
		schemas = append(schemas, s)
	}
	cols := make([]column, len(fields))
	for i, f := range fields {
		for _, s := range schemas {
			if col, ok := s.column(f); ok {
				cols[i] = col
				break
			}
		}
	}
	return cols, nil
}

// stmt is a prepared statement, which runs as its text each time.
type stmt struct {
	c     *conn
	query string
}

// ensure the interfaces.
var (
	_ driver.StmtQueryContext = (*stmt)(nil)
	_ driver.StmtExecContext  = (*stmt)(nil)
)

func (s *stmt) Close() error  { return nil }
func (s *stmt) NumInput() int { return -1 }

func (s *stmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, fmt.Errorf("running a statement without a context: %w", dbimp.ErrNotSupported)
}

func (s *stmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, fmt.Errorf("running a statement without a context: %w", dbimp.ErrNotSupported)
}

func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.c.ExecContext(ctx, s.query, args)
}

func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.c.QueryContext(ctx, s.query, args)
}

// result is the result of a statement that runs with Exec. The SQL of Solr
// changes no rows, and has no id of an insert.
type result struct{}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: the SQL of Solr takes no insert: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("reading the rows affected: the SQL of Solr changes no rows: %w", dbimp.ErrNotSupported)
}
