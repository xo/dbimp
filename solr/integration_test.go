package solr_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/solr"
)

// These tests need a server. SOLR_DSN names the server for the
// administrator, and SOLR_ORDINARY_DSN for the ordinary user (D9). Each is
// the url of dbrun, which is solr://user:pass@host:port, with no path. The
// tests add the path of the collection that they make. The SQL of Solr takes
// no write (D163), so the tests write their collections through the update
// handler as the administrator, and read them through the driver as each
// principal. The ordinary user of the dbmeta entry has the role search, which
// reads a collection and runs SQL on it, and cannot write (measured). A test
// skips when a variable that it needs is empty.
//
// TestMain makes the collections of the run, each with a configuration set of
// its own, so that a change of one schema does not reload another collection,
// and removes them at the end. Each name starts with a prefix that holds the
// time of the run.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each collection and each configuration set of this run.
var prefix = "dbimp_it_" + suffix

// The collections of the run. The main collection holds a document of every
// type and 300 more, the three collections of a small catalog hold the rows
// of the tests of CRUD, and the last holds the values that fail a query.
var (
	collMain    = prefix
	collAuthors = prefix + "_authors"
	collBooks   = prefix + "_books"
	collReviews = prefix + "_reviews"
	collInf     = prefix + "_inf"
	collAlias   = prefix + "_alias"
)

// visible bounds the time that a write takes to answer a query.
const visible = 10 * time.Second

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "SOLR_DSN"}
	ordinary   = principal{"ordinary", "SOLR_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// toDSN returns the URL of dbrun as the DSN of the driver. A URL whose
// scheme is already solr stays as it is.
func toDSN(v string) string {
	switch {
	case strings.HasPrefix(v, "http://"):
		return "solr://" + strings.TrimPrefix(v, "http://")
	case strings.HasPrefix(v, "https://"):
		return "solr://" + strings.TrimPrefix(v, "https://") + "?tls=true"
	}
	return v
}

// dsn returns the DSN of p with the path of the collection, or skips the test
// when it is empty.
func dsn(t *testing.T, p principal, collection string) string {
	t.Helper()
	v := os.Getenv(p.env)
	if v == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	u, err := url.Parse(toDSN(v))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + collection
	return u.String()
}

// openAs opens the collection as p.
func openAs(t *testing.T, p principal, collection string) *sql.DB {
	t.Helper()
	db, err := sql.Open(solr.Name, dsn(t, p, collection))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// forEach runs f as a subtest for each principal, on the main collection.
func forEach(t *testing.T, f func(t *testing.T, p principal, db *sql.DB)) {
	t.Helper()
	for _, p := range principals {
		t.Run(p.name, func(t *testing.T) {
			f(t, p, openAs(t, p, collMain))
		})
	}
}

// api sends requests to the HTTP API of the server, for what the SQL of Solr
// cannot do: the update handler, the schema, and the handlers that the driver
// does not read.
type api struct {
	base       string
	user, pass string
	client     *http.Client
}

// newAPI returns the api of the URL v.
func newAPI(v string) (*api, error) {
	cfg, err := solr.ParseDSN(toDSN(v))
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
		client: &http.Client{},
	}, nil
}

// apiAs returns the api of p, or skips the test when its address is empty.
func apiAs(t *testing.T, p principal) *api {
	t.Helper()
	v := os.Getenv(p.env)
	if v == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	a, err := newAPI(v)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// do sends one request with body as JSON, or no body for nil, and returns the
// status and the body of the answer.
func (a *api) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, r)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth(a.user, a.pass)
	res, err := a.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return res.StatusCode, b, err
}

// ok sends one request and returns an error unless the status is 200 and the
// header of the answer says 0.
func (a *api) ok(ctx context.Context, method, path string, body any) error {
	status, b, err := a.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	var res struct {
		ResponseHeader struct {
			Status int `json:"status"`
		} `json:"responseHeader"`
	}
	if status != http.StatusOK || json.Unmarshal(b, &res) != nil || res.ResponseHeader.Status != 0 {
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, status, strings.TrimSpace(string(b)))
	}
	return nil
}

