package elasticsearch_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/elasticsearch"
)

// These tests hold each entry of testdata/elasticsearch/features.json against
// a real server (step 14a). They need ELASTICSEARCH_DSN and
// ELASTICSEARCH_ORDINARY_DSN, and skip when one is empty.

// refused runs query through the driver, and fails the test unless the server
// refuses it with HTTP 400 and the type parsing_exception, with a message that
// holds want.
func refused(t *testing.T, db *sql.DB, query, want string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), query)
	e, ok := errors.AsType[*elasticsearch.Error](err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || e.Type != "parsing_exception" || !strings.Contains(err.Error(), want) {
		t.Errorf("%q gave %v, want the parse error of the server with %q", query, err, want)
	}
}

// refusedAs runs query through the driver, and fails the test unless the
// server refuses it with HTTP 400 and the root cause type, and a message that
// holds want.
func refusedAs(t *testing.T, db *sql.DB, query, root, want string, opts ...any) {
	t.Helper()
	_, _, err := readAll(t, db, query, opts...)
	e, ok := errors.AsType[*elasticsearch.Error](err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || e.RootType != root || !strings.Contains(err.Error(), want) {
		t.Errorf("%q gave %v, want HTTP 400 with %s and %q", query, err, root, want)
	}
}

// expect runs query as each principal, and compares its rows with want.
func expect(t *testing.T, query string, want [][]any, opts ...any) {
	t.Helper()
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		_, got, err := readAll(t, db, query, opts...)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%.80q gave %#v, %v, want %#v", query, got, err, want)
		}
	})
}

// crudMapping is the mapping of the indices of the CRUD test.
var crudMapping = map[string]any{"mappings": map[string]any{"properties": map[string]any{"id": m("integer"), "s": m("keyword"), "n": m("long")}}}

// TestIntegrationCRUD holds each statement of CRUD on three indices. SQL
// refuses INSERT, UPDATE and DELETE, and the driver returns the refusal
// (D163). The administrator writes through the document API instead: an
// index, an update, an upsert and a delete. Each principal reads the result
// through the driver, and compares every value that it returns with the value
// that the test wrote.
func TestIntegrationCRUD(t *testing.T) {
	s := newAdminAPI(t)
	names := []string{prefix + "crud_a", prefix + "crud_b", prefix + "crud_c"}
	for _, name := range names {
		s.index(t, name, crudMapping)
	}
	selectAll := func(name string) string { return "SELECT id, s, n FROM " + name + " ORDER BY id" }
	each := func(t *testing.T, want [][]any) {
		t.Helper()
		for _, name := range names {
			expect(t, selectAll(name), want)
		}
	}
	t.Run("insert", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			for _, name := range names {
				refused(t, db, "INSERT INTO "+name+" (id, s) VALUES (1, 'one')", "mismatched input 'INSERT'")
			}
		})
		for _, name := range names {
			s.bulk(t, name, []map[string]any{{"id": 1, "s": "one", "n": 10}, {"id": 2, "s": "two", "n": 20}})
		}
	})
	t.Run("select", func(t *testing.T) {
		each(t, [][]any{{int64(1), "one", int64(10)}, {int64(2), "two", int64(20)}})
	})
	t.Run("update", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			for _, name := range names {
				refused(t, db, "UPDATE "+name+" SET s = 'uno' WHERE id = 1", "mismatched input 'UPDATE'")
			}
		})
		for _, name := range names {
			s.must(t, http.MethodPost, "/"+name+"/_update/1?refresh=true", map[string]any{"doc": map[string]any{"s": "uno", "n": 11}})
		}
		each(t, [][]any{{int64(1), "uno", int64(11)}, {int64(2), "two", int64(20)}})
	})
	t.Run("upsert", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refused(t, db, "UPSERT INTO "+names[0]+" (id, s) VALUES (3, 'three')", "mismatched input 'UPSERT'")
		})
		for _, name := range names {
			s.must(t, http.MethodPost, "/"+name+"/_update/3?refresh=true", map[string]any{"doc": map[string]any{"id": 3, "s": "three", "n": 30}, "doc_as_upsert": true})
		}
		each(t, [][]any{{int64(1), "uno", int64(11)}, {int64(2), "two", int64(20)}, {int64(3), "three", int64(30)}})
	})
	t.Run("merge", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refused(t, db, "MERGE INTO "+names[0]+" USING "+names[1]+" ON "+names[0]+".id = "+names[1]+".id WHEN MATCHED THEN UPDATE SET s = 'm'", "mismatched input 'MERGE'")
		})
	})
	t.Run("select into", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refused(t, db, "SELECT * INTO "+prefix+"crud_d FROM "+names[0], "mismatched input '"+prefix+"crud_d'")
		})
	})
	t.Run("delete", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			for _, name := range names {
				refused(t, db, "DELETE FROM "+name+" WHERE id = 2", "mismatched input 'DELETE'")
			}
		})
		for _, name := range names {
			s.must(t, http.MethodDelete, "/"+name+"/_doc/2?refresh=true", nil)
		}
		each(t, [][]any{{int64(1), "uno", int64(11)}, {int64(3), "three", int64(30)}})
		for _, name := range names {
			s.must(t, http.MethodPost, "/"+name+"/_delete_by_query?refresh=true", map[string]any{"query": map[string]any{"match_all": map[string]any{}}})
		}
		each(t, nil)
	})
}

