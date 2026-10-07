package dynamodb_test

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dynamodb"
)

// one is a page of one item.
var one = map[string]page{"": {body: `{"Items":[{"a":{"N":"1"}}]}`}}

// TestRequest holds what the driver sends: the target, the content type, a
// signature of the access key and no secret key, and the body of D169.
func TestRequest(t *testing.T) {
	t.Parallel()
	f := &fake{pages: one}
	db := f.open(t)
	rows, err := db.QueryContext(t.Context(), "SELECT a FROM t WHERE pk = ? AND n = ?", "x", int64(7))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ids(rows); err != nil {
		t.Fatal(err)
	}
	h := f.headers[0]
	if h.Get("X-Amz-Target") != "DynamoDB_20120810.ExecuteStatement" || h.Get("Content-Type") != "application/x-amz-json-1.0" {
		t.Errorf("headers = %v", h)
	}
	auth := h.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=key/") || !strings.Contains(auth, "/us-east-1/dynamodb/aws4_request") || strings.Contains(auth, "secret") {
		t.Errorf("Authorization = %q", auth)
	}
	b := f.bodies[0]
	params, _ := b["Parameters"].([]any)
	if b["Statement"] != "SELECT a FROM t WHERE pk = ? AND n = ?" || len(params) != 2 {
		t.Errorf("body = %v", b)
	}
	if _, ok := b["NextToken"]; ok {
		t.Errorf("body = %v, want no NextToken on the first page", b)
	}
}

// TestNoParameters holds that a statement with no argument sends no
// Parameters, which the server takes as an empty list.
func TestNoParameters(t *testing.T) {
	t.Parallel()
	f := &fake{pages: one}
	db := f.open(t)
	rows, err := db.QueryContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ids(rows); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.bodies[0]["Parameters"]; ok {
		t.Errorf("body = %v, want no Parameters", f.bodies[0])
	}
}

// TestArgumentsAreCounted holds D169: the driver sends nothing for a count of
// arguments that is not the count of the placeholders.
func TestArgumentsAreCounted(t *testing.T) {
	t.Parallel()
	f := &fake{pages: one}
	db := f.open(t)
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t WHERE pk = ?", "x", "y"); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("too many arguments: %v, want dbimp.ErrArguments", err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t WHERE pk = ?"); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("too few arguments: %v, want dbimp.ErrArguments", err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t WHERE pk = ?", sql.Named("p", "x")); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a named argument: %v, want dbimp.ErrNotSupported", err)
	}
	if len(f.bodies) != 0 {
		t.Errorf("the driver sent %d requests, want none", len(f.bodies))
	}
}

