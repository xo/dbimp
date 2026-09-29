package couchbase_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/couchbase"
	"github.com/xo/dbimp/dbimptest"
)

// These tests need a server. COUCHBASE_DSN names it for the administrator,
// and COUCHBASE_ORDINARY_DSN for the ordinary user (D9). A test skips when
// its DSN is empty. TestMain makes a scope of its own with four
// collections, and drops it at the end. A statement names its columns in the
// order of their names, because 7.2 sorts the columns by name.

// scope is the scope that the tests make, in the bucket dbmeta.
var scope = "dbmeta.dbimp_it_" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var principals = []principal{
	{"administrator", "COUCHBASE_DSN"},
	{"ordinary", "COUCHBASE_ORDINARY_DSN"},
}

// withTestKeys adds the keys that every integration test uses: no
// durability, because the one node of dbrun cannot meet the default (D43),
// and request_plus, because an index is updated after a write.
func withTestKeys(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "durability_level=none&scan_consistency=request_plus"
}

// openAs opens the server as p, or skips the test when its DSN is empty.
func openAs(t *testing.T, p principal) *sql.DB {
	t.Helper()
	dsn := os.Getenv(p.env)
	if dsn == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	db, err := sql.Open("couchbase", withTestKeys(dsn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

// run makes the scope of the tests when COUCHBASE_DSN names a server, runs
// the tests, and drops the scope.
func run(m *testing.M) (int, error) {
	dsn := os.Getenv("COUCHBASE_DSN")
	if dsn == "" {
		return m.Run(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := sql.Open("couchbase", withTestKeys(dsn))
	if err != nil {
		return 1, err
	}
	defer db.Close()
	if err := makeScope(ctx, db); err != nil {
		_, _ = db.ExecContext(ctx, "DROP SCOPE "+scope)
		return 1, err
	}
	code := m.Run()
	if _, err := db.ExecContext(ctx, "DROP SCOPE "+scope); err != nil {
		return 1, fmt.Errorf("dropping the scope of the tests: %w", err)
	}
	// The server drops a scope a moment after DROP SCOPE returns. While it
	// drops it, a read of system:scopes can meet the scope as it goes, and
	// fail with 12021, "Scope not found" (8.0.3 in CI on 2026-09-27). So that
	// error means that the drop is still running, and the loop reads again.
	name := strings.TrimPrefix(scope, "dbmeta.")
	for range 60 {
		var n int
		err := db.QueryRowContext(ctx, "SELECT RAW COUNT(*) FROM system:scopes WHERE `bucket` = 'dbmeta' AND name = $1", name).Scan(&n)
		if e, ok := errors.AsType[couchbase.Error](err); ok && e.Code == 12021 {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if err != nil {
			return 1, fmt.Errorf("reading the scopes left: %w", err)
		}
		if n == 0 {
			return code, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 1, fmt.Errorf("removing the scope %s: it is still there after the tests", scope)
}

// collections are the collections of the scope. INFER reads samples, which
// holds only documents that no test deletes, because INFER on 7.2 can find
// no documents in a collection that holds many deleted ones (7014).
var collections = []string{"people", "orders", "types", "samples"}

// makeScope makes the scope, its collections, their primary indexes and a
// secondary index. The server makes a scope or a collection a moment after
// the statement returns, so each later statement is tried again for a while.
func makeScope(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "CREATE SCOPE "+scope); err != nil {
		return fmt.Errorf("making the scope of the tests: %w", err)
	}
	var stmts []string
	for _, c := range collections {
		stmts = append(stmts, "CREATE COLLECTION "+scope+"."+c)
	}
	for _, c := range collections {
		stmts = append(stmts, "CREATE PRIMARY INDEX ON "+scope+"."+c)
	}
	stmts = append(stmts, "CREATE INDEX person ON "+scope+".orders(person)")
	for _, s := range stmts {
		var err error
		for range 60 {
			// A try that fails can still make the index, and the next
			// try then finds it (4300).
			if _, err = db.ExecContext(ctx, s); err == nil || code(err) == 4300 {
				err = nil
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if err != nil {
			return fmt.Errorf("making the scope of the tests: %s: %w", s, err)
		}
	}
	return nil
}

// version returns the major and the minor release of the server.
func version(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	var v string
	if err := db.QueryRowContext(t.Context(), "SELECT RAW ds_version()").Scan(&v); err != nil {
		t.Fatalf("reading the version: %v", err)
	}
	parts := strings.SplitN(v, ".", 3)
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	return major, minor
}

// since skips the test on a release older than major.minor.
func since(t *testing.T, db *sql.DB, major, minor int, what string) {
	t.Helper()
	if m, n := version(t, db); m < major || m == major && n < minor {
		t.Skipf("%s arrived in %d.%d, and the server is %d.%d (docs/COUCHBASE.md)", what, major, minor, m, n)
	}
}

// code returns the code of the first error of the query service in err, or 0.
func code(err error) int {
	if e, ok := errors.AsType[couchbase.Error](err); ok {
		return e.Code
	}
	return 0
}

// query returns the rows of a statement, each value read into *any.
func query(t *testing.T, ctx context.Context, db *sql.DB, stmt string, args ...any) [][]any {
	t.Helper()
	rows, err := rowsOfCtx(t, ctx, db, stmt, args...)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return rows
}

func rowsOfCtx(t *testing.T, ctx context.Context, db *sql.DB, stmt string, args ...any) ([][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out [][]any
	for rows.Next() {
		row := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range dest {
			dest[i] = &row[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func exec(t *testing.T, db *sql.DB, stmt string, args ...any) int64 {
	t.Helper()
	res, err := db.ExecContext(t.Context(), stmt, args...)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func equal(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s is %#v, want %#v", what, got, want)
	}
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

// TestIntegrationCRUD runs every statement of CRUD on the collections people
// and orders, as each principal, and compares each value that it reads back.
func TestIntegrationCRUD(t *testing.T) {
	people, orders := scope+".people", scope+".orders"
	key := func(p principal, k string) string { return p.name + "-" + k }
	steps := []struct {
		name string
		f    func(t *testing.T, p principal, db *sql.DB)
	}{
		{"insert", func(t *testing.T, p principal, db *sql.DB) {
			n := exec(t, db, "INSERT INTO "+people+" (KEY, VALUE) VALUES ($1, {\"name\": $2, \"age\": $3})", key(p, "p1"), "Ada", 36)
			equal(t, "the rows affected", n, int64(1))
		}},
		{"select", func(t *testing.T, p principal, db *sql.DB) {
			rows := query(t, t.Context(), db, "SELECT p.age, p.name FROM "+people+" AS p USE KEYS $1", key(p, "p1"))
			equal(t, "the row", rows, [][]any{{int64(36), "Ada"}})
		}},
		{"returning", func(t *testing.T, p principal, db *sql.DB) {
			rows := query(t, t.Context(), db, "INSERT INTO "+people+" (KEY, VALUE) VALUES ($1, {\"name\": \"Bo\", \"age\": 20}) RETURNING META().id AS id, name", key(p, "p2"))
			equal(t, "the row returned", rows, [][]any{{key(p, "p2"), "Bo"}})
		}},
		{"upsert", func(t *testing.T, p principal, db *sql.DB) {
			exec(t, db, "UPSERT INTO "+people+" (KEY, VALUE) VALUES ($1, {\"name\": \"Bo\", \"age\": 21})", key(p, "p2"))
			rows := query(t, t.Context(), db, "SELECT RAW p.age FROM "+people+" AS p USE KEYS $1", key(p, "p2"))
			equal(t, "the age after the upsert", rows, [][]any{{int64(21)}})
		}},
		{"update", func(t *testing.T, p principal, db *sql.DB) {
			exec(t, db, "UPDATE "+people+" AS p USE KEYS $1 SET p.age = $2", key(p, "p1"), 37)
			rows := query(t, t.Context(), db, "SELECT RAW p.age FROM "+people+" AS p USE KEYS $1", key(p, "p1"))
			equal(t, "the age after the update", rows, [][]any{{int64(37)}})
		}},
		{"update with unset", func(t *testing.T, p principal, db *sql.DB) {
			exec(t, db, "UPDATE "+people+" AS p USE KEYS $1 UNSET p.age", key(p, "p2"))
			rows := query(t, t.Context(), db, "SELECT p.age, p.name FROM "+people+" AS p USE KEYS $1", key(p, "p2"))
			equal(t, "the row after the unset", rows, [][]any{{nil, "Bo"}})
		}},
		{"insert from a select", func(t *testing.T, p principal, db *sql.DB) {
			n := exec(t, db, "INSERT INTO "+orders+" (KEY \"o-\" || META(p).id, VALUE {\"person\": META(p).id, \"total\": 10}) SELECT p FROM "+people+" AS p WHERE META(p).id IN [$1, $2]", key(p, "p1"), key(p, "p2"))
			equal(t, "the orders inserted", n, int64(2))
			rows := query(t, t.Context(), db, "SELECT p.name, o.total FROM "+people+" AS p JOIN "+orders+" AS o ON o.person = META(p).id WHERE META(p).id IN [$1, $2] ORDER BY p.name", key(p, "p1"), key(p, "p2"))
			equal(t, "the join", rows, [][]any{{"Ada", int64(10)}, {"Bo", int64(10)}})
		}},
		{"merge", func(t *testing.T, p principal, db *sql.DB) {
			exec(t, db, "MERGE INTO "+people+" AS p USING [{\"id\": $1, \"age\": 40}] AS s ON META(p).id = s.id WHEN MATCHED THEN UPDATE SET p.age = s.age", key(p, "p1"))
			rows := query(t, t.Context(), db, "SELECT RAW p.age FROM "+people+" AS p USE KEYS $1", key(p, "p1"))
			equal(t, "the age after the merge", rows, [][]any{{int64(40)}})
		}},
		{"delete", func(t *testing.T, p principal, db *sql.DB) {
			exec(t, db, "DELETE FROM "+orders+" AS o WHERE o.person IN [$1, $2]", key(p, "p1"), key(p, "p2"))
			n := exec(t, db, "DELETE FROM "+people+" USE KEYS [$1, $2]", key(p, "p1"), key(p, "p2"))
			equal(t, "the rows deleted", n, int64(2))
			rows := query(t, t.Context(), db, "SELECT RAW COUNT(*) FROM "+people+" AS p WHERE META(p).id IN [$1, $2]", key(p, "p1"), key(p, "p2"))
			equal(t, "the rows left", rows, [][]any{{int64(0)}})
		}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			forEach(t, s.f)
		})
	}
}

// TestIntegrationTypes runs the round trip of step 14a for each kind of
// value, in the collection types.
func TestIntegrationTypes(t *testing.T) {
	types := scope + ".types"
	literal := func(key string, v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("INSERT INTO %s (KEY, VALUE) VALUES (%q, {\"v\": %s})", types, key, b), nil
	}
	c := func(name string, values ...dbimptest.Value) dbimptest.RoundTripCase {
		return dbimptest.RoundTripCase{
			Type:    name,
			Insert:  "INSERT INTO " + types + " (KEY, VALUE) VALUES ($1, {\"v\": $2})",
			Literal: literal,
			Select:  "SELECT RAW t.v FROM " + types + " AS t USE KEYS $1",
			Update:  "UPDATE " + types + " AS t USE KEYS $2 SET t.v = $1",
			Delete:  "DELETE FROM " + types + " USE KEYS $1",
			Values:  values,
			Wait:    10 * time.Second,
		}
	}
	long := strings.Repeat("0123456789", 1000)
	large, _, err := apd.NewFromString("-15" + strings.Repeat("0", 299))
	if err != nil {
		t.Fatal(err)
	}
	cases := []dbimptest.RoundTripCase{
		c("null", dbimptest.Value{Name: "null", In: nil}, dbimptest.Value{Name: "null again", In: nil}),
		c("boolean", dbimptest.Value{Name: "true", In: true}, dbimptest.Value{Name: "false", In: false}),
		c("number",
			dbimptest.Value{Name: "zero", In: int64(0)},
			dbimptest.Value{Name: "min", In: int64(-9223372036854775808)},
			dbimptest.Value{Name: "max", In: int64(9223372036854775807)},
			dbimptest.Value{Name: "past 2^53", In: int64(9007199254740993)},
			dbimptest.Value{Name: "fraction", In: 0.1},
			// The server writes a float that is a whole number as its
			// digits, so a large one is read as a *apd.Decimal (D39).
			dbimptest.Value{Name: "large", In: -1.5e300, Want: large},
			dbimptest.Value{Name: "small", In: 1.5e-300},
		),
		c("string",
			dbimptest.Value{Name: "empty", In: ""},
			dbimptest.Value{Name: "unicode", In: "héllo, 世界"},
			dbimptest.Value{Name: "long", In: long},
			dbimptest.Value{Name: "a quote", In: `it's "quoted"`},
		),
		c("array",
			dbimptest.Value{Name: "empty", In: []any{}},
			dbimptest.Value{Name: "mixed", In: []any{int64(1), "a", nil, []any{true}}},
		),
		c("object",
			dbimptest.Value{Name: "empty", In: map[string]any{}},
			dbimptest.Value{Name: "nested", In: map[string]any{"k": map[string]any{"n": int64(1)}, "a": []any{}}},
		),
		// Bytes are stored as a base64 string, and read into *any as that
		// string (D44).
		c("bytes as a base64 string",
			dbimptest.Value{Name: "four bytes", In: []byte{0xde, 0xad, 0xbe, 0xef}, Want: "3q2+7w=="},
			dbimptest.Value{Name: "empty", In: []byte{}, Want: ""},
		),
	}
	for _, rc := range cases {
		t.Run(rc.Type, func(t *testing.T) {
			forEach(t, func(t *testing.T, p principal, db *sql.DB) {
				rc.Setup, rc.Teardown = nil, []string{"DELETE FROM " + types + " WHERE META().id LIKE 'dbimp-rt-%'"}
				dbimptest.RoundTrip(t, db, rc)
			})
		})
	}
	t.Run("missing", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-missing"
			exec(t, db, "INSERT INTO "+types+" (KEY, VALUE) VALUES ($1, {\"w\": 1})", k)
			rows := query(t, t.Context(), db, "SELECT t.v IS MISSING AS m, t.v FROM "+types+" AS t USE KEYS $1", k)
			equal(t, "a missing field", rows, [][]any{{true, nil}})
			exec(t, db, "DELETE FROM "+types+" USE KEYS $1", k)
		})
	})
	// Couchbase has no binary type for a document. A binary value made by
	// BASE64_DECODE is written as a placeholder (docs/COUCHBASE.md, Types).
	t.Run("binary", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			rows := query(t, t.Context(), db, `SELECT TYPE(BASE64_DECODE("eA==")) AS type, BASE64_DECODE("eA==") AS v`)
			equal(t, "a binary value", rows, [][]any{{"binary", "<binary (1 b)>"}})
		})
	})
	// Couchbase has no decimal type. Every number is a float64 on the
	// server, so 0.1 + 0.2 is not 0.3.
	t.Run("decimal", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			rows := query(t, t.Context(), db, "SELECT 0.1 + 0.2 AS sum, TYPE(1.5) AS t")
			equal(t, "a fraction", rows, [][]any{{0.30000000000000004, "number"}})
		})
	})
	t.Run("bytes into a byte slice", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-bytes"
			in := []byte{0xde, 0xad, 0xbe, 0xef}
			exec(t, db, "INSERT INTO "+types+" (KEY, VALUE) VALUES ($1, {\"v\": $2, \"text\": \"not base64!\"})", k, in)
			var got, text []byte
			if err := db.QueryRowContext(t.Context(), "SELECT t.text, t.v FROM "+types+" AS t USE KEYS $1", k).Scan(&text, &got); err != nil {
				t.Fatal(err)
			}
			equal(t, "the bytes", got, in)
			equal(t, "text that is not base64", text, []byte("not base64!"))
			exec(t, db, "DELETE FROM "+types+" USE KEYS $1", k)
		})
	})
}

// TestIntegrationSchema runs the operations on a schema that the survey
// names, and the refusals of the ones that Couchbase does not have.
func TestIntegrationSchema(t *testing.T) {
	admin := principals[0]
	refused := func(t *testing.T, db *sql.DB, stmt string, want int) {
		t.Helper()
		_, err := db.ExecContext(t.Context(), stmt)
		if code(err) != want {
			t.Errorf("%s gave %v, want the code %d", stmt, err, want)
		}
	}
	t.Run("scope and collection", func(t *testing.T) {
		db := openAs(t, admin)
		exec(t, db, "CREATE COLLECTION "+scope+".extra")
		exec(t, db, "DROP COLLECTION "+scope+".extra")
	})
	t.Run("primary index", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			rows := query(t, t.Context(), db, "SELECT RAW COUNT(*) FROM "+scope+".people")
			equal(t, "a count over the primary index", len(rows), 1)
		})
	})
	t.Run("secondary index", func(t *testing.T) {
		db := openAs(t, admin)
		rows := query(t, t.Context(), db, "SELECT RAW name FROM system:indexes WHERE keyspace_id = 'orders' AND scope_id = $1 AND name = 'person'", strings.TrimPrefix(scope, "dbmeta."))
		equal(t, "the secondary index", rows, [][]any{{"person"}})
	})
	t.Run("partial and array index", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			stmt := "CREATE INDEX tags_" + p.name + " ON " + scope + ".people(DISTINCT ARRAY t FOR t IN tags END) WHERE age > 0"
			if p.name == "ordinary" {
				refused(t, db, stmt, 13014)
				return
			}
			exec(t, db, stmt)
			exec(t, db, "DROP INDEX tags_"+p.name+" ON "+scope+".people")
		})
	})
	t.Run("vector index", func(t *testing.T) {
		db := openAs(t, admin)
		since(t, db, 8, 0, "a vector index")
		exec(t, db, "UPSERT INTO "+scope+".types (KEY k, VALUE v) SELECT \"vec\" || TOSTRING(a) AS k, {\"v\": [a, a + 1]} AS v FROM ARRAY_RANGE(0, 16) AS a")
		exec(t, db, "CREATE VECTOR INDEX vec ON "+scope+".types(v VECTOR) WITH {\"dimension\": 2, \"similarity\": \"L2\", \"description\": \"IVF1,SQ8\"}")
		rows := query(t, t.Context(), db, "SELECT RAW META(t).id FROM "+scope+".types AS t WHERE t.v IS VALUED ORDER BY APPROX_VECTOR_DISTANCE(t.v, [1, 2], \"L2\") LIMIT 2")
		equal(t, "the nearest two", rows, [][]any{{"vec1"}, {"vec0"}})
		exec(t, db, "DROP INDEX vec ON "+scope+".types")
		exec(t, db, "DELETE FROM "+scope+".types AS t WHERE META(t).id LIKE 'vec%'")
	})
	t.Run("sequence", func(t *testing.T) {
		db := openAs(t, admin)
		since(t, db, 7, 6, "a sequence")
		exec(t, db, "CREATE SEQUENCE "+scope+".seq")
		rows := query(t, t.Context(), db, "SELECT RAW NEXTVAL FOR "+scope+".seq")
		equal(t, "the first value", rows, [][]any{{int64(0)}})
		exec(t, db, "DROP SEQUENCE "+scope+".seq")
	})
	t.Run("inline function", func(t *testing.T) {
		db := openAs(t, admin)
		exec(t, db, "CREATE OR REPLACE FUNCTION dbimp_it_add(a, b) { a + b }")
		equal(t, "the call", query(t, t.Context(), db, "SELECT RAW dbimp_it_add(1, 2)"), [][]any{{int64(3)}})
		exec(t, db, "DROP FUNCTION dbimp_it_add")
	})
	t.Run("javascript function", func(t *testing.T) {
		db := openAs(t, admin)
		since(t, db, 7, 6, "a JavaScript function")
		exec(t, db, `CREATE OR REPLACE FUNCTION dbimp_it_js(a) LANGUAGE JAVASCRIPT AS "function dbimp_it_js(a) { return a * 2; }"`)
		equal(t, "the call", query(t, t.Context(), db, "SELECT RAW dbimp_it_js(21)"), [][]any{{int64(42)}})
		exec(t, db, "DROP FUNCTION dbimp_it_js")
	})
	t.Run("foreign key", func(t *testing.T) {
		refused(t, openAs(t, admin), "CREATE TABLE t (id INT PRIMARY KEY, other INT REFERENCES o(id))", 3000)
	})
	t.Run("unique constraint", func(t *testing.T) {
		refused(t, openAs(t, admin), "CREATE UNIQUE INDEX u ON "+scope+".people(name)", 3000)
	})
	t.Run("view", func(t *testing.T) {
		refused(t, openAs(t, admin), "CREATE VIEW v AS SELECT 1", 3000)
	})
	t.Run("default value", func(t *testing.T) {
		refused(t, openAs(t, admin), "CREATE TABLE t (id INT DEFAULT 1)", 3000)
	})
	t.Run("search index", func(t *testing.T) {
		rows := query(t, t.Context(), openAs(t, admin), "SELECT RAW META(p).id FROM "+scope+".people AS p WHERE SEARCH(p, \"x\")")
		equal(t, "SEARCH with no Search service", len(rows), 0)
	})
}