// update sends documents or commands to the update handler of the collection
// and commits them, so that the next statement sees them.
func (a *api) update(ctx context.Context, collection string, body any) error {
	return a.ok(ctx, http.MethodPost, "/solr/"+collection+"/update?commit=true", body)
}

// schema sends a command to the Schema API of the collection.
func (a *api) schema(ctx context.Context, collection string, body any) error {
	return a.ok(ctx, http.MethodPost, "/solr/"+collection+"/schema", body)
}

// makeCollection makes a collection with a configuration set of its own, from
// the set _default.
func (a *api) makeCollection(ctx context.Context, name string) error {
	if err := a.ok(ctx, http.MethodGet, "/solr/admin/configs?action=CREATE&name="+name+"&baseConfigSet=_default", nil); err != nil {
		return err
	}
	return a.ok(ctx, http.MethodGet, "/solr/admin/collections?action=CREATE&name="+name+"&numShards=1&replicationFactor=1&collection.configName="+name, nil)
}

// dropCollection removes a collection and its configuration set.
func (a *api) dropCollection(ctx context.Context, name string) error {
	return errors.Join(
		a.ok(ctx, http.MethodGet, "/solr/admin/collections?action=DELETE&name="+name, nil),
		a.ok(ctx, http.MethodGet, "/solr/admin/configs?action=DELETE&name="+name, nil),
	)
}

// fixture makes the collections of the run, and fills them.
func fixture(ctx context.Context, a *api) error {
	for _, name := range []string{collMain, collAuthors, collBooks, collReviews, collInf} {
		if err := a.makeCollection(ctx, name); err != nil {
			return fmt.Errorf("making %s: %w", name, err)
		}
	}
	type f = map[string]any
	steps := []struct {
		collection string
		body       any
	}{
		{collMain, f{"add-field-type": []f{
			{"name": "uuid", "class": "solr.UUIDField"},
			{"name": "daterange", "class": "solr.DateRangeField"},
			{"name": "knn", "class": "solr.DenseVectorField", "vectorDimension": 3, "similarityFunction": "cosine"},
			{"name": "sortabletext", "class": "solr.SortableTextField"},
			{"name": "bbox", "class": "solr.BBoxField", "geo": "true", "numberType": "pdouble", "distanceUnits": "kilometers"},
		}}},
		{collMain, f{
			"add-field": []f{
				{"name": "n_i", "type": "pint"}, {"name": "n_l", "type": "plong"}, {"name": "n_f", "type": "pfloat"},
				{"name": "n_d", "type": "pdouble"}, {"name": "n_b", "type": "boolean"}, {"name": "n_s", "type": "string"},
				{"name": "n_t", "type": "text_general", "multiValued": false}, {"name": "n_st", "type": "sortabletext"},
				{"name": "n_dt", "type": "pdate"}, {"name": "n_bin", "type": "binary"}, {"name": "n_u", "type": "uuid"},
				{"name": "n_loc", "type": "location"}, {"name": "n_is", "type": "pints"}, {"name": "n_ls", "type": "plongs"},
				{"name": "n_ds", "type": "pdoubles"}, {"name": "n_ss", "type": "strings"}, {"name": "n_bs", "type": "booleans"},
				{"name": "n_dts", "type": "pdates"}, {"name": "n_v", "type": "knn"}, {"name": "n_dr", "type": "daterange"},
				{"name": "n_def", "type": "string", "default": "the default"}, {"name": "n_cp", "type": "strings"},
				{"name": "n_rpt", "type": "location_rpt"}, {"name": "n_bbox", "type": "bbox"}, {"name": "n_pt", "type": "point"},
			},
			"add-copy-field": f{"source": "n_s", "dest": "n_cp"},
		}},
		{collAuthors, f{"add-field": []f{{"name": "name", "type": "string"}}}},
		{collBooks, f{"add-field": []f{{"name": "title", "type": "string"}, {"name": "author_id", "type": "string"}, {"name": "pubyear", "type": "pint"}}}},
		{collReviews, f{"add-field": []f{{"name": "book_id", "type": "string"}, {"name": "stars", "type": "pint"}, {"name": "body", "type": "text_general"}}}},
		{collInf, f{"add-field": []f{{"name": "n_d", "type": "pdouble"}, {"name": "n_f", "type": "pfloat"}, {"name": "n_ds", "type": "pdoubles"}}}},
	}
	for _, s := range steps {
		if err := a.schema(ctx, s.collection, s.body); err != nil {
			return fmt.Errorf("changing the schema of %s: %w", s.collection, err)
		}
	}
	docs := []f{
		{"id": "1", "n_i": 2147483647, "n_l": int64(9223372036854775807), "n_f": 3.4028235e38, "n_d": 1.7976931348623157e308, "n_b": true,
			"n_s": "héllo wörld 日本 🙂", "n_t": "the quick brown fox", "n_st": "Sortable Text", "n_dt": "9999-12-31T23:59:59.999Z",
			"n_bin": "AAEC/w==", "n_u": "8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b", "n_loc": "45.5,-122.6", "n_is": []int{1, 2, 3},
			"n_ls": []int64{9223372036854775807, -1}, "n_ds": []float64{0.5, -0.25}, "n_ss": []string{"a", "b"},
			"n_bs": []bool{true, false}, "n_dts": []string{"2026-10-01T00:00:00Z"}, "n_x_s": "a dynamic field"},
		{"id": "2", "n_i": -2147483648, "n_l": int64(-9223372036854775808), "n_f": 0.1, "n_d": 2.0, "n_b": false, "n_s": "", "n_t": "",
			"n_dt": "0001-01-01T00:00:00Z", "n_is": []int{}, "n_ss": []string{""}},
		{"id": "3", "n_dt": "2026-10-01T12:34:56.123456789Z", "n_v": []float64{0.1, 0.2, 0.3}, "n_dr": "[2026-01-01 TO 2026-12-31]"},
		{"id": "x1", "n_rpt": "POINT(-122.6 45.5)", "n_bbox": "ENVELOPE(-10, 20, 15, 10)", "n_pt": "1.5,2.5"},
	}
	for i := range 300 {
		docs = append(docs, f{"id": fmt.Sprintf("p%03d", i), "n_i": i})
	}
	if err := a.update(ctx, collMain, docs); err != nil {
		return fmt.Errorf("adding the documents: %w", err)
	}
	if err := a.update(ctx, collInf, []f{{"id": "i1", "n_d": "Infinity", "n_f": "NaN", "n_ds": []string{"-Infinity"}}}); err != nil {
		return fmt.Errorf("adding the infinities: %w", err)
	}
	return a.ok(ctx, http.MethodGet, "/solr/admin/collections?action=CREATEALIAS&name="+collAlias+"&collections="+collMain, nil)
}

