package cosmos_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp/cosmos"
)

// These tests need a server. COSMOS_DSN names the emulator of Cosmos DB, which
// dbrun starts as cosmos-EN20260907 and which the integration tests of CI use
// (D190 item 6). COSMOS_HOSTED_DSN names a hosted account, for the checks that
// the emulator cannot show: the refusal of the gateway for a query across
// partitions, the answer HTTP 429, and the check of the signature (D190 item
// 13). A test skips when the variable that it needs is empty (hard rule 9).
//
// Each variable holds a DSN of the driver, cosmos://x:key@host:port, with the
// master key of the account as the password. The url that dbrun prints for the
// emulator holds the key as the user and the key InsecureSkipVerify, so the
// tests turn it into the DSN of the driver (toDSN). The account has one
// principal, the master key, and no ordinary user (the manifest says why), so
// each test runs as that key only.
//
// The SQL of Cosmos DB has no statement that writes, and the driver reads only
// (D190), so the tests make their database, their containers and their
// documents through the REST API, with cosmos.Raw, and read through the
// driver. The tests make one database for the run, whose name holds a prefix
// of the run, and TestMain deletes it at the end, even when a test failed.

// The variables that hold the DSNs.
const (
	envEmulator = "COSMOS_DSN"
	envHosted   = "COSMOS_HOSTED_DSN"
)

// suffix makes the name of the database of this run unique.
var suffix = strings.ToLower(rand.Text()[:8])

// databaseName is the name of the database of this run.
var databaseName = "dbimp_it_" + suffix

// account is one account that the tests use, with the database of this run.
type account struct {
	cfg      cosmos.Config
	emulator bool
}

var (
	accountsMu sync.Mutex
	accounts   = map[string]*account{}
)

// toDSN returns the DSN of the driver for the value of a variable. The url of
// dbrun holds the key as the user, and the key InsecureSkipVerify, so it
// becomes the key as the password, and the key insecure.
func toDSN(v string) string {
	u, err := url.Parse(v)
	if err != nil {
		return v
	}
	if pw, _ := u.User.Password(); u.User != nil && pw == "" && u.User.Username() != "" {
		u.User = url.UserPassword("x", u.User.Username())
	}
	q := u.Query()
	if v := q.Get("InsecureSkipVerify"); v != "" {
		q.Del("InsecureSkipVerify")
		q.Set("insecure", v)
	}
	u.RawQuery = q.Encode()
	// The path of the url of dbrun is /, and the tests name the database.
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}

// getAccount returns the account of the variable env, and skips the test when
// the variable is empty. It makes the database of the run on the first call.
func getAccount(t *testing.T, env string) *account {
	t.Helper()
	v := os.Getenv(env)
	if v == "" {
		t.Skipf("%s is empty, so there is no account to test against", env)
	}
	accountsMu.Lock()
	defer accountsMu.Unlock()
	if a, ok := accounts[env]; ok {
		return a
	}
	cfg, err := cosmos.ParseDSN(toDSN(v))
	if err != nil {
		t.Fatalf("reading %s: %v", env, err)
	}
	cfg.Database, cfg.Container = "", ""
	a := &account{cfg: *cfg}
	status, hdr, body := a.rest(t, http.MethodGet, "/", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("reading the account of %s: HTTP %d %s", env, status, body)
	}
	a.emulator = hdr.Get("Server") == "PGSQL"
	// The database shares 400 request units a second among its containers, as
	// the run of step 6 did.
	status, _, body = a.rest(t, http.MethodPost, "/dbs", map[string]string{"X-Ms-Offer-Throughput": "400"}, map[string]any{"id": databaseName})
	if status != http.StatusCreated {
		t.Fatalf("making the database %s: HTTP %d %s", databaseName, status, body)
	}
	accounts[env] = a
	return a
}

// TestMain deletes the database of this run on each account that a test used.
func TestMain(m *testing.M) {
	code := m.Run()
	if !cleanup() {
		code = 1
	}
	os.Exit(code)
}

// cleanup deletes the database of this run on each account that a test used,
// and reports whether it could.
func cleanup() bool {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	ok := true
	for env, a := range accounts {
		// TestMain has no context of its own.
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		status, _, body, err := a.raw(ctx, http.MethodDelete, "/dbs/"+databaseName, nil, nil)
		cancel()
		if err != nil || (status != http.StatusNoContent && status != http.StatusNotFound) {
			fmt.Fprintf(os.Stderr, "deleting the database %s of %s: HTTP %d %s: %v\n", databaseName, env, status, body, err)
			ok = false
		}
	}
	return ok
}

