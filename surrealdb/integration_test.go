package surrealdb //nolint:testpackage // The round trip writes its literals with the literal writer of the driver.

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// These tests need a server. SURREALDB_DSN names it for the administrator,
// and SURREALDB_ORDINARY_DSN for the ordinary user (D9). A test skips when
// its DSN is empty. The tables of the tests have a prefix of their own, which
// TestMain removes at the end. A statement names its columns in the order of
// their names, because the server sorts them (D52).

// prefix starts the name of each table, function and param that the tests
// make.
var prefix = "dbimp_it_" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// tbl returns the name of a table of the tests.
func tbl(name string) string {
	return prefix + "_" + name
}

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var principals = []principal{
	{"administrator", "SURREALDB_DSN"},
	{"ordinary", "SURREALDB_ORDINARY_DSN"},
}

// openAs opens the server as p, or skips the test when its DSN is empty.
func openAs(t *testing.T, p principal) *sql.DB {
	t.Helper()
	dsn := os.Getenv(p.env)
	if dsn == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	db, err := sql.Open(Name, dsn)
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
			f(t, p, openAs(t, p))
		})
	}
}

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

// run makes the tables of the tests when SURREALDB_DSN names a server, runs
// the tests, and removes every table, function and param with the prefix.
func run(m *testing.M) (int, error) {
	dsn := os.Getenv("SURREALDB_DSN")
	if dsn == "" {
		return m.Run(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := sql.Open(Name, dsn)
	if err != nil {
		return 1, err
	}
	defer db.Close()
	for _, name := range []string{"people", "knows", "types"} {
		if _, err := db.ExecContext(ctx, "DEFINE TABLE "+tbl(name)+" SCHEMALESS"); err != nil {
			return 1, fmt.Errorf("making the table %s: %w", tbl(name), err)
		}
	}
	code := m.Run()
	left, err := leftovers(ctx, db)
	if err != nil {
		return 1, err
	}
	for _, stmt := range left {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return 1, fmt.Errorf("removing what the tests left: %s: %w", stmt, err)
		}
	}
	left, err = leftovers(ctx, db)
	if err != nil {
		return 1, fmt.Errorf("listing what the tests left: %w", err)
	}
	if len(left) > 0 {
		return 1, fmt.Errorf("removing what the tests made: %q is left after the cleanup", left)
	}
	return code, nil
}

// leftovers returns the statements that remove each table, function, param,
// analyzer and sequence whose name starts with the prefix.
func leftovers(ctx context.Context, db *sql.DB) ([]string, error) {
	info, err := object(ctx, db, "INFO FOR DB")
	if err != nil {
		return nil, fmt.Errorf("reading the database: %w", err)
	}
	var out []string
	for kind, stmt := range map[string]string{
		"tables":    "REMOVE TABLE %s",
		"functions": "REMOVE FUNCTION fn::%s",
		"params":    "REMOVE PARAM $%s",
		"analyzers": "REMOVE ANALYZER %s",
		"sequences": "REMOVE SEQUENCE %s",
	} {
		defs, _ := info[kind].(map[string]any)
		for name := range defs {
			if strings.HasPrefix(name, prefix) {
				out = append(out, fmt.Sprintf(stmt, name))
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

// object returns the one row of stmt, which returns one object, by its
// columns (D52).
func object(ctx context.Context, db *sql.DB, stmt string) (map[string]any, error) {
	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("reading %s: %w", stmt, err)
		}
		return nil, fmt.Errorf("reading %s: it returned no row", stmt)
	}
	vals := make([]any, len(cols))
	dest := make([]any, len(cols))
	for i := range dest {
		dest[i] = &vals[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	m := make(map[string]any, len(cols))
	for i, c := range cols {
		m[c] = vals[i]
	}
	return m, rows.Err()
}

// release returns the major and the minor release of the server, from
// GET /version.
func release(t *testing.T) (int, int) {
	t.Helper()
	cfg, err := ParseDSN(os.Getenv("SURREALDB_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	v := get(t, cfg.baseURL()+"/version")
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimPrefix(v, "surrealdb-"), "%d.%d", &major, &minor); err != nil {
		t.Fatalf("reading the version %q: %v", v, err)
	}
	return major, minor
}

// since skips the test on a release older than major.minor.
func since(t *testing.T, major, minor int, what string) {
	t.Helper()
	if m, n := release(t); m < major || m == major && n < minor {
		t.Skipf("%s arrived in %d.%d, and the server is %d.%d (docs/SURREALDB.md)", what, major, minor, m, n)
	}
}

// is3 reports whether the server is a release of the 3.x line.
func is3(t *testing.T) bool {
	t.Helper()
	m, _ := release(t)
	return m >= 3
}

// refusesOtherDatabase reports whether the server refuses a user of a
// database that names another one, which 3.3.0 does, and 3.2.4 and earlier
// do not.
func refusesOtherDatabase(t *testing.T) bool {
	t.Helper()
	major, minor := release(t)
	return major > 3 || major == 3 && minor >= 3
}

// get returns the body of a GET of url.
func get(t *testing.T, url string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// query returns the rows of each result set of stmt, each value read into
// *any, or fails the test.
func query(t *testing.T, db *sql.DB, stmt string, args ...any) [][][]any {
	t.Helper()
	all, err := queryErr(t, db, stmt, args...)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return all
}

// queryErr returns the rows of each result set of stmt, and the error that
// ends them.
func queryErr(t *testing.T, db *sql.DB, stmt string, args ...any) ([][][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all [][][]any
	for {
		cols, err := rows.Columns()
		if err != nil {
			return all, err
		}
		var set [][]any
		for rows.Next() {
			row := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range dest {
				dest[i] = &row[i]
			}
			if err := rows.Scan(dest...); err != nil {
				return append(all, set), err
			}
			set = append(set, row)
		}
		all = append(all, set)
		if err := rows.Err(); err != nil {
			return all, err
		}
		if !rows.NextResultSet() {
			return all, rows.Err()
		}
	}
}

// last returns the rows of the last result set.
func last(all [][][]any) [][]any {
	if len(all) == 0 {
		return nil
	}
	return all[len(all)-1]
}

// exec runs stmt, or fails the test.
func exec(t *testing.T, db *sql.DB, stmt string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), stmt, args...); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

// refused fails the test unless err is an error of the server whose message
// holds text.
func refused(t *testing.T, what string, err error, text string) {
	t.Helper()
	var e Error
	var se *dbimp.StatusError
	switch {
	case errors.As(err, &e) && strings.Contains(e.Msg, text):
	case errors.As(err, &se) && strings.Contains(se.Body, text):
	default:
		t.Errorf("%s gave %v, want an error of the server that holds %q", what, err, text)
	}
}

func equal(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s is %#v, want %#v", what, got, want)
	}
}

// key returns a key of the table people for the principal p.
func key(p principal, k string) RecordID {
	return RecordID{Table: tbl("people"), ID: p.name + "_" + k}
}

// TestIntegrationCRUD runs every statement of CRUD of the survey, as each
// principal, and compares what it reads back.
func TestIntegrationCRUD(t *testing.T) {
	people, knows := tbl("people"), tbl("knows")
	steps := []struct {
		name string
		f    func(t *testing.T, p principal, db *sql.DB)
	}{
		{"insert", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "i1")
			exec(t, db, "INSERT INTO "+people+" {id: $id, name: 'Ada', age: 36}", sql.Named("id", k.ID))
			equal(t, "the row", last(query(t, db, "SELECT age, name FROM $k", sql.Named("k", k))), [][]any{{int64(36), "Ada"}})
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"select", func(t *testing.T, p principal, db *sql.DB) {
			a, b := key(p, "s1"), key(p, "s2")
			exec(t, db, "CREATE $a SET name = 'Ada', age = 36; CREATE $b SET name = 'Bo', age = 20", sql.Named("a", a), sql.Named("b", b))
			rows := last(query(t, db, "SELECT age, name FROM "+people+" WHERE age > 30 AND id IN [$a, $b]", sql.Named("a", a), sql.Named("b", b)))
			equal(t, "the rows", rows, [][]any{{int64(36), "Ada"}})
			exec(t, db, "DELETE $a, $b", sql.Named("a", a), sql.Named("b", b))
		}},
		{"update", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "u1")
			exec(t, db, "CREATE $k SET age = 36; UPDATE $k SET age += 1", sql.Named("k", k))
			equal(t, "the age", last(query(t, db, "SELECT VALUE age FROM $k", sql.Named("k", k))), [][]any{{int64(37)}})
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"delete", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "d1")
			exec(t, db, "CREATE $k; DELETE $k", sql.Named("k", k))
			equal(t, "the rows after the delete", last(query(t, db, "SELECT * FROM $k", sql.Named("k", k))), [][]any(nil))
		}},
		{"create", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "c1")
			rows := last(query(t, db, "CREATE $k SET name = 'Ada'", sql.Named("k", k)))
			equal(t, "the record created", rows, [][]any{{k, "Ada"}})
			_, err := queryErr(t, db, "CREATE $k", sql.Named("k", k))
			refused(t, "a second CREATE of the same record", err, "already exists")
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"upsert", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "up")
			exec(t, db, "UPSERT $k SET n = 1; UPSERT $k SET n = 2", sql.Named("k", k))
			equal(t, "n", last(query(t, db, "SELECT VALUE n FROM $k", sql.Named("k", k))), [][]any{{int64(2)}})
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"update with merge", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "m1")
			exec(t, db, "CREATE $k SET age = 1; UPDATE $k MERGE {city: 'Oslo'}", sql.Named("k", k))
			equal(t, "the row", last(query(t, db, "SELECT age, city FROM $k", sql.Named("k", k))), [][]any{{int64(1), "Oslo"}})
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"update with content", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "ct")
			exec(t, db, "CREATE $k SET age = 1, name = 'x'; UPDATE $k CONTENT {age: 9}", sql.Named("k", k))
			equal(t, "the row", last(query(t, db, "SELECT age, name FROM $k", sql.Named("k", k))), [][]any{{int64(9), nil}})
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"update with patch", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "pt")
			exec(t, db, "CREATE $k SET age = 1; UPDATE $k PATCH [{op: 'replace', path: '/age', value: 10}]", sql.Named("k", k))
			equal(t, "the age", last(query(t, db, "SELECT VALUE age FROM $k", sql.Named("k", k))), [][]any{{int64(10)}})
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"relate", func(t *testing.T, p principal, db *sql.DB) {
			a, b := key(p, "r1"), key(p, "r2")
			exec(t, db, "CREATE $a, $b; RELATE $a->"+knows+"->$b SET since = 2020", sql.Named("a", a), sql.Named("b", b))
			equal(t, "the friends", last(query(t, db, "SELECT VALUE ->"+knows+"->"+people+" FROM $a", sql.Named("a", a))), [][]any{{[]any{b}}})
			exec(t, db, "DELETE "+knows+" WHERE in = $a; DELETE $a, $b", sql.Named("a", a), sql.Named("b", b))
		}},
		{"insert relation", func(t *testing.T, p principal, db *sql.DB) {
			a, b := key(p, "ir1"), key(p, "ir2")
			exec(t, db, "CREATE $a, $b; INSERT RELATION INTO "+knows+" {in: $a, out: $b}", sql.Named("a", a), sql.Named("b", b))
			equal(t, "the relation", last(query(t, db, "SELECT VALUE out FROM "+knows+" WHERE in = $a", sql.Named("a", a))), [][]any{{b}})
			exec(t, db, "DELETE "+knows+" WHERE in = $a; DELETE $a, $b", sql.Named("a", a), sql.Named("b", b))
		}},
		{"insert on duplicate key update", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "dk")
			stmt := "INSERT INTO " + people + " {id: $id, n: 1} ON DUPLICATE KEY UPDATE n += 1"
			exec(t, db, stmt, sql.Named("id", k.ID))
			exec(t, db, stmt, sql.Named("id", k.ID))
			equal(t, "n", last(query(t, db, "SELECT VALUE n FROM $k", sql.Named("k", k))), [][]any{{int64(2)}})
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"return clause", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "rc")
			all := query(t, db, "CREATE $k SET n = 1 RETURN NONE; UPDATE $k SET n = 2 RETURN BEFORE; DELETE $k RETURN BEFORE", sql.Named("k", k))
			equal(t, "RETURN NONE", all[0], [][]any(nil))
			equal(t, "RETURN BEFORE of the update", all[1], [][]any{{k, int64(1)}})
			equal(t, "RETURN BEFORE of the delete", all[2], [][]any{{k, int64(2)}})
		}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			forEach(t, s.f)
		})
	}
}