// teardown removes what fixture made.
func teardown(ctx context.Context, a *api) error {
	errs := []error{a.ok(ctx, http.MethodGet, "/solr/admin/collections?action=DELETEALIAS&name="+collAlias, nil)}
	for _, name := range []string{collMain, collAuthors, collBooks, collReviews, collInf} {
		errs = append(errs, a.dropCollection(ctx, name))
	}
	return errors.Join(errs...)
}

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run makes the collections, runs the tests, removes the collections, and
// returns the code of the exit.
func run(m *testing.M) int {
	v := os.Getenv(admin.env)
	if v == "" {
		return m.Run()
	}
	a, err := newAPI(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading the address of the administrator:", err)
		return 1
	}
	// TestMain has no context of its own.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	code := 1
	if err := fixture(ctx, a); err != nil {
		fmt.Fprintln(os.Stderr, "making the collections of the tests:", err)
	} else {
		code = m.Run()
	}
	if err := teardown(ctx, a); err != nil {
		fmt.Fprintln(os.Stderr, "removing the collections of the tests:", err)
		code = 1
	}
	return code
}

// query runs a statement and returns the names of its columns and its rows,
// each value as *any holds it. It fails the test on an error.
func query(t *testing.T, db *sql.DB, stmt string, args ...any) ([]string, [][]any) {
	t.Helper()
	cols, rows, err := tryQuery(t.Context(), db, stmt, args...)
	if err != nil {
		t.Fatalf("running %q: %v", stmt, err)
	}
	return cols, rows
}

