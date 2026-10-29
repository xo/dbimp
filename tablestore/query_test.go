package tablestore //nolint:testpackage // The tests read the headers and the fields that the connector builds.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// TestSignature holds the signature of a request. The expected text comes from
// an HMAC with SHA-1 that Python computed from the text of the headers, with the
// body of the first recorded request, whose md5 header is the one that the
// recording holds (recorded: "select 1").
func TestSignature(t *testing.T) {
	t.Parallel()
	body := []byte("\n\x18SELECT 1 AS a, 'x' AS b;\x10\x02\x18\x01")
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://dbmeta.ap-southeast-5.ots.aliyuncs.com/SQLQuery", nil)
	if err != nil {
		t.Fatal(err)
	}
	sign(req, body, "key", "secret", "dbmeta", time.Date(2026, 10, 29, 7, 52, 17, 0, time.UTC))
	for name, want := range map[string]string{
		"X-Ots-Contentmd5":   "EAnmhjlCLmlmZ4C6D2GU5A==",
		"X-Ots-Date":         "2026-10-29T07:52:17.000Z",
		"X-Ots-Apiversion":   "2015-12-31",
		"X-Ots-Accesskeyid":  "key",
		"X-Ots-Instancename": "dbmeta",
		"X-Ots-Signature":    "y+K9Pb9JhZ/HXDN3U3a+HlAGqss=",
	} {
		if got := req.Header.Get(name); got != want {
			t.Errorf("the header %s is %q, want %q", name, got, want)
		}
	}
}

// capture is a fake server that keeps the last request and answers with the
// answer to a statement that holds no rows.
type capture struct {
	req  *http.Request
	body []byte
	hits atomic.Int32
}

func (c *capture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.hits.Add(1)
	c.req = r
	c.body, _ = io.ReadAll(r.Body)
	_, _ = w.Write(answerTail(nil, 5, 0))
}

// TestRequest holds what a statement sends: the path, the headers, and the three
// fields of the message, with the arguments written into the text.
func TestRequest(t *testing.T) {
	t.Parallel()
	c := &capture{}
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	db := open(t, testConfig(), srv.URL)
	if _, err := db.ExecContext(t.Context(), "UPDATE t SET s = ? WHERE id = ? AND note = '?' -- ?", "it's", int64(7)); err != nil {
		t.Fatal(err)
	}
	if c.req.Method != http.MethodPost || c.req.URL.Path != "/SQLQuery" {
		t.Errorf("the request is %s %s, want POST /SQLQuery", c.req.Method, c.req.URL.Path)
	}
	if got := c.req.Header.Get("Content-Type"); got != "application/x-protobuf" {
		t.Errorf("the content type is %q", got)
	}
	if got := c.req.Header.Get("X-Ots-Instancename"); got != "dbmeta" {
		t.Errorf("the instance is %q, want dbmeta", got)
	}
	if c.req.Header.Get("X-Ots-Signature") == "" || strings.Contains(c.req.Header.Get("X-Ots-Signature"), "secret") {
		t.Error("the signature is missing, or holds the secret")
	}
	if c.req.Header.Get("Authorization") != "" {
		t.Error("the request has an Authorization header, which Tablestore does not read")
	}
	want := appendString(nil, fieldQuery, "UPDATE t SET s = 'it''s' WHERE id = 7 AND note = '?' -- ?")
	want = appendInt(want, fieldVersion, 2)
	want = appendInt(want, fieldSQLVersion, 1)
	if string(c.body) != string(want) {
		t.Errorf("the body is %q, want %q", c.body, want)
	}
}

// TestPing sends ListTable.
func TestPing(t *testing.T) {
	t.Parallel()
	c := &capture{}
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	db := open(t, testConfig(), srv.URL)
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.req.URL.Path != "/ListTable" || len(c.body) != 0 {
		t.Errorf("Ping sent %s with %d bytes, want ListTable with none", c.req.URL.Path, len(c.body))
	}
}

// TestNoCredentials holds that a connector with no key opens no connection, and
// that the driver reads no key from the environment (D7).
func TestNoCredentials(t *testing.T) {
	t.Setenv("TABLESTORE_ACCESS_KEY_ID", "key")
	t.Setenv("ALIBABA_CLOUD_ACCESS_KEY_ID", "key")
	t.Setenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "secret")
	cfg := testConfig()
	cfg.User, cfg.Password = "", ""
	c := NewConnector(cfg)
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Connect(t.Context()); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("Connect gave %v, want ErrNoCredentials", err)
	}
}

// TestCancel holds that a context that ends stops a statement that the server
// still runs, and that the error is the error of the context, and never
// driver.ErrBadConn (D36).
func TestCancel(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// The server sees that the client left only after it has read the body.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	db := open(t, testConfig(), srv.URL)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-started
		cancel()
	}()
	_, err := db.ExecContext(ctx, "SELECT 1")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	if errors.Is(err, driver.ErrBadConn) {
		t.Error("a cancelled statement is a bad connection")
	}
}

