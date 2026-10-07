package opensearch_test

import (
	"database/sql"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/opensearch"
)

// feature runs f as the subtest name, as each principal. The name is the name of
// an entry of testdata/opensearch/features.json (step 14a).
func feature(t *testing.T, name string, f func(t *testing.T, e *target)) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		forEach(t, f)
	})
}

// skipFor skips the test when the release is of the 2 series and old is false,
// or of the 3 series and old is true, with the reason.
func (e *target) skipUnless(t *testing.T, old bool, reason string) {
	t.Helper()
	if e.old != old {
		t.Skip(reason)
	}
}

// skipOrdinaryOld skips the test for the ordinary user of the 2 series. The
// legacy engine of 2.19.6 needs indices:admin/aliases/get, which the role of
// dbmeta v0.4.0 does not hold (measured on 2.19.6).
func (e *target) skipOrdinaryOld(t *testing.T, why string) {
	t.Helper()
	if e.old && e.p == ordinary {
		t.Skip("on the 2 series the ordinary user is refused " + why)
	}
}

// rawJSON sends a body to a path of the SQL plugin as the principal, and
// decodes the answer as a JSON object, or an object with the key text for an
// answer that is not JSON.
func rawJSON(t *testing.T, p principal, path string, body any) (int, http.Header, map[string]any) {
	t.Helper()
	status, h, text := rawSQL(t, p, path, body)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		m = map[string]any{"text": text}
	}
	return status, h, m
}

// refusalOf holds that err is the refusal of the SQL plugin for a statement that
// it does not take: HTTP 400 with the exception SQLFeatureNotSupportedException.
func refusalOf(t *testing.T, name string, err error) {
	t.Helper()
	oe, ok := errors.AsType[*opensearch.Error](err)
	if !ok || oe.HTTPStatus != http.StatusBadRequest || oe.Type != "SQLFeatureNotSupportedException" || !strings.Contains(oe.Details, "Query must start with SELECT, DELETE, SHOW or DESCRIBE") {
		t.Errorf("%s gave %v, want the refusal of HTTP 400 with SQLFeatureNotSupportedException", name, err)
	}
}