// raw sends one request of the REST API. body is nil, a string or bytes, which
// are sent as they are, or a value that is sent as JSON. It sends the request
// again after HTTP 429, as long as the context lasts.
func (a *account) raw(ctx context.Context, method, path string, header map[string]string, body any) (int, http.Header, []byte, error) {
	var b []byte
	switch body := body.(type) {
	case nil:
	case string:
		b = []byte(body)
	case []byte:
		b = body
	default:
		var err error
		if b, err = json.Marshal(body); err != nil {
			return 0, nil, nil, fmt.Errorf("writing the body: %w", err)
		}
	}
	if header == nil {
		header = map[string]string{}
	}
	if len(b) > 0 && header["Content-Type"] == "" {
		header["Content-Type"] = "application/json"
	}
	for {
		status, hdr, res, err := cosmos.Raw(ctx, a.cfg, method, path, header, b)
		if err != nil || status != http.StatusTooManyRequests {
			return status, hdr, res, err
		}
		wait := time.Second
		if ms, err := strconv.Atoi(hdr.Get("X-Ms-Retry-After-Ms")); err == nil && ms > 0 {
			wait = time.Duration(ms) * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return status, hdr, res, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// rest is raw for a test, with a bound on the time, which fails the test for an
// error of the transport.
func (a *account) rest(t *testing.T, method, path string, header map[string]string, body any) (int, http.Header, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	status, hdr, res, err := a.raw(ctx, method, path, header, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return status, hdr, res
}

// cosmosKey limits a statement to one partition key, which a query that the
// gateway refuses across partitions needs.
func cosmosKey(v any) cosmos.Option {
	return cosmos.WithPartitionKey(v)
}

// pk returns the header of a partition key that is the value v.
func pk(v any) map[string]string {
	b, err := json.Marshal([]any{v})
	if err != nil {
		panic(err)
	}
	return map[string]string{"X-Ms-Documentdb-Partitionkey": string(b)}
}

// merge returns the headers of a and of b.
func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}

// collPath returns the path of a container of the database of the run.
func collPath(name string) string {
	return "/dbs/" + databaseName + "/colls/" + url.PathEscape(name)
}

// container makes a container with the partition key /pk, with the members of
// extra added to its definition, and deletes it when the test ends, even when
// it fails. It fails the test if the server refuses the container.
func (a *account) container(t *testing.T, name string, extra map[string]any) {
	t.Helper()
	status, body := a.tryContainer(t, name, extra)
	if status != http.StatusCreated {
		t.Fatalf("making the container %s: HTTP %d %s", name, status, body)
	}
}

// tryContainer is container, and returns the status and the body of the answer
// instead of failing. The container is deleted when the test ends.
func (a *account) tryContainer(t *testing.T, name string, extra map[string]any) (int, []byte) {
	t.Helper()
	def := map[string]any{"id": name, "partitionKey": map[string]any{"paths": []string{"/pk"}, "kind": "Hash"}}
	maps.Copy(def, extra)
	status, _, body := a.rest(t, http.MethodPost, "/dbs/"+databaseName+"/colls", nil, def)
	if status == http.StatusCreated {
		t.Cleanup(func() {
			// The context of a test ends before its cleanup runs.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
			defer cancel()
			if status, _, body, err := a.raw(ctx, http.MethodDelete, collPath(name), nil, nil); err != nil || (status != http.StatusNoContent && status != http.StatusNotFound) {
				t.Errorf("deleting the container %s: HTTP %d %s: %v", name, status, body, err)
			}
		})
	}
	return status, body
}

// put writes the document doc, which has the keys id and pk, with the verb and
// the headers that the call names, and returns the status. A create is a POST,
// and an upsert is a POST with the header of an upsert.
func (a *account) put(t *testing.T, coll string, doc map[string]any, header map[string]string) int {
	t.Helper()
	status, _, body := a.rest(t, http.MethodPost, collPath(coll)+"/docs", merge(pk(doc["pk"]), header), doc)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("writing the document %v of %s: HTTP %d %s", doc["id"], coll, status, body)
	}
	return status
}

// seed writes the documents with batches, one batch for each partition key and
// for each 100 documents, which are the limits of a batch (recorded: "a batch of
// 101 operations").
func (a *account) seed(t *testing.T, coll string, docs []map[string]any) {
	t.Helper()
	byKey := map[any][]map[string]any{}
	var keys []any
	for _, d := range docs {
		if _, ok := byKey[d["pk"]]; !ok {
			keys = append(keys, d["pk"])
		}
		byKey[d["pk"]] = append(byKey[d["pk"]], d)
	}
	for _, key := range keys {
		list := byKey[key]
		for len(list) > 0 {
			n := min(len(list), 100)
			ops := make([]map[string]any, n)
			for i, d := range list[:n] {
				ops[i] = map[string]any{"operationType": "Create", "resourceBody": d}
			}
			list = list[n:]
			status, _, body := a.rest(t, http.MethodPost, collPath(coll)+"/docs", merge(pk(key), map[string]string{
				"X-Ms-Cosmos-Is-Batch-Request": "True",
				"X-Ms-Cosmos-Batch-Atomic":     "True",
			}), ops)
			if status != http.StatusOK {
				t.Fatalf("seeding %s: HTTP %d %s", coll, status, body)
			}
		}
	}
}

// open returns a database on the container coll of the run, as the master key.
func (a *account) open(t *testing.T, coll string) *sql.DB {
	t.Helper()
	cfg := a.cfg
	cfg.Database, cfg.Container = databaseName, coll
	db := sql.OpenDB(cosmos.NewConnector(cfg))
	// Closing the database closes the connector.
	t.Cleanup(func() { db.Close() })
	return db
}

// query runs a statement through the driver, and returns its columns and its
// rows. It runs the statement again after HTTP 429, because a container with a
// small throughput refuses a read that arrives too soon after a burst, and the
// request did not run (recorded: "lead: a burst of reads, number 1").
func query(t *testing.T, db *sql.DB, statement string, args ...any) ([]string, [][]any) {
	t.Helper()
	cols, rows, err := tryQuery(t, db, statement, args...)
	if err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	return cols, rows
}

// tryQuery is query, and returns the error instead of failing.
func tryQuery(t *testing.T, db *sql.DB, statement string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	for {
		cols, rows, err := scanAll(ctx, db, statement, args...)
		cerr, ok := errors.AsType[*cosmos.Error](err)
		if !ok || cerr.HTTPStatus != http.StatusTooManyRequests {
			return cols, rows, err
		}
		wait := max(cerr.RetryAfter, 100*time.Millisecond)
		select {
		case <-ctx.Done():
			return cols, rows, err
		case <-time.After(wait):
		}
	}
}

// scanAll reads every row of a statement into Go values, as Rows.Scan gives
// them to a caller, with a *any for each column.
func scanAll(ctx context.Context, db *sql.DB, statement string, args ...any) ([]string, [][]any, error) {
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("reading the columns: %w", err)
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, fmt.Errorf("scanning: %w", err)
		}
		out = append(out, vals)
	}
	return cols, out, rows.Err()
}