// TestIntegrationFeatures runs each feature of SQL++ that the survey names,
// and compares what it returns.
func TestIntegrationFeatures(t *testing.T) {
	people := scope + ".people"
	type feature struct {
		name  string
		since [2]int
		stmt  string
		opts  []couchbase.Option
		want  [][]any
		code  int
		check func(t *testing.T, rows [][]any)
	}
	features := []feature{
		{name: "use keys", stmt: "SELECT RAW META(p).id FROM " + people + " AS p USE KEYS \"none\"", want: nil},
		{name: "unnest", stmt: `SELECT RAW t FROM [{"a": [1, 2]}] AS d UNNEST d.a AS t`, want: [][]any{{int64(1)}, {int64(2)}}},
		{name: "nest", stmt: "SELECT RAW ARRAY_LENGTH(os) FROM [{\"id\": 1}] AS p NEST [{\"p\": 1}, {\"p\": 1}] AS os ON os.p = p.id", code: -1},
		{name: "missing", stmt: "SELECT MISSING IS MISSING AS m, TYPE(MISSING) AS t", want: [][]any{{true, "missing"}}},
		{name: "meta", stmt: "SELECT RAW META(p).id FROM " + people + " AS p LIMIT 0", want: nil},
		{name: "let", stmt: "SELECT RAW x FROM ARRAY_RANGE(0, 3) AS a LET x = a * 2", want: [][]any{{int64(0)}, {int64(2)}, {int64(4)}}},
		{name: "letting", stmt: "SELECT g, n FROM ARRAY_RANGE(0, 4) AS a GROUP BY a % 2 AS g LETTING n = COUNT(*) ORDER BY g", want: [][]any{{int64(0), int64(2)}, {int64(1), int64(2)}}},
		{name: "array comprehension", stmt: "SELECT ARRAY v * 2 FOR v IN [1, 2] END AS a, ANY v IN [1, 2] SATISFIES v > 1 END AS b", want: [][]any{{[]any{int64(2), int64(4)}, true}}},
		{name: "select raw, element and value", stmt: "SELECT ELEMENT 1", want: [][]any{{int64(1)}}},
		{name: "window function", stmt: "SELECT a, ROW_NUMBER() OVER (ORDER BY a DESC) AS rn FROM ARRAY_RANGE(0, 3) AS a ORDER BY a", want: [][]any{{int64(0), int64(3)}, {int64(1), int64(2)}, {int64(2), int64(1)}}},
		{name: "with", stmt: "WITH x AS (SELECT RAW 1) SELECT RAW x", want: [][]any{{[]any{int64(1)}}}},
		{name: "with recursive", since: [2]int{7, 6}, stmt: "WITH RECURSIVE r AS (SELECT 1 AS n UNION SELECT r.n + 1 AS n FROM r WHERE r.n < 3) SELECT RAW n FROM r", want: [][]any{{int64(1)}, {int64(2)}, {int64(3)}}},
		{name: "lateral", since: [2]int{7, 6}, stmt: "SELECT a, b FROM [1, 2] AS a, LATERAL (SELECT RAW a * 10) AS b", want: [][]any{{int64(1), int64(10)}, {int64(2), int64(20)}}},
		{name: "explain", stmt: "EXPLAIN SELECT RAW 1", check: func(t *testing.T, rows [][]any) {
			if len(rows) != 1 {
				t.Errorf("EXPLAIN gave %d rows, want one plan", len(rows))
			}
		}},
		{name: "advise", stmt: "ADVISE SELECT * FROM dbmeta WHERE x = 1", check: func(t *testing.T, rows [][]any) {
			if len(rows) != 1 {
				t.Errorf("ADVISE gave %d rows, want one advice", len(rows))
			}
		}},
		{name: "vector distance", since: [2]int{7, 6}, stmt: `SELECT RAW VECTOR_DISTANCE(d.v, [1, 2], "L2") FROM [{"v": [1, 3]}] AS d`, want: [][]any{{int64(1)}}},
		{name: "timeseries", since: [2]int{7, 6}, stmt: `SELECT t._t, t._v0 FROM [{"ts_start": 0, "ts_end": 10, "ts_interval": 5, "ts_data": [1, 2]}] AS d UNNEST _TIMESERIES(d, {"ts_ranges": [0, 10]}) AS t`, want: [][]any{{int64(0), int64(1)}, {int64(5), int64(2)}}},
		{name: "scan consistency request plus", stmt: "SELECT RAW 1", opts: []couchbase.Option{couchbase.WithScanConsistency("request_plus")}, want: [][]any{{int64(1)}}},
		{name: "query context", stmt: "SELECT RAW COUNT(*) FROM people WHERE META().id = 'none'", opts: []couchbase.Option{couchbase.WithQueryContext("default:" + scope)}, want: [][]any{{int64(0)}}},
		{name: "readonly", stmt: "UPSERT INTO " + people + " (KEY, VALUE) VALUES ('ro', {})", opts: []couchbase.Option{couchbase.WithReadonly(true)}, code: 1000},
		{name: "timeout", stmt: "SELECT COUNT(*) AS c FROM ARRAY_RANGE(0, 3000) AS a, ARRAY_RANGE(0, 3000) AS b", opts: []couchbase.Option{couchbase.WithTimeout(50 * time.Millisecond)}, code: 1080},
		{name: "client context id", stmt: "SELECT RAW 1", opts: []couchbase.Option{couchbase.WithParameter("client_context_id", "dbimp-it")}, want: [][]any{{int64(1)}}},
		{name: "metrics", stmt: "SELECT RAW 1", opts: []couchbase.Option{couchbase.WithParameter("metrics", false)}, want: [][]any{{int64(1)}}},
		{name: "profile", stmt: "SELECT RAW 1", opts: []couchbase.Option{couchbase.WithParameter("profile", "timings")}, want: [][]any{{int64(1)}}},
		{name: "max parallelism", stmt: "SELECT RAW 1", opts: []couchbase.Option{couchbase.WithParameter("max_parallelism", "2")}, want: [][]any{{int64(1)}}},
		{name: "scan and pipeline options", stmt: "SELECT RAW 1", opts: []couchbase.Option{couchbase.WithParameter("scan_cap", "16"), couchbase.WithParameter("pipeline_batch", "8")}, want: [][]any{{int64(1)}}},
		{name: "use replica", since: [2]int{7, 6}, stmt: "SELECT RAW 1", opts: []couchbase.Option{couchbase.WithParameter("use_replica", "on")}, want: [][]any{{int64(1)}}},
		{name: "flex index", stmt: "SELECT RAW 1", opts: []couchbase.Option{couchbase.WithParameter("use_fts", true)}, want: [][]any{{int64(1)}}},
		{name: "pivot", stmt: `SELECT * FROM [{"k": "a", "v": 1}] AS t PIVOT (SUM(v) FOR k IN ("a"))`, code: 3000},
		{name: "curl", stmt: `SELECT CURL("http://127.0.0.1:8091/pools")`, code: -1},
		{name: "scan consistency at plus", stmt: "SELECT RAW 1", opts: []couchbase.Option{couchbase.WithParameter("scan_consistency", "at_plus")}, code: 1050},
	}
	for _, f := range features {
		t.Run(f.name, func(t *testing.T) {
			forEach(t, func(t *testing.T, p principal, db *sql.DB) {
				if f.since != [2]int{} {
					since(t, db, f.since[0], f.since[1], f.name)
				}
				ctx := couchbase.WithOptions(t.Context(), f.opts...)
				rows, err := rowsOfCtx(t, ctx, db, f.stmt)
				switch {
				case f.code > 0:
					if code(err) != f.code {
						t.Errorf("%s gave %v, want the code %d", f.stmt, err, f.code)
					}
				case f.code < 0:
					// The server refuses it with a code that depends on the
					// principal, or returns rows. The survey takes either
					// answer, and an error here must be one of the server.
					if err != nil && code(err) == 0 {
						t.Errorf("%s gave %v, which is not an error of the server", f.stmt, err)
					}
				case err != nil:
					t.Errorf("%s: %v", f.stmt, err)
				case f.check != nil:
					f.check(t, rows)
				default:
					equal(t, f.stmt, rows, f.want)
				}
			})
		})
	}
	t.Run("prepare and execute", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			// The name holds the run, and the statement is deleted at the
			// end, so that a second run does not meet code 4060.
			name := strings.TrimPrefix(scope, "dbmeta.") + "_" + p.name
			if _, err := db.ExecContext(t.Context(), "PREPARE "+name+" FROM SELECT RAW $1"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx := context.WithoutCancel(t.Context())
				if _, err := db.ExecContext(ctx, "DELETE FROM system:prepareds WHERE name = $1", name); err != nil {
					t.Errorf("deleting the prepared statement %s: %v", name, err)
				}
			})
			equal(t, "EXECUTE", query(t, t.Context(), db, "EXECUTE "+name, 7), [][]any{{int64(7)}})
		})
	})
	t.Run("infer", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-infer"
			samples := scope + ".samples"
			exec(t, db, "UPSERT INTO "+samples+" (KEY, VALUE) VALUES ($1, {\"name\": \"Ada\"})", k)
			// INFER samples the documents at random, and can find none
			// for a moment after a write (7014), or for some seconds after
			// a collection of the bucket is dropped, so it is tried again.
			var rows [][]any
			var err error
			for range 120 {
				if rows, err = rowsOfCtx(t, t.Context(), db, "INFER "+samples); code(err) != 7014 {
					break
				}
				time.Sleep(250 * time.Millisecond)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Errorf("INFER gave %d rows, want one schema", len(rows))
			}
		})
	})
	t.Run("expiry", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-expiry"
			exec(t, db, "UPSERT INTO "+people+" (KEY, VALUE, OPTIONS) VALUES ($1, {\"n\": 1}, {\"expiration\": 3600})", k)
			rows := query(t, t.Context(), db, "SELECT RAW META(p).expiration FROM "+people+" AS p USE KEYS $1", k)
			if len(rows) != 1 || rows[0][0] == int64(0) {
				t.Errorf("the expiration is %v, want a time", rows)
			}
			exec(t, db, "DELETE FROM "+people+" USE KEYS $1", k)
		})
	})
	t.Run("preserve expiry", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-preserve"
			exec(t, db, "UPSERT INTO "+people+" (KEY, VALUE, OPTIONS) VALUES ($1, {\"n\": 1}, {\"expiration\": 3600})", k)
			rows := query(t, t.Context(), db, "SELECT RAW META(p).expiration FROM "+people+" AS p USE KEYS $1", k)
			ctx := couchbase.WithOptions(t.Context(), couchbase.WithParameter("preserve_expiry", true))
			if _, err := db.ExecContext(ctx, "UPDATE "+people+" AS p USE KEYS $1 SET p.n = 2", k); err != nil {
				t.Fatal(err)
			}
			again := query(t, t.Context(), db, "SELECT RAW META(p).expiration FROM "+people+" AS p USE KEYS $1", k)
			equal(t, "the expiration after an update that keeps it", again, rows)
			exec(t, db, "DELETE FROM "+people+" USE KEYS $1", k)
		})
	})
}

