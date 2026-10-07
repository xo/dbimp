package opensearch_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/opensearch"
)

// versionServer is a fake server for SELECT version() (D181). GET / answers
// with the status, the header and the body that its fields name, and a POST
// to /_plugins/_sql counts as a statement that reached the product. The
// answer to a statement carries the header too.
type versionServer struct {
	status int
	header string
	body   string

	roots, statements atomic.Int32
}

// open starts the server and returns a database on it.
func (s *versionServer) open(t *testing.T) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.header != "" {
			w.Header().Set("X-Opensearch-Version", s.header)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			s.roots.Add(1)
			reply(w, s.status, s.body)
		case r.Method == http.MethodPost && r.URL.Path == "/_plugins/_sql":
			s.statements.Add(1)
			reply(w, http.StatusOK, `{"schema":[{"name":"a","type":"long"}],"datarows":[[1]],"total":1,"size":1,"status":200}`)
		default:
			reply(w, http.StatusNotFound, `{"error":"no handler"}`)
		}
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(opensearch.Name, "opensearch://"+srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

const (
	rootBody      = `{"name":"n","cluster_name":"docker-cluster","version":{"distribution":"opensearch","number":"2.19.6"},"tagline":"The OpenSearch Project: https://opensearch.org/"}`
	forbiddenBody = `{"error":{"root_cause":[{"type":"security_exception","reason":"no permissions for [cluster:monitor/main] and User [name=dbmeta_user]"}],"type":"security_exception","reason":"no permissions for [cluster:monitor/main] and User [name=dbmeta_user]"},"status":403}`
)

func TestSelectVersion(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		status int
		header string
		body   string
		want   string
	}{
		{"the administrator of the 2 series", http.StatusOK, "", rootBody, "2.19.6"},
		{"the administrator of the 3 series", http.StatusOK, "OpenSearch/3.9.0 (opensearch)", rootBody, "3.9.0"},
		{"the ordinary user of the 3 series", http.StatusForbidden, "OpenSearch/3.9.0 (opensearch)", forbiddenBody, "3.9.0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, query := range []string{"SELECT version()", "select VERSION();", "  SELECT\n\tversion() ;\n"} {
				s := versionServer{status: tt.status, header: tt.header, body: tt.body}
				db := s.open(t)
				cols, got := versionRows(t, db, query)
				if len(cols) != 1 || cols[0] != "version" {
					t.Fatalf("the columns are %v, want [version]", cols)
				}
				if len(got) != 1 || got[0] != tt.want {
					t.Errorf("%q gave the rows %#v, want one row with the string %s", query, got, tt.want)
				}
				if s.roots.Load() != 1 || s.statements.Load() != 0 {
					t.Errorf("the server saw %d requests for GET / and %d statements, want 1 and 0", s.roots.Load(), s.statements.Load())
				}
			}
		})
	}
	t.Run("a prepared statement and an option", func(t *testing.T) {
		t.Parallel()
		s := versionServer{status: http.StatusOK, body: rootBody}
		db := s.open(t)
		stmt, err := db.PrepareContext(t.Context(), "SELECT version()")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		var got string
		if err := stmt.QueryRowContext(t.Context()).Scan(&got); err != nil || got != "2.19.6" {
			t.Errorf("the prepared statement gave %q and %v, want 2.19.6", got, err)
		}
		if err := db.QueryRowContext(t.Context(), "SELECT version()", opensearch.WithFetchSize(5)).Scan(&got); err != nil || got != "2.19.6" {
			t.Errorf("the statement with an option gave %q and %v, want 2.19.6", got, err)
		}
	})
}

// TestSelectVersionFromTheHeader holds that the connection does not send GET /
// when an earlier answer carried the release in the header (D181).
func TestSelectVersionFromTheHeader(t *testing.T) {
	t.Parallel()
	s := versionServer{status: http.StatusForbidden, header: "OpenSearch/3.9.0 (opensearch)", body: forbiddenBody}
	db := s.open(t)
	c, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := drainOn(t, c, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var got string
		if err := c.QueryRowContext(t.Context(), "SELECT version()").Scan(&got); err != nil || got != "3.9.0" {
			t.Fatalf("SELECT version() gave %q and %v, want 3.9.0", got, err)
		}
	}
	if s.roots.Load() != 0 || s.statements.Load() != 1 {
		t.Errorf("the server saw %d requests for GET / and %d statements, want 0 and 1", s.roots.Load(), s.statements.Load())
	}
}

// TestSelectVersionRefused holds that a user that GET / refuses, on a release
// that sends no header, gets the error of the request, as for any other
// statement (D181).
func TestSelectVersionRefused(t *testing.T) {
	t.Parallel()
	s := versionServer{status: http.StatusForbidden, body: forbiddenBody}
	db := s.open(t)
	var got string
	err := db.QueryRowContext(t.Context(), "SELECT version()").Scan(&got)
	e, ok := errors.AsType[*opensearch.Error](err)
	if !ok || e.HTTPStatus != http.StatusForbidden {
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
			s := versionServer{status: http.StatusOK, body: rootBody}
			db := s.open(t)
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
		s := versionServer{status: http.StatusOK, body: rootBody}
		db := s.open(t)
		if _, err := db.ExecContext(t.Context(), "SELECT version()"); err != nil {
			t.Fatal(err)
		}
		if s.roots.Load() != 0 || s.statements.Load() != 1 {
			t.Errorf("the server saw %d requests for GET / and %d statements, want 0 and 1", s.roots.Load(), s.statements.Load())
		}
	})
	t.Run("an argument", func(t *testing.T) {
		t.Parallel()
		// The statement takes the normal path, which refuses an argument that
		// no placeholder takes.
		s := versionServer{status: http.StatusOK, body: rootBody}
		db := s.open(t)
		err := drainQuery(t, db, "SELECT version()", 1)
		if !errors.Is(err, dbimp.ErrArguments) {
			t.Errorf("an argument for SELECT version() gave %v, want dbimp.ErrArguments", err)
		}
		if s.roots.Load() != 0 || s.statements.Load() != 0 {
			t.Errorf("the server saw %d requests for GET / and %d statements, want none", s.roots.Load(), s.statements.Load())
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

// drainOn sends query on c with QueryContext, and reads every row.
func drainOn(t *testing.T, c *sql.Conn, query string) error {
	t.Helper()
	rows, err := c.QueryContext(t.Context(), query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// versionRows runs query, and returns its columns and the value of each row.
func versionRows(t *testing.T, db *sql.DB, query string) ([]string, []any) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatalf("%q: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
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
	return cols, got
}