// TestIntegrationSchema runs each operation on a schema that the survey
// names, as each principal, in tables of its own.
func TestIntegrationSchema(t *testing.T) {
	// own returns a table of the test for the principal p.
	own := func(p principal, name string) string { return tbl(p.name + "_" + name) }
	steps := []struct {
		name string
		f    func(t *testing.T, p principal, db *sql.DB)
	}{
		{"table", func(t *testing.T, p principal, db *sql.DB) {
			tb := own(p, "t")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMALESS; CREATE "+tb+":a SET x = 1")
			equal(t, "the row", last(query(t, db, "SELECT VALUE x FROM "+tb)), [][]any{{int64(1)}})
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"schemafull table", func(t *testing.T, p principal, db *sql.DB) {
			tb := own(p, "sf")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMAFULL; DEFINE FIELD name ON "+tb+" TYPE string")
			_, err := queryErr(t, db, "CREATE "+tb+":a SET name = 'x', extra = 1")
			if err == nil {
				equal(t, "a field that the schema lacks", last(query(t, db, "SELECT VALUE extra FROM "+tb+":a")), [][]any{{nil}})
			} else {
				refused(t, "a field that the schema lacks", err, "extra")
			}
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"field with a type", func(t *testing.T, p principal, db *sql.DB) {
			tb := own(p, "ft")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMAFULL; DEFINE FIELD name ON "+tb+" TYPE string")
			_, err := queryErr(t, db, "CREATE "+tb+":b SET name = 1")
			refused(t, "a field of the wrong type", err, "name")
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"record id as primary key", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "pk")
			exec(t, db, "CREATE $k", sql.Named("k", k))
			_, err := queryErr(t, db, "CREATE $k", sql.Named("k", k))
			refused(t, "a second record with the same id", err, "already exists")
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"record link", func(t *testing.T, p principal, db *sql.DB) {
			a, b := key(p, "l1"), key(p, "l2")
			exec(t, db, "CREATE $b SET name = 'B'; CREATE $a SET best = $b", sql.Named("a", a), sql.Named("b", b))
			equal(t, "the name through the link", last(query(t, db, "SELECT VALUE best.name FROM $a", sql.Named("a", a))), [][]any{{"B"}})
			exec(t, db, "DELETE $a, $b", sql.Named("a", a), sql.Named("b", b))
		}},
		{"reference with on delete", func(t *testing.T, p principal, db *sql.DB) {
			since(t, 3, 0, "REFERENCE without an experimental capability")
			tb, owner := own(p, "ref"), key(p, "owner")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMALESS; DEFINE FIELD owner ON "+tb+" TYPE option<record<"+tbl("people")+">> REFERENCE ON DELETE CASCADE")
			exec(t, db, "CREATE $o; CREATE "+tb+":a SET owner = $o; DELETE $o", sql.Named("o", owner))
			equal(t, "the rows after the owner went", last(query(t, db, "SELECT * FROM "+tb)), [][]any(nil))
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"index", func(t *testing.T, p principal, db *sql.DB) {
			tb := own(p, "ix")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMALESS; DEFINE INDEX i_n ON "+tb+" FIELDS n; CREATE "+tb+":a SET n = 1")
			equal(t, "a read through the index", last(query(t, db, "SELECT VALUE n FROM "+tb+" WITH INDEX i_n WHERE n = 1")), [][]any{{int64(1)}})
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"unique index", func(t *testing.T, p principal, db *sql.DB) {
			tb := own(p, "ux")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMALESS; DEFINE INDEX u_e ON "+tb+" FIELDS e UNIQUE; CREATE "+tb+":a SET e = 'x'")
			_, err := queryErr(t, db, "CREATE "+tb+":b SET e = 'x'")
			refused(t, "a second record with the same value", err, "u_e")
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"view", func(t *testing.T, p principal, db *sql.DB) {
			src, view := own(p, "vsrc"), own(p, "view")
			exec(t, db, "DEFINE TABLE "+src+" SCHEMALESS; DEFINE TABLE "+view+" AS SELECT count() AS n FROM "+src+" GROUP ALL; CREATE "+src+":a; CREATE "+src+":b")
			equal(t, "the view", last(query(t, db, "SELECT VALUE n FROM "+view)), [][]any{{int64(2)}})
			exec(t, db, "REMOVE TABLE "+view+"; REMOVE TABLE "+src)
		}},
		{"default value", func(t *testing.T, p principal, db *sql.DB) {
			tb := own(p, "dv")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMALESS; DEFINE FIELD age ON "+tb+" TYPE int DEFAULT 7; CREATE "+tb+":a")
			equal(t, "the default", last(query(t, db, "SELECT VALUE age FROM "+tb+":a")), [][]any{{int64(7)}})
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"assert", func(t *testing.T, p principal, db *sql.DB) {
			tb := own(p, "as")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMALESS; DEFINE FIELD age ON "+tb+" TYPE int ASSERT $value >= 0")
			_, err := queryErr(t, db, "CREATE "+tb+":a SET age = -1")
			refused(t, "a value that the assertion refuses", err, "age")
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"event", func(t *testing.T, p principal, db *sql.DB) {
			tb, log := own(p, "ev"), own(p, "evlog")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMALESS; DEFINE TABLE "+log+" SCHEMALESS; DEFINE EVENT e ON "+tb+" WHEN $event = 'CREATE' THEN (CREATE "+log+" SET at = $after.id); CREATE "+tb+":a")
			equal(t, "the log", last(query(t, db, "SELECT VALUE at FROM "+log)), [][]any{{RecordID{Table: tb, ID: "a"}}})
			exec(t, db, "REMOVE TABLE "+tb+"; REMOVE TABLE "+log)
		}},
		{"function", func(t *testing.T, p principal, db *sql.DB) {
			fn := "fn::" + own(p, "add")
			exec(t, db, "DEFINE FUNCTION "+fn+"($a: int, $b: int) { RETURN $a + $b; }")
			equal(t, "the call", last(query(t, db, "RETURN "+fn+"(1, 2)")), [][]any{{int64(3)}})
			exec(t, db, "REMOVE FUNCTION "+fn)
		}},
		{"param", func(t *testing.T, p principal, db *sql.DB) {
			param := "$" + own(p, "p")
			exec(t, db, "DEFINE PARAM "+param+" VALUE 5")
			equal(t, "the param", last(query(t, db, "RETURN "+param)), [][]any{{int64(5)}})
			exec(t, db, "REMOVE PARAM "+param)
		}},
		{"full text index", func(t *testing.T, p principal, db *sql.DB) {
			tb, az := own(p, "fts"), own(p, "az")
			// 3.x writes FULLTEXT, and 2.7 writes SEARCH (docs/SURREALDB.md).
			kind := "SEARCH"
			if is3(t) {
				kind = "FULLTEXT"
			}
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMALESS; DEFINE ANALYZER "+az+" TOKENIZERS blank FILTERS lowercase; "+
				"DEFINE INDEX ft ON "+tb+" FIELDS t "+kind+" ANALYZER "+az+" BM25; CREATE "+tb+":a SET t = 'Hello world'")
			equal(t, "the match", last(query(t, db, "SELECT VALUE id FROM "+tb+" WHERE t @@ 'hello'")), [][]any{{RecordID{Table: tb, ID: "a"}}})
			exec(t, db, "REMOVE TABLE "+tb+"; REMOVE ANALYZER "+az)
		}},
		{"vector index", func(t *testing.T, p principal, db *sql.DB) {
			tb := own(p, "vec")
			exec(t, db, "DEFINE TABLE "+tb+" SCHEMALESS; DEFINE INDEX v ON "+tb+" FIELDS e HNSW DIMENSION 2 DIST EUCLIDEAN; "+
				"CREATE "+tb+":a SET e = [0, 0]; CREATE "+tb+":b SET e = [5, 5]")
			equal(t, "the nearest", last(query(t, db, "SELECT VALUE id FROM "+tb+" WHERE e <|1,40|> [1, 1]")), [][]any{{RecordID{Table: tb, ID: "a"}}})
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"sequence", func(t *testing.T, p principal, db *sql.DB) {
			since(t, 3, 0, "DEFINE SEQUENCE")
			seq := own(p, "seq")
			if p.name == "ordinary" {
				_, err := queryErr(t, db, "DEFINE SEQUENCE "+seq)
				refused(t, "a sequence as the ordinary user", err, "Not enough permissions")
				return
			}
			exec(t, db, "DEFINE SEQUENCE "+seq)
			equal(t, "the values", last(query(t, db, "RETURN [sequence::nextval('"+seq+"'), sequence::nextval('"+seq+"')]")), [][]any{{int64(0)}, {int64(1)}})
			exec(t, db, "REMOVE SEQUENCE "+seq)
		}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			forEach(t, s.f)
		})
	}
}

// TestIntegrationFeatures runs each feature of SurrealQL that the survey
// names, as each principal, and the refusal of each one that it marks no.
func TestIntegrationFeatures(t *testing.T) {
	people := tbl("people")
	type feature struct {
		name string
		f    func(t *testing.T, p principal, db *sql.DB)
	}
	// simple is a feature whose statement returns want in its last result
	// set.
	simple := func(name, stmt string, want [][]any) feature {
		return feature{name, func(t *testing.T, _ principal, db *sql.DB) {
			equal(t, stmt, last(query(t, db, stmt)), want)
		}}
	}
	// no is a feature that the server refuses with an error that holds text.
	no := func(name, stmt, text string) feature {
		return feature{name, func(t *testing.T, _ principal, db *sql.DB) {
			_, err := queryErr(t, db, stmt)
			refused(t, stmt, err, text)
		}}
	}
	features := []feature{
		{"record id", func(t *testing.T, p principal, db *sql.DB) {
			id := uuid.MustParse("0192f1c4-3b5e-7a2c-9f00-000000000001")
			for _, k := range []any{int64(5), []any{p.name, int64(1)}, map[string]any{"p": p.name}, id} {
				r := RecordID{Table: people, ID: k}
				equal(t, fmt.Sprintf("the record %v", r), last(query(t, db, "CREATE $r RETURN VALUE id", sql.Named("r", r))), [][]any{{r}})
				exec(t, db, "DELETE $r", sql.Named("r", r))
			}
		}},
		{"graph traversal", func(t *testing.T, p principal, db *sql.DB) {
			a, b, knows := key(p, "g1"), key(p, "g2"), tbl("knows")
			exec(t, db, "CREATE $a, $b SET name = 'x'; RELATE $a->"+knows+"->$b", sql.Named("a", a), sql.Named("b", b))
			equal(t, "the way back", last(query(t, db, "SELECT VALUE <-"+knows+"<-"+people+" FROM $b", sql.Named("b", b))), [][]any{{[]any{a}}})
			exec(t, db, "DELETE "+knows+" WHERE in = $a; DELETE $a, $b", sql.Named("a", a), sql.Named("b", b))
		}},
		{"fetch", func(t *testing.T, p principal, db *sql.DB) {
			a, b := key(p, "f1"), key(p, "f2")
			exec(t, db, "CREATE $b SET name = 'B'; CREATE $a SET best = $b", sql.Named("a", a), sql.Named("b", b))
			equal(t, "the fetched record", last(query(t, db, "SELECT best FROM $a FETCH best", sql.Named("a", a))), [][]any{{map[string]any{"id": b, "name": "B"}}})
			exec(t, db, "DELETE $a, $b", sql.Named("a", a), sql.Named("b", b))
		}},
		simple("split", "SELECT * FROM [{t: [1, 2]}] SPLIT t", [][]any{{int64(1)}, {int64(2)}}),
		simple("group all", "SELECT count() AS n FROM [1, 2, 3] GROUP ALL", [][]any{{int64(3)}}),
		simple("omit", "SELECT * OMIT b FROM [{a: 1, b: 2}]", [][]any{{int64(1)}}),
		simple("only", "SELECT * FROM ONLY {a: 1, b: 2}", [][]any{{int64(1), int64(2)}}),
		no("live select", "LIVE SELECT * FROM "+people, "realtime"),
		{"let", func(t *testing.T, _ principal, db *sql.DB) {
			equal(t, "LET and RETURN", query(t, db, "LET $a = 5; RETURN $a * 2"), [][][]any{{{nil}}, {{int64(10)}}})
		}},
		{"transaction in one request", func(t *testing.T, p principal, db *sql.DB) {
			k := key(p, "tx")
			// Each statement of a cancelled transaction fails.
			_, err := queryErr(t, db, "BEGIN; CREATE $k SET v = 1; CANCEL", sql.Named("k", k))
			refused(t, "a statement of a cancelled transaction", err, "cancelled transaction")
			equal(t, "the record after CANCEL", last(query(t, db, "SELECT * FROM $k", sql.Named("k", k))), [][]any(nil))
			all := query(t, db, "BEGIN; CREATE $k SET v = 1; COMMIT; SELECT VALUE v FROM $k", sql.Named("k", k))
			equal(t, "the record after COMMIT", last(all), [][]any{{int64(1)}})
			exec(t, db, "DELETE $k", sql.Named("k", k))
		}},
		{"interactive transaction", func(t *testing.T, _ principal, db *sql.DB) {
			if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("BeginTx gave %v, want ErrNotSupported (D54)", err)
			}
			// BEGIN alone runs, and ends with its request
			// (docs/SURREALDB.md).
			if _, err := queryErr(t, db, "BEGIN"); err != nil {
				t.Errorf("BEGIN alone: %v", err)
			}
		}},
		simple("return", "RETURN 1 + 1", [][]any{{int64(2)}}),
		simple("if else", "IF 1 > 0 { RETURN 'yes' } ELSE { RETURN 'no' }", [][]any{{"yes"}}),
		{"for", func(t *testing.T, p principal, db *sql.DB) {
			a, b := key(p, "for1"), key(p, "for2")
			exec(t, db, "FOR $r IN [$a, $b] { CREATE $r; }", sql.Named("a", a), sql.Named("b", b))
			equal(t, "the records", last(query(t, db, "SELECT VALUE id FROM $a, $b", sql.Named("a", a), sql.Named("b", b))), [][]any{{a}, {b}})
			exec(t, db, "DELETE $a, $b", sql.Named("a", a), sql.Named("b", b))
		}},
		no("throw", "THROW 'thrown'", "thrown"),
		{"info for", func(t *testing.T, _ principal, db *sql.DB) {
			// The result is one object, so its keys are the columns (D52).
			info, err := object(t.Context(), db, "INFO FOR TABLE "+people)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := info["fields"]; !ok {
				t.Errorf("INFO FOR TABLE gave %v, want its fields", info)
			}
		}},
		simple("with index", "SELECT * FROM "+people+" WITH NOINDEX WHERE name = 'nobody'", nil),
		{"explain", func(t *testing.T, _ principal, db *sql.DB) {
			rows := last(query(t, db, "SELECT * FROM "+people+" WHERE name = 'x' EXPLAIN"))
			if len(rows) == 0 {
				t.Error("EXPLAIN gave no plan")
			}
		}},
		no("timeout", "SELECT * FROM [1, 2, 3] WHERE sleep(20ms) = NONE TIMEOUT 10ms", "timeout"),
		{"parallel", func(t *testing.T, _ principal, db *sql.DB) {
			_, err := queryErr(t, db, "SELECT * FROM "+people+" PARALLEL")
			if is3(t) {
				refused(t, "PARALLEL on 3.x", err, "PARALLEL")
			} else if err != nil {
				t.Errorf("PARALLEL on 2.7: %v", err)
			}
		}},
		simple("tempfiles", "SELECT * FROM "+people+" WHERE name = 'nobody' ORDER BY name TEMPFILES", nil),
		no("version clause", "SELECT * FROM "+people+" VERSION d'2026-01-01T00:00:00Z'", "versioned queries"),
		{"future", func(t *testing.T, _ principal, db *sql.DB) {
			rows, err := queryErr(t, db, "RETURN <future> { 1 + 1 }")
			if is3(t) {
				refused(t, "a future on 3.x", err, "Parse error")
			} else if err != nil || !reflect.DeepEqual(last(rows), [][]any{{int64(2)}}) {
				t.Errorf("a future on 2.7 gave %v, %v, want 2", rows, err)
			}
		}},
		simple("closure", "LET $f = |$x: int| $x * 2; RETURN $f(21)", [][]any{{int64(42)}}),
		simple("range", "RETURN 1..5", [][]any{{"1..5"}}),
		{"changefeed", func(t *testing.T, p principal, db *sql.DB) {
			tb := tbl(p.name + "_cf")
			all := query(t, db, "DEFINE TABLE "+tb+" CHANGEFEED 1h; CREATE "+tb+":a; SHOW CHANGES FOR TABLE "+tb+" SINCE 0 LIMIT 5")
			// 2.7 shows the change at once. 3.3 showed no change of a new
			// table within 40 seconds (docs/SURREALDB.md), so the test holds
			// only that the statement runs, on 3.x.
			if !is3(t) && len(last(all)) == 0 {
				t.Error("SHOW CHANGES on 2.7 gave no change")
			}
			exec(t, db, "REMOVE TABLE "+tb)
		}},
		{"bound parameters", func(t *testing.T, _ principal, db *sql.DB) {
			now := time.Date(2026, 9, 27, 10, 0, 0, 5, time.UTC)
			rows := last(query(t, db, "RETURN [$s, $i, $t]", sql.Named("s", "it's"), sql.Named("i", 42), sql.Named("t", now)))
			equal(t, "the parameters", rows, [][]any{{"it's"}, {int64(42)}, {now}})
		}},
		{"signin token", func(t *testing.T, p principal, _ *sql.DB) {
			cfg, err := ParseDSN(os.Getenv(p.env))
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]string{"user": cfg.User, "pass": cfg.Password}
			if cfg.Auth == AuthDatabase {
				body["ns"], body["db"] = cfg.Namespace, cfg.Database
			}
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, cfg.baseURL()+"/signin", strings.NewReader(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var out struct {
				Token string `json:"token"`
			}
			if err := json.UnmarshalRead(res.Body, &out); err != nil || res.StatusCode != http.StatusOK || out.Token == "" {
				// The token is a credential, so the test never prints it.
				t.Errorf("POST /signin gave HTTP %d and no token: %v", res.StatusCode, err)
			}
		}},
	}
	for _, f := range features {
		t.Run(f.name, func(t *testing.T) {
			forEach(t, f.f)
		})
	}
}

// TestIntegrationTypes runs the round trip of step 14a for each type that
// the survey marks yes, and the refusal of each one that it marks no.
func TestIntegrationTypes(t *testing.T) {
	types := tbl("types")
	literalOf := func(v any) (string, error) {
		if _, ok := v.([]byte); ok {
			return "", fmt.Errorf("writing bytes: %w", dbimp.ErrNotSupported)
		}
		return literal(v), nil
	}
	decimalEqual := func(got, want any) bool {
		g, ok1 := got.(*apd.Decimal)
		w, ok2 := want.(*apd.Decimal)
		return ok1 && ok2 && g.Cmp(w) == 0
	}
	dec := func(s string) *apd.Decimal {
		d, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	// c returns the case of the type name, whose value the statements write
	// as wrap does. wrap is "%s" for a value that is stored as it is.
	c := func(name, wrap string, values ...dbimptest.Value) dbimptest.RoundTripCase {
		v := fmt.Sprintf(wrap, "$value")
		return dbimptest.RoundTripCase{
			Type:   name,
			Insert: "CREATE $key CONTENT {v: " + v + "}",
			Literal: func(key string, val any) (string, error) {
				lit, err := literalOf(val)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("CREATE %s CONTENT {v: %s}", RecordID{Table: types, ID: key}, fmt.Sprintf(wrap, lit)), nil
			},
			// The field is selected by name, and not as a VALUE, because an
			// object in an array is a row (D52).
			Select:   "SELECT v FROM $key",
			Update:   "UPDATE $key SET v = " + v,
			Delete:   "DELETE $key",
			Values:   values,
			Named:    true,
			KeyArg:   func(key string) any { return RecordID{Table: types, ID: key} },
			Teardown: []string{"DELETE " + types},
		}
	}
	long := strings.Repeat("0123456789", 1000)
	vid := uuid.MustParse("0192f1c4-3b5e-7a2c-9f00-000000000001")
	cases := []dbimptest.RoundTripCase{
		c("null", "%s", dbimptest.Value{Name: "null", In: nil}, dbimptest.Value{Name: "null again", In: nil}),
		c("bool", "%s", dbimptest.Value{Name: "true", In: true}, dbimptest.Value{Name: "false", In: false}),
		c("int", "%s",
			dbimptest.Value{Name: "zero", In: int64(0)},
			dbimptest.Value{Name: "max", In: int64(9223372036854775807)},
			dbimptest.Value{Name: "min", In: int64(-9223372036854775808)},
		),
		c("float", "%s",
			dbimptest.Value{Name: "fraction", In: 0.1},
			dbimptest.Value{Name: "large", In: -1.5e300},
			dbimptest.Value{Name: "whole", In: 1.0},
		),
		c("string", "%s",
			dbimptest.Value{Name: "empty", In: ""},
			dbimptest.Value{Name: "unicode", In: "héllo, 世界"},
			dbimptest.Value{Name: "long", In: long},
			dbimptest.Value{Name: "quotes", In: `it's "q" \ `},
		),
		c("datetime", "%s",
			dbimptest.Value{Name: "now", In: time.Date(2026, 9, 27, 10, 0, 0, 123456789, time.UTC)},
			dbimptest.Value{Name: "1600", In: time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC)},
			dbimptest.Value{Name: "9999", In: time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)},
		),
		c("duration", "%s",
			dbimptest.Value{Name: "90m", In: 90 * time.Minute},
			dbimptest.Value{Name: "1ns", In: time.Nanosecond},
			dbimptest.Value{Name: "zero", In: time.Duration(0)},
		),
		c("uuid", "%s", dbimptest.Value{Name: "v7", In: vid}, dbimptest.Value{Name: "zero", In: uuid.UUID{}}),
		c("bytes", "%s", dbimptest.Value{Name: "two", In: []byte{0, 255}}, dbimptest.Value{Name: "empty", In: []byte{}}),
		c("array", "%s",
			dbimptest.Value{Name: "mixed", In: []any{int64(1), "a", nil}},
			dbimptest.Value{Name: "empty", In: []any{}},
		),
		c("set", "<set> %s",
			dbimptest.Value{Name: "ints", In: []any{int64(1), int64(2), int64(3)}},
			dbimptest.Value{Name: "strings", In: []any{"a", "b"}},
		),
		c("object", "%s",
			dbimptest.Value{Name: "nested", In: map[string]any{"a": int64(1), "b": []any{"x"}}},
			dbimptest.Value{Name: "empty", In: map[string]any{}},
		),
		c("record", "%s",
			dbimptest.Value{Name: "string key", In: RecordID{Table: tbl("people"), ID: "tobie"}},
			dbimptest.Value{Name: "int key", In: RecordID{Table: tbl("people"), ID: int64(5)}},
		),
		c("geometry", "%s",
			dbimptest.Value{Name: "point", In: map[string]any{"type": "Point", "coordinates": []any{1.5, 2.5}}},
			dbimptest.Value{Name: "line", In: map[string]any{"type": "LineString", "coordinates": []any{[]any{0.5, 0.5}, []any{1.5, 1.5}}}},
		),
		c("table", "type::table(%s)", dbimptest.Value{Name: "people", In: "people"}, dbimptest.Value{Name: "orders", In: "orders"}),
	}
	decimalCase := c("decimal", "%s",
		dbimptest.Value{Name: "fraction", In: dec("0.1")},
		dbimptest.Value{Name: "long", In: dec("-12345678901234567890.123456789")},
	)
	decimalCase.Equal = decimalEqual
	cases = append(cases, decimalCase)
	// A range is written from its two ends, because no Go type holds a range
	// (D53), and 2.7 cannot cast a string to one.
	rangeCase := c("range", "%s", dbimptest.Value{Name: "1..5", In: map[string]any{"a": int64(1), "b": int64(5)}, Want: "1..5"},
		dbimptest.Value{Name: "2..8", In: map[string]any{"a": int64(2), "b": int64(8)}, Want: "2..8"})
	// 2.7 reads $value.a..$value.b as a path, so each end is a LET first.
	rangeCase.Insert = "LET $a = $value.a; LET $b = $value.b; CREATE $key CONTENT {v: $a..$b}"
	rangeCase.Update = "LET $a = $value.a; LET $b = $value.b; UPDATE $key SET v = $a..$b"
	rangeCase.Literal = func(key string, val any) (string, error) {
		m, _ := val.(map[string]any)
		return fmt.Sprintf("CREATE %s CONTENT {v: %v..%v}", RecordID{Table: types, ID: key}, m["a"], m["b"]), nil
	}
	cases = append(cases, rangeCase)
	// NONE is written by the statement, because a nil argument is NULL. The
	// value of the case is a field beside it.
	noneCase := c("none", "%s", dbimptest.Value{Name: "none", In: nil}, dbimptest.Value{Name: "none again", In: nil})
	noneCase.Insert = "CREATE $key CONTENT {v: NONE, n: $value}"
	noneCase.Update = "UPDATE $key SET n = $value, v = NONE"
	noneCase.Literal = func(key string, val any) (string, error) {
		return fmt.Sprintf("CREATE %s CONTENT {v: NONE, n: %s}", RecordID{Table: types, ID: key}, literal(val)), nil
	}
	cases = append(cases, noneCase)
	for _, rc := range cases {
		t.Run(rc.Type, func(t *testing.T) {
			forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
				dbimptest.RoundTrip(t, db, rc)
			})
		})
	}
	for _, tt := range []struct {
		name, stmt, text string
	}{
		{"file", "RETURN f'bucket:/a.txt'", "Parse error"},
		{"regex", "RETURN /a.b/", ""},
		{"closure", "RETURN |$x| $x + 1", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
				_, err := queryErr(t, db, tt.stmt)
				refused(t, tt.stmt, err, tt.text)
			})
		})
	}
}