// TestIntegrationSchema holds each operation on a schema. SQL has no DDL, and
// the driver returns the refusal of each statement (D163). It reads the
// catalog with SHOW, DESCRIBE and SYS, and reads an alias with a filter, which
// the administrator makes through the alias API.
func TestIntegrationSchema(t *testing.T) {
	s := newAdminAPI(t)
	paging := pagingIndex(t)
	types := typesIndex(t)
	alias := prefix + "alias"
	s.must(t, http.MethodPost, "/_aliases", map[string]any{"actions": []any{map[string]any{"add": map[string]any{"index": paging, "alias": alias, "filter": map[string]any{"range": map[string]any{"n": map[string]any{"lte": 3}}}}}}})
	t.Cleanup(func() {
		_, _, _ = s.do(context.WithoutCancel(t.Context()), http.MethodPost, "/_aliases", map[string]any{"actions": []any{map[string]any{"remove": map[string]any{"index": paging, "alias": alias}}}})
	})
	for _, tt := range []struct{ name, query, want string }{
		{"table", "CREATE TABLE " + prefix + "x (a INT PRIMARY KEY, b INT UNIQUE DEFAULT 1)", "mismatched input 'CREATE'"},
		{"primary key", "ALTER TABLE " + paging + " ADD PRIMARY KEY (n)", "mismatched input 'ALTER'"},
		{"foreign key", "ALTER TABLE " + paging + " ADD FOREIGN KEY (n) REFERENCES " + types + " (id)", "mismatched input 'ALTER'"},
		{"index", "CREATE INDEX " + prefix + "i ON " + paging + " (n)", "mismatched input 'CREATE'"},
		{"unique constraint", "ALTER TABLE " + paging + " ADD UNIQUE (s)", "mismatched input 'ALTER'"},
		{"view", "CREATE VIEW " + prefix + "v AS SELECT n FROM " + paging, "mismatched input 'CREATE'"},
		{"default value", "ALTER TABLE " + paging + " ALTER COLUMN n SET DEFAULT 1", "mismatched input 'ALTER'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
				refused(t, db, tt.query, tt.want)
			})
		})
	}
	t.Run("alias with a filter", func(t *testing.T) {
		expect(t, "SELECT n FROM "+alias+" ORDER BY n", [][]any{{int64(1)}, {int64(2)}, {int64(3)}})
	})
	t.Run("show tables", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			cols, got, err := readAll(t, db, "SHOW TABLES LIKE '"+prefix+"%'")
			if err != nil || !reflect.DeepEqual(cols, []string{"catalog", "name", "type", "kind"}) {
				t.Fatalf("SHOW TABLES gave %v, %v", cols, err)
			}
			kinds := map[string]string{}
			for _, row := range got {
				kinds[fmt.Sprint(row[1])] = fmt.Sprint(row[2]) + " " + fmt.Sprint(row[3])
			}
			if kinds[alias] != "VIEW ALIAS" || kinds[paging] != "TABLE INDEX" || kinds[types] != "TABLE INDEX" {
				t.Errorf("SHOW TABLES names %v, want the alias as a VIEW of the kind ALIAS and each index as a TABLE of the kind INDEX", kinds)
			}
		})
	})
	t.Run("describe", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			cols, got, err := readAll(t, db, "DESCRIBE "+paging)
			want := [][]any{{"id", "BIGINT", "long"}, {"n", "INTEGER", "integer"}, {"s", "VARCHAR", "keyword"}}
			if err != nil || !reflect.DeepEqual(cols, []string{"column", "type", "mapping"}) || !reflect.DeepEqual(got, want) {
				t.Errorf("DESCRIBE gave %v, %v, %v, want %v", cols, got, err, want)
			}
		})
	})
	t.Run("show columns", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SHOW COLUMNS IN "+paging)
			want := [][]any{{"id", "BIGINT", "long"}, {"n", "INTEGER", "integer"}, {"s", "VARCHAR", "keyword"}}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("SHOW COLUMNS gave %v, %v, want %v", got, err, want)
			}
		})
	})
	t.Run("sys columns", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			cols, got, err := readAll(t, db, "SYS COLUMNS TABLE LIKE '"+paging+"'")
			if err != nil || len(got) != 3 || len(cols) < 18 || cols[3] != "COLUMN_NAME" || got[1][3] != "n" || got[1][5] != "INTEGER" {
				t.Errorf("SYS COLUMNS gave %v, %v, %v", cols, got, err)
			}
		})
	})
	t.Run("sys types", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			cols, got, err := readAll(t, db, "SYS TYPES")
			names := map[string]bool{}
			for _, row := range got {
				names[fmt.Sprint(row[0])] = true
			}
			if err != nil || len(cols) != 19 || !names["INTERVAL_DAY_TO_SECOND"] || !names["UNSIGNED_LONG"] || !names["GEO_POINT"] {
				t.Errorf("SYS TYPES gave %d columns, %d rows and %v", len(cols), len(got), err)
			}
		})
	})
	t.Run("information_schema", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refused(t, db, "SELECT * FROM information_schema.tables", "mismatched input '.'")
		})
	})
	t.Run("show functions", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SHOW FUNCTIONS LIKE 'DATE%'")
			if err != nil || len(got) < 5 {
				t.Errorf("SHOW FUNCTIONS gave %v, %v", got, err)
			}
		})
	})
	t.Run("show catalogs", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			cols, got, err := readAll(t, db, "SHOW CATALOGS")
			if err != nil || !reflect.DeepEqual(cols, []string{"name", "type"}) || len(got) != 1 || got[0][1] != "local" {
				t.Errorf("SHOW CATALOGS gave %v, %v, %v", cols, got, err)
			}
		})
	})
	t.Run("index pattern", func(t *testing.T) {
		expect(t, `SELECT COUNT(*) FROM "`+prefix+`p*"`, [][]any{{int64(250)}})
	})
}

