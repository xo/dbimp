package opensearch_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/opensearch"
)

// TestReplayErrors holds D168 against the recorded errors: the status, the type
// and the reason of each, with no row before it.
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		query  string
		status int
		typ    string
		reason string
	}{
		{"a syntax error", "SELEC 1", http.StatusBadRequest, "SQLFeatureNotSupportedException", "Invalid SQL query"},
		{"an error of the parser", "SELECT FROM dbmeta_rows", http.StatusBadRequest, "ParserException", "Invalid SQL query"},
		{"an unknown index", "SELECT * FROM dbmeta_nosuch", http.StatusNotFound, "IndexNotFoundException", "no such index [dbmeta_nosuch]"},
		{"an unknown column", "SELECT nosuch FROM dbmeta_rows", http.StatusBadRequest, "SemanticCheckException", "Invalid SQL query"},
		{"a function of the wrong type", "SELECT ABS('x')", http.StatusBadRequest, "ExpressionEvaluationException", "Invalid SQL query"},
		{"a cast that fails in a row", "SELECT CAST(s AS INT) FROM dbmeta_rows", http.StatusBadRequest, "NumberFormatException", "Invalid SQL query"},
		{"two columns with one name", "SELECT n, n FROM dbmeta_rows ORDER BY n LIMIT 2", http.StatusBadRequest, "IllegalArgumentException", "Invalid SQL query"},
		{"two statements", "SELECT 1; SELECT 2", http.StatusBadRequest, "ParserException", "Invalid SQL query"},
		{"an insert", "INSERT INTO dbmeta_rows (n, s) VALUES (301, 'row 301')", http.StatusBadRequest, "SQLFeatureNotSupportedException", "Invalid SQL query"},
		{"an update", "UPDATE dbmeta_rows SET s = 'x' WHERE n = 1", http.StatusBadRequest, "SQLFeatureNotSupportedException", "Invalid SQL query"},
		{"a create function", "CREATE FUNCTION f() RETURNS INT", http.StatusBadRequest, "SQLFeatureNotSupportedException", "Invalid SQL query"},
		{"a create table", "CREATE TABLE dbmeta_new (a INT)", http.StatusBadRequest, "SQLFeatureNotSupportedException", "Invalid SQL query"},
		{"a drop table", "DROP TABLE dbmeta_new", http.StatusBadRequest, "SQLFeatureNotSupportedException", "Invalid SQL query"},
		{"begin", "BEGIN", http.StatusBadRequest, "SQLFeatureNotSupportedException", "Invalid SQL query"},
	} {
		for _, release := range releases {
			db := replay(t, release)
			_, err := db.ExecContext(t.Context(), tt.query)
			e, ok := errors.AsType[*opensearch.Error](err)
			if !ok {
				t.Errorf("%s on %s: the error is %v, want an *opensearch.Error", tt.name, release, err)
				continue
			}
			if e.HTTPStatus != tt.status || e.Status != tt.status || e.Type != tt.typ || !strings.Contains(e.Reason, tt.reason) || errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("%s on %s: the error is %+v, want HTTP %d, %s and %q", tt.name, release, *e, tt.status, tt.typ, tt.reason)
			}
		}
	}
}

// TestReplayErrorsWithHTTP200 holds D168 against the recorded errors that
// arrive with HTTP 200: the status is in the body (recorded: "a union" and
// "the function multi_value").
func TestReplayErrorsWithHTTP200(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, query, typ string
	}{
		{"a union", "SELECT n FROM dbmeta_rows WHERE n = 1 UNION SELECT n FROM dbmeta_rows WHERE n = 2", "NullPointerException"},
		{"the function multi_value", "SELECT id, MULTI_VALUE(i) FROM dbmeta_types", "UnsupportedOperationException"},
	} {
		for _, release := range releases {
			db := replay(t, release)
			_, _, err := readAll(t, db, tt.query)
			e, ok := errors.AsType[*opensearch.Error](err)
			if !ok || e.HTTPStatus != http.StatusOK || e.Status != http.StatusInternalServerError || e.Type != tt.typ || errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("%s on %s: the error is %v, want HTTP 200 with the status 500 and %s", tt.name, release, err, tt.typ)
			}
		}
	}
}

