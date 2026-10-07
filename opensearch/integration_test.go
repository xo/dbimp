package opensearch_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/opensearch"
)

// These tests need a server. OPENSEARCH_DSN names the server for the
// administrator, and OPENSEARCH_ORDINARY_DSN for the ordinary user (D9). Each
// is the url of dbrun, which is opensearch://user:pass@host:port, and the tests
// read it as the DSN of the driver. SQL in OpenSearch takes no write (D163), so
// the tests write their indices through the document API as the administrator,
// and read them through the driver as each principal. The ordinary user of the
// dbmeta entry can read the indices whose names start with dbmeta (measured), so
// every index of the tests starts with a prefix of its own that starts with
// dbmeta, which TestMain removes at the end. A test skips when a variable that
// it needs is empty.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each index of this run. It starts with dbmeta so that the
// ordinary user can read it.
var prefix = "dbmeta_it_" + suffix + "_"

// secretPrefix starts the index that the ordinary user cannot read, because its
// name does not start with dbmeta.
var secretPrefix = "dbimp_it_" + suffix + "_"

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "OPENSEARCH_DSN"}
	ordinary   = principal{"ordinary", "OPENSEARCH_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// toDSN returns the URL of dbrun as the DSN of the driver. A URL whose scheme
// is already opensearch stays as it is.
func toDSN(v string) string {
	switch {
	case strings.HasPrefix(v, "http://"):
		return "opensearch://" + strings.TrimPrefix(v, "http://")
	case strings.HasPrefix(v, "https://"):
		return "opensearch://" + strings.TrimPrefix(v, "https://") + "?tls=true"
	}
	return v
}

// dsn returns the DSN of p, or skips the test when it is empty.
func dsn(t *testing.T, p principal) string {
	t.Helper()
	v := os.Getenv(p.env)
	if v == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	return toDSN(v)
}

// target is a database that a test reads as one principal.
type target struct {
	p  principal
	db *sql.DB
	// old is true for a release of the 2 series.
	old bool
}

// openAs opens the server as p, with the query of a DSN.
func openAs(t *testing.T, p principal, query string) *sql.DB {
	t.Helper()
	db, err := sql.Open(opensearch.Name, dsn(t, p)+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// forEach runs f as a subtest for each principal.
func forEach(t *testing.T, f func(t *testing.T, e *target)) {
	t.Helper()
	old := serverIsOld(t)
	for _, p := range principals {
		t.Run(p.name, func(t *testing.T) {
			f(t, &target{p: p, db: openAs(t, p, ""), old: old})
		})
	}
}

// cannotPage is true when the principal cannot read a cursor: on 2.19.6 a
// cursor opens a point in time, whose search names no index, and the grant of
// the ordinary user on the indices dbmeta* does not cover it (recorded as
// dbmeta_user: "a cursor as the ordinary user").
func (e *target) cannotPage() bool {
	return e.old && e.p == ordinary
}

// ctx returns the context of a statement of e. For a principal that cannot read
// a cursor, it turns the page size off, which WithParameter does by replacing
// the key fetch_size, and a result is then cut at the size limit of the server.
func (e *target) ctx(t *testing.T) context.Context {
	t.Helper()
	if e.cannotPage() {
		return opensearch.WithOptions(t.Context(), opensearch.WithParameter("fetch_size", 0))
	}
	return t.Context()
}

// read runs query as e, and reads its columns and every row into *any.
func (e *target) read(t *testing.T, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	return readAllContext(t, e.ctx(t), e.db, query, args...)
}

// columns runs query as e, and returns the names and the types of its columns.
func (e *target) columns(t *testing.T, query string, args ...any) ([]string, []*sql.ColumnType, error) {
	t.Helper()
	r, err := e.db.QueryContext(e.ctx(t), query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	cols, err := r.Columns()
	if err != nil {
		return nil, nil, err
	}
	cts, err := r.ColumnTypes()
	if err != nil {
		return nil, nil, err
	}
	return cols, cts, r.Err()
}

// skipCursor skips the test of a cursor for a principal that cannot read one.
func (e *target) skipCursor(t *testing.T) {
	t.Helper()
	if e.cannotPage() {
		t.Skip("on the 2 series the ordinary user cannot read a cursor: it needs indices:data/read/search on every index (recorded: a cursor as the ordinary user)")
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, "removing the indices of the tests:", err)
		code = 1
	}
	os.Exit(code)
}

// cleanup removes each index of this run.
func cleanup() error {
	v := os.Getenv(admin.env)
	if v == "" {
		return nil
	}
	s, err := newAPI(v)
	if err != nil {
		return err
	}
	// TestMain has no context of its own.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var errs []error
	for _, p := range []string{prefix, secretPrefix} {
		var indices []struct {
			Index string `json:"index"`
		}
		if err := s.getJSON(ctx, "/_cat/indices/"+p+"*?h=index&format=json", &indices); err != nil {
			return err
		}
		for _, i := range indices {
			errs = append(errs, s.drop(ctx, i.Index))
		}
	}
	return errors.Join(errs...)
}

// api sends requests to the HTTP API of the server as one principal, for what
// SQL cannot do: the document API that writes, the cursors, the formats of an
// answer that the driver does not read, and the cluster settings.
type api struct {
	base       string
	user, pass string
	client     *http.Client
}

// newAPI returns the api of the URL v of a principal.
func newAPI(v string) (*api, error) {
	cfg, err := opensearch.ParseDSN(toDSN(v))
	if err != nil {
		return nil, err
	}
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return &api{
		base:   scheme + "://" + cfg.Host + ":" + strconv.Itoa(cfg.Port),
		user:   cfg.User,
		pass:   cfg.Password,
		client: &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

// newAdminAPI returns the api of the administrator, or skips the test when its
// variable is empty.
func newAdminAPI(t *testing.T) *api {
	t.Helper()
	s, err := newAPI(dsn(t, admin))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// apiAs returns the api of p.
func apiAs(t *testing.T, p principal) *api {
	t.Helper()
	s, err := newAPI(dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// do sends one request with the content type of JSON, and returns the status
// and the body of the answer.
func (s *api) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	status, _, b, err := s.send(ctx, method, path, body, "application/json")
	return status, b, err
}

// send sends one request. A body that is a string or []byte is sent as it is.
func (s *api) send(ctx context.Context, method, path string, body any, contentType string) (int, http.Header, []byte, error) {
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	case []byte:
		r = bytes.NewReader(b)
	default:
		enc, err := json.Marshal(b)
		if err != nil {
			return 0, nil, nil, err
		}
		r = bytes.NewReader(enc)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, r)
	if err != nil {
		return 0, nil, nil, err
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.SetBasicAuth(s.user, s.pass)
	res, err := s.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, b, err
}

// getJSON sends GET path and decodes the answer into v.
func (s *api) getJSON(ctx context.Context, path string, v any) error {
	status, b, err := s.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("GET %s gave HTTP %d: %s", path, status, b)
	}
	return json.Unmarshal(b, v)
}

// must sends a request that must answer with a status below 300.
func (s *api) must(t *testing.T, method, path string, body any) []byte {
	t.Helper()
	status, b, err := s.do(t.Context(), method, path, body)
	if err != nil {
		t.Fatal(err)
	}
	if status >= 300 {
		t.Fatalf("%s %s gave HTTP %d: %s", method, path, status, b)
	}
	return b
}

// drop removes the index name. An index that does not exist is no error.
func (s *api) drop(ctx context.Context, name string) error {
	status, b, err := s.do(ctx, http.MethodDelete, "/"+url.PathEscape(name)+"?ignore_unavailable=true", nil)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("removing the index %s gave HTTP %d: %s", name, status, b)
	}
	return nil
}

// index makes the index name with the body of its settings and mappings, and
// removes it when the test ends.
func (s *api) index(t *testing.T, name string, body any) {
	t.Helper()
	// The context of the test ends before its cleanup runs.
	t.Cleanup(func() { _ = s.drop(context.WithoutCancel(t.Context()), name) })
	s.must(t, http.MethodPut, "/"+name, body)
}

// bulk indexes the documents in the index name, and waits until a query can see
// them. A document with the key _id names its id.
func (s *api) bulk(t *testing.T, name string, docs []map[string]any) {
	t.Helper()
	var b strings.Builder
	for i, d := range docs {
		id, ok := d["_id"]
		if !ok {
			id = i + 1
		}
		fmt.Fprintf(&b, `{"index":{"_id":%q}}`+"\n", fmt.Sprint(id))
		doc := map[string]any{}
		for k, v := range d {
			if k != "_id" {
				doc[k] = v
			}
		}
		enc, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(enc)
		b.WriteString("\n")
	}
	status, _, res, err := s.send(t.Context(), http.MethodPost, "/"+name+"/_bulk?refresh=true", b.String(), "application/x-ndjson")
	if err != nil || status != http.StatusOK || strings.Contains(string(res), `"errors":true`) {
		t.Fatalf("indexing in %s gave HTTP %d, %v: %s", name, status, err, res)
	}
}

// rawSQL sends a body to a path of the SQL plugin as p, and returns the status,
// the headers and the answer, for what the driver does not read.
func rawSQL(t *testing.T, p principal, path string, body any) (int, http.Header, string) {
	t.Helper()
	status, h, b, err := apiAs(t, p).send(t.Context(), http.MethodPost, path, body, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	return status, h, string(b)
}

// versionOnce reads the release of the server once.
var (
	versionOnce sync.Once
	versionText string
)

// serverVersion returns the release of the server, which the administrator
// reads with GET /. The ordinary user cannot (recorded as dbmeta_user: "the
// version"). It skips the test when the administrator has no server.
func serverVersion(t *testing.T) string {
	t.Helper()
	s := newAdminAPI(t)
	versionOnce.Do(func() {
		var v struct {
			Version struct {
				Number       string `json:"number"`
				Distribution string `json:"distribution"`
			} `json:"version"`
		}
		if err := s.getJSON(t.Context(), "/", &v); err != nil {
			t.Fatal(err)
		}
		if v.Version.Distribution != "opensearch" {
			t.Fatalf("the server is %q, want opensearch", v.Version.Distribution)
		}
		versionText = v.Version.Number
	})
	return versionText
}

// serverIsOld reports whether the server is of the 2 series.
func serverIsOld(t *testing.T) bool {
	t.Helper()
	return strings.HasPrefix(serverVersion(t), "2.")
}

// m returns the mapping of a field of the type typ.
func m(typ string) map[string]any { return map[string]any{"type": typ} }

// settings returns the settings of an index of one shard and no replica, as the
// setup of step 6 made them.
func settings() map[string]any {
	return map[string]any{"number_of_shards": 1, "number_of_replicas": 0}
}

// typesName is the index of every type, which typesIndex makes once for the
// run, as the setup of step 6 made dbmeta_types (requests.json).
var (
	typesOnce sync.Once
	typesName = prefix + "types"
)

// typesMapping is the mapping of the index of every type that SQL reads, and of
// the types that it leaves out of SELECT * (recorded: "every type").
var typesMapping = map[string]any{"settings": settings(), "mappings": map[string]any{"properties": map[string]any{
	"id": m("integer"), "bo": m("boolean"), "by": m("byte"), "sh": m("short"), "i": m("integer"), "l": m("long"),
	"ul": m("unsigned_long"), "hf": m("half_float"), "f": m("float"), "d": m("double"),
	"sf": map[string]any{"type": "scaled_float", "scaling_factor": 100},
	"k":  m("keyword"), "t": m("text"), "tk": map[string]any{"type": "text", "fields": map[string]any{"raw": m("keyword")}},
	"dt": m("date"), "dtn": m("date_nanos"), "dtf": map[string]any{"type": "date", "format": "yyyy-MM-dd"},
	"tm": map[string]any{"type": "date", "format": "HH:mm:ss"}, "ip": m("ip"), "bin": m("binary"), "gp": m("geo_point"),
	"gs": m("geo_shape"), "ir": m("integer_range"), "w": m("wildcard"),
	"o":  map[string]any{"type": "object", "properties": map[string]any{"a": m("integer"), "b": m("keyword")}},
	"n":  map[string]any{"type": "nested", "properties": map[string]any{"a": m("integer"), "b": m("keyword")}},
	"fo": m("flat_object"),
}}}

// typesDocs are the documents of the index of every type: the largest values,
// NULL for every field, a document with only its id, a document with fields that
// hold several values, and a document whose text the mapping reads as a number
// (recorded: "every type").
var typesDocs = []map[string]any{
	{"id": 1, "bo": true, "by": -128, "sh": -32768, "i": -2147483648, "l": jsontext.Value(`-9223372036854775808`), "ul": jsontext.Value(`18446744073709551615`),
		"hf": 0.1, "f": 3.4028235e+38, "d": 0.1, "sf": 12.345, "k": "é'\"\\ x", "t": "hello world", "tk": "Hello Raw", "w": "wild*card",
		"dt": "2026-10-01T12:34:56.123+05:30", "dtn": "2026-10-01T12:34:56.123456789Z", "dtf": "2026-10-01", "tm": "12:34:56", "ip": "192.168.0.1",
		"bin": "AP8=", "gp": map[string]any{"lat": 41.12, "lon": -71.34}, "gs": map[string]any{"type": "point", "coordinates": []any{-71.34, 41.12}},
		"ir": map[string]any{"gte": 1, "lte": 5}, "o": map[string]any{"a": 1, "b": "x"},
		"n": []any{map[string]any{"a": 1, "b": "x"}, map[string]any{"a": 2, "b": "y"}}, "fo": map[string]any{"p": map[string]any{"q": "r"}}},
	{"id": 2, "bo": nil, "by": nil, "sh": nil, "i": nil, "l": nil, "ul": nil, "hf": nil, "f": nil, "d": nil, "sf": nil, "k": nil, "t": nil, "tk": nil,
		"w": nil, "dt": nil, "dtn": nil, "dtf": nil, "tm": nil, "ip": nil, "bin": nil, "gp": nil, "gs": nil, "ir": nil, "o": nil, "n": nil, "fo": nil},
	{"id": 3},
	{"id": 4, "bo": false, "by": 127, "sh": 32767, "i": []any{1, 2, 3}, "l": jsontext.Value(`9223372036854775807`), "ul": 0, "hf": 65504, "f": 1.4e-45,
		"d": []any{1.5, nil, 2.5}, "sf": -0.01, "k": []any{"a", "b"}, "t": "", "dt": 1790858096123, "dtn": "2262-04-11T23:47:16.854775807Z",
		"dtf": "0001-01-01", "tm": "23:59:59", "ip": "::1", "bin": "", "o": []any{map[string]any{"a": 1, "b": "x"}, map[string]any{"a": 2, "b": "y"}},
		"n": map[string]any{"a": 3, "b": "z"}},
	{"id": 5, "i": "42", "bo": "true", "d": "1.5", "l": 1.9, "dt": "-1000-01-01", "k": "", "dtn": "1970-01-01T00:00:00Z"},
}

// typesIndex makes the index of every type once for the run, and returns its
// name.
func typesIndex(t *testing.T) string {
	t.Helper()
	s := newAdminAPI(t)
	typesOnce.Do(func() {
		t.Helper()
		status, b, err := s.do(t.Context(), http.MethodPut, "/"+typesName, typesMapping)
		if err != nil || status >= 300 {
			t.Fatalf("making the index of every type gave HTTP %d, %v: %s", status, err, b)
		}
		s.bulk(t, typesName, typesDocs)
	})
	return typesName
}

// rowsName is the index of 300 rows, which rowsIndex makes once for the run, as
// the setup of step 6 made dbmeta_rows. Each row has the number n, the text "row
// n", an object and a nested field.
var (
	rowsOnce sync.Once
	rowsName = prefix + "rows"
)

// groupsName is the index of 1050 rows with a distinct text each, as the setup
// of step 6 made dbmeta_groups, for a GROUP BY of 1050 groups.
var (
	groupsOnce sync.Once
	groupsName = prefix + "groups"
)

// leadName is the index of 150 rows whose text turns bad at the row 121, as the
// setup of step 6 made dbmeta_lead, so that a cast fails on the second page.
var (
	leadOnce sync.Once
	leadName = prefix + "lead"
)

// numberedRows returns the documents of n rows, with the number i and the text
// "row i".
func numberedRows(n int, text func(i int) string) []map[string]any {
	docs := make([]map[string]any, n)
	for i := range docs {
		docs[i] = map[string]any{"_id": i + 1, "n": i + 1, "s": text(i + 1)}
	}
	return docs
}

// fixture makes the index name once with the mapping of an integer n and a
// keyword s, and fills it with docs.
func fixture(t *testing.T, once *sync.Once, name string, docs []map[string]any) string {
	t.Helper()
	s := newAdminAPI(t)
	once.Do(func() {
		t.Helper()
		status, b, err := s.do(t.Context(), http.MethodPut, "/"+name, map[string]any{
			"settings": settings(),
			"mappings": map[string]any{"properties": map[string]any{"n": m("integer"), "s": m("keyword"),
				"o":  map[string]any{"properties": map[string]any{"a": m("integer")}},
				"nn": map[string]any{"type": "nested", "properties": map[string]any{"a": m("integer")}}}},
		})
		if err != nil || status >= 300 {
			t.Fatalf("making the index %s gave HTTP %d, %v: %s", name, status, err, b)
		}
		s.bulk(t, name, docs)
	})
	return name
}

// rowsIndex makes the index of 300 rows once for the run, and returns its name.
func rowsIndex(t *testing.T) string {
	t.Helper()
	docs := numberedRows(300, func(i int) string { return "row " + strconv.Itoa(i) })
	for i, d := range docs {
		n := i + 1
		d["o"] = map[string]any{"a": n}
		d["nn"] = []any{map[string]any{"a": n}, map[string]any{"a": n + 1}}
	}
	return fixture(t, &rowsOnce, rowsName, docs)
}

// groupsIndex makes the index of 1050 groups once for the run, and returns its
// name.
func groupsIndex(t *testing.T) string {
	t.Helper()
	return fixture(t, &groupsOnce, groupsName, numberedRows(1050, func(i int) string { return "g" + strconv.Itoa(i) }))
}

// leadIndex makes the index of 150 rows whose text turns bad once for the run,
// and returns its name.
func leadIndex(t *testing.T) string {
	t.Helper()
	return fixture(t, &leadOnce, leadName, numberedRows(150, func(i int) string {
		if i >= 121 {
			return "x" + strconv.Itoa(i)
		}
		return strconv.Itoa(i)
	}))
}

// settingsOf sets the transient settings of the cluster, and removes them when
// the test ends. A value of nil removes a setting. The settings are global to
// the server, so a test that sets one never runs in parallel with another.
func (s *api) settingsOf(t *testing.T, transient map[string]any) {
	t.Helper()
	reset := map[string]any{}
	for k := range transient {
		reset[k] = nil
	}
	t.Cleanup(func() {
		// The context of the test ends before its cleanup runs.
		_, _, _ = s.do(context.WithoutCancel(t.Context()), http.MethodPut, "/_cluster/settings", map[string]any{"transient": reset})
	})
	s.must(t, http.MethodPut, "/_cluster/settings", map[string]any{"transient": transient})
}

// openPITs returns the points in time that the cluster holds open, which a
// cursor that is not closed keeps until its keep_alive ends (measured).
func openPITs(t *testing.T, s *api) int {
	t.Helper()
	var raw struct {
		PITs []map[string]any `json:"pits"`
	}
	if err := s.getJSON(t.Context(), "/_search/point_in_time/_all", &raw); err != nil {
		t.Fatal(err)
	}
	return len(raw.PITs)
}

// TestIntegrationTypes reads every type through Rows.Scan, the path of a
// caller, into the Go type of the type table and into sql.Null of it, as each
// principal (step 14).
func TestIntegrationTypes(t *testing.T) {
	name := typesIndex(t)
	forEach(t, func(t *testing.T, e *target) {
		rows, err := e.db.QueryContext(e.ctx(t), "SELECT id, bo, `by`, sh, i, l, hf, f, d, sf, k, t, tk, dt, dtn, dtf, tm, ip, bin, gp, o FROM "+name+" WHERE id < 4 ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var (
				id            int64
				bo            sql.Null[bool]
				by, sh, i, l  sql.Null[int64]
				hf, f, d, sf  sql.Null[float64]
				k, tx, tk, ip sql.Null[string]
				dt, dtn       sql.Null[time.Time]
				dtf           sql.Null[dbimp.Date]
				tm            sql.Null[dbimp.LocalTime]
				bin           []byte
				gp, o         sql.Null[map[string]any]
			)
			if err := rows.Scan(&id, &bo, &by, &sh, &i, &l, &hf, &f, &d, &sf, &k, &tx, &tk, &dt, &dtn, &dtf, &tm, &ip, &bin, &gp, &o); err != nil {
				t.Fatal(err)
			}
			got = append(got, fmt.Sprintf("%d|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v", id, bo, by, sh, i, l, hf, f, d, sf, k, tx, tk,
				formatTime(dt), formatTime(dtn), dtf, tm, ip, bin, gp, o, ""))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"1|{true true}|{-128 true}|{-32768 true}|{-2147483648 true}|{-9223372036854775808 true}|{0.1 true}|{3.4028235e+38 true}|{0.1 true}|{12.345 true}|{é'\"\\ x true}|{hello world true}|{Hello Raw true}|2026-10-01T07:04:56.123Z|2026-10-01T12:34:56.123456789Z|{2026-10-01 true}|{12:34:56 true}|{192.168.0.1 true}|[0 255]|{map[lat:41.12 lon:-71.34] true}|{map[a:1 b:x] true}|",
			"2|{false false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{ false}|{ false}|{ false}|null|null|{0000-00-00 false}|{00:00:00 false}|{ false}|[]|{map[] false}|{map[] false}|",
			"3|{false false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{ false}|{ false}|{ false}|null|null|{0000-00-00 false}|{00:00:00 false}|{ false}|[]|{map[] false}|{map[] false}|",
		}
		if !equalLines(got, want) {
			t.Errorf("the rows are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})
}

// equalLines reports whether two lists of text are equal.
func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// formatTime returns the text of a time, or null.
func formatTime(v sql.Null[time.Time]) string {
	if !v.Valid {
		return "null"
	}
	return v.V.Format(time.RFC3339Nano)
}
