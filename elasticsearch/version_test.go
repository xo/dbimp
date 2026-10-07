package elasticsearch_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/xo/dbimp/elasticsearch"
)

// versionServer is a fake server for SELECT version() (D181). GET / answers
// with root, and a POST to /_sql counts as a statement that reached the
// product.
type versionServer struct {
	roots, statements atomic.Int32
}

// open starts the server, whose GET / gives the status and the body, and
// returns a database on it.
func (s *versionServer) open(t *testing.T, status int, body string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			s.roots.Add(1)
			reply(w, status, body)
		case r.Method == http.MethodPost && r.URL.Path == "/_sql":
			s.statements.Add(1)
			reply(w, http.StatusOK, `{"columns":[{"name":"a","type":"long"}],"rows":[[1]]}`)
		default:
			reply(w, http.StatusNotFound, `{"error":"no handler"}`)
		}
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(elasticsearch.Name, "elasticsearch://"+srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

const rootBody = `{"name":"n","cluster_name":"docker-cluster","version":{"number":"9.5.3","build_flavor":"default"},"tagline":"You Know, for Search"}`

func TestSelectVersion(t *testing.T) {
	t.Parallel()
	for _, query := range []string{
		"SELECT version()",
		"select VERSION()",
		"  SELECT\n\tversion() ;\n",
		"select version();",
	} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			var s versionServer
			db := s.open(t, http.StatusOK, rootBody)
			rows, err := db.QueryContext(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			cols, err := rows.Columns()
			if err != nil || len(cols) != 1 || cols[0] != "version" {
				t.Fatalf("the columns are %v and %v, want [version]", cols, err)
			}
			var got []any
			for rows.Next() {
				var v any
				if err := rows.Scan(&v); err != nil {
					t.Fatal(err)
				}
				got = append(got, v)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0] != "9.5.3" {
				t.Errorf("the rows are %#v, want one row with the string 9.5.3", got)
			}
			if s.roots.Load() != 1 || s.statements.Load() != 0 {
				t.Errorf("the server saw %d requests for GET / and %d statements, want 1 and 0", s.roots.Load(), s.statements.Load())
			}
		})
	}
	t.Run("a prepared statement and an option", func(t *testing.T) {
		t.Parallel()
		var s versionServer
		db := s.open(t, http.StatusOK, rootBody)
		stmt, err := db.PrepareContext(t.Context(), "SELECT version()")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		var got string
		if err := stmt.QueryRowContext(t.Context()).Scan(&got); err != nil || got != "9.5.3" {
			t.Errorf("the prepared statement gave %q and %v, want 9.5.3", got, err)
		}
		if err := db.QueryRowContext(t.Context(), "SELECT version()", elasticsearch.WithFetchSize(5)).Scan(&got); err != nil || got != "9.5.3" {
			t.Errorf("the statement with an option gave %q and %v, want 9.5.3", got, err)
		}
	})
}

// TestSelectVersionRefused holds that a user that GET / refuses gets the error
// of the request, as for any other statement (D181).
func TestSelectVersionRefused(t *testing.T) {
	t.Parallel()
	var s versionServer
	db := s.open(t, http.StatusForbidden, `{"error":{"root_cause":[{"type":"security_exception","reason":"action [cluster:monitor/main] is unauthorized"}],"type":"security_exception","reason":"action [cluster:monitor/main] is unauthorized"},"status":403}`)
	var got string
	err := db.QueryRowContext(t.Context(), "SELECT version()").Scan(&got)
	e, ok := errors.AsType[*elasticsearch.Error](err)
	if !ok || e.HTTPStatus != http.StatusForbidden || e.Type != "security_exception" {
		t.Fatalf("the error is %v, want the HTTP 403 of GET /", err)
	}
	if errors.Is(err, driver.ErrBadConn) {
		t.Error("an answer of the server wraps driver.ErrBadConn, and database/sql would run the statement again")
	}
	if s.statements.Load() != 0 {
		t.Errorf("the server saw %d statements, want none", s.statements.Load())
	}
}

// TestSelectVersionOnlyThat holds that any other statement goes to the
// product, and that SELECT version() through Exec does too (D181).
func TestSelectVersionOnlyThat(t *testing.T) {
	t.Parallel()
	for _, query := range []string{
		"SELECT version() FROM t",
		"SELECT version(), 1",
		"SELECT 'version()'",
		"SELECT version( )",
		"SELECT version(1)",
		"/* c */ SELECT version()",
		"-- c\nSELECT version()",
		"SELECT version();;",
		"SELECT version(); SELECT 1",
		"SELECT 1; SELECT version()",
	} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			var s versionServer
			db := s.open(t, http.StatusOK, rootBody)
			if err := drainQuery(t, db, query); err != nil {
				t.Fatal(err)
			}
			if s.roots.Load() != 0 || s.statements.Load() != 1 {
				t.Errorf("the server saw %d requests for GET / and %d statements, want 0 and 1", s.roots.Load(), s.statements.Load())
			}
		})
	}
	t.Run("Exec", func(t *testing.T) {
		t.Parallel()
		var s versionServer
		db := s.open(t, http.StatusOK, rootBody)
		if _, err := db.ExecContext(t.Context(), "SELECT version()"); err != nil {
			t.Fatal(err)
		}
		if s.roots.Load() != 0 || s.statements.Load() != 1 {
			t.Errorf("the server saw %d requests for GET / and %d statements, want 0 and 1", s.roots.Load(), s.statements.Load())
		}
	})
	t.Run("an argument", func(t *testing.T) {
		t.Parallel()
		// The statement takes the normal path, where the driver sends the
		// argument as a parameter of the statement.
		var s versionServer
		db := s.open(t, http.StatusOK, rootBody)
		if err := drainQuery(t, db, "SELECT version()", 1); err != nil {
			t.Fatal(err)
		}
		if s.roots.Load() != 0 || s.statements.Load() != 1 {
			t.Errorf("the server saw %d requests for GET / and %d statements, want 0 and 1", s.roots.Load(), s.statements.Load())
		}
	})
}

// drainQuery sends query with QueryContext, reads every row, and returns the error.
func drainQuery(t *testing.T, db *sql.DB, query string, args ...any) error {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}
