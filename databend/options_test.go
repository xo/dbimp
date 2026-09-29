package databend_test

import (
	"database/sql"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/databend"
)

// optionServer is a fake server for the option tests. It answers each
// statement with one row, and the session that the request sent, with the
// setting x set to 1 when the statement is SET x = 1, as the server does.
type optionServer struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (s *optionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s.mu.Lock()
	s.bodies = append(s.bodies, m)
	s.mu.Unlock()
	sess, _ := m["session"].(map[string]any)
	if m["sql"] == "SET x = 1" {
		settings, _ := sess["settings"].(map[string]any)
		settings["x"] = "1"
	}
	out, _ := json.Marshal(sess)
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"q","state":"Succeeded","session":`+string(out)+`,"error":null,"has_result_set":true,"schema":[{"name":"a","type":"Int32"}],"data":[["1"]],"next_uri":null,"final_uri":null}`)
}

// open returns a database of one connection on the fake server.
func (s *optionServer) open(t *testing.T) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db, err := sql.Open(databend.Name, strings.Replace(srv.URL, "http://", "databend://u:p@", 1)+"/dbmeta?cancel=none&timezone=UTC")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	return db
}

// requests returns the bodies that the server received, and forgets them.
func (s *optionServer) requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bodies
	s.bodies = nil
	return b
}

// settingsOf returns the settings of the session of a body.
func settingsOf(b map[string]any) map[string]any {
	sess, _ := b["session"].(map[string]any)
	m, _ := sess["settings"].(map[string]any)
	return m
}

// databaseOf returns the database of the session of a body.
func databaseOf(b map[string]any) any {
	sess, _ := b["session"].(map[string]any)
	return sess["database"]
}

// TestOptionArguments holds that the options of one statement reach its
// session and its body, that the other arguments keep their order, and that
// the next statement returns to the session of the connection (D109 and
// D122).
func TestOptionArguments(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t)
	var a int64
	err := db.QueryRowContext(t.Context(), "SELECT ? + ? AS a", int64(1),
		databend.WithTimeout(1500*time.Millisecond),
		databend.WithDatabase("other"),
		databend.WithTimezone("Asia/Tokyo"),
		databend.WithParameter("pagination", map[string]any{"max_rows_per_page": 7}),
		int64(2)).Scan(&a)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT 1 AS a").Scan(&a); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("the server received %d requests, want 2", len(reqs))
	}
	first, second := reqs[0], reqs[1]
	params, _ := first["params"].([]any)
	fs := settingsOf(first)
	switch {
	case databaseOf(first) != "other", fs["max_execute_time_in_seconds"] != "2", fs["timezone"] != "Asia/Tokyo":
		t.Errorf("the first session is %v, want the database other, a limit of 2 seconds and Asia/Tokyo", first["session"])
	case fs["format_null_as_str"] != "0", fs["geometry_output_format"] != "WKT":
		t.Errorf("the first session is %v, want the settings of the DSN (D118)", first["session"])
	case len(params) != 2 || params[0] != 1.0 || params[1] != 2.0:
		t.Errorf("the first params are %v, want [1 2]", first["params"])
	case first["pagination"] == nil:
		t.Errorf("the first body is %v, want the pagination of WithParameter", first)
	}
	ss := settingsOf(second)
	if databaseOf(second) != "dbmeta" || ss["max_execute_time_in_seconds"] != nil || ss["timezone"] != "UTC" || second["pagination"] != nil {
		t.Errorf("the second body is %v, want the session of the DSN, with no option of the first", second)
	}
}

// TestOptionSession holds that a SET stays in the session of the connection,
// and that ResetSession, when database/sql reuses the connection, sets it back
// to the session of the DSN (D122).
func TestOptionSession(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), "SET x = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), "SELECT 1 AS a"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT 1 AS a"); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 3 {
		t.Fatalf("the server received %d requests, want 3", len(reqs))
	}
	if settingsOf(reqs[1])["x"] != "1" {
		t.Errorf("the statement after SET sent %v, want the setting x of SET", reqs[1]["session"])
	}
	if settingsOf(reqs[2])["x"] != nil {
		t.Errorf("the statement after the reset sent %v, want the session of the DSN", reqs[2]["session"])
	}
}

// TestOptionValues holds that WithReadonly fails with dbimp.ErrNotSupported
// (D117), that an option with a value that the DSN refuses fails with
// dbimp.ErrInvalidValue, and that neither sends anything.
func TestOptionValues(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t)
	for _, tt := range []struct {
		opt  databend.Option
		want error
	}{
		{databend.WithReadonly(true), dbimp.ErrNotSupported},
		{databend.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{databend.WithCancel("tag"), dbimp.ErrInvalidValue},
	} {
		if _, err := db.ExecContext(t.Context(), "SELECT 1 AS a", tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("the option gave %v, want %v", err, tt.want)
		}
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
}