// TestReplayWrongPassword holds that a wrong password is HTTP 401 with a body of
// plain text, and that the message holds no password (recorded: "a wrong
// password").
func TestReplayWrongPassword(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db, _ := replayOnly(t, release, func(ex *dbimptest.Exchange) bool { return ex.Response.Status == http.StatusUnauthorized })
		_, err := db.ExecContext(t.Context(), "SELECT 1")
		e, ok := errors.AsType[*opensearch.Error](err)
		if !ok || e.HTTPStatus != http.StatusUnauthorized || e.Reason != "Unauthorized" || e.Type != "" || strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: a wrong password gave %v, want HTTP 401 with the text Unauthorized and no password", release, err)
		}
	}
}

// TestReplayPages holds D168 against the recorded cursor: a result of 300 rows
// in pages of 100 reads the rows in order, sends each cursor once, and sends the
// cursor of the last page, which holds no rows (recorded: "a cursor of 100
// rows", "the second page", "the third page" and "the page after the last
// row").
func TestReplayPages(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db, s := replayOnly(t, release, func(ex *dbimptest.Exchange) bool { return !refused(ex) })
		_, got, err := readAll(t, db, "SELECT n FROM dbmeta_rows ORDER BY n", opensearch.WithFetchSize(100))
		if err != nil || len(got) != 300 {
			t.Fatalf("%s: read %d rows and %v, want 300", release, len(got), err)
		}
		for i, row := range got {
			if row[0] != int64(i+1) {
				t.Fatalf("%s: row %d is %v", release, i+1, row)
			}
		}
		if reqs := s.requests(); len(reqs) != 4 {
			t.Errorf("%s: the server received %v, want 4 requests: the query and three cursors", release, reqs)
		}
	}
}

// TestReplayLegacyPages holds that the driver reads the pages of the legacy
// engine, which the server picks for a LIMIT with a page size: the cursor comes
// before the rows, and a page after the first has no schema (recorded: "a
// cursor with a limit" and "the second page of a cursor with a limit"). The
// driver sends the page size to a statement with a LIMIT only when the caller
// sets it with WithParameter.
func TestReplayLegacyPages(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db, s := replayOnly(t, release, nil)
		s.tolerate = true
		_, got, err := readAll(t, db, "SELECT n FROM dbmeta_rows ORDER BY n LIMIT 250", opensearch.WithParameter("fetch_size", 100))
		// The recordings hold the first two pages, and the request for the third
		// gets HTTP 418 from the fake server.
		if e, ok := errors.AsType[*opensearch.Error](err); !ok || e.HTTPStatus != http.StatusTeapot || !errors.Is(err, dbimp.ErrIncomplete) || len(got) != 200 {
			t.Fatalf("%s: read %d rows and %v, want 200 rows and the failure of the third request", release, len(got), err)
		}
		for i, row := range got {
			if row[0] != int64(i+1) {
				t.Fatalf("%s: row %d is %v", release, i+1, row)
			}
		}
		if reqs := s.requests(); len(reqs) != 3 {
			t.Errorf("%s: the server received %v, want 3 requests", release, reqs)
		}
	}
}

// TestReplayLegacyGroupBy holds that a result that the legacy engine types as
// double for a keyword fails with a clear error and not with a wrong value
// (recorded: "a group by with a fetch size"): the engine sends the type double
// for a column that holds text.
func TestReplayLegacyGroupBy(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		_, got, err := readAll(t, db, "SELECT s, COUNT(*) FROM dbmeta_groups GROUP BY s", opensearch.WithParameter("fetch_size", 500))
		if !errors.Is(err, dbimp.ErrInvalidValue) || len(got) != 0 {
			t.Errorf("%s: read %d rows and %v, want no row and dbimp.ErrInvalidValue", release, len(got), err)
		}
	}
}

// TestReplayErrorOnALaterPage holds D168 and D107 against the recorded error of
// the second page: the first page of 100 rows comes, and then the page that
// holds the row 121 fails with HTTP 400 (recorded: "a cursor whose second page
// fails" and "the second page that fails").
func TestReplayErrorOnALaterPage(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		_, got, err := readAll(t, db, "SELECT n, CAST(s AS INT) FROM dbmeta_lead", opensearch.WithFetchSize(100))
		e, ok := errors.AsType[*opensearch.Error](err)
		if len(got) != 100 || !errors.Is(err, dbimp.ErrIncomplete) || !ok || e.HTTPStatus != http.StatusBadRequest || e.Type != "NumberFormatException" || !strings.Contains(e.Details, "x121") {
			t.Errorf("%s: read %d rows and %v, want 100 rows, dbimp.ErrIncomplete and the error of HTTP 400 for x121", release, len(got), err)
		}
	}
}