// tryQuery runs a statement and reads every row, and returns what it read with
// the first error, from the query or from the rows.
func tryQuery(ctx context.Context, db *sql.DB, stmt string, args ...any) ([]string, [][]any, error) {
	rows, err := db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		row := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, err
		}
		out = append(out, row)
	}
	return cols, out, rows.Err()
}

// refusal returns the *solr.Error of err, and fails the test if err is nil or
// is not one.
func refusal(t *testing.T, err error) *solr.Error {
	t.Helper()
	var serr *solr.Error
	if !errors.As(err, &serr) {
		t.Fatalf("the error is %v, want a *solr.Error", err)
	}
	return serr
}

// ids returns the first column of rows as strings.
func ids(rows [][]any) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i], _ = r[0].(string)
	}
	return out
}

func utcTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

// TestIntegrationPing holds that Ping reaches the handler of SQL as each
// principal, and that a wrong password is a *solr.Error with HTTP 401 that
// holds no password (recorded: "a wrong password").
func TestIntegrationPing(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("wrong password", func(t *testing.T) {
		u, err := url.Parse(dsn(t, admin, collMain))
		if err != nil {
			t.Fatal(err)
		}
		u.User = url.UserPassword("admin", "not-the-password")
		db, err := sql.Open(solr.Name, u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		err = db.PingContext(t.Context())
		serr := refusal(t, err)
		if serr.HTTPStatus != http.StatusUnauthorized || errors.Is(err, driver.ErrBadConn) || strings.Contains(err.Error(), "not-the-password") {
			t.Errorf("the error is %v, want HTTP 401, not a bad connection, and no password", err)
		}
	})
}

// everyTypeStmt reads one field of each type of the fixture.
const everyTypeStmt = "SELECT id, n_i, n_l, n_f, n_d, n_b, n_s, n_t, n_st, n_dt, n_bin, n_u, n_loc, n_is, n_ls, n_ds, n_ss, n_bs, n_dts FROM %s WHERE id = '1' OR id = '2' OR id = '3' ORDER BY id LIMIT 3"

// TestIntegrationEveryType reads every type through Rows.Scan, the real path
// of a caller, as each principal (D166).
func TestIntegrationEveryType(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		_, got := query(t, db, fmt.Sprintf(everyTypeStmt, collMain))
		want := [][]any{
			{
				"1", int64(2147483647), int64(9223372036854775807), 3.4028235e38, 1.7976931348623157e308, true,
				"héllo wörld 日本 🙂", "the quick brown fox", "Sortable Text", utcTime("9999-12-31T23:59:59.999Z"),
				[]byte{0, 1, 2, 0xff}, uuid.MustParse("8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b"), "45.5,-122.6",
				[]any{int64(1), int64(2), int64(3)}, []any{int64(9223372036854775807), int64(-1)}, []any{0.5, -0.25},
				[]any{"a", "b"}, []any{true, false}, []any{"2026-10-01T00:00:00Z"},
			},
			{
				"2", int64(-2147483648), int64(-9223372036854775808), 0.1, 2.0, false,
				nil, nil, nil, utcTime("0001-01-01T00:00:00Z"),
				nil, nil, nil, nil, nil, nil, nil, nil, nil,
			},
			{
				"3", nil, nil, nil, nil, nil, nil, nil, nil, utcTime("2026-10-01T12:34:56.123Z"),
				nil, nil, nil, nil, nil, nil, nil, nil, nil,
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("rows are\n%#v\nwant\n%#v", got, want)
		}
	})
}