// TestIntegrationFeatures holds an entry of features.json for each feature that
// the survey of step 5a names, as each principal. A feature that differs between
// the 2 series and the 3 series has an entry for each, and the test of the other
// series skips, with the reason.
func TestIntegrationFeatures(t *testing.T) {
	types, rows, groups, lead := typesIndex(t), rowsIndex(t), groupsIndex(t), leadIndex(t)
	s := newAdminAPI(t)

	feature(t, "cursor paging with fetch size", func(t *testing.T, e *target) {
		_, got, err := e.read(t, "SELECT n FROM "+rows+" ORDER BY n", opensearch.WithFetchSize(100))
		if err != nil || len(got) != 300 {
			t.Fatalf("read %d rows and %v, want 300", len(got), err)
		}
		status, _, first := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT n FROM " + rows + " ORDER BY n", "fetch_size": 100})
		cursor, _ := first["cursor"].(string)
		datarows, _ := first["datarows"].([]any)
		if status != http.StatusOK || !strings.HasPrefix(cursor, "n:") || len(datarows) != 100 {
			t.Errorf("the first page is HTTP %d with %d rows and the cursor %.10q, want 100 rows and a cursor that starts with n:", status, len(datarows), cursor)
		}
		_, _, _ = rawJSON(t, e.p, "/_plugins/_sql/close", map[string]any{"cursor": cursor})
	})

	feature(t, "close a cursor", func(t *testing.T, e *target) {
		_, _, first := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT n FROM " + rows + " ORDER BY n", "fetch_size": 100})
		cursor, _ := first["cursor"].(string)
		for range 2 {
			status, _, got := rawJSON(t, e.p, "/_plugins/_sql/close", map[string]any{"cursor": cursor})
			if status != http.StatusOK || got["succeeded"] != true {
				t.Errorf("closing the cursor gave HTTP %d and %v, want {succeeded: true}, also the second time", status, got)
			}
		}
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"cursor": cursor})
		if status != http.StatusNotFound {
			t.Errorf("a page of a closed cursor gave HTTP %d and %v, want 404", status, got)
		}
		status, _, got = rawJSON(t, e.p, "/_plugins/_sql/close", map[string]any{"cursor": "n:0"})
		if status != http.StatusInternalServerError {
			t.Errorf("closing a cursor that is not one gave HTTP %d and %v, want 500", status, got)
		}
	})

	feature(t, "the jdbc format", func(t *testing.T, e *target) {
		status, h, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT n, s FROM " + rows + " WHERE n <= 2 ORDER BY n"})
		status2, _, again := rawJSON(t, e.p, "/_plugins/_sql?format=jdbc", map[string]any{"query": "SELECT n, s FROM " + rows + " WHERE n <= 2 ORDER BY n"})
		datarows, _ := got["datarows"].([]any)
		if status != http.StatusOK || status2 != http.StatusOK || !reflect.DeepEqual(got, again) || got["schema"] == nil || len(datarows) != 2 {
			t.Errorf("the default format gave HTTP %d and %v, the jdbc format gave HTTP %d and %v, want one answer with schema and datarows", status, got, status2, again)
		}
		if ct := h.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("the content type is %q, want application/json", ct)
		}
	})

	feature(t, "the json format on 2.19", func(t *testing.T, e *target) {
		e.skipUnless(t, true, "the json format is a format of the 2 series")
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql?format=json", map[string]any{"query": "SELECT id, k FROM " + types + " ORDER BY id"})
		if _, ok := got["hits"]; status != http.StatusOK || !ok {
			t.Errorf("the json format gave HTTP %d and %v, want the answer of the search engine with hits", status, got)
		}
	})

	feature(t, "the json format on 3", func(t *testing.T, e *target) {
		e.skipUnless(t, false, "the 2 series has the json format")
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql?format=json", map[string]any{"query": "SELECT id, k FROM " + types + " ORDER BY id"})
		if status != http.StatusBadRequest || !strings.Contains(string(mustJSON(t, got)), "unknown response format: json") {
			t.Errorf("the json format gave HTTP %d and %v, want HTTP 400 for an unknown response format", status, got)
		}
	})

	feature(t, "the csv format", func(t *testing.T, e *target) {
		status, _, text := rawSQL(t, e.p, "/_plugins/_sql?format=csv", map[string]any{"query": "SELECT id, i FROM " + types + " WHERE id = 1"})
		if status != http.StatusOK || !strings.Contains(text, "'-2147483648") {
			t.Errorf("the csv format gave HTTP %d and %q, want the number with a quote before it, which guards against formulas", status, text)
		}
	})

	feature(t, "the csv format with no sanitizing", func(t *testing.T, e *target) {
		status, _, text := rawSQL(t, e.p, "/_plugins/_sql?format=csv&sanitize=false", map[string]any{"query": "SELECT id, i FROM " + types + " WHERE id = 1"})
		if status != http.StatusOK || strings.Contains(text, "'-2147483648") || !strings.Contains(text, "-2147483648") {
			t.Errorf("the csv format with no sanitizing gave HTTP %d and %q, want the number as it is", status, text)
		}
	})

	feature(t, "the raw format", func(t *testing.T, e *target) {
		status, _, text := rawSQL(t, e.p, "/_plugins/_sql?format=raw", map[string]any{"query": "SELECT id, i FROM " + types + " WHERE id = 1"})
		if status != http.StatusOK || !strings.Contains(text, "1|-2147483648") {
			t.Errorf("the raw format gave HTTP %d and %q, want the values with a bar between them", status, text)
		}
	})

	feature(t, "the tsv format", func(t *testing.T, e *target) {
		status, _, text := rawSQL(t, e.p, "/_plugins/_sql?format=tsv", map[string]any{"query": "SELECT 1"})
		if status != http.StatusBadRequest || !strings.Contains(text, "unknown response format: tsv") {
			t.Errorf("the tsv format gave HTTP %d and %q, want HTTP 400 for an unknown response format", status, text)
		}
	})

	feature(t, "explain", func(t *testing.T, e *target) {
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql/_explain", map[string]any{"query": "SELECT s, COUNT(*) FROM " + groups + " GROUP BY s"})
		if e.p == ordinary {
			if status != http.StatusOK && status != http.StatusForbidden {
				t.Errorf("explain as the ordinary user gave HTTP %d and %v", status, got)
			}
			return
		}
		if status != http.StatusOK || (got["root"] == nil && got["from"] == nil && got["size"] == nil && got["aggregations"] == nil) {
			t.Errorf("explain gave HTTP %d and %v, want the plan", status, got)
		}
	})

	feature(t, "a filter of the query DSL", func(t *testing.T, e *target) {
		e.skipOrdinaryOld(t, "a filter: it needs indices:admin/aliases/get (recorded: a statement with a filter of the query DSL)")
		filter := map[string]any{"range": map[string]any{"n": map[string]any{"lte": 3}}}
		_, got, err := e.read(t, "SELECT n FROM "+rows, opensearch.WithParameter("filter", filter))
		if err != nil || !slices.Equal(numbersOf(t, got), []int64{1, 2, 3}) {
			t.Errorf("a filter read %v and %v, want the numbers 1 to 3", got, err)
		}
	})

	feature(t, "parameters with a type and a value", func(t *testing.T, e *target) {
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT id FROM " + types + " WHERE id = ?", "parameters": []any{map[string]any{"type": "integer", "value": 1}}})
		if status != http.StatusOK || !reflect.DeepEqual(got["datarows"], []any{[]any{1.0}}) {
			t.Errorf("a parameter gave HTTP %d and %v, want the row 1", status, got)
		}
	})

	feature(t, "named parameters", func(t *testing.T, e *target) {
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT n FROM " + rows + " WHERE n = :p", "parameters": []any{map[string]any{"type": "integer", "value": 1}}})
		if status != http.StatusBadRequest {
			t.Errorf("a named parameter gave HTTP %d and %v, want HTTP 400", status, got)
		}
	})

	feature(t, "a timestamp parameter", func(t *testing.T, e *target) {
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT ?", "parameters": []any{map[string]any{"type": "timestamp", "value": "2026-10-01 12:00:00"}}})
		if status != http.StatusBadRequest || !strings.Contains(string(mustJSON(t, got)), "Unsupported parameter type timestamp") {
			t.Errorf("a timestamp parameter gave HTTP %d and %v, want HTTP 400", status, got)
		}
	})

	feature(t, "a time parameter", func(t *testing.T, e *target) {
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT ?", "parameters": []any{map[string]any{"type": "time", "value": "12:00:00"}}})
		if status != http.StatusBadRequest || !strings.Contains(string(mustJSON(t, got)), "Unsupported parameter type time") {
			t.Errorf("a time parameter gave HTTP %d and %v, want HTTP 400", status, got)
		}
	})

	feature(t, "a byte parameter", func(t *testing.T, e *target) {
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT ?", "parameters": []any{map[string]any{"type": "byte", "value": 1}}})
		if status != http.StatusOK || !reflect.DeepEqual(got["datarows"], []any{[]any{1.0}}) {
			t.Errorf("a byte parameter gave HTTP %d and %v, want 1", status, got)
		}
	})

	stringParameter := func(t *testing.T, e *target) any {
		t.Helper()
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT ? AS x", "parameters": []any{map[string]any{"type": "string", "value": `it's a\b`}}})
		datarows, _ := got["datarows"].([]any)
		if status != http.StatusOK || len(datarows) != 1 {
			t.Fatalf("a string parameter gave HTTP %d and %v", status, got)
		}
		row, _ := datarows[0].([]any)
		if len(row) != 1 {
			t.Fatalf("a string parameter gave the row %v", datarows[0])
		}
		return row[0]
	}
	feature(t, "a string parameter with a quote on 2.19", func(t *testing.T, e *target) {
		e.skipUnless(t, true, "the 3 series keeps a string with a quote")
		if got := stringParameter(t, e); got == `it's a\b` {
			t.Errorf("the string parameter came back as it was, %q: the 2 series changes a quote and a backslash", got)
		}
	})
	feature(t, "a string parameter with a quote on 3", func(t *testing.T, e *target) {
		e.skipUnless(t, false, "the 2 series changes a string with a quote")
		if got := stringParameter(t, e); got != `it's a\b` {
			t.Errorf("the string parameter came back as %q, want it's a\\b", got)
		}
	})

	oneRow := func(t *testing.T, e *target, query string, want int64) {
		t.Helper()
		_, got, err := e.read(t, query)
		if err != nil || len(got) != 1 || got[0][0] != want {
			t.Errorf("%s read %v and %v, want the row %d", query, got, err, want)
		}
	}
	feature(t, "match", func(t *testing.T, e *target) {
		oneRow(t, e, "SELECT id FROM "+types+" WHERE MATCH(t, 'hello')", 1)
	})
	feature(t, "match phrase", func(t *testing.T, e *target) {
		oneRow(t, e, "SELECT id FROM "+types+" WHERE MATCH_PHRASE(t, 'hello world')", 1)
	})
	feature(t, "multi match", func(t *testing.T, e *target) {
		oneRow(t, e, "SELECT id FROM "+types+" WHERE MULTI_MATCH(['t', 'tk'], 'hello')", 1)
	})
	feature(t, "query string", func(t *testing.T, e *target) {
		oneRow(t, e, "SELECT id FROM "+types+" WHERE QUERY('t:hello')", 1)
	})
	feature(t, "simple query string", func(t *testing.T, e *target) {
		oneRow(t, e, "SELECT id FROM "+types+" WHERE SIMPLE_QUERY_STRING(['t'], 'hello')", 1)
	})
	feature(t, "wildcard query", func(t *testing.T, e *target) {
		oneRow(t, e, "SELECT id FROM "+types+" WHERE WILDCARD_QUERY(k, 'a*')", 4)
	})
	feature(t, "score", func(t *testing.T, e *target) {
		cols, got, err := e.read(t, "SELECT id, _score FROM "+types+" WHERE MATCH(t, 'hello')")
		if err != nil || !slices.Equal(cols, []string{"id", "_score"}) || len(got) != 1 || got[0][0] != int64(1) {
			t.Errorf("_score read %v %v and %v, want the row 1 with its score", cols, got, err)
		}
	})

	feature(t, "the nested function", func(t *testing.T, e *target) {
		cols, got, err := e.read(t, "SELECT id, nested(n.a) FROM "+types+" ORDER BY id")
		want := [][]any{{int64(1), int64(1)}, {int64(1), int64(2)}, {int64(4), int64(3)}}
		if err != nil || !slices.Equal(cols, []string{"id", "nested(n.a)"}) || !reflect.DeepEqual(got, want) {
			t.Errorf("nested() read %v %v and %v, want %v: one row for each nested object", cols, got, err, want)
		}
	})

	feature(t, "a path into an object", func(t *testing.T, e *target) {
		_, got, err := e.read(t, "SELECT id, o.a FROM "+types+" WHERE id = 1")
		if err != nil || !reflect.DeepEqual(got, [][]any{{int64(1), int64(1)}}) {
			t.Errorf("o.a read %v and %v, want the row 1 1", got, err)
		}
	})

	feature(t, "a path into a nested field", func(t *testing.T, e *target) {
		_, got, err := e.read(t, "SELECT id, n.a FROM "+types+" WHERE id = 1")
		if err != nil || !reflect.DeepEqual(got, [][]any{{int64(1), int64(1)}}) {
			t.Errorf("n.a read %v and %v, want the first nested object only", got, err)
		}
	})

	feature(t, "a multi-valued field", func(t *testing.T, e *target) {
		_, got, err := e.read(t, "SELECT id, i FROM "+types+" WHERE i = 2")
		if err != nil || !reflect.DeepEqual(got, [][]any{{int64(4), []any{int64(1), int64(2), int64(3)}}}) {
			t.Errorf("a condition on a field with several values read %v and %v, want the row 4 with the array", got, err)
		}
	})

	feature(t, "the function multi_value", func(t *testing.T, e *target) {
		e.skipOrdinaryOld(t, "the legacy engine, which this function reaches: it needs indices:admin/aliases/get")
		_, _, err := e.read(t, "SELECT id, MULTI_VALUE(i) FROM "+types)
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.HTTPStatus != http.StatusOK || oe.Status != http.StatusInternalServerError || oe.Type != "UnsupportedOperationException" {
			t.Errorf("MULTI_VALUE gave %v, want HTTP 200 with the status 500 in the body", err)
		}
	})

	feature(t, "an index pattern", func(t *testing.T, e *target) {
		_, got, err := e.read(t, "SELECT COUNT(*) FROM "+prefix+"row*")
		if err != nil || !reflect.DeepEqual(got, [][]any{{int64(300)}}) {
			t.Errorf("an index pattern read %v and %v, want 300", got, err)
		}
	})

	feature(t, "the metadata fields", func(t *testing.T, e *target) {
		cols, got, err := e.read(t, "SELECT _id, _index, n FROM "+rows+" WHERE n = 1")
		if err != nil || !slices.Equal(cols, []string{"_id", "_index", "n"}) || !reflect.DeepEqual(got, [][]any{{"1", rows, int64(1)}}) {
			t.Errorf("the metadata fields read %v %v and %v, want the id, the index and the number", cols, got, err)
		}
	})

	feature(t, "a join", func(t *testing.T, e *target) {
		e.skipOrdinaryOld(t, "a join: it needs indices:admin/aliases/get (recorded: a join)")
		_, got, err := e.read(t, "SELECT a.n, b.s FROM "+rows+" a JOIN "+groups+" b ON a.n = b.n WHERE a.n < 3 ORDER BY a.n")
		if err != nil || len(got) != 2 {
			t.Errorf("a join read %v and %v, want two rows", got, err)
		}
	})

	feature(t, "a subquery", func(t *testing.T, e *target) {
		_, got, err := e.read(t, "SELECT n FROM (SELECT n FROM "+rows+" WHERE n < 3) t")
		if err != nil || len(got) != 2 {
			t.Errorf("a subquery read %v and %v, want two rows", got, err)
		}
	})

	feature(t, "a union", func(t *testing.T, e *target) {
		_, _, err := e.read(t, "SELECT n FROM "+rows+" WHERE n = 1 UNION SELECT n FROM "+rows+" WHERE n = 2")
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.HTTPStatus != http.StatusOK || oe.Status != http.StatusInternalServerError {
			t.Errorf("a union gave %v, want HTTP 200 with the status 500 in the body", err)
		}
	})

	feature(t, "a window function", func(t *testing.T, e *target) {
		_, got, err := e.read(t, "SELECT n, ROW_NUMBER() OVER (ORDER BY n) FROM "+rows+" WHERE n < 3")
		if err != nil || len(got) != 2 || got[1][1] != int64(2) {
			t.Errorf("a window function read %v and %v, want the numbers of the rows", got, err)
		}
	})

	feature(t, "a user function", func(t *testing.T, e *target) {
		_, err := e.db.ExecContext(t.Context(), "CREATE FUNCTION f() RETURNS INT")
		refusalOf(t, "CREATE FUNCTION", err)
	})

	feature(t, "the legacy engine", func(t *testing.T, e *target) {
		e.skipOrdinaryOld(t, "a filter, which sends a statement to the legacy engine")
		// ir is a field that SQL cannot read, and the legacy engine names it.
		_, cts, err := e.columns(t, "SELECT hf, sf, ir FROM "+types+" ORDER BY id", opensearch.WithParameter("filter", matchAll))
		if err != nil || len(cts) != 3 {
			t.Fatalf("the column types are %v and %v", cts, err)
		}
		t.Logf("the legacy engine names the types %s, %s and %s", cts[0].DatabaseTypeName(), cts[1].DatabaseTypeName(), cts[2].DatabaseTypeName())
		if cts[2].DatabaseTypeName() != "INTEGER_RANGE" {
			t.Errorf("the legacy engine names an integer_range field %s, want INTEGER_RANGE", cts[2].DatabaseTypeName())
		}
	})

	feature(t, "a group by of more than 1000 groups on 2.19", func(t *testing.T, e *target) {
		e.skipUnless(t, true, "the 3 series reads every group")
		_, got, err := e.read(t, "SELECT s, COUNT(*) FROM "+groups+" GROUP BY s")
		if err != nil || len(got) != 1000 {
			t.Errorf("a group by of 1050 groups read %d groups and %v, want 1000 with no sign that groups are missing", len(got), err)
		}
		_, got, err = e.read(t, "SELECT DISTINCT s FROM "+groups)
		if err != nil || len(got) != 1000 {
			t.Errorf("a distinct of 1050 values read %d values and %v, want 1000", len(got), err)
		}
	})

	feature(t, "a group by of more than 1000 groups on 3", func(t *testing.T, e *target) {
		e.skipUnless(t, false, "the 2 series stops at 1000 groups")
		_, got, err := e.read(t, "SELECT s, COUNT(*) FROM "+groups+" GROUP BY s")
		if err != nil || len(got) != 1050 {
			t.Errorf("a group by of 1050 groups read %d groups and %v, want 1050", len(got), err)
		}
		_, got, err = e.read(t, "SELECT s, COUNT(*) FROM "+groups+" GROUP BY s ORDER BY s LIMIT 50 OFFSET 1000")
		if err != nil || len(got) != 50 {
			t.Errorf("an offset past 1000 read %d groups and %v, want 50", len(got), err)
		}
	})

	feature(t, "a group by in pages", func(t *testing.T, e *target) {
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT s, COUNT(*) FROM " + groups + " GROUP BY s", "fetch_size": 500})
		datarows, _ := got["datarows"].([]any)
		if status != http.StatusOK || got["cursor"] != nil || len(datarows) != 200 {
			t.Errorf("a group by with a page size gave HTTP %d, %d groups and the cursor %v, want 200 groups and no cursor: the legacy engine", status, len(datarows), got["cursor"])
		}
	})

	for _, setting := range []string{"plugins.sql.query.bucket.size", "plugins.sql.group_by.limit"} {
		name := "a setting for the size of a group by"
		if strings.HasSuffix(setting, "limit") {
			name = "a setting for the limit of a group by"
		}
		feature(t, name, func(t *testing.T, e *target) {
			if e.p == ordinary {
				t.Skip("the ordinary user cannot set a cluster setting")
			}
			status, b, err := s.do(t.Context(), http.MethodPut, "/_cluster/settings", map[string]any{"transient": map[string]any{setting: 2000}})
			if err != nil || status != http.StatusBadRequest || !strings.Contains(string(b), "not recognized") {
				t.Errorf("the setting %s gave HTTP %d, %v and %.150s, want HTTP 400 for a setting that is not recognized", setting, status, err, b)
			}
		})
	}

	feature(t, "an error on a later page", func(t *testing.T, e *target) {
		_, got, err := e.read(t, "SELECT n, CAST(s AS INT) FROM "+lead, opensearch.WithFetchSize(100))
		if len(got) != 100 || !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("read %d rows and %v, want 100 rows and dbimp.ErrIncomplete", len(got), err)
		}
	})

	feature(t, "a cancel endpoint", func(t *testing.T, e *target) {
		status, _, text := rawSQL(t, e.p, "/_plugins/_sql/_cancel", map[string]any{})
		if status != http.StatusBadRequest || !strings.Contains(text, "no handler found") {
			t.Errorf("a cancel endpoint gave HTTP %d and %q, want HTTP 400 for no handler", status, text)
		}
	})

	feature(t, "an async query", func(t *testing.T, e *target) {
		status, _, text := rawSQL(t, e.p, "/_plugins/_async_query", map[string]any{"datasource": "dbmeta", "lang": "sql", "query": "SELECT 1"})
		if status < 400 {
			t.Errorf("an async query gave HTTP %d and %q, want an error: the cluster has no data source", status, text)
		}
	})

	feature(t, "a time to wait for completion", func(t *testing.T, e *target) {
		status, _, got := rawJSON(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT 1", "wait_for_completion_timeout": "5s"})
		if status != http.StatusBadRequest {
			t.Errorf("wait_for_completion_timeout gave HTTP %d and %v, want HTTP 400: the key sends the statement to the legacy engine", status, got)
		}
	})

	feature(t, "transactions", func(t *testing.T, e *target) {
		if _, err := e.db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
		}
		_, err := e.db.ExecContext(t.Context(), "BEGIN")
		refusalOf(t, "BEGIN", err)
	})

	feature(t, "PPL", func(t *testing.T, e *target) {
		status, _, got := rawJSON(t, e.p, "/_plugins/_ppl", map[string]any{"query": "source=" + rows + " | where n < 3 | fields n, s"})
		if e.p == ordinary {
			if status != http.StatusForbidden {
				t.Errorf("PPL as the ordinary user gave HTTP %d and %v, want 403 for cluster:admin/opensearch/ppl", status, got)
			}
			return
		}
		datarows, _ := got["datarows"].([]any)
		if status != http.StatusOK || len(datarows) != 2 {
			t.Errorf("PPL gave HTTP %d and %v, want two rows", status, got)
		}
	})

	feature(t, "the path of Open Distro on 2.19", func(t *testing.T, e *target) {
		e.skipUnless(t, true, "the 3 series has no path of Open Distro")
		status, _, got := rawJSON(t, e.p, "/_opendistro/_sql", map[string]any{"query": "SELECT 1"})
		if status != http.StatusOK || got["schema"] == nil {
			t.Errorf("the old path gave HTTP %d and %v, want an answer", status, got)
		}
	})

	feature(t, "the path of Open Distro on 3", func(t *testing.T, e *target) {
		e.skipUnless(t, false, "the 2 series has the path of Open Distro")
		status, _, text := rawSQL(t, e.p, "/_opendistro/_sql", map[string]any{"query": "SELECT 1"})
		if status != http.StatusBadRequest || !strings.Contains(text, "no handler found") {
			t.Errorf("the old path gave HTTP %d and %q, want HTTP 400 for no handler", status, text)
		}
	})

	feature(t, "a cursor as the ordinary user", func(t *testing.T, e *target) {
		// The role holds indices:data/read/search on every index since dbmeta
		// v0.4.0, so the ordinary user reads a cursor on both series.
		_, got, err := readAllContext(t, t.Context(), e.db, "SELECT n FROM "+rows+" ORDER BY n", opensearch.WithFetchSize(100))
		if err != nil || len(got) != 300 {
			t.Errorf("a cursor read %d rows and %v, want 300", len(got), err)
		}
	})

	columnType := func(t *testing.T, e *target, query, name, scan string) {
		t.Helper()
		_, cts, err := e.columns(t, query)
		if err != nil || len(cts) != 1 || cts[0].DatabaseTypeName() != name || cts[0].ScanType().String() != scan {
			t.Errorf("%s: the column type is %v and %v, want %s and %s", query, cts, err, name, scan)
		}
	}
	feature(t, "a half_float field reads as float", func(t *testing.T, e *target) {
		columnType(t, e, "SELECT hf FROM "+types, "FLOAT", "float64")
	})
	feature(t, "a scaled_float field reads as double", func(t *testing.T, e *target) {
		columnType(t, e, "SELECT sf FROM "+types, "DOUBLE", "float64")
	})
	feature(t, "a date_nanos field reads as timestamp", func(t *testing.T, e *target) {
		columnType(t, e, "SELECT dtn FROM "+types, "TIMESTAMP", "time.Time")
	})
	feature(t, "a date field with a format of a day reads as date", func(t *testing.T, e *target) {
		columnType(t, e, "SELECT dtf FROM "+types, "DATE", "dbimp.Date")
	})
	feature(t, "a date field with a format of a time reads as time", func(t *testing.T, e *target) {
		columnType(t, e, "SELECT tm FROM "+types, "TIME", "dbimp.LocalTime")
	})

	// A field of a type that the 3 series reads and the 2 series does not.
	type release3 struct {
		name, typ string
		mapping   map[string]any
		doc       any
		check     func(t *testing.T, e *target, index string)
	}
	for _, tt := range []release3{
		{"a match_only_text field", "match_only_text", m("match_only_text"), "only text", func(t *testing.T, e *target, index string) {
			columnType(t, e, "SELECT v FROM "+index, "TEXT", "string")
		}},
		{"a constant_keyword field", "constant_keyword", map[string]any{"type": "constant_keyword", "value": "c"}, "c", func(t *testing.T, e *target, index string) {
			columnType(t, e, "SELECT v FROM "+index, "KEYWORD", "string")
		}},
		{"an alias field", "alias", map[string]any{"type": "alias", "path": "id"}, nil, func(t *testing.T, e *target, index string) {
			columnType(t, e, "SELECT v FROM "+index, "INTEGER", "int64")
		}},
		{"a knn_vector field", "knn_vector", map[string]any{"type": "knn_vector", "dimension": 3}, []any{1.0, 2.0, 3.0}, func(t *testing.T, e *target, index string) {
			columnType(t, e, "SELECT v FROM "+index, "NESTED", "[]interface {}")
		}},
	} {
		on := func(old bool) string {
			if old {
				return tt.name + " on 2.19"
			}
			return tt.name + " reads as " + map[string]string{"match_only_text": "text", "constant_keyword": "keyword", "alias": "its target", "knn_vector": "nested"}[tt.typ] + " on 3"
		}
		for _, old := range []bool{true, false} {
			feature(t, on(old), func(t *testing.T, e *target) {
				e.skipUnless(t, old, "the other series differs")
				index := prefix + "r3_" + strings.ReplaceAll(tt.typ, "_", "") + "_" + e.p.name[:3]
				body := map[string]any{"settings": settings(), "mappings": map[string]any{"properties": map[string]any{"id": m("integer"), "v": tt.mapping}}}
				if tt.typ == "knn_vector" {
					body["settings"] = map[string]any{"number_of_shards": 1, "number_of_replicas": 0, "index.knn": true}
				}
				s.index(t, index, body)
				doc := map[string]any{"id": 1}
				if tt.doc != nil {
					doc["v"] = tt.doc
				}
				s.must(t, http.MethodPut, "/"+index+"/_doc/1?refresh=true", doc)
				if old {
					_, _, err := e.read(t, "SELECT id, v FROM "+index)
					if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.Status != http.StatusBadRequest || oe.Type != "SemanticCheckException" {
						t.Errorf("selecting a field of the type %s gave %v, want the refusal of the server", tt.typ, err)
					}
					return
				}
				tt.check(t, e, index)
			})
		}
	}

	feature(t, "a subfield of a text", func(t *testing.T, e *target) {
		_, _, err := e.read(t, "SELECT id, tk.raw FROM "+types)
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.Status != http.StatusBadRequest || oe.Type != "SemanticCheckException" {
			t.Errorf("a subfield of a text gave %v, want the refusal of the server", err)
		}
	})
	_ = url.PathEscape
	_ = strconv.Itoa
	_ = sql.ErrNoRows
}

// mustJSON returns the JSON text of v.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
