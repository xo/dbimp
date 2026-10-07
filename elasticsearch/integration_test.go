package elasticsearch_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
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

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/elasticsearch"
)

// These tests need a server. ELASTICSEARCH_DSN names the server for the
// administrator, and ELASTICSEARCH_ORDINARY_DSN for the ordinary user (D9).
// Each is the url of dbrun, which is elasticsearch://user:pass@host:port, and
// the tests read it as the DSN of the driver. SQL in Elasticsearch takes no
// write (D163), so the tests write their indices through the document API as
// the administrator, and read them through the driver as each principal. The
// ordinary user of the dbmeta entry can read the indices whose names start
// with dbmeta (measured), so every index of the tests starts with a prefix of
// its own that starts with dbmeta, which TestMain removes at the end. A test
// skips when a variable that it needs is empty.

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
	admin      = principal{"administrator", "ELASTICSEARCH_DSN"}
	ordinary   = principal{"ordinary", "ELASTICSEARCH_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// toDSN returns the URL of dbrun as the DSN of the driver. A URL whose scheme
// is already elasticsearch stays as it is.
func toDSN(v string) string {
	switch {
	case strings.HasPrefix(v, "http://"):
		return "elasticsearch://" + strings.TrimPrefix(v, "http://")
	case strings.HasPrefix(v, "https://"):
		return "elasticsearch://" + strings.TrimPrefix(v, "https://") + "?tls=true"
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

// openAs opens the server as p, with the query of a DSN.
func openAs(t *testing.T, p principal, query string) *sql.DB {
	t.Helper()
	db, err := sql.Open(elasticsearch.Name, dsn(t, p)+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// forEach runs f as a subtest for each principal.
func forEach(t *testing.T, f func(t *testing.T, p principal, db *sql.DB)) {
	t.Helper()
	for _, p := range principals {
		t.Run(p.name, func(t *testing.T) {
			f(t, p, openAs(t, p, ""))
		})
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

// api sends requests to the HTTP API of the server as the administrator, for
// what SQL cannot do: the document API that writes, the tasks, and the result
// formats that the driver does not read.
type api struct {
	base       string
	user, pass string
	client     *http.Client
}

// newAPI returns the api of the URL v of the administrator.
func newAPI(v string) (*api, error) {
	cfg, err := elasticsearch.ParseDSN(toDSN(v))
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

// newAdminAPI returns the api of the administrator, or skips the test when
// its variable is empty.
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
	return s.send(ctx, method, path, body, "application/json")
}

// send sends one request. A body that is a string or []byte is sent as it is.
func (s *api) send(ctx context.Context, method, path string, body any, contentType string) (int, []byte, error) {
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
			return 0, nil, err
		}
		r = bytes.NewReader(enc)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, r)
	if err != nil {
		return 0, nil, err
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.SetBasicAuth(s.user, s.pass)
	res, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return res.StatusCode, b, err
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

// bulk indexes the documents of lines, each an action line and a document, in
// the index name, and waits until they are visible to a query.
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
	status, res, err := s.send(t.Context(), http.MethodPost, "/"+name+"/_bulk?refresh=true", b.String(), "application/x-ndjson")
	if err != nil || status != http.StatusOK || strings.Contains(string(res), `"errors":true`) {
		t.Fatalf("indexing in %s gave HTTP %d, %v: %s", name, status, err, res)
	}
}

// rawSQL sends a statement to POST /_sql as p, with the keys of body, and
// returns the status and the answer, for what the driver does not read.
func rawSQL(t *testing.T, p principal, query string, params map[string]any) (int, string) {
	t.Helper()
	body := map[string]any{"query": query}
	maps.Copy(body, params)
	status, b, err := apiAs(t, p).do(t.Context(), http.MethodPost, "/_sql?format=json", body)
	if err != nil {
		t.Fatal(err)
	}
	return status, string(b)
}

// indexOf makes the index "every type" once for the run, as the setup of step 6
// made dbmeta_types (requests.json), and returns its name.
var (
	typesOnce sync.Once
	typesName = prefix + "types"
)

// typesMapping is the mapping of the index of every type.
var typesMapping = map[string]any{"mappings": map[string]any{"dynamic": "strict", "properties": map[string]any{
	"id": m("integer"), "b": m("boolean"), "by": m("byte"), "sh": m("short"), "i": m("integer"), "l": m("long"),
	"ul": m("unsigned_long"), "hf": m("half_float"), "f": m("float"), "d": m("double"),
	"sf": map[string]any{"type": "scaled_float", "scaling_factor": 100},
	"k":  m("keyword"), "t": map[string]any{"type": "text", "fields": map[string]any{"raw": m("keyword")}},
	"ck":  map[string]any{"type": "constant_keyword", "value": "fixed"},
	"w":   m("wildcard"),
	"mot": m("match_only_text"), "dt": m("date"), "dn": m("date_nanos"), "ip": m("ip"), "v": m("version"), "bin": m("binary"),
	"o":  map[string]any{"properties": map[string]any{"a": m("integer"), "s": m("keyword")}},
	"n":  map[string]any{"type": "nested", "properties": map[string]any{"a": m("integer")}},
	"gp": m("geo_point"), "gs": m("geo_shape"), "pt": m("point"), "shp": m("shape"), "fl": m("flattened"),
	"ir": m("integer_range"), "dv": map[string]any{"type": "dense_vector", "dims": 3},
	"al":  map[string]any{"type": "alias", "path": "i"},
	"amd": map[string]any{"type": "aggregate_metric_double", "metrics": []string{"min", "max"}, "default_metric": "max"},
}}}

// m returns the mapping of a field of the type typ.
func m(typ string) map[string]any { return map[string]any{"type": typ} }

// typesDocs are the documents of the index of every type: the largest values,
// the smallest, NULL for every field, a document with no field, and a document
// whose fields hold several values.
var typesDocs = []map[string]any{
	{"id": 1, "b": true, "by": 127, "sh": 32767, "i": 2147483647, "l": jsontext.Value(`9223372036854775807`), "ul": jsontext.Value(`18446744073709551615`),
		"hf": 65504, "f": 3.4028235e+38, "d": 1.7976931348623157e+308, "sf": 1234.5678, "k": "é'\"\\ x", "t": "Some text", "w": "wild",
		"mot": "match only", "dt": "2026-10-01T12:34:56.789+05:30", "dn": "2026-10-01T12:34:56.123456789Z", "ip": "2001:db8::1", "v": "1.2.3-beta",
		"bin": "AP8=", "o": map[string]any{"a": 1, "s": "x"}, "n": []any{map[string]any{"a": 1}, map[string]any{"a": 2}},
		"gp": map[string]any{"lat": 41.12, "lon": -71.34}, "gs": map[string]any{"type": "point", "coordinates": []any{-71.34, 41.12}},
		"pt": map[string]any{"x": 1.5, "y": 2.5}, "shp": map[string]any{"type": "point", "coordinates": []any{1.5, 2.5}},
		"fl": map[string]any{"x": "1", "y": map[string]any{"z": "2"}}, "ir": map[string]any{"gte": 1, "lt": 10}, "dv": []any{1.0, 2.0, 3.0},
		"amd": map[string]any{"min": 1.5, "max": 9.5}},
	{"id": 2, "b": false, "by": -128, "sh": -32768, "i": -2147483648, "l": jsontext.Value(`-9223372036854775808`), "ul": 0,
		"hf": -65504, "f": -1.4e-45, "d": 5e-324, "sf": 0, "k": "", "t": "", "w": "", "mot": "", "dt": "1970-01-01", "dn": "1970-01-01T00:00:00Z",
		"ip": "0.0.0.0", "v": "0.0.0", "bin": "", "o": map[string]any{}, "n": []any{}, "fl": map[string]any{}},
	{"id": 3, "b": nil, "by": nil, "sh": nil, "i": nil, "l": nil, "ul": nil, "hf": nil, "f": nil, "d": nil, "sf": nil, "k": nil, "t": nil,
		"w": nil, "mot": nil, "dt": nil, "dn": nil, "ip": nil, "v": nil, "bin": nil, "o": nil, "n": nil, "gp": nil, "gs": nil, "pt": nil,
		"shp": nil, "fl": nil, "ir": nil, "dv": nil, "amd": nil},
	{"id": 4},
	{"id": 5, "i": []any{1, 2, 3}, "k": []any{"a", "b"}, "o": []any{map[string]any{"a": 1}, map[string]any{"a": 2}}},
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

// pagingName is the index of 250 rows, which pagingIndex makes once for the
// run, as the setup of step 6 made dbmeta_paging.
var (
	pagingOnce sync.Once
	pagingName = prefix + "paging"
)

// windowName is the index of 250 rows whose result window is 100.
var (
	windowOnce sync.Once
	windowName = prefix + "window"
)

// rowsOf returns the documents of n rows, with the number i and the text "row i".
func rowsOf(n int) []map[string]any {
	docs := make([]map[string]any, n)
	for i := range docs {
		docs[i] = map[string]any{"id": i + 1, "n": i + 1, "s": "row " + strconv.Itoa(i+1)}
	}
	return docs
}

// pagingIndex makes the index of 250 rows once for the run, and returns its
// name.
func pagingIndex(t *testing.T) string {
	t.Helper()
	s := newAdminAPI(t)
	pagingOnce.Do(func() {
		t.Helper()
		status, b, err := s.do(t.Context(), http.MethodPut, "/"+pagingName, map[string]any{"mappings": map[string]any{"properties": map[string]any{"n": m("integer"), "s": m("keyword")}}})
		if err != nil || status >= 300 {
			t.Fatalf("making the index of 250 rows gave HTTP %d, %v: %s", status, err, b)
		}
		s.bulk(t, pagingName, rowsOf(250))
	})
	return pagingName
}

// windowIndex makes the index whose result window is 100 once for the run.
func windowIndex(t *testing.T) string {
	t.Helper()
	s := newAdminAPI(t)
	windowOnce.Do(func() {
		t.Helper()
		status, b, err := s.do(t.Context(), http.MethodPut, "/"+windowName, map[string]any{
			"settings": map[string]any{"index.max_result_window": 100},
			"mappings": map[string]any{"properties": map[string]any{"n": m("integer"), "s": m("keyword")}},
		})
		if err != nil || status >= 300 {
			t.Fatalf("making the index with a result window of 100 gave HTTP %d, %v: %s", status, err, b)
		}
		s.bulk(t, windowName, rowsOf(250))
	})
	return windowName
}

// TestIntegrationTypes reads every type through Rows.Scan, the path of a
// caller, into the Go type of the type table and into sql.Null of it, as each
// principal (step 14).
func TestIntegrationTypes(t *testing.T) {
	name := typesIndex(t)
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		rows, err := db.QueryContext(t.Context(), "SELECT id, b, \"by\", sh, i, l, ul, hf, f, d, sf, k, t, ck, w, mot, dt, dn, ip, v, bin, gp, gs, shp, al FROM "+name+" WHERE id < 5 ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var (
				id        int64
				b         sql.Null[bool]
				by, sh, i sql.Null[int64]
				l         sql.Null[int64]
				ul        sql.Null[uint64]
				hf, f, d  sql.Null[float64]
				sf        sql.Null[float64]
				k, tx, ck sql.Null[string]
				w, mot    sql.Null[string]
				dt, dn    sql.Null[time.Time]
				ip, v     sql.Null[string]
				bin       []byte
				gp, gs    sql.Null[string]
				shp       sql.Null[string]
				al        sql.Null[int64]
			)
			if err := rows.Scan(&id, &b, &by, &sh, &i, &l, &ul, &hf, &f, &d, &sf, &k, &tx, &ck, &w, &mot, &dt, &dn, &ip, &v, &bin, &gp, &gs, &shp, &al); err != nil {
				t.Fatal(err)
			}
			got = append(got, fmt.Sprintf("%d|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v",
				id, b, by, sh, i, l, ul, hf, f, d, sf, k, tx, ck, w, mot, formatTime(dt), formatTime(dn), ip, v, bin, gp, gs, shp, al))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"1|{true true}|{127 true}|{32767 true}|{2147483647 true}|{9223372036854775807 true}|{18446744073709551615 true}|{65504 true}|{3.4028235e+38 true}|{1.7976931348623157e+308 true}|{1234.57 true}|{é'\"\\ x true}|{Some text true}|{fixed true}|{wild true}|{match only true}|2026-10-01T07:04:56.789Z|2026-10-01T12:34:56.123456789Z|{2001:db8::1 true}|{1.2.3-beta true}|[0 255]|{POINT (-71.34 41.12) true}|{POINT (-71.34 41.12) true}|{POINT (1.5 2.5) true}|{2147483647 true}",
			"2|{false true}|{-128 true}|{-32768 true}|{-2147483648 true}|{-9223372036854775808 true}|{0 true}|{-65504 true}|{-1.4e-45 true}|{5e-324 true}|{0 true}|{ true}|{ true}|{fixed true}|{ true}|{ true}|1970-01-01T00:00:00Z|1970-01-01T00:00:00Z|{0.0.0.0 true}|{0.0.0 true}|[]|{ false}|{ false}|{ false}|{-2147483648 true}",
			"3|{false false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{ false}|{ false}|{fixed true}|{ false}|{ false}|null|null|{ false}|{ false}|[]|{ false}|{ false}|{ false}|{0 false}",
			"4|{false false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{0 false}|{ false}|{ false}|{fixed true}|{ false}|{ false}|null|null|{ false}|{ false}|[]|{ false}|{ false}|{ false}|{0 false}",
		}
		if !slices.Equal(got, want) {
			t.Errorf("the rows are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})
}

// formatTime returns the text of a time, or null.
func formatTime(v sql.Null[time.Time]) string {
	if !v.Valid {
		return "null"
	}
	return v.V.Format(time.RFC3339Nano)
}

// TestIntegrationVersion holds the answers of step 16: the version of the
// server, which the administrator reads with GET /, and which the ordinary
// user is refused with HTTP 403. SQL has no function for the version, so the
// driver answers SELECT version() itself from GET / (D181), and the ordinary
// user gets the HTTP 403 of that request. DATABASE() and USER() work for both
// principals (recorded: "the version" and "the database and the user").
func TestIntegrationVersion(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		status, b, err := apiAs(t, p).do(t.Context(), http.MethodGet, "/", nil)
		if err != nil {
			t.Fatal(err)
		}
		if p == ordinary {
			if status != http.StatusForbidden {
				t.Errorf("GET / as the ordinary user gave HTTP %d, want 403: %s", status, b)
			}
		} else {
			var v struct {
				Version struct {
					Number string `json:"number"`
				} `json:"version"`
			}
			if err := json.Unmarshal(b, &v); err != nil || status != http.StatusOK || v.Version.Number == "" {
				t.Fatalf("GET / gave HTTP %d, %v: %s", status, err, b)
			}
			t.Logf("the version is %s", v.Version.Number)
			for _, query := range []string{"SELECT version()", "select VERSION();"} {
				var got string
				if err := db.QueryRowContext(t.Context(), query).Scan(&got); err != nil {
					t.Fatalf("%s: %v", query, err)
				}
				if got != v.Version.Number {
					t.Errorf("%s gave %q, want %q", query, got, v.Version.Number)
				}
			}
			stmt, err := db.PrepareContext(t.Context(), "SELECT version()")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			var got string
			if err := stmt.QueryRowContext(t.Context()).Scan(&got); err != nil || got != v.Version.Number {
				t.Errorf("the prepared SELECT version() gave %q and %v, want %q", got, err, v.Version.Number)
			}
		}
		if p == ordinary {
			var got string
			err := db.QueryRowContext(t.Context(), "SELECT version()").Scan(&got)
			if e, ok := errors.AsType[*elasticsearch.Error](err); !ok || e.HTTPStatus != http.StatusForbidden {
				t.Errorf("SELECT version() as the ordinary user gave %q and %v, want HTTP 403", got, err)
			}
		}
		// Any other statement goes to the server, which has no such function.
		_, _, err = readAll(t, db, "SELECT version() FROM "+prefix+"none")
		if e, ok := errors.AsType[*elasticsearch.Error](err); !ok || e.HTTPStatus != http.StatusBadRequest {
			t.Errorf("SELECT version() with a FROM gave %v, want the HTTP 400 of the server", err)
		}
		var cluster, user string
		if err := db.QueryRowContext(t.Context(), "SELECT DATABASE(), USER()").Scan(&cluster, &user); err != nil {
			t.Fatalf("DATABASE and USER: %v", err)
		}
		if want := map[principal]string{admin: "elastic", ordinary: "dbmeta_user"}[p]; user != want {
			t.Errorf("USER() is %q, want %q", user, want)
		}
		t.Logf("the cluster is %s, and the user is %s", cluster, user)
		if err := db.PingContext(t.Context()); err != nil {
			t.Errorf("Ping: %v", err)
		}
	})
}

// TestIntegrationErrors holds that an error before any rows is the error of the
// server, that an error after some rows wraps dbimp.ErrIncomplete after
// exactly those rows (D167), and that a wrong password is HTTP 401.
func TestIntegrationErrors(t *testing.T) {
	name := typesIndex(t)
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		_, _, err := readAll(t, db, "SELEC 1")
		if e, ok := errors.AsType[*elasticsearch.Error](err); !ok || e.HTTPStatus != http.StatusBadRequest || e.Type != "parsing_exception" {
			t.Errorf("a syntax error gave %v", err)
		}
		_, _, err = readAll(t, db, "SELECT a FROM "+prefix+"none")
		if e, ok := errors.AsType[*elasticsearch.Error](err); !ok || e.HTTPStatus != http.StatusBadRequest || !strings.Contains(err.Error(), "Unknown index") {
			t.Errorf("an unknown index gave %v", err)
		}
		_, got, err := readAll(t, db, "SELECT id, i FROM "+name+" ORDER BY id", elasticsearch.WithFetchSize(2))
		if len(got) != 4 || !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("an error after some rows gave %d rows and %v, want 4 and then dbimp.ErrIncomplete", len(got), err)
		}
		if e, ok := errors.AsType[*elasticsearch.Error](err); !ok || e.HTTPStatus != http.StatusBadRequest || e.Type != "invalid_argument_exception" {
			t.Errorf("an error after some rows gave %v, want the error of the server", err)
		}
		u, err := url.Parse(dsn(t, p))
		if err != nil {
			t.Fatal(err)
		}
		u.User = url.UserPassword(u.User.Username(), "wrong")
		wrong, err := sql.Open(elasticsearch.Name, u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer wrong.Close()
		err = wrong.PingContext(t.Context())
		if e, ok := errors.AsType[*elasticsearch.Error](err); !ok || e.HTTPStatus != http.StatusUnauthorized || strings.Contains(err.Error(), "wrong") {
			t.Errorf("a wrong password gave %v, want HTTP 401 with no password in it", err)
		}
	})
}

// openContexts returns the search contexts that the node holds open, which a
// cursor that is not closed keeps until its keep_alive ends (measured).
func openContexts(t *testing.T, s *api) int {
	t.Helper()
	var stats struct {
		Nodes map[string]struct {
			Indices struct {
				Search struct {
					Open int `json:"open_contexts"`
				} `json:"search"`
			} `json:"indices"`
		} `json:"nodes"`
	}
	if err := s.getJSON(t.Context(), "/_nodes/stats/indices/search", &stats); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, node := range stats.Nodes {
		n += node.Indices.Search.Open
	}
	return n
}

// TestIntegrationPages holds D167 against the server: a result of 250 rows in
// pages of 100 and in the page of 1000, a result whose page the result window
// of an index refuses, and the cursor of rows that the caller closes before
// the end, which the server drops at once (measured: open_contexts).
func TestIntegrationPages(t *testing.T) {
	paging, window := pagingIndex(t), windowIndex(t)
	s := newAdminAPI(t)
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		for _, size := range []int{1, 7, 100, 249, 250, 251, 1000} {
			_, got, err := readAll(t, db, "SELECT n, s FROM "+paging+" ORDER BY n", elasticsearch.WithFetchSize(size))
			if err != nil || len(got) != 250 {
				t.Fatalf("fetch_size %d: read %d rows and %v, want 250", size, len(got), err)
			}
			for i, row := range got {
				if row[0] != int64(i+1) || row[1] != "row "+strconv.Itoa(i+1) {
					t.Fatalf("fetch_size %d: row %d is %v", size, i+1, row)
				}
			}
		}
		// A GROUP BY and a LIMIT page too (measured).
		_, got, err := readAll(t, db, "SELECT n, COUNT(*) AS c FROM "+paging+" GROUP BY n", elasticsearch.WithFetchSize(100))
		if err != nil || len(got) != 250 {
			t.Errorf("a group by in pages: read %d rows and %v, want 250", len(got), err)
		}
		_, got, err = readAll(t, db, "SELECT n FROM "+paging+" ORDER BY n LIMIT 150", elasticsearch.WithFetchSize(100))
		if err != nil || len(got) != 150 {
			t.Errorf("a limit across pages: read %d rows and %v, want 150", len(got), err)
		}
		// The default page of 1000 is larger than the window of 100.
		_, _, err = readAll(t, db, "SELECT n FROM "+window+" ORDER BY n")
		if e, ok := errors.AsType[*elasticsearch.Error](err); !ok || e.HTTPStatus != http.StatusBadRequest || !strings.Contains(err.Error(), "Result window is too large") {
			t.Errorf("a page larger than the window gave %v", err)
		}
		_, got, err = readAll(t, db, "SELECT n FROM "+window+" ORDER BY n", elasticsearch.WithFetchSize(100))
		if err != nil || len(got) != 250 {
			t.Errorf("pages past the window: read %d rows and %v, want 250", len(got), err)
		}
		before := openContexts(t, s)
		for _, read := range []int{1, 100, 101, 150, 249} {
			func() {
				rows, err := db.QueryContext(t.Context(), "SELECT n FROM "+paging+" ORDER BY n", elasticsearch.WithFetchSize(100))
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				for range read {
					if !rows.Next() {
						t.Fatalf("no row: %v", rows.Err())
					}
				}
			}()
		}
		if after := openContexts(t, s); after != before {
			t.Errorf("the server holds %d search contexts after the rows closed, and held %d before: the driver left a cursor open", after, before)
		}
	})
}

// slowFilter is a filter whose script runs for about two seconds on the index
// of 250 rows, as the query to cancel of step 6 does (requests.json).
var slowFilter = map[string]any{"script": map[string]any{"script": map[string]any{
	"source": `double x = doc["n"].value; for (int i = 0; i < 900000; i++) { x = Math.sin(x) + Math.sqrt(i); } return x != 12345.0;`,
}}}

// TestIntegrationCancel holds D167: when the context ends while the server
// works, the driver closes the request, the server cancels the task of the
// statement (measured), and the error is the error of the context. The
// timeout of WithTimeout ends the statement on the server.
func TestIntegrationCancel(t *testing.T) {
	paging := pagingIndex(t)
	s := newAdminAPI(t)
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := db.ExecContext(ctx, "SELECT n FROM "+paging+" ORDER BY s DESC", elasticsearch.WithFetchSize(10), elasticsearch.WithParameter("filter", slowFilter))
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) {
			t.Errorf("a statement whose context ended gave %v, want context.DeadlineExceeded", err)
		}
		if d := time.Since(start); d > 30*time.Second {
			t.Errorf("the statement took %v", d)
		}
		// The task ends on the server soon after the client left.
		deadline := time.Now().Add(20 * time.Second)
		for {
			var tasks struct {
				Nodes map[string]struct {
					Tasks map[string]struct {
						Action    string `json:"action"`
						Cancelled bool   `json:"cancelled"`
					} `json:"tasks"`
				} `json:"nodes"`
			}
			if err := s.getJSON(t.Context(), "/_tasks?actions=indices:data/read/sql&detailed=true", &tasks); err != nil {
				t.Fatal(err)
			}
			running := 0
			for _, n := range tasks.Nodes {
				for _, task := range n.Tasks {
					if !task.Cancelled {
						running++
					}
				}
			}
			if running == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d tasks of the statement still run 20 seconds after the client left", running)
			}
			time.Sleep(100 * time.Millisecond)
		}
		_, _, err = readAll(t, db, "SELECT n FROM "+paging+" ORDER BY s DESC", elasticsearch.WithFetchSize(10),
			elasticsearch.WithParameter("filter", slowFilter), elasticsearch.WithTimeout(100*time.Millisecond))
		e, ok := errors.AsType[*elasticsearch.Error](err)
		if !ok || e.RootType != "search_timeout_exception" || (e.HTTPStatus != http.StatusGatewayTimeout && e.HTTPStatus != http.StatusTooManyRequests) {
			t.Errorf("a statement that passed its request_timeout gave %v", err)
		}
	})
}

