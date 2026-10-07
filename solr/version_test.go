package solr_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xo/dbimp/solr"
)

// versionServer is a fake server for SELECT version() (D181). GET
// /solr/admin/info/system answers with the status and the body that its fields
// name, and a POST to the SQL handler counts as a statement that reached the
// product.
type versionServer struct {
	status int
	body   string

	roots, statements atomic.Int32
}

// open starts the server and returns a database on it, with the collection of
// the DSN, which can be empty.
func (s *versionServer) open(t *testing.T, collection string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/solr/admin/info/system":
			s.roots.Add(1)
			w.WriteHeader(s.status)
			_, _ = io.WriteString(w, s.body)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sql"):
			s.statements.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"result-set":{"docs":[{"EOF":true,"RESPONSE_TIME":1}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(solr.Name, "solr://"+srv.Listener.Addr().String()+"/"+collection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

const (
	systemBody    = `{"responseHeader":{"status":0,"QTime":1},"mode":"std","lucene":{"solr-spec-version":"9.10.1","solr-impl-version":"9.10.1 abc","lucene-spec-version":"9.12.2"}}`
	forbiddenPage = "<html>\n<head>\n<meta http-equiv=\"Content-Type\" content=\"text/html;charset=utf-8\"/>\n<title>Error 403 Unauthorized request, Response code: 403</title>\n</head>\n<body></body>\n</html>\n"
)

func TestSelectVersion(t *testing.T) {
	t.Parallel()
	for _, collection := range []string{"coll", ""} {
		for _, query := range []string{"SELECT version()", "select VERSION();", "  SELECT\n\tversion() ;\n"} {
			t.Run(query+" on the collection "+collection, func(t *testing.T) {
				t.Parallel()
				s := versionServer{status: http.StatusOK, body: systemBody}
				db := s.open(t, collection)
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
				if len(got) != 1 || got[0] != "9.10.1" {
					t.Errorf("the rows are %#v, want one row with the string 9.10.1", got)
				}
				if s.roots.Load() != 1 || s.statements.Load() != 0 {
					t.Errorf("the server saw %d requests for the release and %d statements, want 1 and 0", s.roots.Load(), s.statements.Load())
				}
			})
		}
	}
	t.Run("a prepared statement and an option", func(t *testing.T) {
		t.Parallel()
		s := versionServer{status: http.StatusOK, body: systemBody}
		db := s.open(t, "coll")
		stmt, err := db.PrepareContext(t.Context(), "SELECT version()")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		var got string
		if err := stmt.QueryRowContext(t.Context()).Scan(&got); err != nil || got != "9.10.1" {
			t.Errorf("the prepared statement gave %q and %v, want 9.10.1", got, err)
		}
		if err := db.QueryRowContext(t.Context(), "SELECT version()", solr.WithDatabase("other")).Scan(&got); err != nil || got != "9.10.1" {
			t.Errorf("the statement with an option gave %q and %v, want 9.10.1", got, err)
		}
	})
}

// TestSelectVersionRefused holds that a user that the role refuses gets the
// error of the request, as for any other statement (D181).
func TestSelectVersionRefused(t *testing.T) {
	t.Parallel()
	s := versionServer{status: http.StatusForbidden, body: forbiddenPage}
	db := s.open(t, "coll")
	var got string
	err := db.QueryRowContext(t.Context(), "SELECT version()").Scan(&got)
	e, ok := errors.AsType[*solr.Error](err)
	if !ok || e.HTTPStatus != http.StatusForbidden {
		t.Fatalf("the error is %v, want the HTTP 403 of the request", err)
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
			s := versionServer{status: http.StatusOK, body: systemBody}
			db := s.open(t, "coll")
			_ = drainQuery(t, db, query)
			if s.roots.Load() != 0 || s.statements.Load() != 1 {
				t.Errorf("the server saw %d requests for the release and %d statements, want 0 and 1", s.roots.Load(), s.statements.Load())
			}
		})
	}
	t.Run("Exec", func(t *testing.T) {
		t.Parallel()
		s := versionServer{status: http.StatusOK, body: systemBody}
		db := s.open(t, "coll")
		_, _ = db.ExecContext(t.Context(), "SELECT version()")
		if s.roots.Load() != 0 || s.statements.Load() != 1 {
			t.Errorf("the server saw %d requests for the release and %d statements, want 0 and 1", s.roots.Load(), s.statements.Load())
		}
	})
	t.Run("an argument", func(t *testing.T) {
		t.Parallel()
		s := versionServer{status: http.StatusOK, body: systemBody}
		db := s.open(t, "coll")
		if err := drainQuery(t, db, "SELECT version()", 1); err == nil {
			t.Error("an argument for SELECT version() gave no error")
		}
		if s.roots.Load() != 0 {
			t.Errorf("the server saw %d requests for the release, want none", s.roots.Load())
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