// TestReplayFilter holds that WithParameter sends a filter of the Query DSL,
// which the server runs (recorded: "a statement with a filter of the query
// DSL").
func TestReplayFilter(t *testing.T) {
	t.Parallel()
	filter := map[string]any{"range": map[string]any{"n": map[string]any{"lte": 3}}}
	for _, release := range releases {
		db, _ := replayOnly(t, release, func(ex *dbimptest.Exchange) bool { return !refused(ex) })
		_, got, err := readAll(t, db, "SELECT n FROM dbmeta_rows", opensearch.WithParameter("filter", filter))
		if err != nil || len(got) != 3 || got[2][0] != int64(3) {
			t.Errorf("%s: read %v and %v, want the numbers 1 to 3", release, got, err)
		}
	}
}

// TestReplayRefusals holds what the ordinary user meets (recorded as
// dbmeta_user): on 2.19.6 a cursor fails with HTTP 403, and a join or a
// statement with a filter fails with HTTP 200 and the status 403 in the body.
// The driver returns each as the error of the server.
func TestReplayRefusals(t *testing.T) {
	t.Parallel()
	t.Run("a cursor on 2.19.6", func(t *testing.T) {
		t.Parallel()
		db, _ := replayOnly(t, "opensearch-2.19.6", refused)
		_, _, err := readAll(t, db, "SELECT n FROM dbmeta_rows", opensearch.WithFetchSize(100))
		e := serverError(t, err)
		if e.HTTPStatus != http.StatusForbidden || e.Status != http.StatusForbidden || e.Type != "OpenSearchSecurityException" || !strings.Contains(e.Reason, "indices:data/read/search") {
			t.Errorf("a cursor of the ordinary user gave %v, want HTTP 403 for indices:data/read/search", err)
		}
	})
	t.Run("a filter with HTTP 200", func(t *testing.T) {
		t.Parallel()
		db, _ := replayOnly(t, "opensearch-2.19.6", refused)
		filter := map[string]any{"range": map[string]any{"n": map[string]any{"lte": 3}}}
		_, _, err := readAll(t, db, "SELECT n FROM dbmeta_rows", opensearch.WithParameter("filter", filter))
		e := serverError(t, err)
		if e.HTTPStatus != http.StatusOK || e.Status != http.StatusForbidden || !strings.Contains(e.Reason, "indices:admin/aliases/get") {
			t.Errorf("a filter of the ordinary user gave %v, want HTTP 200 with the status 403", err)
		}
	})
	t.Run("a show tables on 3", func(t *testing.T) {
		t.Parallel()
		db, _ := replayOnly(t, "opensearch-3.9.0", refused)
		_, _, err := readAll(t, db, "SHOW TABLES LIKE dbmeta%")
		e := serverError(t, err)
		if e.HTTPStatus != http.StatusOK || e.Status != http.StatusForbidden || !strings.Contains(e.Reason, "indices:admin/get") {
			t.Errorf("SHOW TABLES of the ordinary user gave %v, want HTTP 200 with the status 403", err)
		}
	})
}

// TestReplayRequestsAreOnePathOnly holds that the driver sends every request of
// a statement to the path of the SQL plugin, and never to the path of Open
// Distro (recorded: "the old path of Open Distro").
func TestReplayRequestsAreOnePathOnly(t *testing.T) {
	t.Parallel()
	db, s := replayOnly(t, "opensearch-3.9.0", nil)
	if _, _, err := readAll(t, db, "SELECT n FROM dbmeta_rows ORDER BY n", opensearch.WithFetchSize(100)); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.requests() {
		if r != "POST /_plugins/_sql" {
			t.Errorf("the driver sent %q, want POST /_plugins/_sql", r)
		}
	}
	if !slices.Contains(s.requests(), "POST /_plugins/_sql") {
		t.Error("the driver sent no request")
	}
}
