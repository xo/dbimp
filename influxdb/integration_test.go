package influxdb //nolint:testpackage // The tests build the URL of the server from the configuration of the driver.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
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
)

// These tests need a server. INFLUXDB_DSN names it for the administrator,
// and INFLUXDB_ORDINARY_DSN for the ordinary user, which InfluxDB 3 Core does
// not have (D9). Each DSN names the database dbmeta. A test skips when its
// DSN is empty. Each measurement that the tests write has a prefix of its
// own, and TestMain removes each one at the end.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// made holds each measurement that the tests wrote, for TestMain to remove.
var made struct {
	mu    sync.Mutex
	names []string
}

// measurement returns the name of a measurement of the tests.
func measurement(name string) string {
	m := "dbimp_it_" + suffix + "_" + name
	made.mu.Lock()
	defer made.mu.Unlock()
	if !slices.Contains(made.names, m) {
		made.names = append(made.names, m)
	}
	return m
}

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "INFLUXDB_DSN"}
	ordinary   = principal{"ordinary", "INFLUXDB_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// config returns the configuration of the DSN of p, or skips the test when
// its DSN is empty.
func config(t *testing.T, p principal) Config {
	t.Helper()
	dsn := os.Getenv(p.env)
	if dsn == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	cfg, err := ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	return *cfg
}

// openWith opens the server as p, with the configuration that f changes.
func openWith(t *testing.T, p principal, f func(*Config)) *sql.DB {
	t.Helper()
	cfg := config(t, p)
	if f != nil {
		f(&cfg)
	}
	db := sql.OpenDB(NewConnector(cfg))
	t.Cleanup(func() { db.Close() })
	return db
}

// release returns the major release of the server of p.
func release(t *testing.T, p principal) int {
	t.Helper()
	db := openWith(t, p, nil)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var v string
	if err := conn.Raw(func(dc any) error {
		v, err = Version(t.Context(), dc)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	n, err := major(v)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// dialects returns the dialects that the server of p speaks: InfluxQL, and
// SQL on InfluxDB 3 and later (D78).
func dialects(t *testing.T, p principal) []string {
	t.Helper()
	if release(t, p) >= 3 {
		return []string{InfluxQL, SQL}
	}
	return []string{InfluxQL}
}

// openDialect opens the server as p in the dialect d, with the key version
// set to its release, so that InfluxQL asks InfluxDB 1 for chunks (D83).
func openDialect(t *testing.T, p principal, d string, f func(*Config)) *sql.DB {
	t.Helper()
	n := release(t, p)
	return openWith(t, p, func(cfg *Config) {
		cfg.SQLMode, cfg.Version = SQLModeDisable, n
		if d == SQL {
			cfg.SQLMode = SQLModeAllow
		}
		if f != nil {
			f(cfg)
		}
	})
}

// forEach runs f as a subtest for each principal and each dialect.
func forEach(t *testing.T, f func(t *testing.T, p principal, d string, db *sql.DB)) {
	t.Helper()
	for _, p := range principals {
		t.Run(p.name, func(t *testing.T) {
			for _, d := range dialects(t, p) {
				t.Run(d, func(t *testing.T) {
					f(t, p, d, openDialect(t, p, d, nil))
				})
			}
		})
	}
}

// write writes lines of line protocol as the administrator, through /write,
// which every release has (measured). This helper writes without the
// driver.
func write(t *testing.T, lines ...string) {
	t.Helper()
	if err := writeLines(t.Context(), config(t, admin), strings.Join(lines, "\n")); err != nil {
		t.Fatal(err)
	}
}

// writeLines sends lines of line protocol to the server of cfg.
func writeLines(ctx context.Context, cfg Config, lines string) error {
	return call(ctx, cfg, http.MethodPost, "/write?db="+url.QueryEscape(cfg.Database), lines)
}

// call sends one request with the credentials of cfg, and returns an error
// for a status that is not 2xx.
func call(ctx context.Context, cfg Config, method, path, body string) error {
	c := NewConnector(cfg)
	defer c.transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	// The API of InfluxDB 3 takes JSON, and /write takes line protocol.
	if strings.HasPrefix(body, "{") {
		req.Header.Set("Content-Type", "application/json")
	}
	c.auth(req)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if err := checkStatus(res); err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	_, err = io.Copy(io.Discard, res.Body)
	return err
}

func TestMain(m *testing.M) {
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

// cleanup removes each measurement that the tests wrote: with DROP
// MEASUREMENT on InfluxDB 1 and 2, and with the table API on InfluxDB 3,
// which has no DROP MEASUREMENT (measured).
func cleanup() error {
	dsn := os.Getenv(admin.env)
	if dsn == "" || len(made.names) == 0 {
		return nil
	}
	cfg, err := ParseDSN(dsn)
	if err != nil {
		return err
	}
	// TestMain has no context of its own.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db := sql.OpenDB(NewConnector(*cfg))
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var dialect string
	if err := conn.Raw(func(dc any) error {
		dialect, err = Dialect(dc)
		return err
	}); err != nil {
		return err
	}
	var errs []error
	for _, m := range made.names {
		if dialect == SQL {
			path := "/api/v3/configure/table?db=" + url.QueryEscape(cfg.Database) + "&table=" + url.QueryEscape(m)
			err := call(ctx, *cfg, http.MethodDelete, path, "")
			// A test names some measurements that it never writes, such as
			// one that does not exist.
			if ierr, ok := errors.AsType[*Error](err); ok && ierr.HTTPStatus == http.StatusNotFound {
				err = nil
			}
			errs = append(errs, err)
			continue
		}
		_, err := conn.ExecContext(ctx, `DROP MEASUREMENT "`+m+`"`)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// quoted returns a measurement quoted for the dialect d.
func quoted(_, m string) string {
	return `"` + m + `"`
}

func TestIntegrationConnect(t *testing.T) {
	for _, p := range principals {
		t.Run(p.name, func(t *testing.T) {
			db := openWith(t, p, nil)
			if err := db.PingContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var d, v string
			if err := conn.Raw(func(dc any) error {
				if d, err = Dialect(dc); err != nil {
					return err
				}
				v, err = Version(t.Context(), dc)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			n, err := major(v)
			if err != nil {
				t.Fatal(err)
			}
			want := InfluxQL
			if n >= 3 {
				want = SQL
			}
			if d != want {
				t.Errorf("InfluxDB %s speaks %q with sqlmode=prefer, want %q (D78)", v, d, want)
			}
			req := openWith(t, p, func(cfg *Config) { cfg.SQLMode = SQLModeRequire })
			err = req.PingContext(t.Context())
			switch {
			case n >= 3 && err != nil:
				t.Errorf("sqlmode=require on InfluxDB %s: %v", v, err)
			case n < 3 && !errors.Is(err, dbimp.ErrNotSupported):
				t.Errorf("sqlmode=require on InfluxDB %s gave %v, want dbimp.ErrNotSupported (D78)", v, err)
			}
		})
	}
}

func TestIntegrationTypes(t *testing.T) {
	m := measurement("types")
	n := release(t, admin)
	lines := []string{
		m + `,host=a f=1.5,i=9223372036854775807i,s="text é",b=true 1700000000000000000`,
		m + `,host=b f=0,i=-9223372036854775808i,s="",b=false 1700000001000000000`,
		m + `,host=c,region=x f=-2.25 1700000002000000000`,
	}
	if n >= 2 {
		// InfluxDB 1 has no unsigned integer (measured).
		lines = append(lines, m+`,host=d u=18446744073709551615u 1700000003000000000`)
	}
	write(t, lines...)
	at := func(sec int64) time.Time { return time.Unix(sec, 0).UTC() }
	forEach(t, func(t *testing.T, _ principal, d string, db *sql.DB) {
		q := `SELECT time, host, region, f, i, s, b FROM ` + quoted(d, m) + ` ORDER BY time`
		if d == InfluxQL {
			q = `SELECT "host", "region", "f", "i", "s", "b" FROM ` + quoted(d, m)
		}
		rows, err := db.QueryContext(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		type row struct {
			at     time.Time
			host   string
			region sql.Null[string]
			f      sql.Null[float64]
			i      sql.Null[int64]
			s      sql.Null[string]
			b      sql.Null[bool]
		}
		var got []row
		for rows.Next() {
			var r row
			dests := []any{&r.at, &r.host, &r.region, &r.f, &r.i, &r.s, &r.b}
			if d == InfluxQL {
				var name string
				dests = append([]any{&name}, dests...)
			}
			if err := rows.Scan(dests...); err != nil {
				t.Fatal(err)
			}
			got = append(got, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		want := []row{
			{at(1700000000), "a", sql.Null[string]{}, sql.Null[float64]{V: 1.5, Valid: true},
				sql.Null[int64]{V: math.MaxInt64, Valid: true}, sql.Null[string]{V: "text é", Valid: true}, sql.Null[bool]{V: true, Valid: true}},
			{at(1700000001), "b", sql.Null[string]{}, sql.Null[float64]{Valid: true},
				sql.Null[int64]{V: math.MinInt64, Valid: true}, sql.Null[string]{Valid: true}, sql.Null[bool]{Valid: true}},
			{at(1700000002), "c", sql.Null[string]{V: "x", Valid: true}, sql.Null[float64]{V: -2.25, Valid: true},
				sql.Null[int64]{}, sql.Null[string]{}, sql.Null[bool]{}},
		}
		if n >= 2 && d == SQL {
			want = append(want, row{at: at(1700000003), host: "d"})
		}
		same := func(a, b row) bool {
			at := a.at.Equal(b.at)
			a.at, b.at = time.Time{}, time.Time{}
			return at && a == b
		}
		if !slices.EqualFunc(got, want, same) {
			t.Errorf("the rows are\n%+v\nwant\n%+v", got, want)
		}
		if n < 2 {
			return
		}
		q = `SELECT u FROM ` + quoted(d, m) + ` WHERE host = 'd'`
		var u any
		dests := []any{&u}
		if d == InfluxQL {
			var name, ts any
			dests = []any{&name, &ts, &u}
		}
		if err := db.QueryRowContext(t.Context(), q).Scan(dests...); err != nil {
			t.Fatal(err)
		}
		if u != uint64(math.MaxUint64) {
			t.Errorf("the unsigned integer is %#v, want %d", u, uint64(math.MaxUint64))
		}
	})
}

func TestIntegrationSeries(t *testing.T) {
	m := measurement("series")
	write(t, m+",host=a v=1i 1700000000000000000", m+",host=b v=2i 1700000000000000000")
	forEach(t, func(t *testing.T, _ principal, d string, db *sql.DB) {
		if d == SQL {
			t.Skip("SQL has no series")
		}
		rows, err := db.QueryContext(t.Context(), `SELECT v FROM `+quoted(d, m)+` GROUP BY host`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var hosts []string
		for {
			cols, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cols, []string{"measurement", "host", "time", "v"}) {
				t.Errorf("a series has the columns %q, want measurement, host, time, v (D96)", cols)
			}
			for rows.Next() {
				var (
					name, host string
					at         time.Time
					v          int64
				)
				if err := rows.Scan(&name, &host, &at, &v); err != nil {
					t.Fatal(err)
				}
				hosts = append(hosts, host)
			}
			if !rows.NextResultSet() {
				break
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(hosts, []string{"a", "b"}) {
			t.Errorf("the series are %q, want a and b, one set for each", hosts)
		}
	})
}

// sets reads every result set of a query, as the number of rows of each.
func sets(t *testing.T, db *sql.DB, q string) ([]int, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var counts []int
	for {
		n := 0
		for rows.Next() {
			n++
		}
		if err := rows.Err(); err != nil {
			return append(counts, n), err
		}
		counts = append(counts, n)
		if !rows.NextResultSet() {
			return counts, rows.Err()
		}
	}
}

func TestIntegrationStatements(t *testing.T) {
	m := measurement("statements")
	write(t, m+" v=1i 1700000000000000000")
	for _, chunked := range []string{ChunkedPrefer, ChunkedDisable} {
		t.Run("chunked="+chunked, func(t *testing.T) {
			db := openDialect(t, admin, InfluxQL, func(cfg *Config) { cfg.Chunked = chunked })
			none := measurement("none")
			got, err := sets(t, db, `SELECT * FROM "`+none+`"; SELECT v FROM "`+m+`"; SELECT * FROM "`+none+`"`)
			if err != nil {
				t.Fatal(err)
			}
			// A statement with no series is an empty set, in each form (D83).
			if !slices.Equal(got, []int{0, 1, 0}) {
				t.Errorf("the sets have %v rows, want 0, 1 and 0 (D83)", got)
			}
		})
	}
	if release(t, admin) != 2 {
		return
	}
	t.Run("the gaps of InfluxDB 2", func(t *testing.T) {
		gap := measurement("gap")
		write(t, gap+" v=1i 1700000000000000000")
		db := openDialect(t, admin, InfluxQL, nil)
		got, err := sets(t, db, `DELETE FROM "`+gap+`"; SELECT v FROM "`+m+`"; DROP MEASUREMENT "`+gap+`"; SELECT v FROM "`+m+`"`)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []int{0, 1, 0, 1}) {
			t.Errorf("the sets have %v rows, want 0, 1, 0 and 1, with an empty set for each result that is missing (D83)", got)
		}
	})
}

func TestIntegrationParameters(t *testing.T) {
	m := measurement("params")
	write(t, m+",host=a v=1.5 1700000000000000000", m+",host=b v=2.5 1700000001000000000")
	forEach(t, func(t *testing.T, _ principal, d string, db *sql.DB) {
		q := `SELECT v FROM ` + quoted(d, m) + ` WHERE host = $h AND time >= $t`
		dests := func(v *float64) []any { return []any{v} }
		if d == InfluxQL {
			dests = func(v *float64) []any { return []any{new(any), new(any), v} }
		}
		var v float64
		err := db.QueryRowContext(t.Context(), q, sql.Named("h", "b"), sql.Named("t", time.Unix(1700000000, 0).UTC())).Scan(dests(&v)...)
		if err != nil {
			t.Fatal(err)
		}
		if v != 2.5 {
			t.Errorf("the named parameters gave %v, want 2.5", v)
		}
		if d == SQL {
			if err := db.QueryRowContext(t.Context(), `SELECT v FROM `+quoted(d, m)+` WHERE host = $1`, "a").Scan(&v); err != nil {
				t.Fatal(err)
			}
			if v != 1.5 {
				t.Errorf("the positional parameter gave %v, want 1.5", v)
			}
		}
	})
}

func TestIntegrationErrors(t *testing.T) {
	m := measurement("big")
	var b bytes.Buffer
	for i := range 12000 {
		fmt.Fprintf(&b, "%s n=%di %d\n", m, i, int64(1600000000+i)*1e9)
	}
	write(t, b.String())
	forEach(t, func(t *testing.T, _ principal, d string, db *sql.DB) {
		_, err := sets(t, db, `SELECT nope(n) FROM `+quoted(d, m))
		if _, ok := errors.AsType[*Error](err); !ok {
			t.Errorf("an error before any rows gave %v, want an *Error", err)
		}
		if d == SQL {
			got, err := sets(t, db, `SELECT n / (n - 10000) AS c FROM `+quoted(d, m)+` ORDER BY time`)
			if !errors.Is(err, dbimp.ErrIncomplete) || len(got) != 1 || got[0] == 0 {
				t.Errorf("an error after %v rows gave %v, want some rows and then dbimp.ErrIncomplete", got, err)
			}
			return
		}
		got, err := sets(t, db, `SELECT n FROM "`+m+`" LIMIT 2; SELECT nope(n) FROM "`+m+`"`)
		ierr, ok := errors.AsType[*Error](err)
		if !ok || ierr.Statement != 1 || len(got) == 0 || got[0] != 2 {
			t.Errorf("an error in the second statement gave %v after %v, want the two rows and then the error of statement 1", err, got)
		}
		got, err = sets(t, db, `SELECT n FROM "`+m+`"`)
		if err != nil || len(got) != 1 || got[0] != 12000 {
			t.Errorf("12000 rows gave %v and %v, want one set of 12000 rows, in chunks on InfluxDB 1 (D83)", got, err)
		}
	})
}

func TestIntegrationCancel(t *testing.T) {
	m := measurement("cancel")
	var b bytes.Buffer
	for i := range 20000 {
		fmt.Fprintf(&b, "%s n=%di %d\n", m, i, int64(1600000000+i)*1e9)
	}
	write(t, b.String())
	forEach(t, func(t *testing.T, _ principal, d string, db *sql.DB) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		rows, err := db.QueryContext(ctx, `SELECT n FROM `+quoted(d, m))
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		cancel()
		for rows.Next() {
		}
		// A small answer can arrive whole before the cancel, and then the
		// rows end with no error.
		if err := rows.Err(); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled read gave %v, want context.Canceled", err)
		}
	})
}

func TestIntegrationOrdinary(t *testing.T) {
	db := openDialect(t, ordinary, InfluxQL, nil)
	m := measurement("ordinary")
	write(t, m+" v=1i 1700000000000000000")
	var name string
	var at time.Time
	var v int64
	if err := db.QueryRowContext(t.Context(), `SELECT v FROM "`+m+`"`).Scan(&name, &at, &v); err != nil {
		t.Fatal(err)
	}
	_, err := db.ExecContext(t.Context(), `DROP MEASUREMENT "`+m+`"`)
	if _, ok := errors.AsType[*Error](err); !ok {
		t.Errorf("DROP MEASUREMENT as the ordinary user gave %v, want the refusal of the server", err)
	}
}

func TestIntegrationDescribeDisable(t *testing.T) {
	if release(t, admin) < 3 {
		t.Skip("SQL needs InfluxDB 3 or later")
	}
	m := measurement("describe")
	write(t, m+" a=1i,b=2i 1700000000000000000")
	db := openDialect(t, admin, SQL, func(cfg *Config) { cfg.Describe = DescribeDisable })
	rows, err := db.QueryContext(t.Context(), `SELECT a, b FROM "`+m+`"`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cols, []string{"a", "b"}) {
		t.Errorf("the columns are %q, want a and b from the first row (D77)", cols)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationNotFinite(t *testing.T) {
	if release(t, admin) < 3 {
		t.Skip("SQL needs InfluxDB 3 or later")
	}
	db := openDialect(t, admin, SQL, nil)
	var nan, inf, ninf float64
	var null sql.Null[float64]
	q := `SELECT 'NaN'::DOUBLE AS nan, 'inf'::DOUBLE AS inf, '-inf'::DOUBLE AS ninf, CAST(NULL AS DOUBLE) AS n`
	if err := db.QueryRowContext(t.Context(), q).Scan(&nan, &inf, &ninf, &null); err != nil {
		t.Fatal(err)
	}
	// The JSON writes all three as an explicit null, which is a NaN and not
	// a NULL, and a NULL leaves out its key (D80).
	for _, f := range []float64{nan, inf, ninf} {
		if !math.IsNaN(f) {
			t.Errorf("a value that is not finite is %v, want NaN (D80)", f)
		}
	}
	if null.Valid {
		t.Errorf("a NULL is %v, want NULL", null)
	}
}
