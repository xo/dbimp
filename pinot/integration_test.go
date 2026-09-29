package pinot_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/pinot"
)

// These tests need a server. PINOT_DSN names its Broker for the
// administrator, and PINOT_ORDINARY_DSN for the ordinary user (D9). The
// Broker takes no write (D128), so the tests make their tables and load their
// rows through the Controller, which PINOT_SECOND_ADDRESS names as host:port,
// and read them through the driver. A test skips when a variable that it
// needs is empty. Each table of the tests starts with a prefix of its own,
// which TestMain drops at the end.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each table of this run.
var prefix = "dbimp_it_" + suffix + "_"

// controllerEnv names the address of the Controller.
const controllerEnv = "PINOT_SECOND_ADDRESS"

// visible bounds the time that a loaded segment takes to answer a query.
const visible = 30 * time.Second

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "PINOT_DSN"}
	ordinary   = principal{"ordinary", "PINOT_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// dsn returns the DSN of p, or skips the test when it is empty.
func dsn(t *testing.T, p principal) string {
	t.Helper()
	v := os.Getenv(p.env)
	if v == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	return v
}

// openAs opens the Broker as p, with the query of a DSN.
func openAs(t *testing.T, p principal, query string) *sql.DB {
	t.Helper()
	db, err := sql.Open(pinot.Name, dsn(t, p)+query)
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

// readsOwnTables reports whether p can read the tables of the tests. The
// ordinary user of the dbmeta entry can read only baseballStats (dbmeta
// D112), so it checks that the Broker refuses it one of them, and says so.
func readsOwnTables(t *testing.T, p principal, db *sql.DB, table string) bool {
	t.Helper()
	if p == admin {
		return true
	}
	_, _, err := readAll(t, db, "SELECT count(*) FROM "+table)
	if e, ok := errors.AsType[*pinot.Error](err); !ok || e.HTTPStatus != http.StatusForbidden {
		t.Errorf("the ordinary user read %s with %v, want HTTP 403", table, err)
	}
	t.Logf("the ordinary user can read only baseballStats, so the administrator runs the rest")
	return false
}

func TestMain(m *testing.M) {
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, "dropping the tables of the tests:", err)
		code = 1
	}
	os.Exit(code)
}

// cleanup drops each table and each schema of this run.
func cleanup() error {
	addr := os.Getenv(controllerEnv)
	if addr == "" {
		return nil
	}
	c := &controller{base: "http://" + addr}
	// TestMain has no context of its own.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var tables struct {
		Tables []string `json:"tables"`
	}
	if err := c.getJSON(ctx, "/tables", &tables); err != nil {
		return err
	}
	var schemas []string
	if err := c.getJSON(ctx, "/schemas", &schemas); err != nil {
		return err
	}
	var errs []error
	for _, name := range tables.Tables {
		if strings.HasPrefix(name, prefix) {
			errs = append(errs, c.dropTable(ctx, name))
		}
	}
	for _, name := range schemas {
		if strings.HasPrefix(name, prefix) {
			errs = append(errs, c.send(ctx, http.MethodDelete, "/schemas/"+name, nil, ""))
		}
	}
	return errors.Join(errs...)
}

// controller makes tables and loads rows through the REST API of the
// Controller of Pinot, which has no authentication in the dbmeta entry.
type controller struct {
	base string
}

// newController returns the controller of the server, or skips the test
// when its address is empty.
func newController(t *testing.T) *controller {
	t.Helper()
	addr := os.Getenv(controllerEnv)
	if addr == "" {
		t.Skipf("%s is empty, so the tests cannot make their tables", controllerEnv)
	}
	return &controller{base: "http://" + addr}
}

// send sends one request, and returns an error for a status that is not
// 2xx, with the body of the answer.
func (c *controller) send(ctx context.Context, method, path string, body io.Reader, contentType string) error {
	_, err := c.do(ctx, method, path, body, contentType)
	return err
}

// do sends one request, and returns the body of the answer.
func (c *controller) do(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= http.StatusMultipleChoices {
		return b, fmt.Errorf("%s %s: HTTP %d: %s", method, path, res.StatusCode, b)
	}
	return b, nil
}

