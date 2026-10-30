package tablestore_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/tablestore"
)

// These tests make tables with CreateTable, CreateIndex and DeleteTable, and
// check them through the driver (D225). Each table has the prefix of this run, and
// TestMain deletes one that a test left.

// connector returns a connector on the instance that opens a connection for each
// request.
func connector(t *testing.T) *tablestore.Connector {
	t.Helper()
	c := tablestore.NewConnector(integrationConfig(t))
	c.NoKeepAlives()
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// create makes the table, deletes it in a cleanup, and returns the name.
func create(t *testing.T, c *tablestore.Connector, tbl tablestore.Table, opts ...tablestore.TableOption) {
	t.Helper()
	err := retry(func() error { return c.CreateTable(t.Context(), tbl, opts...) })
	if err != nil {
		t.Fatalf("creating table %s: %v", tbl.Name, err)
	}
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		_ = retry(func() error { return c.DeleteTable(ctx, tbl.Name) })
	})
}

// TestIntegrationCreateTable makes a table with a string and an integer key, a
// defined column of each type and an index, maps it, and writes and reads a row
// through the mapping (recorded: "create a table with defined columns").
func TestIntegrationCreateTable(t *testing.T) {
	c := connector(t)
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	name := table("made")
	start := time.Now()
	create(t, c, tablestore.Table{
		Name: name,
		Keys: []tablestore.Column{{Name: "id", Type: tablestore.String}, {Name: "n", Type: tablestore.Integer}},
		Columns: []tablestore.Column{
			{Name: "s", Type: tablestore.String},
			{Name: "i", Type: tablestore.Integer},
			{Name: "d", Type: tablestore.Double},
			{Name: "b", Type: tablestore.Boolean},
			{Name: "x", Type: tablestore.Binary},
		},
		Indexes: []tablestore.Index{{Name: name + "_ix", Keys: []string{"s"}, Columns: []string{"i"}}},
	})
	t.Logf("CreateTable took %v", time.Since(start))
	t.Cleanup(func() {
		_ = retry(func() error {
			_, err := db.ExecContext(context.WithoutCancel(t.Context()), "DROP MAPPING TABLE IF EXISTS "+name)
			return err
		})
	})
	err := retry(func() error {
		_, err := db.ExecContext(t.Context(), "CREATE TABLE "+name+" (id VARCHAR(255), n BIGINT, s MEDIUMTEXT, i BIGINT, d DOUBLE, b BOOL, x MEDIUMBLOB, PRIMARY KEY(id, n));")
		return err
	})
	if err != nil {
		t.Fatalf("mapping the table: %v", err)
	}
	// DescribeTable takes the name as its one field, as DeleteTable does.
	status, desc := raw(t, "DescribeTable", tablestore.DeleteTableBody(name))
	if status != 200 || !strings.Contains(string(desc), name+"_ix") {
		t.Errorf("DescribeTable answered %d, and the answer %q does not name the index %s_ix", status, desc, name)
	}
	exec(t, db, "INSERT INTO "+name+" (id, n, s, i, d, b, x) VALUES ('a', 1, 'text', 7, 1.5, true, x'00ff');")
	got := rowsOf(t, db, "SELECT id, n, s, i, d, b, x FROM "+name)
	want := [][]any{{"a", int64(1), "text", int64(7), 1.5, true, []byte{0, 0xff}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the row is %v, want %v", got, want)
	}
}

// TestIntegrationCreateTableErrors holds the errors of the three calls.
func TestIntegrationCreateTableErrors(t *testing.T) {
	c := connector(t)
	name := table("errs")
	tbl := tablestore.Table{
		Name:    name,
		Keys:    []tablestore.Column{{Name: "id", Type: tablestore.Integer}},
		Columns: []tablestore.Column{{Name: "s", Type: tablestore.String}},
	}
	create(t, c, tbl)
	var e *tablestore.Error
	err := retry(func() error { return c.CreateTable(t.Context(), tbl) })
	if !errors.Is(err, tablestore.ErrExists) || !errors.As(err, &e) || e.Code != "OTSObjectAlreadyExist" {
		t.Errorf("creating a table that is there gave %v, want ErrExists", err)
	}
	err = retry(func() error { return c.DeleteTable(t.Context(), table("none")) })
	if !errors.Is(err, tablestore.ErrNotFound) {
		t.Errorf("deleting a table that is not there gave %v, want ErrNotFound", err)
	}
	ix := tablestore.Index{Name: name + "_ix", Keys: []string{"s"}}
	if err := retry(func() error { return c.CreateIndex(t.Context(), name, ix) }); err != nil {
		t.Fatalf("creating an index: %v", err)
	}
	err = retry(func() error { return c.CreateIndex(t.Context(), name, ix) })
	if !errors.Is(err, tablestore.ErrExists) {
		t.Errorf("creating an index that is there gave %v, want ErrExists", err)
	}
	err = retry(func() error {
		return c.CreateIndex(t.Context(), table("none"), tablestore.Index{Name: "ix", Keys: []string{"s"}})
	})
	if !errors.Is(err, tablestore.ErrNotFound) {
		t.Errorf("creating an index on a table that is not there gave %v, want ErrNotFound", err)
	}
	if err := retry(func() error { return c.DeleteTable(t.Context(), name) }); err != nil {
		t.Fatalf("deleting the table: %v", err)
	}
	if err := retry(func() error { return c.DeleteTable(t.Context(), name) }); !errors.Is(err, tablestore.ErrNotFound) {
		t.Errorf("deleting a table twice gave %v, want ErrNotFound the second time", err)
	}
	// The name is free at once or after a moment (recorded: "create after delete").
	start := time.Now()
	err = retry(func() error { return c.CreateTable(t.Context(), tbl) })
	t.Logf("CreateTable after DeleteTable gave %v after %v", err, time.Since(start))
	if err != nil && !strings.Contains(err.Error(), "OTSObjectAlreadyExist") {
		t.Errorf("creating the table again gave %v", err)
	}
}

// fields reads the varint fields and the fields of bytes of a protobuf message,
// without the code of the package.
func fields(t *testing.T, b []byte) map[int][]any {
	t.Helper()
	out := map[int][]any{}
	read := func() uint64 {
		var v uint64
		for i := range 10 {
			if len(b) == 0 {
				t.Fatal("the message ends inside a varint")
			}
			c := b[0]
			b = b[1:]
			v |= uint64(c&0x7f) << (7 * i)
			if c < 0x80 {
				return v
			}
		}
		t.Fatal("a varint is too long")
		return 0
	}
	for len(b) > 0 {
		key := read()
		switch key & 7 {
		case 0:
			out[int(key>>3)] = append(out[int(key>>3)], read())
		case 2:
			n := read()
			if uint64(len(b)) < n {
				t.Fatal("a field is longer than the message")
			}
			out[int(key>>3)] = append(out[int(key>>3)], append([]byte(nil), b[:n]...))
			b = b[n:]
		default:
			t.Fatalf("the field %d has the wire type %d", key>>3, key&7)
		}
	}
	return out
}

// TestIntegrationCreateTableOptions makes a table with a time to live, three
// versions and no update, and reads the settings back with DescribeTable, as
// the members of its field 3. It also sends a time to live under one day, and
// logs what the server says.
func TestIntegrationCreateTableOptions(t *testing.T) {
	c := connector(t)
	name := table("opts")
	create(t, c, tablestore.Table{
		Name: name,
		Keys: []tablestore.Column{{Name: "id", Type: tablestore.Integer}},
	}, tablestore.WithTimeToLive(24*time.Hour), tablestore.WithMaxVersions(3), tablestore.WithAllowUpdate(false))
	status, desc := raw(t, "DescribeTable", tablestore.DeleteTableBody(name))
	if status != 200 {
		t.Fatalf("DescribeTable answered %d %q", status, desc)
	}
	b, _ := fields(t, desc)[3][0].([]byte)
	opts := fields(t, b)
	if ttl, _ := opts[1][0].(uint64); ttl != 86400 {
		t.Errorf("the time to live is %d, want 86400", ttl)
	}
	if v, _ := opts[2][0].(uint64); v != 3 {
		t.Errorf("the number of versions is %d, want 3", v)
	}
	if v, ok := opts[6]; !ok || v[0] != uint64(0) {
		t.Errorf("allow_update is %v, want 0", v)
	}
	short := table("opts_short")
	err := c.CreateTable(t.Context(), tablestore.Table{
		Name: short,
		Keys: []tablestore.Column{{Name: "id", Type: tablestore.Integer}},
	}, tablestore.WithTimeToLive(time.Second))
	t.Logf("a time to live of one second gave %v", err)
	if err == nil {
		_ = c.DeleteTable(context.WithoutCancel(t.Context()), short)
	}
	if err := retry(func() error { return c.DeleteTable(t.Context(), table("opts_none"), tablestore.WithIfExists()) }); err != nil {
		t.Errorf("DeleteTable with WithIfExists gave %v for a table that is not there", err)
	}
}
