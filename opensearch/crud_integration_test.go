package opensearch_test

import (
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xo/dbimp/opensearch"
)

// TestIntegrationCRUD holds the entries of CRUD of features.json (step 14a). SQL
// in OpenSearch reads only (D163): each statement that writes is refused with
// HTTP 400, and the data stays as it was. A write goes through the document API,
// which the administrator sends, and the driver reads what it wrote.
func TestIntegrationCRUD(t *testing.T) {
	rows := rowsIndex(t)
	s := newAdminAPI(t)

	// refused sends a statement that writes, and holds the refusal and that no row
	// changed.
	refused := func(t *testing.T, e *target, query string) {
		t.Helper()
		_, err := e.db.ExecContext(t.Context(), query)
		oe, ok := errors.AsType[*opensearch.Error](err)
		if !ok || oe.HTTPStatus != http.StatusBadRequest || !strings.HasPrefix(oe.Type, "SQLFeature") {
			t.Errorf("%s gave %v, want the refusal of HTTP 400 with SQLFeatureNotSupportedException", query, err)
		}
		_, got, err := e.read(t, "SELECT n FROM "+rows+" WHERE n = 1 OR n = 301")
		if err != nil || len(got) != 1 {
			t.Errorf("after %s the rows with n of 1 or 301 are %v, %v, want the row 1 only", query, got, err)
		}
	}
	feature(t, "insert", func(t *testing.T, e *target) {
		refused(t, e, "INSERT INTO "+rows+" (n, s) VALUES (301, 'row 301')")
	})
	feature(t, "update", func(t *testing.T, e *target) {
		refused(t, e, "UPDATE "+rows+" SET s = 'x' WHERE n = 1")
	})
	feature(t, "delete", func(t *testing.T, e *target) {
		// The setting plugins.sql.delete.enabled is false, so the 2 series says that
		// the clause is disabled (recorded: "a delete").
		refused(t, e, "DELETE FROM "+rows+" WHERE n = 301")
	})
	feature(t, "upsert", func(t *testing.T, e *target) {
		refused(t, e, "INSERT INTO "+rows+" (n, s) VALUES (1, 'x') ON DUPLICATE KEY UPDATE s = 'x'")
	})
	feature(t, "merge", func(t *testing.T, e *target) {
		refused(t, e, "MERGE INTO "+rows+" USING "+rows+" ON 1 = 1 WHEN MATCHED THEN UPDATE SET s = 'x'")
	})

	// The cycle of step 14a on three indices that refer to each other by a value,
	// since OpenSearch has no foreign key: customers, orders and items. The
	// administrator writes through the document API, and the principal reads
	// through the driver, and compares each value with what was written.
	feature(t, "select", func(t *testing.T, e *target) {
		tag := strconv.FormatInt(int64(len(e.p.name)), 10) + e.p.name[:3]
		customers, orders, items := prefix+"cu_"+tag, prefix+"or_"+tag, prefix+"it_"+tag
		for _, name := range []string{customers, orders, items} {
			s.index(t, name, map[string]any{"settings": settings(), "mappings": map[string]any{"properties": map[string]any{
				"id": m("integer"), "name": m("keyword"), "customer": m("integer"), "order": m("integer"), "amount": m("double"), "note": m("text")}}})
		}
		s.bulk(t, customers, []map[string]any{{"_id": 1, "id": 1, "name": "Ada"}, {"_id": 2, "id": 2, "name": "Grace"}, {"_id": 3, "id": 3, "name": "é日😀"}})
		s.bulk(t, orders, []map[string]any{{"_id": 10, "id": 10, "customer": 1, "amount": 12.5}, {"_id": 11, "id": 11, "customer": 1, "amount": 7.25}, {"_id": 12, "id": 12, "customer": 3, "amount": 100.0}})
		s.bulk(t, items, []map[string]any{{"_id": 100, "id": 100, "order": 10, "note": "red apple"}, {"_id": 101, "id": 101, "order": 10, "note": "green pear"}, {"_id": 102, "id": 102, "order": 12, "note": "blue plum"}})
		read := func(query string, args ...any) [][]any {
			t.Helper()
			_, got, err := e.read(t, query, args...)
			if err != nil {
				t.Fatalf("%s: %v", query, err)
			}
			return got
		}
		// Select the rows of each table, and one that an index serves: the
		// keyword field name, and the text field note.
		if got := read("SELECT id, name FROM " + customers + " ORDER BY id"); !reflect.DeepEqual(got, [][]any{{int64(1), "Ada"}, {int64(2), "Grace"}, {int64(3), "é日😀"}}) {
			t.Errorf("the customers are %v", got)
		}
		if got := read("SELECT id, amount FROM "+orders+" WHERE customer = ? ORDER BY id", int64(1)); !reflect.DeepEqual(got, [][]any{{int64(10), 12.5}, {int64(11), 7.25}}) {
			t.Errorf("the orders of customer 1 are %v", got)
		}
		if got := read("SELECT id FROM "+items+" WHERE MATCH(note, ?)", "pear"); !reflect.DeepEqual(got, [][]any{{int64(101)}}) {
			t.Errorf("the items that match pear are %v", got)
		}
		if got := read("SELECT id FROM "+customers+" WHERE name = ?", "é日😀"); !reflect.DeepEqual(got, [][]any{{int64(3)}}) {
			t.Errorf("the customer with the name é日😀 is %v", got)
		}
		// Update a document of each index, and read again.
		s.must(t, http.MethodPut, "/"+customers+"/_doc/2?refresh=true", map[string]any{"id": 2, "name": "Hopper"})
		s.must(t, http.MethodPut, "/"+orders+"/_doc/11?refresh=true", map[string]any{"id": 11, "customer": 2, "amount": 8.0})
		s.must(t, http.MethodPut, "/"+items+"/_doc/101?refresh=true", map[string]any{"id": 101, "order": 11, "note": "yellow pear"})
		if got := read("SELECT name FROM " + customers + " WHERE id = 2"); !reflect.DeepEqual(got, [][]any{{"Hopper"}}) {
			t.Errorf("the updated customer is %v", got)
		}
		if got := read("SELECT id, amount FROM "+orders+" WHERE customer = ? ORDER BY id", int64(1)); !reflect.DeepEqual(got, [][]any{{int64(10), 12.5}}) {
			t.Errorf("the orders of customer 1 after the update are %v", got)
		}
		if got := read("SELECT note FROM " + items + " WHERE id = 101"); !reflect.DeepEqual(got, [][]any{{"yellow pear"}}) {
			t.Errorf("the updated item is %v", got)
		}
		// Delete a document of each index, and read to see that it is gone.
		for _, d := range []string{"/" + customers + "/_doc/3", "/" + orders + "/_doc/12", "/" + items + "/_doc/102"} {
			s.must(t, http.MethodDelete, d+"?refresh=true", nil)
		}
		if got := read("SELECT id FROM " + customers + " ORDER BY id"); !reflect.DeepEqual(got, [][]any{{int64(1)}, {int64(2)}}) {
			t.Errorf("the customers after the delete are %v", got)
		}
		if got := read("SELECT id FROM " + orders + " ORDER BY id"); !reflect.DeepEqual(got, [][]any{{int64(10)}, {int64(11)}}) {
			t.Errorf("the orders after the delete are %v", got)
		}
		if got := read("SELECT id FROM " + items + " ORDER BY id"); !reflect.DeepEqual(got, [][]any{{int64(100)}, {int64(101)}}) {
			t.Errorf("the items after the delete are %v", got)
		}
		// A query of the 3 series with an aggregate sees the same rows.
		if got := read("SELECT COUNT(*) FROM " + items); !reflect.DeepEqual(got, [][]any{{int64(2)}}) {
			t.Errorf("the count of the items is %v", got)
		}
	})

	feature(t, "a write through the document API", func(t *testing.T, e *target) {
		index := prefix + "doc_" + e.p.name[:3]
		status, b, err := apiAs(t, e.p).do(t.Context(), http.MethodPut, "/"+index+"/_doc/1?refresh=true", map[string]any{"n": 1})
		if e.p == ordinary {
			if err != nil || status != http.StatusForbidden || !strings.Contains(string(b), "security_exception") {
				t.Errorf("the ordinary user wrote a document: HTTP %d, %v: %.200s, want HTTP 403", status, err, b)
			}
			return
		}
		if err != nil || status >= 300 {
			t.Fatalf("the administrator wrote a document: HTTP %d, %v: %s", status, err, b)
		}
		t.Cleanup(func() { _ = s.drop(t.Context(), index) })
		_, got, err := e.read(t, "SELECT n FROM "+index)
		if err != nil || !reflect.DeepEqual(got, [][]any{{int64(1)}}) {
			t.Errorf("SQL read %v and %v from the document that the API wrote, want the number 1", got, err)
		}
	})
}