// getJSON sends GET to path, and decodes the answer into v.
func (c *controller) getJSON(ctx context.Context, path string, v any) error {
	b, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// postJSON sends POST to path with v as JSON.
func (c *controller) postJSON(ctx context.Context, path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.send(ctx, http.MethodPost, path, bytes.NewReader(b), "application/json")
}

// makeTable makes the schema and the OFFLINE table of the same name, and
// drops both when the test ends.
func (c *controller) makeTable(t *testing.T, schema, table map[string]any) {
	t.Helper()
	name, _ := table["tableName"].(string)
	t.Cleanup(func() {
		// The context of the test ends before its cleanup runs.
		ctx := context.WithoutCancel(t.Context())
		if err := c.dropTable(ctx, name); err != nil {
			t.Errorf("dropping the table %s: %v", name, err)
		}
		if err := c.send(ctx, http.MethodDelete, "/schemas/"+name, nil, ""); err != nil {
			t.Errorf("dropping the schema %s: %v", name, err)
		}
	})
	if err := c.postJSON(t.Context(), "/schemas", schema); err != nil {
		t.Fatalf("making the schema %s: %v", name, err)
	}
	if err := c.postJSON(t.Context(), "/tables", table); err != nil {
		t.Fatalf("making the table %s: %v", name, err)
	}
}

// dropTable drops the OFFLINE table name.
func (c *controller) dropTable(ctx context.Context, name string) error {
	return c.send(ctx, http.MethodDelete, "/tables/"+name+"?type=offline", nil, "")
}

// load loads rows into the table as one segment, named segment, or named by
// the Controller when segment is empty. A second load with the same name
// replaces the segment, and so its rows (measured).
func (c *controller) load(ctx context.Context, table, segment string, rows []map[string]any) error {
	cfg := map[string]string{"inputFormat": "json"}
	if segment != "" {
		cfg["segmentNameGenerator.type"] = "fixed"
		cfg["segmentNameGenerator.configs.segment.name"] = segment
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "rows.json")
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	q := url.Values{"tableNameWithType": {table + "_OFFLINE"}, "batchConfigMapStr": {string(cfgJSON)}}
	return c.send(ctx, http.MethodPost, "/ingestFromFile?"+q.Encode(), &body, w.FormDataContentType())
}

// dropSegment drops the segment of the table.
func (c *controller) dropSegment(ctx context.Context, table, segment string) error {
	return c.send(ctx, http.MethodDelete, "/segments/"+table+"/"+url.PathEscape(segment)+"?type=OFFLINE", nil, "")
}

// field returns the specification of a dimension column.
func field(name, typ string, multi bool) map[string]any {
	f := map[string]any{"name": name, "dataType": typ}
	if multi {
		f["singleValueField"] = false
	}
	return f
}

// offline returns the configuration of an OFFLINE table, with extra added.
func offline(name string, extra map[string]any) map[string]any {
	t := map[string]any{
		"tableName": name, "tableType": "OFFLINE",
		"segmentsConfig":   map[string]any{"replication": "1"},
		"tableIndexConfig": map[string]any{"loadMode": "MMAP"},
		"tenants":          map[string]any{}, "metadata": map[string]any{},
	}
	maps.Copy(t, extra)
	return t
}

// waitRows reads query until it returns n rows, and returns them.
func waitRows(t *testing.T, db *sql.DB, query string, n int) [][]any {
	t.Helper()
	deadline := time.Now().Add(visible)
	for {
		_, got, err := readAll(t, db, query)
		if err == nil && len(got) == n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q gave %d rows and %v after %v, want %d rows", query, len(got), err, visible, n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// code returns the code of the error of the server, or 0.
func code(err error) int {
	if e, ok := errors.AsType[*pinot.Error](err); ok {
		return e.Code
	}
	return 0
}

// refused runs stmt and checks that the server refuses it with the code.
func refused(t *testing.T, db *sql.DB, stmt string, want int, args ...any) {
	t.Helper()
	_, _, err := readAll(t, db, stmt, args...)
	if got := code(err); got != want {
		t.Errorf("%q gave %v, want the code %d", stmt, err, want)
	}
}

// release returns the release of the server, from the Controller, such as
// 1.5.1.
func release(t *testing.T, c *controller) string {
	t.Helper()
	var v map[string]string
	if err := c.getJSON(t.Context(), "/version", &v); err != nil {
		t.Fatal(err)
	}
	for _, s := range v {
		r, _, _ := strings.Cut(s, "-")
		return r
	}
	t.Fatal("the Controller named no version")
	return ""
}

func TestIntegrationConnect(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
		_, got, err := readAll(t, db, "SELECT playerName, yearID FROM baseballStats ORDER BY playerID, yearID LIMIT 2")
		want := [][]any{{"David Allan", int64(2004)}, {"David Allan", int64(2006)}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("the first rows are %v, %v, want %v", got, err, want)
		}
	})
}

// TestIntegrationErrors holds the errors of the server, which arrive with
// HTTP 200, and a wrong password, which is HTTP 401.
func TestIntegrationErrors(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		refused(t, db, "SELECT * FROM dbimp_none", 190)
		refused(t, db, "SELEC 1", 150)
		refused(t, db, "SELECT 1 FROM baseballStats LIMIT 1; SELECT 2 FROM baseballStats LIMIT 1", 150)
		refused(t, db, "SELECT playerName FROM baseballStats WHERE playerID = ? LIMIT 1", 450)
		u, err := url.Parse(dsn(t, p))
		if err != nil {
			t.Fatal(err)
		}
		pass, _ := u.User.Password()
		u.User = url.UserPassword(u.User.Username(), pass+"-wrong")
		wrong, err := sql.Open(pinot.Name, u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer wrong.Close()
		err = wrong.PingContext(t.Context())
		if e, ok := errors.AsType[*pinot.Error](err); !ok || e.HTTPStatus != http.StatusUnauthorized {
			t.Errorf("a wrong password gave %v, want HTTP 401", err)
		}
		if strings.Contains(fmt.Sprint(err), pass) {
			t.Errorf("the error %v holds the password", err)
		}
	})
}

