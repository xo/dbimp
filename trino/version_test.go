package trino_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"testing"

	"github.com/xo/dbimp/trino"
)

// infoBody is the answer of GET /v1/info on Presto.
const infoBody = `{"nodeVersion":{"version":"0.299-7d50721"},"environment":"test","coordinator":true,"starting":false,"uptime":"1.00m"}`

// versionHandler answers GET /v1/info with infoBody and any statement with a
// page of one row.
func versionHandler(w http.ResponseWriter, r *http.Request, _ string, _ int) {
	if r.URL.Path == "/v1/info" {
		writePage(w, infoBody)
		return
	}
	one(w, r, "", 0)
}

// count returns the number of requests to GET /v1/info and to POST /v1/statement.
func count(f *fake) (int, int) {
	var infos, statements int
	for _, r := range f.requests() {
		switch {
		case r.method == http.MethodGet && r.uri == "/v1/info":
			infos++
		case r.method == http.MethodPost:
			statements++
		}
	}
	return infos, statements
}

// TestSelectVersionOnPresto holds D181: Presto has no function for the
// release, so the driver answers SELECT version() from GET /v1/info, and asks
// once, with the key flavor and without it.
func TestSelectVersionOnPresto(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		query string
		infos int
	}{
		{"with the key flavor", "flavor=presto", 1},
		{"with the flavor from the server", "", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake(t, versionHandler)
			db := f.open(t, f.dsn(tt.query))
			for _, query := range []string{"SELECT version()", "select VERSION();", "  SELECT\n\tversion() ;\n"} {
				cols, got := versionRows(t, db, query)
				if len(cols) != 1 || cols[0] != "version" {
					t.Fatalf("the columns are %v, want [version]", cols)
				}
				if len(got) != 1 || got[0] != "0.299-7d50721" {
					t.Errorf("%q gave the rows %#v, want one row with the string 0.299-7d50721", query, got)
				}
			}
			stmt, err := db.PrepareContext(t.Context(), "SELECT version()")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			var got string
			if err := stmt.QueryRowContext(t.Context()).Scan(&got); err != nil || got != "0.299-7d50721" {
				t.Errorf("the prepared statement gave %q and %v, want 0.299-7d50721", got, err)
			}
			if err := db.QueryRowContext(t.Context(), "SELECT version()", trino.WithSource("x")).Scan(&got); err != nil || got != "0.299-7d50721" {
				t.Errorf("the statement with an option gave %q and %v, want 0.299-7d50721", got, err)
			}
			infos, statements := count(f)
			if infos != tt.infos || statements != 0 {
				t.Errorf("the server saw %d requests for GET /v1/info and %d statements, want %d and 0", infos, statements, tt.infos)
			}
		})
	}
}

// TestSelectVersionOnTrino holds D181: Trino answers the statement itself, so
// the driver sends it, and asks GET /v1/info only to learn the flavor.
func TestSelectVersionOnTrino(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, r *http.Request, body string, n int) {
		if r.URL.Path == "/v1/info" {
			writePage(w, `{"nodeVersion":{"version":"483"},"environment":"docker","coordinator":true,"starting":false,"uptime":"1.00m"}`)
			return
		}
		one(w, r, body, n)
	})
	db := f.open(t, f.dsn(""))
	var got int
	if err := db.QueryRowContext(t.Context(), "SELECT version()").Scan(&got); err != nil {
		t.Fatal(err)
	}
	infos, statements := count(f)
	if infos != 1 || statements != 1 {
		t.Errorf("the server saw %d requests for GET /v1/info and %d statements, want 1 and 1", infos, statements)
	}
	db = f.open(t, f.dsn("flavor=trino"))
	if err := db.QueryRowContext(t.Context(), "SELECT version()").Scan(&got); err != nil {
		t.Fatal(err)
	}
	infos, statements = count(f)
	if infos != 1 || statements != 2 {
		t.Errorf("the server saw %d more requests for GET /v1/info and %d statements in all, want 1 and 2", infos, statements)
	}
}

// TestSelectVersionRefused holds that an error of GET /v1/info reaches the
// caller as the error of the request, and that nothing sends the request
// again (D181).
func TestSelectVersionRefused(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, r *http.Request, body string, n int) {
		if r.URL.Path == "/v1/info" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Access Denied"))
			return
		}
		one(w, r, body, n)
	})
	db := f.open(t, f.dsn("flavor=presto"))
	var got string
	err := db.QueryRowContext(t.Context(), "SELECT version()").Scan(&got)
	e, ok := errors.AsType[*trino.Error](err)
	if !ok || e.HTTPStatus != http.StatusForbidden {
		t.Fatalf("the error is %v, want the HTTP 403 of the request", err)
	}
	if errors.Is(err, driver.ErrBadConn) {
		t.Error("an answer of the server wraps driver.ErrBadConn, and database/sql would run the statement again")
	}
	infos, statements := count(f)
	if infos != 1 || statements != 0 {
		t.Errorf("the server saw %d requests for GET /v1/info and %d statements, want 1 and 0", infos, statements)
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
			f := newFake(t, versionHandler)
			db := f.open(t, f.dsn("flavor=presto"))
			if err := drainQuery(t, db, query); err != nil {
				t.Fatal(err)
			}
			if infos, statements := count(f); infos != 0 || statements != 1 {
				t.Errorf("the server saw %d requests for GET /v1/info and %d statements, want 0 and 1", infos, statements)
			}
		})
	}
	t.Run("Exec", func(t *testing.T) {
		t.Parallel()
		f := newFake(t, versionHandler)
		db := f.open(t, f.dsn("flavor=presto"))
		exec(t, db, "SELECT version()")
		if infos, statements := count(f); infos != 0 || statements != 1 {
			t.Errorf("the server saw %d requests for GET /v1/info and %d statements, want 0 and 1", infos, statements)
		}
	})
	t.Run("an argument", func(t *testing.T) {
		t.Parallel()
		f := newFake(t, versionHandler)
		db := f.open(t, f.dsn("flavor=presto"))
		if err := drainQuery(t, db, "SELECT version()", 1); err != nil {
			t.Fatal(err)
		}
		if infos, statements := count(f); infos != 0 || statements != 1 {
			t.Errorf("the server saw %d requests for GET /v1/info and %d statements, want 0 and 1", infos, statements)
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