// TestOptions holds D109: an option comes from the context, then from an
// argument, and a later one wins. An option that the server cannot honor
// fails, and sends nothing.
func TestOptions(t *testing.T) {
	t.Parallel()
	f := &fake{pages: one}
	db := f.open(t)
	ctx := dynamodb.WithOptions(t.Context(), dynamodb.WithParameter("Limit", 5), dynamodb.WithParameter("ConsistentRead", false))
	rows, err := db.QueryContext(ctx, "SELECT a FROM t WHERE pk = ?", dynamodb.WithParameter("ConsistentRead", true), "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ids(rows); err != nil {
		t.Fatal(err)
	}
	b := f.bodies[0]
	if b["Limit"] != float64(5) || b["ConsistentRead"] != true {
		t.Errorf("body = %v, want Limit 5 and ConsistentRead true", b)
	}
	if params, _ := b["Parameters"].([]any); len(params) != 1 {
		t.Errorf("body = %v, want the option taken out of the arguments, and one parameter", b)
	}
	// A key of WithParameter replaces a key of the driver, as in Couchbase.
	rows, err = db.QueryContext(t.Context(), "SELECT a FROM t", dynamodb.WithParameter("Statement", "SELECT a FROM u"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ids(rows); err != nil {
		t.Fatal(err)
	}
	if f.bodies[1]["Statement"] != "SELECT a FROM u" {
		t.Errorf("body = %v, want the statement of the option", f.bodies[1])
	}
	n := len(f.bodies)
	for _, tt := range []struct {
		name string
		opt  dynamodb.Option
		want error
	}{
		{"a timeout", dynamodb.WithTimeout(time.Second), dbimp.ErrNotSupported},
		{"a negative timeout", dynamodb.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"read only", dynamodb.WithReadonly(true), dbimp.ErrNotSupported},
		{"a database", dynamodb.WithDatabase("d"), dbimp.ErrNotSupported},
	} {
		if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, err, tt.want)
		}
	}
	for _, opt := range []dynamodb.Option{dynamodb.WithTimeout(0), dynamodb.WithReadonly(false), dynamodb.WithDatabase("")} {
		rows, err := db.QueryContext(t.Context(), "SELECT a FROM t", opt)
		if err != nil {
			t.Errorf("an option that asks for nothing: %v", err)
			continue
		}
		if _, err := ids(rows); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if len(f.bodies) != n {
		t.Errorf("the driver sent %d requests, want %d", len(f.bodies), n)
	}
}

// TestNoRedirect holds D169: the driver follows no redirect, so it sends its
// signature to the host of the DSN only.
func TestNoRedirect(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(other.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(dynamodb.Name, strings.Replace(srv.URL, "http://", "dynamodb://key:secret@", 1)+"?region=us-east-1&tls=false")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t"); err == nil {
		t.Error("a redirect gave no error")
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("the other host got %d requests, want none", n)
	}
}

// TestPing holds that Ping sends ListTables, which the server answers with
// JSON, and that it returns the error of the server.
func TestPing(t *testing.T) {
	t.Parallel()
	var target atomic.Value
	status := atomic.Int32{}
	status.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target.Store(r.Header.Get("X-Amz-Target"))
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		w.WriteHeader(int(status.Load()))
		if status.Load() == http.StatusOK {
			_, _ = w.Write([]byte(`{"TableNames":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"__type":"com.amazon.coral.service#UnrecognizedClientException","message":"The security token included in the request is invalid."}`))
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(dynamodb.Name, strings.Replace(srv.URL, "http://", "dynamodb://key:secret@", 1)+"?region=us-east-1&tls=false")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := target.Load(); got != "DynamoDB_20120810.ListTables" {
		t.Errorf("Ping sent %v, want ListTables", got)
	}
	status.Store(http.StatusBadRequest)
	err = db.PingContext(t.Context())
	var derr *dynamodb.Error
	if !errors.As(err, &derr) || derr.Type != "UnrecognizedClientException" || derr.HTTPStatus != http.StatusBadRequest {
		t.Errorf("Ping = %v, want the UnrecognizedClientException", err)
	}
}

// TestError holds the fields of an Error, from the answers that step 6
// recorded.
func TestError(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		status int
		body   string
		typ    string
		msg    string
		str    string
	}{
		{"a syntax error", 400, `{"__type":"com.amazon.coral.validate#ValidationException","Message":"Statement wasn't well formed, can't be processed: SELEC"}`, "ValidationException", "Statement wasn't well formed, can't be processed: SELEC", "dynamodb: ValidationException: Statement wasn't well formed, can't be processed: SELEC"},
		{"an unknown table", 400, `{"__type":"com.amazonaws.dynamodb.v20120810#ResourceNotFoundException","Message":"Cannot do operations on a non-existent table"}`, "ResourceNotFoundException", "Cannot do operations on a non-existent table", ""},
		{"a message in lower case", 400, `{"__type":"com.amazonaws.dynamodb.v20120810#UnknownOperationException","message":"Unsupported operation ExecuteStatement"}`, "UnknownOperationException", "Unsupported operation ExecuteStatement", ""},
		{"an internal failure", 500, `{"__type":"com.amazon.coral.service#InternalFailure"}`, "InternalFailure", "Internal Server Error", "dynamodb: InternalFailure: Internal Server Error"},
		{"a type with no namespace", 400, `{"__type":"Boom","Message":"m"}`, "Boom", "m", ""},
		{"text", 503, "slow down", "", "slow down", "dynamodb: 503: slow down"},
		{"a page of HTML", 502, "<html>bad</html>", "", "Bad Gateway", "dynamodb: 502: Bad Gateway"},
		{"no body", 429, "", "", "Too Many Requests", "dynamodb: 429: Too Many Requests"},
	} {
		f := &fake{pages: map[string]page{"": {status: tt.status, body: tt.body}}}
		db := f.open(t)
		_, err := db.ExecContext(t.Context(), "SELECT a FROM t")
		var derr *dynamodb.Error
		if !errors.As(err, &derr) {
			t.Errorf("%s: %v, want a *dynamodb.Error", tt.name, err)
			continue
		}
		if derr.HTTPStatus != tt.status || derr.Type != tt.typ || derr.Message != tt.msg {
			t.Errorf("%s: %+v, want status %d, type %q and message %q", tt.name, *derr, tt.status, tt.typ, tt.msg)
		}
		if tt.str != "" && derr.Error() != tt.str {
			t.Errorf("%s: Error() = %q, want %q", tt.name, derr.Error(), tt.str)
		}
		var serr *dbimp.StatusError
		if !errors.As(err, &serr) || serr.Code != tt.status {
			t.Errorf("%s: the error does not wrap the *dbimp.StatusError", tt.name)
		}
		if len(f.bodies) != 1 {
			t.Errorf("%s: the driver sent %d requests, want 1: it never sends a request again (D8)", tt.name, len(f.bodies))
		}
	}
}

// TestNotJSON holds that an answer that is not JSON is an error.
func TestNotJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(dynamodb.Name, strings.Replace(srv.URL, "http://", "dynamodb://key:secret@", 1)+"?region=us-east-1&tls=false")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t"); err == nil || !strings.Contains(err.Error(), "text/html") {
		t.Errorf("QueryContext = %v, want an error that names the content type", err)
	}
}

