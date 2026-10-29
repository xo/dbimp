package tablestore_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/xo/dbimp/tablestore"
)

// TestIntegrationSchema tests the statements that make and drop a mapping, and
// the statements of the schema that the server refuses. A table of Tablestore is
// made with the API, and SQL maps it to a name (docs/TABLESTORE.md, "Statements").
// Each subtest is one entry of testdata/tablestore/features.json.
func TestIntegrationSchema(t *testing.T) {
	db := connect(t)
	full := table("schema")
	if status, body := raw(t, "CreateTable", tablestore.CreateTableBody(full, tablestore.KeyInteger)); status != 200 {
		t.Fatalf("CreateTable %s answered %d %q", full, status, body)
	}
	t.Cleanup(func() {
		_, _, _ = tablestore.Raw(context.WithoutCancel(t.Context()), integrationConfig(t), "DeleteTable", tablestore.DeleteTableBody(full))
	})

	t.Run("create mapping table", func(t *testing.T) {
		res := exec(t, db, fmt.Sprintf("CREATE TABLE %s (id BIGINT, s MEDIUMTEXT, i BIGINT, PRIMARY KEY(id));", full))
		if n := affected(t, res); n != 0 {
			t.Errorf("CREATE TABLE changed %d rows, want 0", n)
		}
		// A mapping of a table that does not exist is refused.
		refused(t, db, fmt.Sprintf("CREATE TABLE %s_nosuch (id BIGINT, PRIMARY KEY(id));", full), "OTSParameterInvalid")
	})
	t.Run("describe", func(t *testing.T) {
		got := rowsOf(t, db, "DESC "+full+";")
		var names, types []any
		for _, row := range got {
			names, types = append(names, row[0]), append(types, row[1])
		}
		if !reflect.DeepEqual(names, []any{"id", "s", "i"}) || !reflect.DeepEqual(types, []any{"bigint(20)", "mediumtext", "bigint(20)"}) {
			t.Errorf("DESC gave the names %v and the types %v", names, types)
		}
	})
	t.Run("show tables", func(t *testing.T) {
		var names []string
		for _, row := range rowsOf(t, db, "SHOW TABLES;") {
			name, _ := row[0].(string)
			names = append(names, name)
		}
		if !slices.Contains(names, full) {
			t.Errorf("SHOW TABLES gave %v, want %s in it", names, full)
		}
	})
	t.Run("show index", func(t *testing.T) {
		got := rowsOf(t, db, "SHOW INDEX FROM "+full+";")
		if len(got) != 1 || got[0][0] != full {
			t.Errorf("SHOW INDEX gave %#v, want one row for the primary key of %s", got, full)
		}
	})
	t.Run("show create table", func(t *testing.T) {
		got := rowsOf(t, db, "SHOW CREATE TABLE "+full+";")
		if len(got) != 1 {
			t.Fatalf("SHOW CREATE TABLE gave %#v", got)
		}
		if text, _ := got[0][1].(string); !strings.Contains(text, "CREATE TABLE") {
			t.Errorf("SHOW CREATE TABLE gave %#v", got)
		}
	})
	t.Run("alter table add column", func(t *testing.T) {
		exec(t, db, "ALTER TABLE "+full+" ADD COLUMN z BIGINT;")
		var names []any
		for _, row := range rowsOf(t, db, "DESC "+full+";") {
			names = append(names, row[0])
		}
		if !reflect.DeepEqual(names, []any{"id", "s", "i", "z"}) {
			t.Errorf("DESC after the ALTER gave %v", names)
		}
	})
	t.Run("secondary index", func(t *testing.T) {
		refused(t, db, "CREATE INDEX dbimp_it_ix ON "+full+" (s);", "OTSUnsupportOperation")
	})
	t.Run("view", func(t *testing.T) {
		refused(t, db, "CREATE VIEW "+table("view")+" AS SELECT id FROM "+full+";", "OTSUnsupportOperation")
	})
	t.Run("drop table", func(t *testing.T) {
		// DROP TABLE is refused: only a mapping drops, and only the API deletes a
		// table.
		refused(t, db, "DROP TABLE "+full+";", "OTSUnsupportOperation")
	})
	t.Run("drop mapping table", func(t *testing.T) {
		exec(t, db, "DROP MAPPING TABLE "+full+";")
		// DESC refuses the dropped name at once. A SELECT can still read it for a
		// short time after the drop (measured on 2026-10-29), so the test does not
		// ask a SELECT.
		refused(t, db, "DESC "+full+";", "OTSParameterInvalid")
		// IF EXISTS makes a second drop pass.
		exec(t, db, "DROP MAPPING TABLE IF EXISTS "+full+";")
	})
}

// TestIntegrationTypesRefused sends the mapping of a column of each type that the
// server refuses, and checks the refusal. Each subtest is one entry of
// testdata/tablestore/features.json whose verdict is no.
func TestIntegrationTypesRefused(t *testing.T) {
	db := connect(t)
	full := table("refused")
	if status, body := raw(t, "CreateTable", tablestore.CreateTableBody(full, tablestore.KeyInteger)); status != 200 {
		t.Fatalf("CreateTable %s answered %d %q", full, status, body)
	}
	t.Cleanup(func() {
		_, _, _ = tablestore.Raw(context.WithoutCancel(t.Context()), integrationConfig(t), "DeleteTable", tablestore.DeleteTableBody(full))
	})
	for _, typ := range []string{"INTEGER", "TEXT", "BINARY", "DECIMAL(10,2)", "JSON", "VARCHAR(100)", "VARBINARY(100)"} {
		t.Run(strings.SplitN(typ, "(", 2)[0], func(t *testing.T) {
			refused(t, db, fmt.Sprintf("CREATE TABLE %s (id BIGINT, c %s, PRIMARY KEY(id));", full, typ), "OTSParameterInvalid")
		})
	}
}