// TestIntegrationPrincipals holds what the ordinary user can and cannot do
// (recorded as dbmeta_user): it reads the indices dbmeta* through the driver,
// finds an index outside them unknown, and writes nothing through the
// document API.
func TestIntegrationPrincipals(t *testing.T) {
	s := newAdminAPI(t)
	secret := secretPrefix + "secret"
	s.index(t, secret, map[string]any{"mappings": map[string]any{"properties": map[string]any{"a": m("integer")}}})
	s.bulk(t, secret, []map[string]any{{"a": 1}})
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		_, got, err := readAll(t, db, "SELECT a FROM "+secret)
		if p == admin {
			if err != nil || len(got) != 1 {
				t.Errorf("the administrator read %v and %v from the index, want one row", got, err)
			}
			return
		}
		if e, ok := errors.AsType[*elasticsearch.Error](err); !ok || e.HTTPStatus != http.StatusBadRequest || !strings.Contains(err.Error(), "Unknown index ["+secret+"]") {
			t.Errorf("the ordinary user read an index that it cannot read: %v, %v", got, err)
		}
		_, tables, err := readAll(t, db, "SHOW TABLES LIKE '"+secretPrefix+"%'")
		if err != nil || len(tables) != 0 {
			t.Errorf("SHOW TABLES names %v for the ordinary user, %v, want none", tables, err)
		}
		status, b, err := apiAs(t, p).do(t.Context(), http.MethodPut, "/"+prefix+"denied/_doc/1", map[string]any{"a": 1})
		if err != nil || status != http.StatusForbidden || !strings.Contains(string(b), "security_exception") {
			t.Errorf("the ordinary user wrote a document: HTTP %d, %v: %.200s, want HTTP 403", status, err, b)
		}
	})
}