// TestNoTransactionNoCount holds D169: BeginTx fails, and an Exec result has no
// count, because the server gives none.
func TestNoTransactionNoCount(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{"": {body: `{"Items":[]}`}}}
	db := f.open(t)
	if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx = %v, want dbimp.ErrNotSupported", err)
	}
	res, err := db.ExecContext(t.Context(), "UPDATE t SET a = 1 WHERE pk = 'x'")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected = %v, want dbimp.ErrNotSupported", err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId = %v, want dbimp.ErrNotSupported", err)
	}
	// A prepared statement runs as its text.
	st, err := db.PrepareContext(t.Context(), "SELECT a FROM t WHERE pk = ?")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.QueryContext(t.Context(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ids(rows); err != nil {
		t.Fatal(err)
	}
}

// TestConnectorCloses holds that Close of the connector releases the idle
// connections, and that Open without a context is refused.
func TestConnectorCloses(t *testing.T) {
	t.Parallel()
	c, err := dynamodb.Driver{}.OpenConnector("dynamodb://key:secret@localhost?region=us-east-1")
	if err != nil {
		t.Fatal(err)
	}
	if cl, ok := c.(interface{ Close() error }); !ok || cl.Close() != nil {
		t.Errorf("the connector %T does not close", c)
	}
	if _, err := (dynamodb.Driver{}).Open("dynamodb://key:secret@localhost?region=us-east-1"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("Open = %v, want dbimp.ErrNotSupported", err)
	}
	if _, err := (dynamodb.Driver{}).OpenConnector("mysql://x"); !errors.Is(err, dbimp.ErrScheme) {
		t.Errorf("OpenConnector of another scheme = %v, want dbimp.ErrScheme", err)
	}
}