// TestIntegrationScan scans each type into the targets that a caller uses.
func TestIntegrationScan(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		var (
			id   string
			i    sql.Null[int64]
			f    float64
			b    bool
			nb   sql.Null[bool]
			ts   time.Time
			bin  []byte
			u    uuid.UUID
			nu   sql.Null[uuid.UUID]
			nstr sql.Null[string]
			num  int
			text string
		)
		err := db.QueryRowContext(t.Context(), "SELECT id, n_i, n_f, n_b, n_dt, n_bin, n_u, n_s FROM "+collMain+" WHERE id = '1' LIMIT 1").Scan(&id, &i, &f, &b, &ts, &bin, &u, &text)
		if err != nil {
			t.Fatal(err)
		}
		if id != "1" || i != (sql.Null[int64]{V: 2147483647, Valid: true}) || f != 3.4028235e38 || !b || !ts.Equal(utcTime("9999-12-31T23:59:59.999Z")) ||
			!bytes.Equal(bin, []byte{0, 1, 2, 0xff}) || u.String() != "8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b" || text != "héllo wörld 日本 🙂" {
			t.Errorf("scanned %q %v %v %v %v %v %v %q", id, i, f, b, ts, bin, u, text)
		}
		err = db.QueryRowContext(t.Context(), "SELECT n_b, n_u, n_s, n_i FROM "+collMain+" WHERE id = '3'").Scan(&nb, &nu, &nstr, &i)
		if err != nil || nb.Valid || nu.Valid || nstr.Valid || i.Valid {
			t.Errorf("scanning NULL into sql.Null gave %v %v %v %v and %v, want no value", nb, nu, nstr, i, err)
		}
		// A NULL into a plain target is an error of database/sql.
		if err := db.QueryRowContext(t.Context(), "SELECT n_i FROM "+collMain+" WHERE id = '3'").Scan(&num); err == nil {
			t.Error("scanning NULL into an int gave no error")
		}
		// A number scans into a string, as database/sql converts it.
		if err := db.QueryRowContext(t.Context(), "SELECT n_i FROM "+collMain+" WHERE id = '1'").Scan(&text); err != nil || text != "2147483647" {
			t.Errorf("scanning an int into a string gave %q and %v", text, err)
		}
	})
}

// TestIntegrationErrors holds D107 and D166: an error that the server reports
// before any row is the error of the query, and one after some rows comes from
// the rows and wraps dbimp.ErrIncomplete. No error is a bad connection.
func TestIntegrationErrors(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		for _, stmt := range []string{
			"SELEC id FROM " + collMain,
			"SELECT id FROM nothere_" + suffix + " LIMIT 1",
			"SELECT nothere FROM " + collMain + " LIMIT 1",
			"SELECT id FROM " + collMain + " WHERE",
		} {
			_, _, err := tryQuery(t.Context(), db, stmt)
			serr := refusal(t, err)
			if serr.HTTPStatus != http.StatusOK || errors.Is(err, dbimp.ErrIncomplete) || errors.Is(err, driver.ErrBadConn) {
				t.Errorf("%q gave %v, want an exception of HTTP 200 before any row", stmt, err)
			}
		}
		// The driver refuses a statement that ends inside a literal before it
		// sends it.
		if _, _, err := tryQuery(t.Context(), db, "SELECT id FROM "+collMain+" WHERE n_t = 'it's'"); !errors.Is(err, dbimp.ErrUnterminated) {
			t.Errorf("a statement that ends inside a literal gave %v, want dbimp.ErrUnterminated", err)
		}
		rows, err := db.QueryContext(t.Context(), "SELECT id, n_dr FROM "+collMain+" ORDER BY id LIMIT 3")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		err = rows.Err()
		if n != 2 || !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("read %d rows and %v, want 2 rows and an error that wraps ErrIncomplete", n, err)
		}
		serr := refusal(t, err)
		if !strings.Contains(serr.Message, "could not be parsed") {
			t.Errorf("the message is %q, want the date range that the SQL layer cannot parse", serr.Message)
		}
	})
}

// TestIntegrationContext holds D36: a context that ends stops the read of a
// result with the error of the context, and the next statement runs.
func TestIntegrationContext(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		rows, err := db.QueryContext(ctx, "SELECT id, n_i FROM "+collMain)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("reading the first row: %v", rows.Err())
		}
		cancel()
		for rows.Next() {
		}
		// The rows that the driver already read can end the result before it
		// sees the cancel, so the result of a small answer can be complete.
		if err := rows.Err(); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("the error is %v, want context.Canceled or none", err)
		}
		short, cancelShort := context.WithTimeout(t.Context(), time.Nanosecond)
		defer cancelShort()
		if _, err := db.ExecContext(short, "SELECT id FROM "+collMain+" LIMIT 1"); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("a statement past its deadline gave %v, want context.DeadlineExceeded", err)
		}
		_, got := query(t, db, "SELECT count(*) AS n FROM "+collMain)
		if len(got) != 1 {
			t.Errorf("the statement after the cancel gave %v", got)
		}
	})
}

