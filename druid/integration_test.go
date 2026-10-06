package druid_test

import (
	"bytes"
	"context"
	"database/sql"
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
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/druid"
)

// These tests need a server. DRUID_DSN names the Router for the
// administrator, and DRUID_ORDINARY_DSN for the ordinary user (D9). Each is
// the url of dbrun, which is http://user:pass@host:port for Druid, and the
// tests read it as the DSN druid://user:pass@host:port. The SQL API takes no
// write (D163), so the tests write their datasources through the task API
// as the administrator, and read them through the driver as each principal.
// The ordinary user of the dbmeta entry can read every datasource and run no
// task (measured). A test skips when a variable that it needs is empty. Each
// datasource of the tests starts with a prefix of its own, which TestMain
// drops at the end.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each datasource of this run.
var prefix = "dbimp_it_" + suffix + "_"

// visible bounds the time that a write takes to answer a query, and that a
// dropped datasource takes to leave the answers.
const visible = 3 * time.Minute

// taskTimeout bounds the time of one task of the multi-stage engine.
const taskTimeout = 5 * time.Minute

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "DRUID_DSN"}
	ordinary   = principal{"ordinary", "DRUID_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// toDSN returns the URL of dbrun as the DSN of the driver. A URL whose
// scheme is already druid stays as it is.
func toDSN(v string) string {
	switch {
	case strings.HasPrefix(v, "http://"):
		return "druid://" + strings.TrimPrefix(v, "http://")
	case strings.HasPrefix(v, "https://"):
		return "druid://" + strings.TrimPrefix(v, "https://") + "?tls=true"
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
	db, err := sql.Open(druid.Name, dsn(t, p)+query)
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
		fmt.Fprintln(os.Stderr, "dropping the datasources of the tests:", err)
		code = 1
	}
	os.Exit(code)
}

// cleanup drops each datasource of this run.
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
	var names []string
	if err := s.getJSON(ctx, "/druid/coordinator/v1/datasources", &names); err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			errs = append(errs, s.drop(ctx, name))
		}
	}
	return errors.Join(errs...)
}

// api sends requests to the HTTP API of the server as the administrator, for
// what the SQL API cannot do: the tasks that write, the Coordinator, and the
// result formats that the driver does not read.
type api struct {
	base       string
	user, pass string
	client     *http.Client
}

// newAPI returns the api of the URL v of the administrator.
func newAPI(v string) (*api, error) {
	cfg, err := druid.ParseDSN(toDSN(v))
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

// newAdminAPI returns the api of the server, or skips the test when its
// address is empty.
func newAdminAPI(t *testing.T) *api {
	t.Helper()
	s, err := newAPI(dsn(t, admin))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// do sends one request with body as JSON, or no body for nil, and returns
// the status and the body of the answer.
func (s *api) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, r)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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

// getJSON sends GET to path, and decodes the answer into v.
func (s *api) getJSON(ctx context.Context, path string, v any) error {
	status, b, err := s.do(ctx, http.MethodGet, path, nil)
	switch {
	case err != nil:
		return err
	case status != http.StatusOK:
		return fmt.Errorf("GET %s: HTTP %d: %s", path, status, b)
	}
	return json.Unmarshal(b, v)
}

// submit sends query to the task API, and returns the status and the body
// of the answer. The rows of a task answer a query some seconds after it
// ends (recorded: "crud: select"), so each read after a write runs until it
// sees the write.
func (s *api) submit(ctx context.Context, query string) (int, []byte, error) {
	return s.do(ctx, http.MethodPost, "/druid/v2/sql/task", map[string]any{"query": query})
}

// task runs query as a task of the multi-stage engine, and waits until it
// ends. The nano quickstart has two slots, and a task takes both, so the
// tests run one task at a time, and a task that the test leaves is shut
// down (measured).
func (s *api) task(ctx context.Context, query string) error {
	id, err := s.start(ctx, query)
	if err != nil {
		return err
	}
	if err := s.wait(ctx, id, query); err != nil {
		// A task that runs on takes the slots of the next one.
		_, _, _ = s.do(context.WithoutCancel(ctx), http.MethodPost, "/druid/indexer/v1/task/"+url.PathEscape(id)+"/shutdown", nil)
		return err
	}
	return nil
}

// start submits query to the task API, and returns the id of its task. The
// Overlord can be out of reach for a moment under load, and the task API
// then refuses the task before it runs, with an error that names HTTP 503
// (measured). start submits such a task again, a few times.
func (s *api) start(ctx context.Context, query string) (string, error) {
	for attempt := 1; ; attempt++ {
		status, b, err := s.submit(ctx, query)
		if err != nil {
			return "", err
		}
		var answer struct {
			TaskID string `json:"taskId"`
		}
		switch {
		case status == http.StatusAccepted && json.Unmarshal(b, &answer) == nil:
			return answer.TaskID, nil
		case status >= http.StatusInternalServerError && strings.Contains(string(b), "503") && attempt < 5:
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(5 * time.Second):
			}
		default:
			return "", fmt.Errorf("submitting the task %q: HTTP %d: %s", query, status, b)
		}
	}
}