// TestIntegrationVersion reads the version with Version, as each principal,
// which step 16 asks for. No statement of SurrealQL returns it (D57), so the
// driver answers SELECT version() itself with the same value (D181).
func TestIntegrationVersion(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		c, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		var v string
		err = c.Raw(func(dc any) error {
			v, err = Version(t.Context(), dc)
			return err
		})
		if err != nil || !strings.HasPrefix(v, "surrealdb-") {
			t.Fatalf("the version is %q, %v", v, err)
		}
		t.Logf("the %s user reads the version %s", p.name, v)
		for _, query := range []string{"SELECT version()", "select VERSION();"} {
			var got string
			if err := db.QueryRowContext(t.Context(), query).Scan(&got); err != nil || got != v {
				t.Errorf("%s gave %q and %v, want %q", query, got, err, v)
			}
		}
		stmt, err := db.PrepareContext(t.Context(), "SELECT version()")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		var got string
		if err := stmt.QueryRowContext(t.Context()).Scan(&got); err != nil || got != v {
			t.Errorf("the prepared SELECT version() gave %q and %v, want %q", got, err, v)
		}
		// Any other statement goes to the server, which has no such function.
		if _, err := queryErr(t, db, "SELECT version(), 1"); err == nil {
			t.Error("SELECT version(), 1 gave no error, want the error of the server")
		}
	})
}