// TestIntegrationLimit holds D131: a query with no LIMIT returns every row on
// the multi-stage engine, and 10 on the single-stage engine.
func TestIntegrationLimit(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		const q = "SELECT DISTINCT yearID FROM baseballStats ORDER BY yearID"
		_, multi, err := readAll(t, db, q)
		if err != nil || len(multi) != 143 {
			t.Errorf("the multi-stage engine gave %d rows, %v, want 143", len(multi), err)
		}
		_, one, err := readAll(t, db, q, pinot.WithEngine(pinot.EngineSingle))
		if err != nil || len(one) != 10 {
			t.Errorf("the single-stage engine gave %d rows, %v, want 10", len(one), err)
		}
		_, all, err := readAll(t, db, "SELECT playerID FROM baseballStats")
		if err != nil || len(all) != 97889 {
			t.Errorf("the whole table gave %d rows, %v, want 97889", len(all), err)
		}
	})
}

// slow is a query of about five seconds that uses little memory, and that
// finds no row. Its argument is a marker, which names it in GET /queries.
const slow = "SET maxRowsInJoin = 2000000000; SELECT count(*) FROM baseballStats a JOIN baseballStats b ON a.yearID = b.yearID " +
	"WHERE strpos(concat(a.playerID, b.playerID, '-'), ?) > 0"

// TestIntegrationCancel holds D133: when the context ends before the answer,
// the driver cancels the query, and GET /queries of the Broker stops naming
// it at once. With cancel=none, the query runs on. The Broker of 1.4.0 has
// cancellation off, so there every query runs on (measured).
func TestIntegrationCancel(t *testing.T) {
	c := newController(t)
	if release(t, c) == "1.4.0" {
		t.Skip("the Broker of 1.4.0 has query cancellation off, so a cancel fails with HTTP 500 (measured)")
	}
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		for _, how := range []string{pinot.CancelKill, pinot.CancelNone} {
			marker := "dbimp-" + suffix + "-" + p.name + "-" + how
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			_, err := db.ExecContext(ctx, slow, marker, pinot.WithCancel(how))
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s: the query gave %v, want context.DeadlineExceeded", how, err)
			}
			time.Sleep(500 * time.Millisecond)
			n := running(t, p, marker)
			switch {
			case how == pinot.CancelKill && n != 0:
				t.Errorf("GET /queries names the query %d times after its cancel, want none", n)
			case how == pinot.CancelNone && n != 1:
				t.Errorf("GET /queries names the query %d times with cancel=none, want once, because it runs on", n)
			}
		}
	})
}

// running returns how many running queries of the Broker hold text.
func running(t *testing.T, p principal, text string) int {
	t.Helper()
	u, err := url.Parse(dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+u.Host+"/queries", nil)
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := u.User.Password()
	req.SetBasicAuth(u.User.Username(), pass)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), text)
}

// TestIntegrationTimeout holds that WithTimeout reaches the server, which
// stops the query with 250.
func TestIntegrationTimeout(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		_, _, err := readAll(t, db, slow, "dbimp-"+suffix+"-timeout-"+p.name, pinot.WithTimeout(500*time.Millisecond))
		if code(err) != 250 {
			t.Errorf("a timeout of 500 ms gave %v, want 250", err)
		}
	})
}

// TestIntegrationOptions holds the options that the Broker refuses or that
// the driver refuses, on a real server.
func TestIntegrationOptions(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		if _, err := db.ExecContext(t.Context(), "SELECT 1", pinot.WithDatabase("x")); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithDatabase gave %v, want dbimp.ErrNotSupported", err)
		}
		if _, err := db.ExecContext(t.Context(), "SELECT 1", pinot.WithReadonly(true)); err != nil {
			t.Errorf("WithReadonly gave %v, want nothing, because every statement is read-only", err)
		}
		if _, err := db.ExecContext(t.Context(), "SELECT 1", pinot.WithParameter("trace", true)); err != nil {
			t.Errorf("WithParameter(trace) gave %v", err)
		}
	})
}