// TestIntegrationConcurrent holds that two queries run at the same time on one
// sql.DB.
func TestIntegrationConcurrent(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		var wg sync.WaitGroup
		errs := make(chan error, 16)
		for i := range 16 {
			wg.Go(func() {
				_, rows, err := tryQuery(t.Context(), db, "SELECT id, n_i FROM "+collMain+" WHERE n_i = ?", int64(i))
				if err == nil && (len(rows) != 1 || rows[0][1] != int64(i)) {
					err = fmt.Errorf("the query for %d gave %v", i, rows)
				}
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Error(err)
			}
		}
	})
}

// TestIntegrationLargeResult reads every document through the export handler,
// which a statement with no LIMIT uses (D21): one body holds every row, and
// the driver reads it to its end.
func TestIntegrationLargeResult(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		var n int64
		if err := db.QueryRowContext(t.Context(), "SELECT count(*) AS n FROM "+collMain).Scan(&n); err != nil {
			t.Fatal(err)
		}
		_, rows := query(t, db, "SELECT id, n_i FROM "+collMain)
		if int64(len(rows)) != n || n < 303 {
			t.Errorf("read %d rows of %d documents, want every one, at least 303", len(rows), n)
		}
	})
}

// TestIntegrationArguments holds D34 and D166: the server binds no argument,
// so the driver writes each one as a literal, and a value that holds a quote, a
// backslash or a double quote reads back as it was written.
func TestIntegrationArguments(t *testing.T) {
	a := apiAs(t, admin)
	const id = "args-1"
	// A ? or a * in the text of an equality is a wildcard of the server
	// (recorded: "feature: a wildcard in equality"), so the value has none.
	odd := `it's"q"\back--x@y`
	if err := a.update(t.Context(), collMain, []map[string]any{{"id": id, "n_s": odd, "n_i": 7, "n_b": true, "n_dt": "2026-10-01T12:34:56.123Z", "n_f": 0.1}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = a.update(context.WithoutCancel(t.Context()), collMain, map[string]any{"delete": map[string]any{"id": id}})
	})
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		for _, tt := range []struct {
			where string
			args  []any
		}{
			{"n_s = ?", []any{odd}},
			{"n_i = ? AND n_s = ?", []any{int64(7), odd}},
			{"n_b = ? AND id = ?", []any{true, id}},
			{"n_dt = ? AND id = ?", []any{time.Date(2026, 10, 1, 12, 34, 56, 123_000_000, time.UTC), id}},
			{"n_dt > ? AND id = ?", []any{time.Date(2026, 1, 1, 7, 0, 0, 0, time.FixedZone("x", 7*3600)), id}},
			{"n_f = ? AND id = ?", []any{0.1, id}},
			{"id = @id", []any{sql.Named("id", id)}},
		} {
			_, got := query(t, db, "SELECT id FROM "+collMain+" WHERE "+tt.where+" LIMIT 5", tt.args...)
			if !reflect.DeepEqual(ids(got), []string{id}) {
				t.Errorf("%s with %v found %v, want %s", tt.where, tt.args, ids(got), id)
			}
		}
		_, got := query(t, db, "SELECT id FROM "+collMain+" WHERE n_s = ? LIMIT 5", "no such text")
		if len(got) != 0 {
			t.Errorf("a text that no document holds found %v", got)
		}
	})
}

// TestIntegrationPrepared holds that a prepared statement runs again with new
// arguments.
func TestIntegrationPrepared(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		st, err := db.PrepareContext(t.Context(), "SELECT n_i FROM "+collMain+" WHERE id = ?")
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		for id, want := range map[string]int64{"1": 2147483647, "2": -2147483648, "p007": 7} {
			var got int64
			if err := st.QueryRowContext(t.Context(), id).Scan(&got); err != nil || got != want {
				t.Errorf("the statement for %s gave %d and %v, want %d", id, got, err, want)
			}
		}
	})
}