// TestIntegrationOptions holds the options of one statement on the server
// (D109).
func TestIntegrationOptions(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		const current = "RETURN session::db()"
		equal(t, "the database", query(t, db, current), [][][]any{{{"dbmeta"}}})
		all, err := queryErr(t, db, current, WithDatabase("other"))
		switch {
		case p.name == "ordinary" && refusesOtherDatabase(t):
			// 3.3.0 refuses a user of dbmeta in another database, with HTTP
			// 401 (measured by hand on 3.3.0).
			refused(t, "the ordinary user in another database", err, "authentication")
		case err != nil:
			t.Errorf("%s with WithDatabase: %v", current, err)
		default:
			// 2.7.0, 3.1.6 and 3.2.4 let a user of dbmeta name another
			// database (measured by hand on 2.7.0 and 3.2.4, and by the
			// nightly run on 3.1.6).
			equal(t, "the database with WithDatabase", all, [][][]any{{{"other"}}})
		}
		equal(t, "a statement with WithParameter", query(t, db, "RETURN 1", WithParameter("id", int64(7))), [][][]any{{{int64(1)}}})
		if _, err := db.ExecContext(t.Context(), "RETURN 1", WithTimeout(time.Second)); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithTimeout gave %v, want dbimp.ErrNotSupported", err)
		}
	})
}
