package surrealdb_test

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xo/dbimp/surrealdb"
)

// versionServer is a fake server for SELECT version() (D181). The RPC method
// version answers with the status and the body that its fields name, and any
// other RPC method counts as a statement that reached the product.
type versionServer struct {
	status int
	body   string

	versions, statements atomic.Int32
}

// open starts the server and returns a database on it.
func (s *versionServer) open(t *testing.T) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(b, &m)
		w.Header().Set("Content-Type", "application/json")
		if m.Method == "version" {
			s.versions.Add(1)
			w.WriteHeader(s.status)
			_, _ = io.WriteString(w, s.body)
			return
		}
		s.statements.Add(1)
		_, _ = io.WriteString(w, `{"result":[{"result":1,"status":"OK","time":"1µs"}]}`)
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(surrealdb.Name, strings.Replace(srv.URL, "http://", "surrealdb://u:p@", 1)+"/ns/db?encoding=json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSelectVersion(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"SELECT version()", "select VERSION();", "  SELECT\n\tversion() ;\n"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			s := versionServer{status: http.StatusOK, body: `{"result":"surrealdb-3.3.0"}`}
			db := s.open(t)
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
			if len(got) != 1 || got[0] != "surrealdb-3.3.0" {
				t.Errorf("the rows are %#v, want one row with the string surrealdb-3.3.0", got)
			}
			if s.versions.Load() != 1 || s.statements.Load() != 0 {
				t.Errorf("the server saw %d requests for the version and %d statements, want 1 and 0", s.versions.Load(), s.statements.Load())
			}
		})
	}
	t.Run("a prepared statement and an option", func(t *testing.T) {
		t.Parallel()
		s := versionServer{status: http.StatusOK, body: `{"result":"surrealdb-3.3.0"}`}
		db := s.open(t)
		stmt, err := db.PrepareContext(t.Context(), "SELECT version()")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		var got string
		if err := stmt.QueryRowContext(t.Context()).Scan(&got); err != nil || got != "surrealdb-3.3.0" {
			t.Errorf("the prepared statement gave %q and %v, want surrealdb-3.3.0", got, err)
		}
		if err := db.QueryRowContext(t.Context(), "SELECT version()", surrealdb.WithDatabase("other")).Scan(&got); err != nil || got != "surrealdb-3.3.0" {
			t.Errorf("the statement with an option gave %q and %v, want surrealdb-3.3.0", got, err)
		}
	})
}

// TestSelectVersionRefused holds that a refusal of the request reaches the
// caller as the error of the request, as for any other statement (D181).
func TestSelectVersionRefused(t *testing.T) {
	t.Parallel()
	s := versionServer{status: http.StatusUnauthorized, body: `{"code":401,"details":"Authentication failed","description":"Your authentication details are invalid.","information":"There was a problem with authentication"}`}
	db := s.open(t)
	var got string
	err := db.QueryRowContext(t.Context(), "SELECT version()").Scan(&got)
	if _, ok := errors.AsType[*surrealdb.ResponseError](err); !ok {
		t.Fatalf("the error is %v, want the refusal of the server", err)
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
			s := versionServer{status: http.StatusOK, body: `{"result":"surrealdb-3.3.0"}`}
			db := s.open(t)
			if err := drainQuery(t, db, query); err != nil {
				t.Fatal(err)
			}
			if s.versions.Load() != 0 || s.statements.Load() != 1 {
				t.Errorf("the server saw %d requests for the version and %d statements, want 0 and 1", s.versions.Load(), s.statements.Load())
			}
		})
	}
	t.Run("Exec", func(t *testing.T) {
		t.Parallel()
		s := versionServer{status: http.StatusOK, body: `{"result":"surrealdb-3.3.0"}`}
		db := s.open(t)
		if _, err := db.ExecContext(t.Context(), "SELECT version()"); err != nil {
			t.Fatal(err)
		}
		if s.versions.Load() != 0 || s.statements.Load() != 1 {
			t.Errorf("the server saw %d requests for the version and %d statements, want 0 and 1", s.versions.Load(), s.statements.Load())
		}
	})
	t.Run("an argument", func(t *testing.T) {
		t.Parallel()
		// The statement takes the normal path, which refuses an argument
		// that has no name.
		s := versionServer{status: http.StatusOK, body: `{"result":"surrealdb-3.3.0"}`}
		db := s.open(t)
		if err := drainQuery(t, db, "SELECT version()", 1); err == nil {
			t.Error("an argument with no name gave no error")
		}
		if s.versions.Load() != 0 || s.statements.Load() != 0 {
			t.Errorf("the server saw %d requests for the version and %d statements, want none", s.versions.Load(), s.statements.Load())
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