// TestIntegrationTransactions runs a transaction of the query service
// through database/sql (D41, D43, D45 and D46).
func TestIntegrationTransactions(t *testing.T) {
	people := scope + ".people"
	count := func(t *testing.T, db *sql.DB, k string) int64 {
		t.Helper()
		rows := query(t, t.Context(), db, "SELECT RAW COUNT(*) FROM "+people+" AS p USE KEYS $1", k)
		n, _ := rows[0][0].(int64)
		return n
	}
	t.Run("transaction", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-tx"
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(t.Context(), "UPSERT INTO "+people+" (KEY, VALUE) VALUES ($1, {\"v\": 1})", k); err != nil {
				t.Fatal(err)
			}
			equal(t, "a write seen outside the transaction", count(t, db, k), int64(0))
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			equal(t, "the write after the commit", count(t, db, k), int64(1))
			exec(t, db, "DELETE FROM "+people+" USE KEYS $1", k)
		})
	})
	t.Run("rollback", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-rollback"
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(t.Context(), "UPSERT INTO "+people+" (KEY, VALUE) VALUES ($1, {\"v\": 1})", k); err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			equal(t, "the write after the rollback", count(t, db, k), int64(0))
		})
	})
	t.Run("savepoint", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			k1, k2 := p.name+"-sp1", p.name+"-sp2"
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range []struct {
				stmt string
				args []any
			}{
				{"UPSERT INTO " + people + " (KEY, VALUE) VALUES ($1, {})", []any{k1}},
				{"SAVEPOINT s1", nil},
				{"UPSERT INTO " + people + " (KEY, VALUE) VALUES ($1, {})", []any{k2}},
				{"ROLLBACK WORK TO SAVEPOINT s1", nil},
			} {
				if _, err := tx.ExecContext(t.Context(), s.stmt, s.args...); err != nil {
					t.Fatalf("%s: %v", s.stmt, err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			equal(t, "the write before the savepoint", count(t, db, k1), int64(1))
			equal(t, "the write after the savepoint", count(t, db, k2), int64(0))
			exec(t, db, "DELETE FROM "+people+" USE KEYS [$1, $2]", k1, k2)
		})
	})
	t.Run("durability level", func(t *testing.T) {
		db := openAs(t, principals[0])
		ctx := couchbase.WithOptions(t.Context(), couchbase.WithDurability("majority"))
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "UPSERT INTO "+people+" (KEY, VALUE) VALUES ('majority', {})"); err != nil {
			t.Fatal(err)
		}
		// The one node of dbrun cannot meet majority (D43).
		if err := tx.Commit(); code(err) != 17007 {
			t.Errorf("a commit with majority on one node gave %v, want the code 17007", err)
		}
	})
	t.Run("single statement transaction", func(t *testing.T) {
		db := openAs(t, principals[0])
		ctx := couchbase.WithOptions(t.Context(), couchbase.WithParameter("tximplicit", true), couchbase.WithParameter("durability_level", "none"))
		if _, err := db.ExecContext(ctx, "UPSERT INTO "+people+" (KEY, VALUE) VALUES ('implicit', {})"); err != nil {
			t.Fatal(err)
		}
		exec(t, db, "DELETE FROM "+people+" USE KEYS 'implicit'")
	})
	t.Run("transaction timeout", func(t *testing.T) {
		db := openAs(t, principals[0])
		ctx := couchbase.WithOptions(t.Context(), couchbase.WithTransactionTimeout(time.Second))
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		_, err = tx.ExecContext(ctx, "SELECT RAW 1")
		if code(err) != 17010 && code(err) != 17004 {
			t.Errorf("a statement after the timeout of the transaction gave %v, want the code 17010 or 17004", err)
		}
		_ = tx.Rollback()
	})
	t.Run("isolation", func(t *testing.T) {
		db := openAs(t, principals[0])
		_, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable})
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("an isolation level gave %v, want ErrNotSupported", err)
		}
	})
	t.Run("read only", func(t *testing.T) {
		db := openAs(t, principals[0])
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		_, err = tx.ExecContext(t.Context(), "UPSERT INTO "+people+" (KEY, VALUE) VALUES ('ro', {})")
		if code(err) != 1000 {
			t.Errorf("a write in a read only transaction gave %v, want the code 1000", err)
		}
	})
}

// TestIntegrationVersion reads the version with the statement that usql
// runs, as each principal (step 16).
func TestIntegrationVersion(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		var v string
		if err := db.QueryRowContext(t.Context(), "SELECT RAW ds_version()").Scan(&v); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(v, "-") || slices.Contains([]string{"", "<unknown>"}, v) {
			t.Errorf("the version is %q", v)
		}
		t.Logf("the %s user reads the version %s", p.name, v)
	})
}