// slowStatement sends the statement of the filter slowFilter, which runs for
// about two seconds, with the header X-Opaque-Id id, in the background, and
// returns the channel that holds its status and its body when it ends.
type slowResult struct {
	status int
	body   string
	err    error
}

// slowStatement runs the slow statement as the administrator.
func slowStatement(s *api, paging, id string) <-chan slowResult {
	done := make(chan slowResult, 1)
	go func() {
		// The statement outlives the test that starts it by a few seconds at
		// most, so it has no context of the test.
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		payload, _ := json.Marshal(map[string]any{"query": "SELECT n FROM " + paging + " ORDER BY s DESC", "fetch_size": 10, "filter": slowFilter})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/_sql?format=json", strings.NewReader(string(payload)))
		if err != nil {
			done <- slowResult{err: err}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Opaque-Id", id)
		req.SetBasicAuth(s.user, s.pass)
		res, err := s.client.Do(req)
		if err != nil {
			done <- slowResult{err: err}
			return
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		done <- slowResult{status: res.StatusCode, body: string(b), err: err}
	}()
	return done
}

// TestIntegrationFeatures holds each feature of the survey that is not a
// statement of CRUD, an operation on a schema or a type. The features that
// the driver sends run through the driver as each principal. The features that
// the driver does not speak, such as the other formats and the async form, run
// through the HTTP API as the administrator.
func TestIntegrationFeatures(t *testing.T) { //nolint:maintidx // One subtest for each entry of the survey.
	s := newAdminAPI(t)
	paging, window, types := pagingIndex(t), windowIndex(t), typesIndex(t)
	rawAs := func(t *testing.T, p principal, query string, extra map[string]any) (int, string) {
		t.Helper()
		return rawSQL(t, p, query, extra)
	}
	subtest := func(name string, f func(t *testing.T)) { t.Run(name, f) }
	subtest("cursor", func(t *testing.T) {
		status, body := rawAs(t, admin, "SELECT n FROM "+paging+" ORDER BY n", map[string]any{"fetch_size": 100})
		var first struct {
			Cursor string  `json:"cursor"`
			Rows   [][]any `json:"rows"`
		}
		if err := json.Unmarshal([]byte(body), &first); err != nil || status != http.StatusOK || first.Cursor == "" || len(first.Rows) != 100 {
			t.Fatalf("the first page gave HTTP %d, %v: %.200s", status, err, body)
		}
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT n FROM "+paging+" ORDER BY n", elasticsearch.WithFetchSize(100))
			if err != nil || len(got) != 250 {
				t.Errorf("read %d rows and %v, want 250 in 3 pages", len(got), err)
			}
		})
	})
	subtest("fetch_size", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT n FROM "+paging+" ORDER BY n", elasticsearch.WithFetchSize(7))
			if err != nil || len(got) != 250 {
				t.Errorf("fetch_size 7: read %d rows and %v", len(got), err)
			}
			if _, _, err := readAll(t, db, "SELECT n FROM "+paging, elasticsearch.WithFetchSize(0)); !errors.Is(err, dbimp.ErrInvalidValue) {
				t.Errorf("fetch_size 0 gave %v, want dbimp.ErrInvalidValue", err)
			}
		})
		if status, body := rawAs(t, admin, "SELECT n FROM "+paging, map[string]any{"fetch_size": 0}); status != http.StatusBadRequest || !strings.Contains(body, "fetch_size must be more than 0") {
			t.Errorf("the server took a fetch_size of 0: HTTP %d: %.200s", status, body)
		}
	})
	subtest("close a cursor", func(t *testing.T) {
		_, body := rawAs(t, admin, "SELECT n FROM "+paging+" ORDER BY n", map[string]any{"fetch_size": 100})
		var first struct {
			Cursor string `json:"cursor"`
		}
		if err := json.Unmarshal([]byte(body), &first); err != nil || first.Cursor == "" {
			t.Fatalf("no cursor: %v: %.200s", err, body)
		}
		for i, want := range []string{`{"succeeded":true}`, `{"succeeded":false}`} {
			status, b, err := s.do(t.Context(), http.MethodPost, "/_sql/close", map[string]any{"cursor": first.Cursor})
			if err != nil || status != http.StatusOK || string(b) != want {
				t.Errorf("close %d gave HTTP %d, %v: %s, want %s", i+1, status, err, b, want)
			}
		}
		status, b, err := s.do(t.Context(), http.MethodPost, "/_sql?format=json", map[string]any{"cursor": first.Cursor})
		if err != nil || status != http.StatusNotFound || !strings.Contains(string(b), "search_context_missing_exception") {
			t.Errorf("a closed cursor gave HTTP %d, %v: %.200s, want HTTP 404", status, err, b)
		}
	})
	subtest("page_timeout", func(t *testing.T) {
		// A page_timeout of 1s did not end a cursor that was read 3 seconds
		// later (recorded: "a cursor after its page timeout").
		_, body := rawAs(t, admin, "SELECT n FROM "+paging+" ORDER BY n", map[string]any{"fetch_size": 100, "page_timeout": "1s"})
		var first struct {
			Cursor string `json:"cursor"`
		}
		if err := json.Unmarshal([]byte(body), &first); err != nil || first.Cursor == "" {
			t.Fatalf("no cursor: %v: %.200s", err, body)
		}
		time.Sleep(3 * time.Second)
		status, b, err := s.do(t.Context(), http.MethodPost, "/_sql?format=json", map[string]any{"cursor": first.Cursor})
		if err != nil || status != http.StatusOK {
			t.Errorf("the cursor of a page_timeout of 1s gave HTTP %d, %v: %.200s after 3 seconds, want HTTP 200", status, err, b)
		}
		_, _, _ = s.do(t.Context(), http.MethodPost, "/_sql/close", map[string]any{"cursor": first.Cursor})
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT n FROM "+paging+" ORDER BY n", elasticsearch.WithFetchSize(100), elasticsearch.WithParameter("page_timeout", "1m"))
			if err != nil || len(got) != 250 {
				t.Errorf("read %d rows and %v with WithParameter(page_timeout)", len(got), err)
			}
		})
	})
	subtest("keep_alive", func(t *testing.T) {
		status, body := rawAs(t, admin, "SELECT n FROM "+paging+" ORDER BY n", map[string]any{"fetch_size": 100, "keep_alive": "1m"})
		if status != http.StatusOK || !strings.Contains(body, `"cursor"`) {
			t.Errorf("keep_alive gave HTTP %d: %.200s", status, body)
		}
	})
	subtest("request_timeout", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, _, err := readAll(t, db, "SELECT n FROM "+paging+" ORDER BY s DESC", elasticsearch.WithFetchSize(10),
				elasticsearch.WithParameter("filter", slowFilter), elasticsearch.WithTimeout(100*time.Millisecond))
			e, ok := errors.AsType[*elasticsearch.Error](err)
			if !ok || e.RootType != "search_timeout_exception" || (e.HTTPStatus != http.StatusGatewayTimeout && e.HTTPStatus != http.StatusTooManyRequests) {
				t.Errorf("a statement that passed its request_timeout gave %v", err)
			}
		})
	})
	subtest("columnar", func(t *testing.T) {
		status, body := rawAs(t, admin, "SELECT n, s FROM "+paging+" WHERE n < 4 ORDER BY n", map[string]any{"columnar": true})
		if status != http.StatusOK || !strings.Contains(body, `"values":[[1,2,3],["row 1","row 2","row 3"]]`) {
			t.Errorf("columnar gave HTTP %d: %.200s", status, body)
		}
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, _, err := readAll(t, db, "SELECT n, s FROM "+paging+" WHERE n < 4 ORDER BY n", elasticsearch.WithParameter("columnar", true))
			if !errors.Is(err, dbimp.ErrInvalidValue) {
				t.Errorf("a columnar answer gave %v, want an error that wraps dbimp.ErrInvalidValue", err)
			}
		})
	})
	subtest("positional parameters", func(t *testing.T) {
		expect(t, "SELECT n, s FROM "+paging+" WHERE n = ? AND s = ?", [][]any{{int64(5), "row 5"}}, int64(5), "row 5")
		expect(t, "SELECT n FROM "+paging+" WHERE n IN (?, ?) ORDER BY n", [][]any{{int64(1)}, {int64(2)}}, int64(1), int64(2))
		expect(t, "SELECT id FROM "+types+" WHERE dn > ?", [][]any{{int64(1)}}, time.Date(2026, 10, 1, 12, 34, 56, 123456788, time.UTC))
	})
	subtest("named parameters", func(t *testing.T) {
		status, body := rawAs(t, admin, "SELECT n FROM "+paging+" WHERE n = :x", map[string]any{"params": map[string]any{"x": 1}})
		if status != http.StatusBadRequest || !strings.Contains(body, "params doesn't support values of type: START_OBJECT") {
			t.Errorf("named parameters gave HTTP %d: %.200s", status, body)
		}
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			if _, _, err := readAll(t, db, "SELECT n FROM "+paging+" WHERE n = :x", sql.Named("x", 1)); !errors.Is(err, dbimp.ErrArguments) {
				t.Errorf("a named argument gave %v, want dbimp.ErrArguments", err)
			}
		})
	})
	subtest("typed parameters", func(t *testing.T) {
		status, body := rawAs(t, admin, "SELECT ? AS a", map[string]any{"params": []any{map[string]any{"type": "integer", "value": 1}}})
		if status != http.StatusBadRequest || !strings.Contains(body, "each entry is a single field") {
			t.Errorf("a typed parameter gave HTTP %d: %.200s", status, body)
		}
	})
	subtest("parameter in LIMIT", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refusedAs(t, db, "SELECT n FROM "+paging+" ORDER BY n LIMIT ?", "parsing_exception", "mismatched input '?'", int64(1))
		})
	})
	subtest("field_multi_value_leniency", func(t *testing.T) {
		expect(t, "SELECT id, i, k FROM "+types+" WHERE id IN (1, 5) ORDER BY id",
			[][]any{{int64(1), int64(2147483647), "é'\"\\ x"}, {int64(5), int64(1), "a"}}, elasticsearch.WithFieldMultiValueLeniency(true))
	})
	subtest("multi-valued field as an array", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refusedAs(t, db, "SELECT id, i FROM "+types+" WHERE id = 5", "invalid_argument_exception", "Arrays (returned by [i]) are not supported")
		})
	})
	subtest("object subfields", func(t *testing.T) {
		expect(t, "SELECT o.a, o.s FROM "+types+" WHERE id = 1", [][]any{{int64(1), "x"}})
	})
	subtest("object as a column", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refusedAs(t, db, "SELECT o FROM "+types+" WHERE id = 1", "verification_exception", "Cannot use field [o] type [object] only its subfields")
		})
	})
	subtest("nested subfields", func(t *testing.T) {
		expect(t, "SELECT id, n.a FROM "+types+" WHERE id < 3 ORDER BY id", [][]any{{int64(1), int64(1)}, {int64(1), int64(2)}})
	})
	subtest("nested as a column", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refusedAs(t, db, "SELECT n FROM "+types+" WHERE id = 1", "verification_exception", "Cannot use field [n] type [nested] only its subfields")
		})
	})
	subtest("filter", func(t *testing.T) {
		filter := map[string]any{"range": map[string]any{"n": map[string]any{"lte": 3}}}
		expect(t, "SELECT n FROM "+paging+" ORDER BY n", [][]any{{int64(1)}, {int64(2)}, {int64(3)}}, elasticsearch.WithParameter("filter", filter))
	})
	subtest("runtime_mappings", func(t *testing.T) {
		expect(t, "SELECT n, d FROM "+paging+" WHERE n < 3 ORDER BY n", [][]any{{int64(1), int64(2)}, {int64(2), int64(4)}},
			elasticsearch.WithParameter("runtime_mappings", map[string]any{"d": map[string]any{"type": "long", "script": map[string]any{"source": `emit(doc["n"].value * 2)`}}}))
	})
	subtest("time_zone", func(t *testing.T) {
		kolkata := time.FixedZone("", 5*3600+1800)
		tokyo := time.FixedZone("", 9*3600)
		expect(t, "SELECT dt, CAST(dt AS DATE) AS d FROM "+types+" WHERE id = 1", [][]any{{time.Date(2026, 10, 1, 12, 34, 56, 789e6, kolkata), dbimp.Date{Year: 2026, Month: 10, Day: 1}}},
			elasticsearch.WithTimeZone("+05:30"))
		expect(t, "SELECT dt FROM "+types+" WHERE id = 1", [][]any{{time.Date(2026, 10, 1, 16, 4, 56, 789e6, tokyo)}}, elasticsearch.WithTimeZone("Asia/Tokyo"))
		expect(t, "SELECT dt FROM "+types+" WHERE id = 1", [][]any{{time.Date(2026, 10, 1, 7, 4, 56, 789e6, time.UTC)}})
		// The zone holds across the pages of a statement.
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT dt FROM "+types+" WHERE id < 3 ORDER BY id", elasticsearch.WithTimeZone("Asia/Tokyo"), elasticsearch.WithFetchSize(1))
			want := [][]any{{time.Date(2026, 10, 1, 16, 4, 56, 789e6, tokyo)}, {time.Date(1970, 1, 1, 9, 0, 0, 0, tokyo)}}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("the zone across 2 pages gave %v, %v, want %v", got, err, want)
			}
		})
	})
	subtest("catalog", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			var cluster string
			if err := db.QueryRowContext(t.Context(), "SELECT DATABASE()").Scan(&cluster); err != nil {
				t.Fatal(err)
			}
			_, got, err := readAll(t, db, "SELECT n FROM "+paging+" WHERE n < 3 ORDER BY n", elasticsearch.WithCatalog(cluster))
			if err != nil || len(got) != 2 {
				t.Errorf("the local catalog gave %v, %v", got, err)
			}
		})
	})
	subtest("remote catalog", func(t *testing.T) {
		// The name of a cluster that is not configured fails with HTTP 404 on
		// 9.4.6 and 9.5.3, and with HTTP 403 on 8.19.22, where the root cause
		// is the same and security_exception wraps it (measured).
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, _, err := readAll(t, db, "SELECT n FROM "+paging+" WHERE n < 3", elasticsearch.WithCatalog("other"))
			e, ok := errors.AsType[*elasticsearch.Error](err)
			if !ok || (e.HTTPStatus != http.StatusNotFound && e.HTTPStatus != http.StatusForbidden) || e.RootType != "no_such_remote_cluster_exception" || !strings.Contains(err.Error(), "no such remote cluster") {
				t.Errorf("a cluster that is not configured gave %v, want HTTP 404 or HTTP 403 with no_such_remote_cluster_exception", err)
			}
		})
	})
	subtest("index_using_frozen", func(t *testing.T) {
		status, body := rawAs(t, admin, "SELECT n FROM "+paging, map[string]any{"index_using_frozen": true})
		if status != http.StatusBadRequest || !strings.Contains(body, "unknown field [index_using_frozen]") {
			t.Errorf("index_using_frozen gave HTTP %d: %.200s", status, body)
		}
	})
	subtest("allow_partial_search_results", func(t *testing.T) {
		expect(t, "SELECT n FROM "+paging+" WHERE n < 3 ORDER BY n", [][]any{{int64(1)}, {int64(2)}}, elasticsearch.WithParameter("allow_partial_search_results", true))
	})
	subtest("project_routing", func(t *testing.T) {
		// 8.19.22 does not know the field, and 9.4.6 and 9.5.3 refuse it
		// without cross-project search (measured).
		status, body := rawAs(t, admin, "SELECT n FROM "+paging, map[string]any{"project_routing": "_alias:_origin"})
		if status != http.StatusBadRequest || !strings.Contains(body, "cross-project search") && !strings.Contains(body, "unknown field [project_routing]") {
			t.Errorf("project_routing gave HTTP %d: %.200s", status, body)
		}
	})
	var async struct {
		once sync.Once
		id   string
	}
	startAsync := func(t *testing.T) string {
		t.Helper()
		async.once.Do(func() {
			status, body := rawAs(t, admin, "SELECT n FROM "+paging+" WHERE n < 3 ORDER BY n", map[string]any{"wait_for_completion_timeout": "0s", "keep_on_completion": true})
			var v struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal([]byte(body), &v); err != nil || status != http.StatusOK || v.ID == "" {
				t.Fatalf("an async statement gave HTTP %d, %v: %.200s", status, err, body)
			}
			async.id = v.ID
			// The statement runs for a moment after the first answer.
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
				status, b, err := s.do(t.Context(), http.MethodGet, "/_sql/async/status/"+v.ID, nil)
				if err != nil || status != http.StatusOK {
					t.Fatalf("the async status gave HTTP %d, %v: %.200s", status, err, b)
				}
				if strings.Contains(string(b), `"is_running":false`) {
					break
				}
			}
		})
		return async.id
	}
	subtest("async search", func(t *testing.T) {
		id := startAsync(t)
		if id == "" {
			t.Fatal("no id")
		}
	})
	subtest("async result", func(t *testing.T) {
		id := startAsync(t)
		status, b, err := s.do(t.Context(), http.MethodGet, "/_sql/async/"+id+"?format=json", nil)
		if err != nil || status != http.StatusOK || !strings.Contains(string(b), `"rows":[[1],[2]]`) {
			t.Errorf("the async result gave HTTP %d, %v: %.200s", status, err, b)
		}
	})
	subtest("async status", func(t *testing.T) {
		id := startAsync(t)
		status, b, err := s.do(t.Context(), http.MethodGet, "/_sql/async/status/"+id, nil)
		if err != nil || status != http.StatusOK || !strings.Contains(string(b), `"is_running":false`) {
			t.Errorf("the async status gave HTTP %d, %v: %.200s", status, err, b)
		}
	})
	subtest("async delete", func(t *testing.T) {
		id := startAsync(t)
		status, b, err := s.do(t.Context(), http.MethodDelete, "/_sql/async/delete/"+id, nil)
		if err != nil || status != http.StatusOK || !strings.Contains(string(b), `"acknowledged":true`) {
			t.Errorf("the async delete gave HTTP %d, %v: %.200s", status, err, b)
		}
		status, _, err = s.do(t.Context(), http.MethodGet, "/_sql/async/"+id+"?format=json", nil)
		if err != nil || status != http.StatusNotFound {
			t.Errorf("a deleted async result gave HTTP %d, %v, want 404", status, err)
		}
	})
	subtest("async execute path", func(t *testing.T) {
		status, b, err := s.do(t.Context(), http.MethodPost, "/_sql/async/execute", map[string]any{"query": "SELECT 1"})
		if err != nil || status != http.StatusMethodNotAllowed {
			t.Errorf("the async execute path gave HTTP %d, %v: %.200s, want 405", status, err, b)
		}
	})
	subtest("translate", func(t *testing.T) {
		status, b, err := s.do(t.Context(), http.MethodPost, "/_sql/translate", map[string]any{"query": "SELECT n FROM " + paging + " WHERE n = 1"})
		if err != nil || status != http.StatusOK || !strings.Contains(string(b), `"term":{"n":{"value":1}}`) {
			t.Errorf("translate gave HTTP %d, %v: %.200s", status, err, b)
		}
	})
	subtest("jdbc mode", func(t *testing.T) {
		status, body := rawAs(t, admin, "SELECT 1 AS one", map[string]any{"mode": "jdbc", "version": "9.5.3"})
		// The basic license of the image does not allow the mode (recorded:
		// "a statement in the jdbc mode"). A release with a license that allows
		// it would answer, and the test would show that the verdict changed.
		if status != http.StatusForbidden || !strings.Contains(body, "non-compliant for [jdbc]") {
			t.Errorf("the jdbc mode gave HTTP %d: %.200s, want HTTP 403", status, body)
		}
	})
	subtest("pivot", func(t *testing.T) {
		expect(t, "SELECT * FROM (SELECT n, s FROM "+paging+" WHERE n < 4) PIVOT (COUNT(n) FOR s IN ('row 1', 'row 2'))", [][]any{{int64(1), int64(1)}})
	})
	subtest("histogram", func(t *testing.T) {
		expect(t, "SELECT HISTOGRAM(n, 100) AS h, COUNT(*) AS c FROM "+paging+" GROUP BY h ORDER BY h", [][]any{{int64(0), int64(99)}, {int64(100), int64(100)}, {int64(200), int64(51)}})
	})
	subtest("match and score", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT t, SCORE() AS sc FROM "+types+" WHERE MATCH(t, 'text')")
			if err != nil || len(got) != 1 || got[0][0] != "Some text" {
				t.Fatalf("MATCH gave %v, %v", got, err)
			}
			if sc, ok := got[0][1].(float64); !ok || sc <= 0 {
				t.Errorf("SCORE() is %#v, want a float64 above zero", got[0][1])
			}
		})
	})
	subtest("join", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refused(t, db, "SELECT a.n FROM "+paging+" a JOIN "+window+" b ON a.n = b.n", "JOIN are not yet supported")
		})
	})
	subtest("common table expression", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refusedAs(t, db, "WITH x AS (SELECT n FROM "+paging+") SELECT n FROM x", "verification_exception", "Unknown index [x]")
		})
	})
	subtest("subquery in from", func(t *testing.T) {
		expect(t, "SELECT n FROM (SELECT n FROM "+paging+" WHERE n < 3) ORDER BY n", [][]any{{int64(1)}, {int64(2)}})
	})
	subtest("limit", func(t *testing.T) {
		expect(t, "SELECT n FROM "+paging+" ORDER BY n LIMIT 2", [][]any{{int64(1)}, {int64(2)}})
	})
	subtest("top", func(t *testing.T) {
		expect(t, "SELECT TOP 2 n FROM "+paging+" ORDER BY n", [][]any{{int64(1)}, {int64(2)}})
	})
	subtest("offset", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refused(t, db, "SELECT n FROM "+paging+" ORDER BY n LIMIT 2 OFFSET 1", "mismatched input 'OFFSET'")
		})
	})
	subtest("odbc escapes", func(t *testing.T) {
		expect(t, "SELECT {d '2026-10-01'} AS d, {t '12:34:56'} AS t",
			[][]any{{dbimp.Date{Year: 2026, Month: 10, Day: 1}, dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56}}}})
	})
	subtest("geo functions", func(t *testing.T) {
		expect(t, "SELECT ST_AsWKT(gp) AS w, ST_X(gp) AS x, ST_Y(gp) AS y, ST_GeometryType(gs) AS g FROM "+types+" WHERE id = 1", [][]any{{"POINT (-71.34 41.12)", -71.34, 41.12, "POINT"}})
	})
	subtest("document id", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refusedAs(t, db, "SELECT _id FROM "+types, "verification_exception", "Unknown column [_id]")
		})
	})
	subtest("cancel a task", func(t *testing.T) {
		const id = "dbimp-it-cancel"
		done := slowStatement(s, paging, id)
		// The task of the statement shows while it runs, with its header.
		var found bool
		for deadline := time.Now().Add(10 * time.Second); !found && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			status, b, err := s.do(t.Context(), http.MethodGet, "/_tasks?actions=indices:data/read/sql&detailed=true", nil)
			if err != nil || status != http.StatusOK {
				t.Fatalf("listing the tasks gave HTTP %d, %v: %.200s", status, err, b)
			}
			found = strings.Contains(string(b), id)
		}
		if !found {
			t.Fatal("the task of the statement did not show with its X-Opaque-Id")
		}
		// The ordinary user cannot list or cancel a task (recorded as
		// dbmeta_user).
		if status, b, err := apiAs(t, ordinary).do(t.Context(), http.MethodPost, "/_tasks/_cancel?actions=indices:data/read/sql", nil); err != nil || status != http.StatusForbidden {
			t.Errorf("the ordinary user cancelled a task: HTTP %d, %v: %.200s", status, err, b)
		}
		if status, b, err := s.do(t.Context(), http.MethodPost, "/_tasks/_cancel?actions=indices:data/read/sql", nil); err != nil || status != http.StatusOK {
			t.Fatalf("the administrator cancelled no task: HTTP %d, %v: %.200s", status, err, b)
		}
		r := <-done
		if r.err != nil || r.status != http.StatusBadRequest || !strings.Contains(r.body, "task_cancelled_exception") {
			t.Errorf("a cancelled statement gave HTTP %d, %v: %.300s, want HTTP 400 with task_cancelled_exception", r.status, r.err, r.body)
		}
	})
	for _, format := range []struct{ name, format, contentType, prefix string }{
		{"csv format", "csv", "text/csv", "n,s\r\n1,row 1"},
		{"tsv format", "tsv", "text/tab-separated-values", "n\ts\n1\trow 1"},
		{"txt format", "txt", "text/plain", "       n       |       s       \n"},
		{"yaml format", "yaml", "application/yaml", "---\ncolumns:"},
		{"cbor format", "cbor", "application/cbor", ""},
		{"smile format", "smile", "application/smile", ":)"},
	} {
		subtest(format.name, func(t *testing.T) {
			status, b, err := s.do(t.Context(), http.MethodPost, "/_sql?format="+format.format, map[string]any{"query": "SELECT n, s FROM " + paging + " WHERE n < 3 ORDER BY n"})
			if err != nil || status != http.StatusOK || len(b) == 0 || !strings.HasPrefix(string(b), format.prefix) {
				t.Errorf("the format %s gave HTTP %d, %v: %.100q", format.format, status, err, b)
			}
		})
	}
	subtest("transactions", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			if tx, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
				if err == nil {
					_ = tx.Rollback()
				}
				t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
			}
			refused(t, db, "BEGIN", "mismatched input 'BEGIN'")
			refused(t, db, "COMMIT", "mismatched input 'COMMIT'")
		})
	})
	columnType := func(name, query, want string) {
		subtest(name, func(t *testing.T) {
			forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
				cts := columnTypes(t, db, query)
				if len(cts) != 1 || cts[0].DatabaseTypeName() != want {
					t.Errorf("%s: the type of the column is %v, want %s", query, cts, want)
				}
			})
		})
	}
	columnType("constant_keyword as keyword", "SELECT ck FROM "+types+" WHERE id = 1", "KEYWORD")
	columnType("wildcard as keyword", "SELECT w FROM "+types+" WHERE id = 1", "KEYWORD")
	columnType("match_only_text as text", "SELECT mot FROM "+types+" WHERE id = 1", "TEXT")
	columnType("date_nanos as datetime", "SELECT dn FROM "+types+" WHERE id = 1", "DATETIME")
	columnType("date as datetime", "SELECT dt FROM "+types+" WHERE id = 1", "DATETIME")
	subtest("alias field", func(t *testing.T) {
		expect(t, "SELECT al FROM "+types+" WHERE id < 3 ORDER BY id", [][]any{{int64(2147483647)}, {int64(-2147483648)}})
		columnType("alias field type", "SELECT al FROM "+types+" WHERE id = 1", "INTEGER")
	})
	subtest("window", func(t *testing.T) {
		// A result window smaller than the page of the statement refuses it,
		// and a page that fits reads the whole result.
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refusedAs(t, db, "SELECT n FROM "+window+" ORDER BY n LIMIT 200", "illegal_argument_exception", "Result window is too large")
			_, got, err := readAll(t, db, "SELECT n FROM "+window+" ORDER BY n", elasticsearch.WithFetchSize(100))
			if err != nil || len(got) != 250 {
				t.Errorf("read %d rows and %v, want 250", len(got), err)
			}
		})
	})
}