// scalar runs a statement that returns one value, such as one with SELECT
// VALUE, and returns it.
func scalar(t *testing.T, db *sql.DB, statement string, args ...any) any {
	t.Helper()
	cols, rows := query(t, db, statement, args...)
	if len(rows) != 1 || len(cols) != 1 {
		t.Fatalf("%s: columns %q and %d rows, want one value", statement, cols, len(rows))
	}
	return rows[0][0]
}

// ids returns the values of the column id of a statement, in order.
func ids(t *testing.T, db *sql.DB, statement string, args ...any) []string {
	t.Helper()
	cols, rows := query(t, db, statement, args...)
	if len(rows) == 0 {
		return nil
	}
	i := -1
	for j, c := range cols {
		if c == "id" {
			i = j
		}
	}
	if i < 0 {
		t.Fatalf("%s: the columns are %q, and none is id", statement, cols)
	}
	out := make([]string, len(rows))
	for j, r := range rows {
		out[j] = fmt.Sprint(r[i])
	}
	return out
}

// TestIntegrationConnect holds that the login works and that Ping runs. The
// account has no ordinary user to compare with (the manifest says so), and no
// statement returns a version (D190).
func TestIntegrationConnect(t *testing.T) {
	a := getAccount(t, envEmulator)
	db := a.open(t, "unused")
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	// A wrong key is refused by the service, and the emulator does not look at
	// the signature (recorded: "a request that the recorder sends with no
	// signature"), so the check is in TestIntegrationFeatures.
	cfg := a.cfg
	cfg.Key = "AAAA"
	bad := sql.OpenDB(cosmos.NewConnector(cfg))
	defer bad.Close()
	if a.emulator {
		return
	}
	if err := bad.PingContext(t.Context()); err == nil {
		t.Error("a ping with a wrong key worked")
	} else if cerr, ok := errors.AsType[*cosmos.Error](err); !ok || cerr.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("the error is %v, want an *Error of HTTP 401", err)
	}
}

// byName returns the rows with their columns in the order of names, for a
// server that does not keep the order of the statement. The hosted account
// keeps it for a projection, and the emulator sorts the keys by length and then
// by name (recorded: "a projection in the order of the statement"), so a test
// that compares values reads them by the name of the column.
func byName(t *testing.T, cols []string, rows [][]any, names ...string) [][]any {
	t.Helper()
	idx := make([]int, len(names))
	for i, n := range names {
		idx[i] = slices.Index(cols, n)
		if idx[i] < 0 {
			t.Fatalf("the columns are %q, and none is %s", cols, n)
		}
	}
	out := make([][]any, len(rows))
	for r, row := range rows {
		out[r] = make([]any, len(names))
		for i, j := range idx {
			out[r][i] = row[j]
		}
	}
	return out
}