// wait reads the state of the task id until it ends.
func (s *api) wait(ctx context.Context, id, query string) error {
	ctx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()
	path := "/druid/indexer/v1/task/" + url.PathEscape(id) + "/status"
	for {
		var st struct {
			Status struct {
				StatusCode string `json:"statusCode"`
				ErrorMsg   string `json:"errorMsg"`
			} `json:"status"`
		}
		status, b, err := s.do(ctx, http.MethodGet, path, nil)
		switch {
		case err != nil:
			return fmt.Errorf("reading the state of the task %q: %w", query, err)
		case status == http.StatusServiceUnavailable:
			// The Router answers HTTP 503 for a moment when it cannot reach
			// the Overlord, under load (measured), and the task runs on.
		case status != http.StatusOK:
			return fmt.Errorf("reading the state of the task %q: HTTP %d: %s", query, status, b)
		default:
			if err := json.Unmarshal(b, &st); err != nil {
				return fmt.Errorf("reading the state of the task %q: %w", query, err)
			}
		}
		switch st.Status.StatusCode {
		case "SUCCESS":
			return nil
		case "FAILED":
			return fmt.Errorf("the task %q failed: %s", query, st.Status.ErrorMsg)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for the task %q: %w", query, ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// mustTask runs query as a task, and fails the test if it fails.
func (s *api) mustTask(t *testing.T, query string) {
	t.Helper()
	if err := s.task(t.Context(), query); err != nil {
		t.Fatal(err)
	}
}

// markUnused marks the segments of the datasource name in the interval
// unused, which drops their rows from the answers, as a delete does.
func (s *api) markUnused(ctx context.Context, name, interval string) error {
	status, b, err := s.do(ctx, http.MethodPost, "/druid/coordinator/v1/datasources/"+url.PathEscape(name)+"/markUnused",
		map[string]any{"interval": interval})
	switch {
	case err != nil:
		return err
	case status != http.StatusOK:
		return fmt.Errorf("marking %s of %s unused: HTTP %d: %s", interval, name, status, b)
	}
	return nil
}

// drop marks every segment of the datasource name unused, which drops it
// (recorded: "teardown: drop the datasource for crud").
func (s *api) drop(ctx context.Context, name string) error {
	status, b, err := s.do(ctx, http.MethodDelete, "/druid/coordinator/v1/datasources/"+url.PathEscape(name), nil)
	switch {
	case err != nil:
		return err
	case status != http.StatusOK && status != http.StatusNotFound:
		return fmt.Errorf("dropping %s: HTTP %d: %s", name, status, b)
	}
	return nil
}

// sqlQuote returns s as a literal of Druid SQL, with each ' doubled.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// waitFor runs query until its rows equal want, or the time of visible
// ends.
func waitFor(t *testing.T, db *sql.DB, query string, want [][]any) {
	t.Helper()
	var (
		got [][]any
		err error
	)
	for deadline := time.Now().Add(visible); ; {
		_, got, err = readAll(t, db, query)
		if err == nil && reflect.DeepEqual(got, want) {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%q gave %#v, %v, want %#v", query, got, err, want)
}

// typesQuery makes the datasource of every type, as the setup of step 6
// made dbimp_types (requests.json).
const typesQuery = `REPLACE INTO %s OVERWRITE ALL
SELECT TIME_PARSE(t) AS __time, id, s, l, CAST(f AS FLOAT) AS f, d, PARSE_JSON(j) AS j,
  CASE WHEN id = 1 THEN ARRAY['a', NULL, 'c'] ELSE NULL END AS sa,
  CASE WHEN id = 1 THEN ARRAY[-9223372036854775808, 9223372036854775807] ELSE NULL END AS la,
  CASE WHEN id = 1 THEN ARRAY[0.1, 1.5e300] ELSE NULL END AS da,
  CASE WHEN id = 1 THEN ARRAY_TO_MV(ARRAY['x', 'y']) WHEN id = 2 THEN 'z' ELSE NULL END AS mv,
  b
FROM (VALUES
  ('2026-10-01T12:34:56.789Z', 1, 'é''"\ x', 9223372036854775807, 3.4e38, 1.7976931348623157e308, '{"k":[1,"two",null,{"n":1.5}]}', TRUE),
  ('2026-10-01T00:00:00Z', 2, '', -9223372036854775808, -1.5, -0.1, '[]', FALSE),
  ('1970-01-01T00:00:00Z', 3, NULL, NULL, NULL, NULL, NULL, NULL)
) AS v(t, id, s, l, f, d, j, b)
PARTITIONED BY ALL`

// typesName is the datasource of every type, which typesDatasource makes
// once for the run.
var typesName = prefix + "types"

// typesMade is true once the datasource of every type exists.
var typesMade bool

// typesDatasource makes the datasource of every type, once for the run, and
// returns its name.
func typesDatasource(t *testing.T) string {
	t.Helper()
	if typesMade {
		return typesName
	}
	s := newAdminAPI(t)
	s.mustTask(t, fmt.Sprintf(typesQuery, typesName))
	waitFor(t, openAs(t, admin, ""), "SELECT COUNT(*) FROM "+typesName, [][]any{{int64(3)}})
	typesMade = true
	return typesName
}

// TestIntegrationTypes reads every type through Rows.Scan, the path of a
// caller, into the Go type of the type table and into sql.Null of it, as
// each principal (step 14).
func TestIntegrationTypes(t *testing.T) {
	name := typesDatasource(t)
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		rows, err := db.QueryContext(t.Context(), "SELECT __time, id, s, l, f, d, j, sa, la, da, mv, b, "+
			"CAST(id AS INTEGER) AS i, CAST(d AS DECIMAL(38, 10)) AS dc, CAST(__time AS DATE) AS dt, id = 1 AS bo, 'x' AS c "+
			"FROM "+name+" ORDER BY __time")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var (
				ts     time.Time
				id, i  int64
				s      sql.Null[string]
				l      sql.Null[int64]
				f, d   sql.Null[float64]
				dc     sql.Null[float64]
				j      any
				sa, mv any
				la, da any
				b      sql.Null[int64]
				dt     dbimp.Date
				bo     bool
				c      string
			)
			if err := rows.Scan(&ts, &id, &s, &l, &f, &d, &j, &sa, &la, &da, &mv, &b, &i, &dc, &dt, &bo, &c); err != nil {
				t.Fatal(err)
			}
			got = append(got, fmt.Sprintf("%s|%d|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%d|%v|%s|%t|%s",
				ts.Format(time.RFC3339Nano), id, s, l, f, d, j, sa, la, da, mv, b, i, dc, dt, bo, c))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"1970-01-01T00:00:00Z|3|{ false}|{0 false}|{0 false}|{0 false}|<nil>|<nil>|<nil>|<nil>|<nil>|{0 false}|3|{0 false}|1970-01-01|false|x",
			"2026-10-01T00:00:00Z|2|{ true}|{-9223372036854775808 true}|{-1.5 true}|{-0.1 true}|[]|<nil>|<nil>|<nil>|z|{0 true}|2|{-0.1 true}|2026-10-01|false|x",
			"2026-10-01T12:34:56.789Z|1|{é'\"\\ x true}|{9223372036854775807 true}|{3.4e+38 true}|{1.7976931348623157e+308 true}|map[k:[1 two <nil> map[n:1.5]]]|[a <nil> c]|[-9223372036854775808 9223372036854775807]|[0.1 1.5e+300]|[x y]|{1 true}|1|{1.7976931348623157e+308 true}|2026-10-01|true|x",
		}
		if !slices.Equal(got, want) {
			t.Errorf("the rows are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})
}

// TestIntegrationVersion holds the answers of step 16: the version of each
// service, which the administrator reads, and which the ordinary user is
// refused with HTTP 403 (recorded: "the version of each service" and "a
// system table that needs a privilege").
func TestIntegrationVersion(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		_, got, err := readAll(t, db, "SELECT server_type, version FROM sys.servers ORDER BY server_type")
		if p == ordinary {
			if e, ok := errors.AsType[*druid.Error](err); !ok || e.HTTPStatus != http.StatusForbidden {
				t.Errorf("the ordinary user read the version with %v, %v, want HTTP 403", got, err)
			}
			t.Logf("the ordinary user is refused the version: %v", err)
			return
		}
		if err != nil || len(got) == 0 {
			t.Fatalf("the version gave %v, %v", got, err)
		}
		t.Logf("the version of each service: %v", got)
	})
}

// TestIntegrationErrors holds that an error before any rows is the error of
// the server, that an error after some rows wraps dbimp.ErrIncomplete and
// druid.ErrCut after exactly those rows (D164), and that a wrong password is
// HTTP 401.
func TestIntegrationErrors(t *testing.T) {
	big := bigDatasource(t)
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		_, _, err := readAll(t, db, "SELEC 1")
		if e, ok := errors.AsType[*druid.Error](err); !ok || e.HTTPStatus != http.StatusBadRequest || e.Category != "INVALID_INPUT" {
			t.Errorf("a syntax error gave %v", err)
		}
		_, got, err := readAll(t, db, "SELECT n, 100 / z AS q FROM "+big+" ORDER BY __time")
		if len(got) != 397 || !errors.Is(err, dbimp.ErrIncomplete) || !errors.Is(err, druid.ErrCut) {
			t.Errorf("an error after some rows gave %d rows and %v, want 397 and then dbimp.ErrIncomplete and druid.ErrCut", len(got), err)
		}
		_, got, err = readAll(t, db, "SELECT n FROM "+big+" ORDER BY __time")
		if err != nil || len(got) != 400 {
			t.Errorf("a result of 400 rows gave %d rows and %v", len(got), err)
		}
		u, err := url.Parse(dsn(t, p))
		if err != nil {
			t.Fatal(err)
		}
		u.User = url.UserPassword(u.User.Username(), "wrong")
		wrong, err := sql.Open(druid.Name, u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer wrong.Close()
		err = wrong.PingContext(t.Context())
		if e, ok := errors.AsType[*druid.Error](err); !ok || e.HTTPStatus != http.StatusUnauthorized || strings.Contains(err.Error(), "wrong") {
			t.Errorf("a wrong password gave %v, want HTTP 401 with no password in it", err)
		}
		if err := db.PingContext(t.Context()); err != nil {
			t.Errorf("Ping: %v", err)
		}
	})
}

// bigName is the datasource of 400 rows, which bigDatasource makes once for
// the run. Its last row is on a day of its own, so a division by z, which is
// 0 in the last row, fails after the rows of the first day (measured).
var bigName = prefix + "big"

// bigMade is true once the datasource of 400 rows exists.
var bigMade bool

// bigDatasource makes the datasource of 400 rows, once for the run, as the
// setup of step 6 made dbimp_big (requests.json), and returns its name.
func bigDatasource(t *testing.T) string {
	t.Helper()
	if bigMade {
		return bigName
	}
	s := newAdminAPI(t)
	s.mustTask(t, "REPLACE INTO "+bigName+` OVERWRITE ALL
SELECT TIMESTAMPADD(SECOND, a.x * 20 + b.y + 1 + CASE WHEN a.x * 20 + b.y + 1 = 400 THEN 86400 ELSE 0 END, TIMESTAMP '2026-10-01 00:00:00') AS __time,
  a.x * 20 + b.y + 1 AS n,
  400 - (a.x * 20 + b.y + 1) AS z,
  RPAD('row', 100, 'x') AS s
FROM UNNEST(ARRAY[`+numbers(0, 19)+`]) AS a(x) CROSS JOIN UNNEST(ARRAY[`+numbers(0, 19)+`]) AS b(y)
PARTITIONED BY DAY`)
	waitFor(t, openAs(t, admin, ""), "SELECT COUNT(*) FROM "+bigName, [][]any{{int64(400)}})
	bigMade = true
	return bigName
}

// numbers returns the integers from first to last, joined by commas.
func numbers(first, last int) string {
	var s []string
	for i := first; i <= last; i++ {
		s = append(s, strconv.Itoa(i))
	}
	return strings.Join(s, ", ")
}

// slowQuery returns a query of the datasource of 400 rows that runs for
// many seconds, as the step 6 query to cancel does (requests.json).
func slowQuery(big string) string {
	return "SELECT SUM(n * x + y) AS s FROM " + big + " CROSS JOIN UNNEST(ARRAY[" + numbers(1, 500) + "]) AS a(x) CROSS JOIN UNNEST(ARRAY[" + numbers(1, 500) + "]) AS b(y)"
}
