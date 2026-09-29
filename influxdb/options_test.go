package influxdb_test

import (
	"database/sql"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/influxdb"
)

// sent is a request that the fake server of the option tests received.
type sent struct {
	path  string
	query url.Values
	form  url.Values
	body  map[string]any
}

// optionServer is a fake server for the option tests. It answers InfluxQL,
// SQL, DESCRIBE and a write with an answer of one row, or of none.
type optionServer struct {
	mu   sync.Mutex
	reqs []sent
}

func (s *optionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	req := sent{path: r.URL.Path, query: r.URL.Query()}
	switch r.URL.Path {
	case "/query":
		req.form, _ = url.ParseQuery(string(b))
	case "/api/v3/query_sql":
		_ = json.Unmarshal(b, &req.body)
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	q, _ := req.body["q"].(string)
	switch {
	case r.URL.Path == "/write":
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/query":
		_, _ = io.WriteString(w, `{"results":[{"statement_id":0,"series":[{"name":"m","columns":["a"],"values":[[1]]}]}]}`)
	case strings.HasPrefix(q, "DESCRIBE "):
		_, _ = io.WriteString(w, `[{"column_name":"a","data_type":"Int64","is_nullable":"YES"}]`)
	default:
		_, _ = io.WriteString(w, `[{"a":1}]`)
	}
}

// open returns a database of one connection on the fake server, with the
// keys query in its DSN.
func (s *optionServer) open(t *testing.T, query string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db, err := sql.Open(influxdb.Name, strings.Replace(srv.URL, "http://", "influxdb://u:secret@", 1)+"/dbmeta?"+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	return db
}

// requests returns what the server received, in order, and forgets it.
func (s *optionServer) requests() []sent {
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs := s.reqs
	s.reqs = nil
	return reqs
}

// TestOptionInfluxQL holds that the options of one statement of InfluxQL
// reach the query string and the form of its request, that the other
// arguments keep their order, and that the options do not reach the next
// statement (D109).
func TestOptionInfluxQL(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "sqlmode=disable&version=1")
	var m string
	var a int64
	err := db.QueryRowContext(t.Context(), "SELECT a FROM m WHERE a = $1 OR a = $2", int64(5),
		influxdb.WithDatabase("other"),
		influxdb.WithRetentionPolicy("rp2"),
		influxdb.WithChunked(influxdb.ChunkedDisable),
		influxdb.WithParameter("epoch", "ms"),
		int64(6)).Scan(&m, &a)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT a FROM m").Scan(&m, &a); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("the server received %d requests, want 2", len(reqs))
	}
	first, second := reqs[0], reqs[1]
	switch {
	case first.query.Encode() != "db=other&rp=rp2":
		t.Errorf("the first query string is %s, want db=other&rp=rp2 and no chunked", first.query.Encode())
	case first.form.Get("epoch") != "ms", first.form.Get("params") != `{"1":5,"2":6}`:
		t.Errorf("the first form is %v, want epoch ms and the parameters 1 and 2", first.form)
	}
	if second.query.Encode() != "chunked=true&db=dbmeta" || second.form.Has("epoch") {
		t.Errorf("the second request is %s with %v, want chunked=true&db=dbmeta and no epoch", second.query.Encode(), second.form)
	}
}

// TestOptionSQL holds that the options of one statement of SQL reach the
// body of its request, and that WithDescribe sends DESCRIBE first (D109).
func TestOptionSQL(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "sqlmode=allow&describe=disable")
	var a int64
	err := db.QueryRowContext(t.Context(), "SELECT a FROM m",
		influxdb.WithDatabase("other"),
		influxdb.WithDescribe(influxdb.DescribeAlways),
		influxdb.WithParameter("x", int64(1))).Scan(&a)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT a FROM m").Scan(&a); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range s.requests() {
		name, _ := r.body["db"].(string)
		q, _ := r.body["q"].(string)
		got = append(got, name+" "+q)
		if strings.HasPrefix(got[len(got)-1], "other") && r.body["x"] != 1.0 {
			t.Errorf("the body is %v, want x 1", r.body)
		}
	}
	want := []string{"other DESCRIBE SELECT a FROM m", "other SELECT a FROM m", "dbmeta SELECT a FROM m"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("the requests were %v, want %v", got, want)
	}
}

// TestOptionInsert holds that WithDatabase and WithRetentionPolicy name
// where an INSERT without INTO writes, and that INTO wins (D109).
func TestOptionInsert(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "sqlmode=disable&version=1")
	for _, q := range []string{"INSERT m v=1i", "INSERT INTO into.irp m v=1i"} {
		if _, err := db.ExecContext(t.Context(), q, influxdb.WithDatabase("other"), influxdb.WithRetentionPolicy("rp2")); err != nil {
			t.Fatal(err)
		}
	}
	reqs := s.requests()
	if len(reqs) != 2 || reqs[0].query.Encode() != "db=other&rp=rp2" || reqs[1].query.Encode() != "db=into&rp=irp" {
		t.Errorf("the writes were %v, want db=other&rp=rp2, then db=into&rp=irp", reqs)
	}
}

// TestOptionValues holds that WithTimeout and WithReadonly fail with
// dbimp.ErrNotSupported (D109), that an option with a value that the DSN
// refuses fails with dbimp.ErrInvalidValue, and that neither sends anything.
func TestOptionValues(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "sqlmode=disable&version=1")
	for _, tt := range []struct {
		opt  influxdb.Option
		want error
	}{
		{influxdb.WithTimeout(time.Second), dbimp.ErrNotSupported},
		{influxdb.WithReadonly(true), dbimp.ErrNotSupported},
		{influxdb.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{influxdb.WithChunked("always"), dbimp.ErrInvalidValue},
		{influxdb.WithDescribe("never"), dbimp.ErrInvalidValue},
	} {
		if _, err := db.ExecContext(t.Context(), "SHOW DATABASES", tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("the option gave %v, want %v", err, tt.want)
		}
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
}
