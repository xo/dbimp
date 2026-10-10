package influxdb_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/influxdb"
)

// TestAuthenticationInfluxdb holds D197. The error of the driver matches
// dbimp.ErrAuthentication for the recorded answer of a wrong credential, and
// for no other recorded refusal. Each case serves the recorded response of
// one file under testdata/influxdb/ and runs one statement.
func TestAuthenticationInfluxdb(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		file string
		want bool
	}{
		{"1.11.8 a wrong password (influxql)", "influxdb-1.11.8-024-post--query.json", true},
		{"2.8.0 a wrong password (influxql)", "influxdb-2.8.0-024-post--query.json", true},
		{"3.10.6 a wrong password (influxql)", "influxdb-3.10.6-049-post--query.json", true},
		{"1.11.8 a refused privilege, HTTP 403", "influxdb-1.11.8-074-post--query.json", false},
		{"1.11.8 a refused write, HTTP 403", "influxdb-1.11.8-078-post--write.json", false},
		{"2.9.1 a refused privilege, HTTP 200", "influxdb-2.9.1-080-post--query.json", false},
		{"1.11.8 a bad request, HTTP 400", "influxdb-1.11.8-019-post--query.json", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ex, err := dbimptest.ReadExchange("../testdata/influxdb/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if ct := ex.Response.Header.Get("Content-Type"); ct != "" {
					w.Header().Set("Content-Type", ct)
				}
				w.WriteHeader(ex.Response.Status)
				_, _ = w.Write(ex.Response.Content())
			}))
			t.Cleanup(srv.Close)
			db, err := sql.Open(influxdb.Name, strings.Replace(srv.URL, "http://", "influxdb://u:secret@", 1)+"/dbmeta?sqlmode=disable&version=1")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			_, err = db.ExecContext(t.Context(), "SELECT 1")
			if err == nil {
				t.Fatal("the statement succeeded, want an error")
			}
			if _, ok := errors.AsType[*influxdb.Error](err); !ok {
				t.Fatalf("the error is %v (%T), want an *influxdb.Error", err, err)
			}
			wrapped := fmt.Errorf("running a statement: %w", err)
			joined := errors.Join(errors.New("other"), wrapped)
			for name, e := range map[string]error{"plain": err, "wrapped": wrapped, "joined": joined} {
				if got := errors.Is(e, dbimp.ErrAuthentication); got != tt.want {
					t.Errorf("%s: errors.Is(%v, dbimp.ErrAuthentication) is %t, want %t", name, err, got, tt.want)
				}
			}
			if errors.Is(err, dbimp.ErrNotSupported) {
				t.Error("the error matches dbimp.ErrNotSupported")
			}
		})
	}
}