// TestIntegrationSchema holds the entries of schema of features.json (step
// 14a). SQL in OpenSearch has no DDL: each statement is refused with HTTP 400.
// SHOW TABLES and DESCRIBE TABLES read the indices, which the administrator makes
// with a mapping through the document API.
func TestIntegrationSchema(t *testing.T) {
	rows := rowsIndex(t)
	s := newAdminAPI(t)
	for _, tt := range []struct{ name, query string }{
		{"create table", "CREATE TABLE " + prefix + "new (a INT)"},
		{"drop table", "DROP TABLE " + rows},
		{"primary key", "CREATE TABLE " + prefix + "new (a INT PRIMARY KEY)"},
		{"foreign key", "CREATE TABLE " + prefix + "new (a INT REFERENCES " + rows + "(n))"},
		{"index", "CREATE INDEX i ON " + rows + " (n)"},
		{"unique constraint", "CREATE TABLE " + prefix + "new (a INT UNIQUE)"},
		{"view", "CREATE VIEW v AS SELECT n FROM " + rows},
		{"default value", "CREATE TABLE " + prefix + "new (a INT DEFAULT 1)"},
	} {
		feature(t, tt.name, func(t *testing.T, e *target) {
			_, err := e.db.ExecContext(t.Context(), tt.query)
			refusalOf(t, tt.query, err)
			if status, _, err := apiAs(t, admin).do(t.Context(), http.MethodGet, "/"+url.PathEscape(prefix+"new"), nil); err != nil || status != http.StatusNotFound {
				t.Errorf("after %s the index %snew has HTTP %d and %v, want none", tt.query, prefix, status, err)
			}
		})
	}

	feature(t, "show tables", func(t *testing.T, e *target) {
		cols, got, err := e.read(t, "SHOW TABLES LIKE "+rows)
		// The ordinary user has indices:admin/get on every index since dbmeta
		// v0.4.0, so both principals read the answer.
		name := slices.Index(cols, "TABLE_NAME")
		if err != nil || name < 0 || len(got) != 1 || got[0][name] != rows {
			t.Errorf("SHOW TABLES gave the columns %v, the rows %v and %v, want one row for %s", cols, got, err, rows)
		}
	})

	feature(t, "describe tables", func(t *testing.T, e *target) {
		cols, got, err := e.read(t, "DESCRIBE TABLES LIKE "+rows)
		if e.old {
			// The 2 series names every column of the answer keyword, and sends
			// numbers in some of them (recorded: "describe tables"). D178, item
			// 17 reads such a number as the value that arrived, measured on
			// 2.19.6 on 2026-10-08.
			ordinal, nullable, radix := slices.Index(cols, "ORDINAL_POSITION"), slices.Index(cols, "NULLABLE"), slices.Index(cols, "NUM_PREC_RADIX")
			if err != nil || len(got) == 0 || ordinal < 0 || nullable < 0 || radix < 0 {
				t.Fatalf("DESCRIBE TABLES on the 2 series gave the columns %v, the rows %v and %v, want its rows", cols, got, err)
			}
			// ORDINAL_POSITION counts from 0 on 2.19.6, and not from 1 as the JDBC
			// documentation says (measured on 2026-10-08).
			seen := map[int64]bool{}
			for _, r := range got {
				n, ok := r[ordinal].(int64)
				if !ok || n < 0 || seen[n] {
					t.Errorf("ORDINAL_POSITION is %#v in %v, want a new int64 from 0", r[ordinal], r)
				}
				seen[n] = true
				if _, ok := r[nullable].(int64); !ok {
					t.Errorf("NULLABLE is %#v in %v, want an int64", r[nullable], r)
				}
				switch r[radix].(type) {
				case nil, int64, float64:
				default:
					t.Errorf("NUM_PREC_RADIX is %#v in %v, want nil or a number", r[radix], r)
				}
			}
			return
		}
		column, typ := slices.Index(cols, "COLUMN_NAME"), slices.Index(cols, "TYPE_NAME")
		var names []string
		if err == nil && column >= 0 {
			for _, r := range got {
				name, _ := r[column].(string)
				names = append(names, name)
			}
		}
		// The 3 series also names the subfields, such as o.a and nn.a.
		have := true
		for _, want := range []string{"n", "nn", "o", "s"} {
			have = have && slices.Contains(names, want)
		}
		if err != nil || typ < 0 || !have {
			t.Errorf("DESCRIBE TABLES gave the columns %v, the rows %v and %v, want a row for each of n, nn, o and s", cols, got, err)
		}
	})

	feature(t, "an index with a mapping made by the administrator", func(t *testing.T, e *target) {
		index := prefix + "map_" + e.p.name[:3]
		s.index(t, index, map[string]any{"settings": settings(), "mappings": map[string]any{"properties": map[string]any{
			"a": m("integer"), "b": m("keyword"), "c": m("double"), "d": m("boolean")}}})
		s.bulk(t, index, []map[string]any{{"a": 1, "b": "x", "c": 1.5, "d": true}})
		cols, cts, err := e.columns(t, "SELECT a, b, c, d FROM "+index)
		if err != nil || !slices.Equal(cols, []string{"a", "b", "c", "d"}) {
			t.Fatalf("the columns are %v and %v", cols, err)
		}
		var got []string
		for _, ct := range cts {
			got = append(got, ct.DatabaseTypeName()+" "+ct.ScanType().String())
		}
		if want := []string{"INTEGER int64", "KEYWORD string", "DOUBLE float64", "BOOLEAN bool"}; !slices.Equal(got, want) {
			t.Errorf("the types of the mapping are %v, want %v", got, want)
		}
		// SELECT * names every field, in an order that is neither the order of the
		// mapping nor the order of the names (recorded: "every column with a star").
		starCols, _, err := e.read(t, "SELECT * FROM "+index)
		slices.Sort(starCols)
		if err != nil || !slices.Equal(starCols, []string{"a", "b", "c", "d"}) {
			t.Errorf("SELECT * names the columns %v and %v", starCols, err)
		}
	})
}
