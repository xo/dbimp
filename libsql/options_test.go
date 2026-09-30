package libsql_test

import (
	"context"
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
	"github.com/xo/dbimp/libsql"
)

// fakeServer is a fake sqld. It keeps each request, answers a cursor with
// one row and the baton "b1", and answers a pipeline with an ok result for
// each request, and the baton "b2" unless the last request closes the
// stream.
type fakeServer struct {
	mu   sync.Mutex
	reqs []fakeRequest
	// expire answers each pipeline that sends a baton with STREAM_EXPIRED.
	expire bool
	// baseURL is the base_url of each answer.
	baseURL string
}

// fakeRequest is one request that the fake server received.
type fakeRequest struct {
	path, auth, namespace string
	body                  map[string]any
}

func (s *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	s.mu.Lock()
	s.reqs = append(s.reqs, fakeRequest{path: r.URL.Path, auth: r.Header.Get("Authorization"), namespace: r.Header.Get("X-Namespace"), body: body})
	expire, base := s.expire, s.baseURL
	s.mu.Unlock()
	baseJSON := "null"
	if base != "" {
		baseJSON = `"` + base + `"`
	}
	if r.URL.Path == "/v3/cursor" {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, `{"baton":"b1","base_url":`+baseJSON+"}\n"+strings.TrimPrefix(head("a"), "{\"baton\":null,\"base_url\":null}\n")+row("1")+tail)
		return
	}
	if expire && body["baton"] != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"The stream has expired due to inactivity","code":"STREAM_EXPIRED"}`)
		return
	}
	reqs, _ := body["requests"].([]any)
	results := make([]string, len(reqs))
	baton := `"b2"`
	for i, q := range reqs {
		m, _ := q.(map[string]any)
		switch m["type"] {
		case "close":
			results[i] = `{"type":"ok","response":{"type":"close"}}`
			baton = "null"
		default:
			results[i] = `{"type":"ok","response":{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":2,"last_insert_rowid":"7"}}}`
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"baton":`+baton+`,"base_url":`+baseJSON+`,"results":[`+strings.Join(results, ",")+`]}`)
}

// open returns a database on the fake server, with the user information
// and the query of a DSN.
func (s *fakeServer) open(t *testing.T, user, query string) (*sql.DB, string) {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	sep := "?"
	if strings.Contains(query, "?") {
		sep = "&"
	}
	db, err := sql.Open(libsql.Name, strings.Replace(srv.URL, "http://", "libsql://"+user, 1)+query+sep+"tls=false")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, srv.URL
}

// take returns the requests that the server received, and forgets them.
func (s *fakeServer) take() []fakeRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.reqs
	s.reqs = nil
	return r
}

// querier runs a query, as a *sql.DB and a *sql.Tx do.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// query runs SELECT 1 with args, and reads it to its end.
func query(ctx context.Context, t *testing.T, db querier, args ...any) error {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT 1", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// TestEndpoints holds D149: a query reads the cursor and then closes its
// stream on the pipeline, and Exec sends the statement and a close in one
// request. The token goes as Bearer (D148).
func TestEndpoints(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db, _ := s.open(t, "admin:tok@", "")
	if err := query(t.Context(), t, db); err != nil {
		t.Fatal(err)
	}
	reqs := s.take()
	if len(reqs) != 2 || reqs[0].path != "/v3/cursor" || reqs[1].path != "/v3/pipeline" || reqs[1].body["baton"] != "b1" {
		t.Fatalf("a query sent %+v, want the cursor, then a close with the baton b1", reqs)
	}
	for _, r := range reqs {
		if r.auth != "Bearer tok" {
			t.Errorf("%s sent Authorization %q, want Bearer tok", r.path, r.auth)
		}
	}
	res, err := db.ExecContext(t.Context(), "INSERT INTO t VALUES (?)", 1)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := res.RowsAffected()
	id, _ := res.LastInsertId()
	reqs = s.take()
	if len(reqs) != 1 || reqs[0].path != "/v3/pipeline" || n != 2 || id != 7 {
		t.Fatalf("Exec sent %+v and gave %d and %d, want one pipeline, 2 and 7", reqs, n, id)
	}
	sent, _ := json.Marshal(reqs[0].body["requests"], json.Deterministic(true))
	if want := `[{"stmt":{"args":[{"type":"integer","value":"1"}],"sql":"INSERT INTO t VALUES (?)"},"type":"execute"},{"type":"close"}]`; string(sent) != want {
		t.Errorf("Exec sent %s, want %s", sent, want)
	}
}

// TestBasicAuth holds D148: auth=basic sends the user and the password.
func TestBasicAuth(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db, _ := s.open(t, "u:p@", "?auth=basic")
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r := s.take()[0]; r.auth != "Basic dTpw" {
		t.Errorf("sent Authorization %q, want basic u:p", r.auth)
	}
}

// TestNamespace holds D148 and D109: the key namespace and WithNamespace send
// x-namespace, and WithDatabase does the same.
func TestNamespace(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db, _ := s.open(t, "", "?namespace=n1")
	for _, tt := range []struct {
		args []any
		want string
	}{
		{nil, "n1"},
		{[]any{libsql.WithNamespace("n2")}, "n2"},
		{[]any{libsql.WithDatabase("n3")}, "n3"},
	} {
		if err := query(t.Context(), t, db, tt.args...); err != nil {
			t.Fatal(err)
		}
		for _, r := range s.take() {
			if r.namespace != tt.want {
				t.Errorf("%s sent x-namespace %q, want %q", r.path, r.namespace, tt.want)
			}
		}
	}
}

// TestOptionsRefused holds D109: an option that the server cannot honor, or
// whose value the DSN would refuse, fails and sends nothing.
func TestOptionsRefused(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db, _ := s.open(t, "", "")
	for _, tt := range []struct {
		opt  libsql.Option
		want error
	}{
		{libsql.WithReadonly(true), dbimp.ErrNotSupported},
		{libsql.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
	} {
		if err := query(t.Context(), t, db, tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("the option gave %v, want %v", err, tt.want)
		}
	}
	if n := len(s.take()); n != 0 {
		t.Errorf("the server received %d requests, want none", n)
	}
	if err := query(t.Context(), t, db, libsql.WithReadonly(false), libsql.WithParameter("want_rows", true)); err != nil {
		t.Fatal(err)
	}
	stmt := s.take()[0].body["batch"].(map[string]any)["steps"].([]any)[0].(map[string]any)["stmt"].(map[string]any) //nolint:forcetypeassert // The fake server read the body.
	if stmt["want_rows"] != true {
		t.Errorf("WithParameter sent %v, want want_rows", stmt)
	}
}

// TestTransaction holds D150: BeginTx sends BEGIN on a new stream, each
// statement sends the baton of the last answer, and Commit sends COMMIT and a
// close.
func TestTransaction(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db, _ := s.open(t, "", "")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO t VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	if err := query(t.Context(), t, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range s.take() {
		b, _ := r.body["baton"].(string)
		got = append(got, r.path+" "+b)
	}
	want := []string{"/v3/pipeline ", "/v3/pipeline b2", "/v3/cursor b2", "/v3/pipeline b1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the transaction sent %q, want %q", got, want)
	}
	for _, opts := range []*sql.TxOptions{{ReadOnly: true}, {Isolation: sql.LevelSerializable}} {
		if _, err := db.BeginTx(t.Context(), opts); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("BeginTx(%+v) gave %v, want %v", opts, err, dbimp.ErrNotSupported)
		}
	}
}

// TestTransactionExpired holds D150: an expired stream is an error that says
// that the server rolled back the transaction, and never driver.ErrBadConn.
func TestTransactionExpired(t *testing.T) {
	t.Parallel()
	s := &fakeServer{expire: true}
	db, _ := s.open(t, "", "")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(t.Context(), "INSERT INTO t VALUES (1)")
	if e, ok := errors.AsType[*libsql.Error](err); !ok || e.Code != libsql.CodeStreamExpired || !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("a statement of an expired stream gave %v, want STREAM_EXPIRED and the rollback", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Errorf("Rollback of an expired stream gave %v, want nil", err)
	}
	if len(s.take()) != 2 {
		t.Error("Rollback of an expired stream sent a request")
	}
}

// TestBaseURL holds D151: a base_url on the host of the DSN is followed, and
// one on another host is an error.
func TestBaseURL(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db, u := s.open(t, "", "")
	s.baseURL = u + "/"
	if err := query(t.Context(), t, db); err != nil {
		t.Errorf("a base_url on the host of the DSN gave %v", err)
	}
	s.baseURL = "http://other.example:9/"
	if err := query(t.Context(), t, db); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a base_url on another host gave %v, want %v", err, dbimp.ErrNotSupported)
	}
}

// TestCursorOfAnExpiredStream holds D149: a cursor whose stream expired
// while its rows were read ends with no error, because the stream is closed
// already (measured in CI).
func TestCursorOfAnExpiredStream(t *testing.T) {
	t.Parallel()
	s := &fakeServer{expire: true}
	db, _ := s.open(t, "", "")
	if err := query(t.Context(), t, db); err != nil {
		t.Errorf("a cursor whose close found the stream expired gave %v, want nil", err)
	}
	if reqs := s.take(); len(reqs) != 2 || reqs[1].body["baton"] != "b1" {
		t.Errorf("the query sent %+v, want the cursor and a close", reqs)
	}
}