// TestIncomplete holds that an error after a row wraps dbimp.ErrIncomplete, and
// that an error before the first row does not (D107).
func TestIncomplete(t *testing.T) {
	t.Parallel()
	strs := []string{"a", "b", "c", "d"}
	msg := buildRows([]tcol{{name: "s", data: dataString, strs: strs}}, 4)
	prefix := appendVarint(appendVarint(nil, fieldRows<<3|wireBytes), uint64(len(msg)))
	for _, tt := range []struct {
		name       string
		body       []byte
		rows       int
		incomplete bool
	}{
		{"cut after two rows", append(prefix, msg[:len(msg)-2*8]...), 2, true},
		{"cut before the first row", append(prefix, msg[:len(msg)-4*8]...), 0, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(tt.body) }))
		db := open(t, testConfig(), srv.URL)
		n, err := countRows(t.Context(), t, db, "SELECT s FROM t")
		srv.Close()
		if err == nil || n != tt.rows {
			t.Errorf("%s: read %d rows and the error %v, want %d rows and an error", tt.name, n, err, tt.rows)
		}
		if got := errors.Is(err, dbimp.ErrIncomplete); got != tt.incomplete {
			t.Errorf("%s: the error matches dbimp.ErrIncomplete is %t, want %t", tt.name, got, tt.incomplete)
		}
	}
}

// TestRowsAffectedUnknown holds that an answer with no count of rows gives an
// error that wraps dbimp.ErrNotSupported, and that a count of 0 is a value (D178).
func TestRowsAffectedUnknown(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		body  []byte
		known bool
	}{
		{"no count", appendInt(nil, fieldStatement, 5), false},
		{"a count of 0", answerTail(nil, 5, 0), true},
		{"a count of 3", answerTail(nil, 8, 3), true},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(tt.body) }))
		db := open(t, testConfig(), srv.URL)
		res, err := db.ExecContext(t.Context(), "DROP MAPPING TABLE t")
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		_, err = res.RowsAffected()
		if tt.known != (err == nil) || (!tt.known && !errors.Is(err, dbimp.ErrNotSupported)) {
			t.Errorf("%s: RowsAffected gave %v, want a count: %t", tt.name, err, tt.known)
		}
		srv.Close()
	}
}

// TestPayloadVersion holds that an answer of the plain buffer payload, which the
// driver never asks for, is an error and not wrong values (D223).
func TestPayloadVersion(t *testing.T) {
	t.Parallel()
	msg := buildRows([]tcol{{name: "a", data: dataLong, longs: []int64{1}}}, 1)
	body := appendBytes(nil, fieldRows, msg)
	body = appendInt(body, fieldPayload, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(srv.Close)
	db := open(t, testConfig(), srv.URL)
	if _, err := countRows(t.Context(), t, db, "SELECT a FROM t"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("the error is %v, want dbimp.ErrNotSupported", err)
	}
}

// TestErrorHidesTheSecret holds that an error of a failed request holds no secret
// and no access key id (D94).
func TestErrorHidesTheSecret(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	cfg := testConfig()
	cfg.User, cfg.Password = "AKIDexample", "S3cretValue"
	db := open(t, cfg, url)
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	if err == nil || strings.Contains(err.Error(), "S3cretValue") || strings.Contains(err.Error(), "AKIDexample") {
		t.Errorf("the error is %v, which holds a secret or is nil", err)
	}
}

// TestRecordedStatements runs the replay of every recorded statement that the
// server accepted (a 200), without error, to make sure that the driver reads
// every recorded answer. A recorded answer that the driver cannot read fails
// here, and the others are in replay_test.go.
func TestRecordedStatements(t *testing.T) {
	t.Parallel()
	srv := dbimptest.Replay(t, testdata, nil)
	db := open(t, testConfig(), srv.URL)
	for _, q := range []string{
		"SELECT * FROM dbimp_it_dt_int;",
		"SELECT 9223372036854775807 AS a, -9223372036854775808 AS b;",
		"SELECT 9223372036854775808 AS a;",
		"SELECT 1 / 0 AS a;",
		"SELECT i + 0 AS a FROM dbimp_it_t WHERE id = 3;",
		"SELECT CONCAT('', i) AS a FROM dbimp_it_t WHERE id = 3;",
		"SELECT CURRENT_TIMESTAMP AS a;",
		"SELECT FROM_UNIXTIME(i) AS a FROM dbimp_it_t WHERE id = 3;",
		"SELECT DATE_FORMAT(FROM_UNIXTIME(i), '%Y') AS a FROM dbimp_it_t WHERE id = 3;",
		"SELECT id FROM dbimp_it_t ORDER BY id LIMIT 1, 2;",
		"SELECT COUNT(*) AS n FROM dbimp_it_big;",
		"SELECT id FROM dbimp_it_big WHERE id < 1000 ORDER BY id;",
		"SELECT id FROM dbimp_it_big LIMIT 20000;",
		"SHOW INDEX FROM dbimp_it_t;",
	} {
		if _, err := countRows(t.Context(), t, db, q); err != nil {
			t.Errorf("%s: %v", q, err)
		}
	}
}

// countRows runs the query, reads every row, and returns the count of the rows
// and the error of the statement or of the rows.
func countRows(ctx context.Context, t *testing.T, db *sql.DB, query string) (int, error) {
	t.Helper()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return 0, err //nolint:wrapcheck // The tests read the error as database/sql gives it.
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n, rows.Err() //nolint:wrapcheck // The tests read the error as database/sql gives it.
}