// TestIntegrationOptions holds D109 and D166: WithDatabase names the collection
// of the path of one statement, and WithTimeout is refused.
func TestIntegrationOptions(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, _ *sql.DB) {
		db := openAs(t, p, collAuthors)
		// The collection of the path only names the handler, and FROM names
		// the table (recorded: "a statement sent to another collection").
		_, got := query(t, db, "SELECT id FROM "+collMain+" WHERE id = '1'")
		if !reflect.DeepEqual(ids(got), []string{"1"}) {
			t.Errorf("a statement on another collection gave %v", got)
		}
		_, got = query(t, db, "SELECT id FROM "+collMain+" WHERE id = '2'", solr.WithDatabase(collMain))
		if !reflect.DeepEqual(ids(got), []string{"2"}) {
			t.Errorf("a statement with WithDatabase gave %v", got)
		}
		if _, _, err := tryQuery(t.Context(), db, "SELECT id FROM "+collMain, solr.WithTimeout(time.Second)); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithTimeout gave %v, want dbimp.ErrNotSupported", err)
		}
		// A collection that does not exist answers an error of HTTP with a
		// page of HTML, which differs by release and user (recorded: "a
		// statement sent to a collection that does not exist").
		_, _, err := tryQuery(t.Context(), db, "SELECT id FROM "+collMain, solr.WithDatabase("nothere_"+suffix))
		if serr := refusal(t, err); serr.HTTPStatus != http.StatusForbidden && serr.HTTPStatus != http.StatusNotFound && serr.HTTPStatus != http.StatusMethodNotAllowed {
			t.Errorf("a collection that does not exist gave HTTP %d, want 403, 404 or 405", serr.HTTPStatus)
		}
	})
}

// TestIntegrationColumnTypes holds D166: the database type name is the SQL
// type of metadata.COLUMNS, and an aggregate names none.
func TestIntegrationColumnTypes(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		for _, tt := range []struct {
			stmt string
			want []string
		}{
			{"SELECT id, n_i, n_f, n_b, n_dt, n_bin, n_u, n_is FROM " + collMain + " LIMIT 1", []string{"VARCHAR", "BIGINT", "DOUBLE", "VARCHAR", "TIMESTAMP", "VARCHAR", "VARCHAR", "ANY"}},
			{"SELECT count(*) AS c, max(n_i) AS m FROM " + collMain, []string{"", ""}},
		} {
			if got := columnTypeNames(t, db, tt.stmt); !slices.Equal(got, tt.want) {
				t.Errorf("the types of the columns of %q are %q, want %q", tt.stmt, got, tt.want)
			}
		}
	})
}

// columnTypeNames returns the database type name of each column of a
// statement.
func columnTypeNames(t *testing.T, db *sql.DB, stmt string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), stmt)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil || rows.Err() != nil {
		t.Fatal(err, rows.Err())
	}
	names := make([]string, len(cts))
	for i, ct := range cts {
		names[i] = ct.DatabaseTypeName()
	}
	return names
}

// isIncomplete reports whether err wraps dbimp.ErrIncomplete.
func isIncomplete(err error) bool {
	return errors.Is(err, dbimp.ErrIncomplete)
}

// TestIntegrationVersion holds step 16 of docs/DRIVER.md: only the
// administrator reads the version, through the system handler, and the
// ordinary user gets HTTP 403. The SQL of Solr has no statement for it
// (recorded: "the version").
func TestIntegrationVersion(t *testing.T) {
	status, body, err := apiAs(t, admin).do(t.Context(), http.MethodGet, "/solr/admin/info/system?wt=json", nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("the administrator got HTTP %d and %v for the version", status, err)
	}
	var info struct {
		Lucene struct {
			Version string `json:"solr-spec-version"`
		} `json:"lucene"`
	}
	if err := json.Unmarshal(body, &info); err != nil || info.Lucene.Version == "" {
		t.Errorf("the version is %q and %v, want a version", info.Lucene.Version, err)
	}
	t.Logf("the administrator reads the version %s", info.Lucene.Version)
	status, _, err = apiAs(t, ordinary).do(t.Context(), http.MethodGet, "/solr/admin/info/system?wt=json", nil)
	if err != nil || status != http.StatusForbidden {
		t.Errorf("the ordinary user got HTTP %d and %v for the version, want 403", status, err)
	}
}
