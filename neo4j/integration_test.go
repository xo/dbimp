package neo4j //nolint:testpackage // The round trip writes its literals with the literal writer here, and reads the release with calendarAtLeast.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// These tests need a server. NEO4J_DSN names it for the administrator, and
// NEO4J_ORDINARY_DSN for the ordinary user (D9). A test skips when its DSN is
// empty. Each label, index, constraint, database and alias that the tests
// make has a prefix of its own, which TestMain removes at the end.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each label, index and constraint that the tests make.
var prefix = "DbimpIt_" + suffix

// dbPrefix starts each database and alias that the tests make. A name of a
// database takes no underscore.
var dbPrefix = "dbimpit-" + suffix

// label returns the label name of the tests, quoted for Cypher.
func label(name string) string {
	return "`" + prefix + "_" + strings.ReplaceAll(name, " ", "_") + "`"
}

// named returns the name of an index or a constraint of the tests.
func named(name string) string {
	return "`" + prefix + "_" + strings.ReplaceAll(name, " ", "_") + "`"
}

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "NEO4J_DSN"}
	ordinary   = principal{"ordinary", "NEO4J_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

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

// openIn opens the server as p in the database dbName.
func openIn(t *testing.T, p principal, dbName string) *sql.DB {
	t.Helper()
	cfg := config(t, p)
	cfg.Database = dbName
	db := sql.OpenDB(NewConnector(cfg))
	t.Cleanup(func() { db.Close() })
	return db
}

// config returns the configuration of the DSN of p.
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

// run makes the index of the CRUD tests when NEO4J_DSN names a server, runs
// the tests, and removes every node, index, constraint, database and alias
// with the prefix.
func run(m *testing.M) (int, error) {
	dsn := os.Getenv("NEO4J_DSN")
	if dsn == "" {
		return m.Run(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cfg, err := ParseDSN(dsn)
	if err != nil {
		return 1, err
	}
	db := sql.OpenDB(NewConnector(*cfg))
	defer db.Close()
	sys := *cfg
	sys.Database = "system"
	sysDB := sql.OpenDB(NewConnector(sys))
	defer sysDB.Close()
	// The ordinary user cannot make an index, so the administrator makes the
	// one that the CRUD tests use.
	if _, err := db.ExecContext(ctx, "CREATE INDEX "+named("people k")+" FOR (n:"+label("people")+") ON (n.k)"); err != nil {
		return 1, fmt.Errorf("making the index of the tests: %w", err)
	}
	if _, err := db.ExecContext(ctx, "CALL db.awaitIndexes(300)"); err != nil {
		return 1, fmt.Errorf("waiting for the index of the tests: %w", err)
	}
	code := m.Run()
	if err := cleanup(ctx, db, sysDB); err != nil {
		return 1, err
	}
	if left, err := leftovers(ctx, db, sysDB); err != nil || len(left) > 0 {
		return 1, fmt.Errorf("the tests left %q after the cleanup: %w", left, err)
	}
	return code, nil
}

// cleanup removes what the tests made.
func cleanup(ctx context.Context, db, sysDB *sql.DB) error {
	if _, err := db.ExecContext(ctx, "MATCH (n) WHERE any(l IN labels(n) WHERE l STARTS WITH $p) DETACH DELETE n", sql.Named("p", prefix)); err != nil {
		return fmt.Errorf("removing the nodes of the tests: %w", err)
	}
	for _, kind := range []string{"CONSTRAINT", "INDEX"} {
		plural := map[string]string{"CONSTRAINT": "CONSTRAINTS", "INDEX": "INDEXES"}[kind]
		names, err := strings1(ctx, db, "SHOW "+plural+" YIELD name WHERE name STARTS WITH $p RETURN name", sql.Named("p", prefix))
		if err != nil {
			return err
		}
		for _, name := range names {
			if _, err := db.ExecContext(ctx, "DROP "+kind+" `"+name+"` IF EXISTS"); err != nil {
				return fmt.Errorf("dropping the %s %s: %w", strings.ToLower(kind), name, err)
			}
		}
	}
	aliases, err := strings1(ctx, sysDB, "SHOW ALIASES FOR DATABASE YIELD name WHERE name STARTS WITH $p RETURN name", sql.Named("p", dbPrefix))
	if err != nil {
		return err
	}
	for _, name := range aliases {
		if _, err := sysDB.ExecContext(ctx, "DROP ALIAS `"+name+"` IF EXISTS FOR DATABASE"); err != nil {
			return fmt.Errorf("dropping the alias %s: %w", name, err)
		}
	}
	dbs, err := strings1(ctx, sysDB, "SHOW DATABASES YIELD name, type WHERE name STARTS WITH $p RETURN DISTINCT name + ' ' + type", sql.Named("p", dbPrefix))
	if err != nil {
		return err
	}
	for _, nt := range dbs {
		name, typ, _ := strings.Cut(nt, " ")
		stmt := "DROP DATABASE `" + name + "` IF EXISTS WAIT"
		if typ == "composite" {
			stmt = "DROP COMPOSITE DATABASE `" + name + "` IF EXISTS"
		}
		if _, err := sysDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("dropping the database %s: %w", name, err)
		}
	}
	return nil
}

// leftovers returns what the tests made that is still there.
func leftovers(ctx context.Context, db, sysDB *sql.DB) ([]string, error) {
	var left []string
	for _, q := range []struct {
		db   *sql.DB
		stmt string
		p    string
	}{
		{db, "MATCH (n) WHERE any(l IN labels(n) WHERE l STARTS WITH $p) RETURN 'node ' + elementId(n)", prefix},
		{db, "SHOW INDEXES YIELD name WHERE name STARTS WITH $p RETURN 'index ' + name", prefix},
		{db, "SHOW CONSTRAINTS YIELD name WHERE name STARTS WITH $p RETURN 'constraint ' + name", prefix},
		{sysDB, "SHOW DATABASES YIELD name WHERE name STARTS WITH $p RETURN DISTINCT 'database ' + name", dbPrefix},
		{sysDB, "SHOW ALIASES FOR DATABASE YIELD name WHERE name STARTS WITH $p RETURN 'alias ' + name", dbPrefix},
	} {
		names, err := strings1(ctx, q.db, q.stmt, sql.Named("p", q.p))
		if err != nil {
			return nil, err
		}
		left = append(left, names...)
	}
	return left, nil
}

// strings1 returns the first column of each row of stmt, as strings.
func strings1(ctx context.Context, db *sql.DB, stmt string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", stmt, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// release returns the release of the server of db, such as 2026.09.0.
func release(t *testing.T, db *sql.DB) string {
	t.Helper()
	var v string
	if err := db.QueryRowContext(t.Context(), "CALL dbms.components() YIELD name, versions WHERE name = 'Neo4j Kernel' RETURN versions[0]").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// since skips the test on a release older than year.month, the monthly
// release in which what arrived.
func since(t *testing.T, db *sql.DB, year, month int, what string) {
	t.Helper()
	if v := release(t, db); !calendarAtLeast(v, year, month) {
		t.Skipf("%s arrived in %d.%02d, and the server is %s (docs/NEO4J.md)", what, year, month, v)
	}
}

// query returns every row of stmt, read into *any.
func query(t *testing.T, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, stmt string, args ...any,
) [][]any {
	t.Helper()
	rows, err := queryErr(t, db, stmt, args...)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return rows
}

// queryErr returns every row of stmt, and the error of the query.
func queryErr(t *testing.T, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, stmt string, args ...any,
) ([][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), stmt, args...)
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

// exec runs stmt, and fails the test on an error.
func exec(t *testing.T, db interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, stmt string, args ...any,
) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), stmt, args...); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

// code returns the code of the first error of the server in err, or "".
func code(err error) string {
	if e, ok := errors.AsType[Error](err); ok {
		return e.Code
	}
	return ""
}

// refused fails the test unless err is an error of the server with the code
// want.
func refused(t *testing.T, what string, err error, want string) {
	t.Helper()
	if got := code(err); got != want {
		t.Errorf("%s gave %v, want the error %s", what, err, want)
	}
}

// equal fails the test unless got and want are deeply equal.
func equal(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s is %#v, want %#v", what, got, want)
	}
}

// ddl runs the statement of a schema as the administrator. The ordinary user
// cannot make an index, a constraint or a database, so for the ordinary user
// it checks the refusal, and the administrator makes it (step 14a).
func ddl(t *testing.T, p principal, db *sql.DB, stmt string) bool {
	t.Helper()
	if p == ordinary {
		_, err := db.ExecContext(t.Context(), stmt)
		refused(t, "the statement of a schema as the ordinary user", err, "Neo.ClientError.Security.Forbidden")
		t.Log("the ordinary user cannot run this statement, so only the administrator runs it")
		return false
	}
	exec(t, db, stmt)
	return true
}

// post sends body to the Query API as p, as a caller of HTTP does, for a
// feature that the driver does not use, such as bookmarks.
func post(t *testing.T, p principal, body map[string]any) (int, map[string]any) {
	t.Helper()
	cfg := config(t, p)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, cfg.baseURL()+"/db/"+cfg.Database+"/query/v2", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(cfg.User, cfg.Password)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err := json.UnmarshalRead(res.Body, &out); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, out
}

func TestIntegrationCRUD(t *testing.T) {
	people, movies := label("people"), label("movies")
	steps := []struct {
		name string
		f    func(t *testing.T, p principal, db *sql.DB)
	}{
		{"insert", func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-i1"
			exec(t, db, "CREATE (:"+people+" {k: $1, name: 'Ada', age: 36})", k)
			equal(t, "the row", query(t, db, "MATCH (n:"+people+" {k: $1}) RETURN n.name, n.age", k), [][]any{{"Ada", int64(36)}})
		}},
		{"select", func(t *testing.T, p principal, db *sql.DB) {
			exec(t, db, "CREATE (:"+people+" {k: $1, name: 'Ada', age: 36}), (:"+people+" {k: $2, name: 'Bo', age: 20})", p.name+"-s1", p.name+"-s2")
			// The index on k serves the query (TestMain makes it).
			rows := query(t, db, "MATCH (n:"+people+") WHERE n.k IN [$1, $2] AND n.age > 30 RETURN n.name", p.name+"-s1", p.name+"-s2")
			equal(t, "the rows", rows, [][]any{{"Ada"}})
		}},
		{"update", func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-u1"
			exec(t, db, "CREATE (:"+people+" {k: $1, age: 36})", k)
			exec(t, db, "MATCH (n:"+people+" {k: $1}) SET n.age = n.age + 1, n.name = 'Ada L'", k)
			equal(t, "the row", query(t, db, "MATCH (n:"+people+" {k: $1}) RETURN n.age, n.name", k), [][]any{{int64(37), "Ada L"}})
		}},
		{"delete", func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-d1"
			exec(t, db, "CREATE (:"+people+" {k: $1})", k)
			exec(t, db, "MATCH (n:"+people+" {k: $1}) DELETE n", k)
			equal(t, "the rows after the delete", query(t, db, "MATCH (n:"+people+" {k: $1}) RETURN n.k", k), [][]any(nil))
		}},
		{"remove", func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-r1"
			exec(t, db, "CREATE (:"+people+" {k: $1, age: 36})", k)
			exec(t, db, "MATCH (n:"+people+" {k: $1}) REMOVE n.age", k)
			equal(t, "the age after the remove", query(t, db, "MATCH (n:"+people+" {k: $1}) RETURN n.age", k), [][]any{{nil}})
		}},
		{"detach delete", func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-dd"
			exec(t, db, "CREATE (:"+people+" {k: $1})-[:ACTED_IN]->(:"+movies+" {k: $1})", k)
			_, err := db.ExecContext(t.Context(), "MATCH (n:"+people+" {k: $1}) DELETE n", k)
			refused(t, "a DELETE of a node with a relationship", err, "Neo.ClientError.Schema.ConstraintValidationFailed")
			exec(t, db, "MATCH (n:"+people+" {k: $1}) DETACH DELETE n", k)
			equal(t, "the relationships after the delete", query(t, db, "MATCH (:"+movies+" {k: $1})<-[r]-() RETURN count(r)", k), [][]any{{int64(0)}})
		}},
		{"merge", func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-m1"
			for range 2 {
				exec(t, db, "MERGE (n:"+movies+" {k: $1}) ON CREATE SET n.c = 1 ON MATCH SET n.c = n.c + 1", k)
			}
			equal(t, "the count", query(t, db, "MATCH (n:"+movies+" {k: $1}) RETURN count(n), max(n.c)", k), [][]any{{int64(1), int64(2)}})
		}},
		{"unwind batch insert", func(t *testing.T, p principal, db *sql.DB) {
			rows := []any{map[string]any{"k": p.name + "-b1", "n": 1}, map[string]any{"k": p.name + "-b2", "n": 2}}
			exec(t, db, "UNWIND $1 AS row CREATE (:"+movies+" {k: row.k, n: row.n})", rows)
			equal(t, "the rows", query(t, db, "MATCH (n:"+movies+") WHERE n.k STARTS WITH $1 RETURN n.k, n.n ORDER BY n.k", p.name+"-b"),
				[][]any{{p.name + "-b1", int64(1)}, {p.name + "-b2", int64(2)}})
		}},
		{"foreach", func(t *testing.T, p principal, db *sql.DB) {
			exec(t, db, "FOREACH (i IN range(1, 3) | CREATE (:"+movies+" {k: $1 + i}))", p.name+"-f")
			equal(t, "the count", query(t, db, "MATCH (n:"+movies+") WHERE n.k STARTS WITH $1 RETURN count(n)", p.name+"-f"), [][]any{{int64(3)}})
		}},
		{"load csv", func(t *testing.T, _ principal, db *sql.DB) {
			// The server runs LOAD CSV, and the import folder of dbrun holds no
			// file, so it fails at the file and not at the syntax (docs/NEO4J.md).
			_, err := queryErr(t, db, "LOAD CSV FROM 'file:///dbimp.csv' AS row RETURN row")
			refused(t, "LOAD CSV of a file that does not exist", err, "Neo.ClientError.Statement.ExternalResourceFailed")
		}},
		{"call in transactions", func(t *testing.T, p principal, db *sql.DB) {
			exec(t, db, "UNWIND range(1, 4) AS i CALL (i) { CREATE (:"+movies+" {k: $1 + i}) } IN TRANSACTIONS OF 2 ROWS", p.name+"-cit")
			equal(t, "the count", query(t, db, "MATCH (n:"+movies+") WHERE n.k STARTS WITH $1 RETURN count(n)", p.name+"-cit"), [][]any{{int64(4)}})
			// Inside an explicit transaction it is refused (measured).
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.ExecContext(t.Context(), "UNWIND range(1, 2) AS i CALL (i) { CREATE (:"+movies+" {k: 'x'}) } IN TRANSACTIONS")
			refused(t, "CALL IN TRANSACTIONS in a transaction", err, "Neo.DatabaseError.Transaction.TransactionStartFailed")
			if err := tx.Rollback(); err != nil {
				t.Errorf("the rollback after the error gave %v", err)
			}
		}},
	}
	for _, step := range steps {
		t.Run(strings.ReplaceAll(step.name, " ", "_"), func(t *testing.T) {
			forEach(t, step.f)
		})
	}
}

func TestIntegrationSchema(t *testing.T) {
	type schema struct {
		name string
		f    func(t *testing.T, p principal, db *sql.DB)
	}
	// index makes an index as the administrator, finds it with SHOW
	// INDEXES, runs use, and drops it.
	index := func(name, stmt, typ string, use func(t *testing.T, db *sql.DB)) schema {
		return schema{name, func(t *testing.T, p principal, db *sql.DB) {
			if !ddl(t, p, db, stmt) {
				return
			}
			exec(t, db, "CALL db.awaitIndexes(300)")
			equal(t, "the index", query(t, db, "SHOW INDEXES YIELD name, type WHERE name = $1 RETURN type", strings.Trim(named(name), "`")), [][]any{{typ}})
			if use != nil {
				use(t, db)
			}
			exec(t, db, "DROP INDEX "+named(name))
		}}
	}
	// constraint makes a constraint as the administrator, runs the write
	// that it refuses, and drops it.
	constraint := func(name, stmt, bad, want string) schema {
		return schema{name, func(t *testing.T, p principal, db *sql.DB) {
			if !ddl(t, p, db, stmt) {
				return
			}
			_, err := db.ExecContext(t.Context(), bad)
			refused(t, "a write that the constraint "+name+" refuses", err, want)
			exec(t, db, "DROP CONSTRAINT "+named(name))
		}}
	}
	l := label("schema")
	schemas := []schema{
		{"uniqueness constraint", func(t *testing.T, p principal, db *sql.DB) {
			if p == ordinary {
				ddl(t, p, db, "CREATE CONSTRAINT "+named("u")+" FOR (n:"+l+") REQUIRE n.u IS UNIQUE")
				return
			}
			exec(t, db, "CREATE CONSTRAINT "+named("u")+" FOR (n:"+l+") REQUIRE n.u IS UNIQUE")
			exec(t, db, "CREATE (:"+l+" {u: 1})")
			_, err := db.ExecContext(t.Context(), "CREATE (:"+l+" {u: 1})")
			refused(t, "a second node with the same u", err, "Neo.ClientError.Schema.ConstraintValidationFailed")
			exec(t, db, "DROP CONSTRAINT "+named("u"))
			exec(t, db, "MATCH (n:"+l+") DELETE n")
		}},
		constraint("node key constraint", "CREATE CONSTRAINT "+named("node key constraint")+" FOR (n:"+l+") REQUIRE (n.a, n.b) IS NODE KEY",
			"CREATE (:"+l+" {a: 1})", "Neo.ClientError.Schema.ConstraintValidationFailed"),
		constraint("existence constraint", "CREATE CONSTRAINT "+named("existence constraint")+" FOR (n:"+l+") REQUIRE n.e IS NOT NULL",
			"CREATE (:"+l+" {x: 1})", "Neo.ClientError.Schema.ConstraintValidationFailed"),
		constraint("property type constraint", "CREATE CONSTRAINT "+named("property type constraint")+" FOR (n:"+l+") REQUIRE n.t IS :: INTEGER",
			"CREATE (:"+l+" {t: 'a'})", "Neo.ClientError.Schema.ConstraintValidationFailed"),
		index("range index", "CREATE RANGE INDEX "+named("range index")+" FOR (n:"+l+") ON (n.r)", "RANGE", func(t *testing.T, db *sql.DB) {
			exec(t, db, "CREATE (:"+l+" {r: 5})")
			equal(t, "a read through the index", query(t, db, "MATCH (n:"+l+") WHERE n.r > 4 RETURN n.r"), [][]any{{int64(5)}})
			exec(t, db, "MATCH (n:"+l+") DELETE n")
		}),
		index("text index", "CREATE TEXT INDEX "+named("text index")+" FOR (n:"+l+") ON (n.s)", "TEXT", func(t *testing.T, db *sql.DB) {
			exec(t, db, "CREATE (:"+l+" {s: 'héllo'})")
			equal(t, "a read through the index", query(t, db, "MATCH (n:"+l+") WHERE n.s CONTAINS 'éll' RETURN n.s"), [][]any{{"héllo"}})
			exec(t, db, "MATCH (n:"+l+") DELETE n")
		}),
		index("point index", "CREATE POINT INDEX "+named("point index")+" FOR (n:"+l+") ON (n.p)", "POINT", func(t *testing.T, db *sql.DB) {
			exec(t, db, "CREATE (:"+l+" {p: point({x: 1, y: 1})})")
			equal(t, "a read through the index", query(t, db, "MATCH (n:"+l+") WHERE point.distance(n.p, point({x: 0, y: 0})) < 2 RETURN n.p"),
				[][]any{{Point{SRID: 7203, X: 1, Y: 1, Dims: 2}}})
			exec(t, db, "MATCH (n:"+l+") DELETE n")
		}),
		index("full text index", "CREATE FULLTEXT INDEX "+named("full text index")+" FOR (n:"+l+") ON EACH [n.ft]", "FULLTEXT", func(t *testing.T, db *sql.DB) {
			exec(t, db, "CREATE (:"+l+" {ft: 'the quick fox'})")
			// A full text index is updated after the write, so the read waits
			// for it, with a limit on the time.
			deadline := time.Now().Add(30 * time.Second)
			for {
				rows := query(t, db, "CALL db.index.fulltext.queryNodes($1, 'quick') YIELD node RETURN node.ft", strings.Trim(named("full text index"), "`"))
				if len(rows) == 1 {
					equal(t, "a read through the index", rows, [][]any{{"the quick fox"}})
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the full text index found nothing after 30 seconds")
				}
				time.Sleep(100 * time.Millisecond)
			}
			exec(t, db, "MATCH (n:"+l+") DELETE n")
		}),
		index("vector index", "CREATE VECTOR INDEX "+named("vector index")+" FOR (n:"+l+") ON n.v OPTIONS {indexConfig: {`vector.dimensions`: 2, `vector.similarity_function`: 'cosine'}}", "VECTOR", nil),
		{"lookup index", func(t *testing.T, _ principal, db *sql.DB) {
			// Every database has the two lookup indexes, and every user reads
			// them.
			equal(t, "the lookup indexes", query(t, db, "SHOW INDEXES YIELD type WHERE type = 'LOOKUP' RETURN count(*)"), [][]any{{int64(2)}})
		}},
		index("composite index", "CREATE INDEX "+named("composite index")+" FOR (n:"+l+") ON (n.a, n.b)", "RANGE", func(t *testing.T, db *sql.DB) {
			exec(t, db, "CREATE (:"+l+" {a: 1, b: 2})")
			equal(t, "a read through the index", query(t, db, "MATCH (n:"+l+") WHERE n.a = 1 AND n.b = 2 RETURN n.a + n.b"), [][]any{{int64(3)}})
			exec(t, db, "MATCH (n:"+l+") DELETE n")
		}),
		{"create database", func(t *testing.T, p principal, _ *sql.DB) {
			sys := openIn(t, p, "system")
			name := dbPrefix + "-db"
			if !ddl(t, p, sys, "CREATE DATABASE `"+name+"` WAIT") {
				return
			}
			equal(t, "the database", query(t, sys, "SHOW DATABASE `"+name+"` YIELD name RETURN DISTINCT name"), [][]any{{name}})
			in := openIn(t, p, name)
			equal(t, "a statement in the database", query(t, in, "RETURN 1 AS a"), [][]any{{int64(1)}})
			in.Close()
			exec(t, sys, "DROP DATABASE `"+name+"` WAIT")
		}},
		{"database alias", func(t *testing.T, p principal, db *sql.DB) {
			sys := openIn(t, p, "system")
			name := dbPrefix + "-alias"
			target := config(t, p).Database
			if !ddl(t, p, sys, "CREATE ALIAS `"+name+"` FOR DATABASE `"+target+"`") {
				return
			}
			in := openIn(t, p, name)
			equal(t, "a statement through the alias", query(t, in, "RETURN 1 AS a"), [][]any{{int64(1)}})
			in.Close()
			exec(t, sys, "DROP ALIAS `"+name+"` FOR DATABASE")
		}},
		{"composite database", func(t *testing.T, p principal, _ *sql.DB) {
			sys := openIn(t, p, "system")
			name := dbPrefix + "-comp"
			if !ddl(t, p, sys, "CREATE COMPOSITE DATABASE `"+name+"`") {
				return
			}
			equal(t, "the database", query(t, sys, "SHOW DATABASE `"+name+"` YIELD type RETURN DISTINCT type"), [][]any{{"composite"}})
			exec(t, sys, "DROP COMPOSITE DATABASE `"+name+"`")
		}},
	}
	for _, s := range schemas {
		t.Run(strings.ReplaceAll(s.name, " ", "_"), func(t *testing.T) {
			forEach(t, s.f)
		})
	}
}

func TestIntegrationFeatures(t *testing.T) {
	g := label("graph")
	type feature struct {
		name string
		f    func(t *testing.T, p principal, db *sql.DB)
	}
	// simple is a feature whose statement, with the graph of the test,
	// returns want.
	simple := func(name, stmt string, want [][]any) feature {
		return feature{name, func(t *testing.T, p principal, db *sql.DB) {
			equal(t, stmt, query(t, db, stmt, p.name), want)
		}}
	}
	features := []feature{
		{"cypher 25", func(t *testing.T, _ principal, db *sql.DB) {
			since(t, db, 2025, 6, "Cypher 25")
			equal(t, "a statement of Cypher 25", query(t, db, "CYPHER 25 RETURN 1 AS a"), [][]any{{int64(1)}})
		}},
		simple("variable length path", "MATCH (a:"+g+" {p: $1, k: 1})-[:NEXT*1..2]->(b) RETURN b.k ORDER BY b.k", [][]any{{int64(2)}, {int64(3)}}),
		simple("shortest path", "MATCH p = shortestPath((a:"+g+" {p: $1, k: 1})-[:NEXT*]->(b:"+g+" {p: $1, k: 3})) RETURN length(p)", [][]any{{int64(2)}}),
		simple("quantified path pattern", "MATCH (a:"+g+" {p: $1, k: 1}) (()-[:NEXT]->()){1,2} (b) RETURN b.k ORDER BY b.k", [][]any{{int64(2)}, {int64(3)}}),
		simple("optional match", "MATCH (a:"+g+" {p: $1, k: 3}) OPTIONAL MATCH (a)-[:NEXT]->(b) RETURN b", [][]any{{nil}}),
		simple("with", "MATCH (a:"+g+" {p: $1}) WITH a.k AS k ORDER BY k WITH collect(k) AS ks RETURN ks", [][]any{{[]any{int64(1), int64(2), int64(3)}}}),
		simple("union", "MATCH (a:"+g+" {p: $1, k: 1}) RETURN a.k AS x UNION MATCH (a:"+g+" {p: $1, k: 2}) RETURN a.k AS x", [][]any{{int64(1)}, {int64(2)}}),
		simple("call subquery", "CALL (){ MATCH (a:"+g+" {p: $1}) RETURN max(a.k) AS inner } RETURN inner", [][]any{{int64(3)}}),
		simple("collect and count subquery", "MATCH (a:"+g+" {p: $1, k: 1}) RETURN COUNT { (a)-[:NEXT]->() } AS c, COLLECT { MATCH (x:"+g+" {p: $1}) RETURN x.k ORDER BY x.k } AS ks",
			[][]any{{int64(1), []any{int64(1), int64(2), int64(3)}}}),
		simple("exists subquery", "MATCH (a:"+g+" {p: $1, k: 1}) RETURN EXISTS { (a)-[:NEXT]->() } AS e", [][]any{{true}}),
		simple("list comprehension", "RETURN [x IN range(1, 3) WHERE x > 1 | x * 10] AS l, $1 AS p", nil),
		simple("pattern comprehension", "MATCH (a:"+g+" {p: $1, k: 1}) RETURN [(a)-[:NEXT]->(b) | b.k] AS l", [][]any{{[]any{int64(2)}}}),
		simple("case", "MATCH (a:"+g+" {p: $1, k: 1}) RETURN CASE a.k WHEN 1 THEN 'yes' ELSE 'no' END AS c", [][]any{{"yes"}}),
		{"apoc", func(t *testing.T, _ principal, db *sql.DB) {
			_, err := queryErr(t, db, "RETURN apoc.version() AS v")
			refused(t, "APOC, which the image of dbrun does not hold", err, "Neo.ClientError.Statement.SyntaxError")
		}},
		{"trailing semicolon", func(t *testing.T, _ principal, db *sql.DB) {
			// A ; ends the statement before the tag, on the line above it (D95).
			equal(t, "a statement that ends with ;", query(t, db, "RETURN 1 AS a;"), [][]any{{int64(1)}})
		}},
		{"error position", func(t *testing.T, _ principal, db *sql.DB) {
			// The tag at the end leaves the position where the caller wrote
			// it, and keeps it out of the message (D95).
			_, err := queryErr(t, db, "RETURN 1 AS a,, 2")
			refused(t, "a syntax error", err, "Neo.ClientError.Statement.SyntaxError")
			if err == nil || !strings.Contains(err.Error(), "line 1, column 15 (offset: 14)") || strings.Contains(err.Error(), "dbimp:") {
				t.Errorf("the syntax error is %v, want line 1, column 15, and no comment of the connection", err)
			}
		}},
		{"show databases", func(t *testing.T, p principal, db *sql.DB) {
			equal(t, "the database of the DSN", query(t, db, "SHOW DATABASES YIELD name WHERE name = $1 RETURN DISTINCT name", config(t, p).Database), [][]any{{config(t, p).Database}})
		}},
		{"show indexes", func(t *testing.T, _ principal, db *sql.DB) {
			equal(t, "the index of the tests", query(t, db, "SHOW INDEXES YIELD name WHERE name = $1 RETURN count(*)", strings.Trim(named("people k"), "`")), [][]any{{int64(1)}})
		}},
		{"show transactions", func(t *testing.T, _ principal, db *sql.DB) {
			// The text of this statement ends with the tag only if the driver
			// sent it, so the match holds the tag and not the literal (D95).
			rows := query(t, db, "SHOW TRANSACTIONS YIELD currentQuery WHERE currentQuery =~ '(?s).*\\n// dbimp:[A-Z2-7]+' RETURN count(*) > 0")
			equal(t, "the transaction of the statement itself", rows, [][]any{{true}})
		}},
		{"terminate transaction", func(t *testing.T, _ principal, db *sql.DB) {
			rows := query(t, db, "TERMINATE TRANSACTION 'dbmeta-transaction-999999999'")
			if len(rows) != 1 || !strings.Contains(fmt.Sprint(rows[0]...), "not found") {
				t.Errorf("TERMINATE of a transaction that does not exist gave %v", rows)
			}
		}},
		{"explain and profile", func(t *testing.T, p principal, db *sql.DB) {
			equal(t, "EXPLAIN", query(t, db, "EXPLAIN MATCH (a:"+g+" {p: $1}) RETURN a.k", p.name), [][]any(nil))
			equal(t, "PROFILE", len(query(t, db, "PROFILE MATCH (a:"+g+" {p: $1}) RETURN a.k", p.name)), 3)
		}},
		{"use clause", func(t *testing.T, p principal, db *sql.DB) {
			equal(t, "a statement with USE", query(t, db, "USE `"+config(t, p).Database+"` RETURN 1 AS a"), [][]any{{int64(1)}})
		}},
		{"parameters", func(t *testing.T, _ principal, db *sql.DB) {
			// An argument has the ordinal of its place among every argument,
			// so the positional argument after a named one fills $2 (D64).
			equal(t, "named and positional parameters", query(t, db, "RETURN $a AS a, $2 AS b", sql.Named("a", "x"), int64(2)), [][]any{{"x", int64(2)}})
		}},
		{"gql clauses", func(t *testing.T, _ principal, db *sql.DB) {
			since(t, db, 2025, 6, "LET and the other clauses of GQL")
			equal(t, "LET", query(t, db, "UNWIND [1, 2] AS x LET y = x * 2 RETURN y"), [][]any{{int64(2)}, {int64(4)}})
		}},
		simple("dynamic labels", "MATCH (a:$($1)) RETURN count(a)", nil),
		simple("element id", "MATCH (a:"+g+" {p: $1, k: 1}) RETURN elementId(a) STARTS WITH '4:' AS e", [][]any{{true}}),
		{"explicit transaction", func(t *testing.T, p principal, db *sql.DB) {
			k := p.name + "-tx"
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			exec(t, tx, "CREATE (:"+g+" {tx: $1})", k)
			equal(t, "the write outside the transaction", query(t, db, "MATCH (a:"+g+" {tx: $1}) RETURN count(a)", k), [][]any{{int64(0)}})
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			equal(t, "the write after the commit", query(t, db, "MATCH (a:"+g+" {tx: $1}) RETURN count(a)", k), [][]any{{int64(1)}})
			tx, err = db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			exec(t, tx, "CREATE (:"+g+" {tx: $1 + 'r'})", k)
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			equal(t, "the write after the rollback", query(t, db, "MATCH (a:"+g+" {tx: $1 + 'r'}) RETURN count(a)", k), [][]any{{int64(0)}})
			tx, err = db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.ExecContext(t.Context(), "CREATE (:"+g+" {tx: 'ro'})")
			refused(t, "a write in a read only transaction", err, "Neo.ClientError.Statement.AccessMode")
			if err := tx.Commit(); err == nil {
				t.Error("the commit after the error gave no error (D65)")
			}
		}},
		{"streaming", func(t *testing.T, _ principal, db *sql.DB) {
			rows, err := db.QueryContext(t.Context(), "UNWIND range(1, 200000) AS x RETURN x")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			n := int64(0)
			for rows.Next() {
				var x int64
				if err := rows.Scan(&x); err != nil {
					t.Fatal(err)
				}
				if n++; x != n {
					t.Fatalf("row %d is %d", n, x)
				}
			}
			if err := rows.Err(); err != nil || n != 200000 {
				t.Errorf("read %d rows and %v, want 200000 rows", n, err)
			}
		}},
		{"typed json", func(t *testing.T, _ principal, db *sql.DB) {
			equal(t, "a date and a string", query(t, db, "RETURN date('2026-09-27') AS d, '2026-09-27' AS s"),
				[][]any{{dbimp.Date{Year: 2026, Month: 9, Day: 27}, "2026-09-27"}})
		}},
		{"bookmarks", func(t *testing.T, p principal, _ *sql.DB) {
			status, res := post(t, p, map[string]any{"statement": "CREATE (a:" + g + " {bm: $k}) RETURN a.bm", "parameters": map[string]any{"k": p.name}})
			marks, _ := res["bookmarks"].([]any)
			if status != http.StatusAccepted || len(marks) == 0 {
				t.Fatalf("a write gave %d and %v, want bookmarks", status, res)
			}
			status, res = post(t, p, map[string]any{"statement": "MATCH (a:" + g + " {bm: $k}) RETURN count(a) AS n", "parameters": map[string]any{"k": p.name}, "bookmarks": marks})
			if status != http.StatusAccepted || fmt.Sprint(res["data"]) != "map[fields:[n] values:[[1]]]" {
				t.Errorf("a read after the bookmark gave %d and %v", status, res)
			}
		}},
		{"impersonation", func(t *testing.T, p principal, _ *sql.DB) {
			status, res := post(t, p, map[string]any{"statement": "SHOW CURRENT USER YIELD user", "impersonatedUser": config(t, admin).User})
			if p == ordinary {
				if status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(res["errors"]), "Forbidden") {
					t.Errorf("the ordinary user impersonated the administrator: %d and %v", status, res)
				}
				return
			}
			if status != http.StatusAccepted {
				t.Errorf("the administrator could not impersonate itself: %d and %v", status, res)
			}
		}},
		{"query counters", func(t *testing.T, p principal, _ *sql.DB) {
			status, res := post(t, p, map[string]any{"statement": "CREATE (:" + g + " {qc: 1})", "includeCounters": true})
			counters, _ := res["counters"].(map[string]any)
			if status != http.StatusAccepted || fmt.Sprint(counters["nodesCreated"]) != "1" {
				t.Errorf("includeCounters gave %d and %v", status, res)
			}
		}},
		{"notifications", func(t *testing.T, p principal, _ *sql.DB) {
			status, res := post(t, p, map[string]any{"statement": "MATCH (n:DbimpNoSuchLabel) RETURN n"})
			if status != http.StatusAccepted || !strings.Contains(fmt.Sprint(res["notifications"]), "UnknownLabelWarning") {
				t.Errorf("a label that does not exist gave %d and %v", status, res)
			}
		}},
	}
	for _, f := range features {
		t.Run(strings.ReplaceAll(f.name, " ", "_"), func(t *testing.T) {
			forEach(t, func(t *testing.T, p principal, db *sql.DB) {
				exec(t, db, "CREATE (a:"+g+" {p: $1, k: 1})-[:NEXT]->(:"+g+" {p: $1, k: 2})-[:NEXT]->(:"+g+" {p: $1, k: 3})", p.name)
				t.Cleanup(func() {
					_, _ = db.ExecContext(context.WithoutCancel(t.Context()), "MATCH (a:"+g+" {p: $1}) DETACH DELETE a", p.name)
				})
				switch f.name {
				case "list comprehension":
					equal(t, "the list", query(t, db, "RETURN [x IN range(1, 3) WHERE x > 1 | x * 10] AS l"), [][]any{{[]any{int64(20), int64(30)}}})
				case "dynamic labels":
					equal(t, "a dynamic label", query(t, db, "MATCH (a:$($1) {p: $2}) RETURN count(a)", strings.Trim(g, "`"), p.name), [][]any{{int64(3)}})
				default:
					f.f(t, p, db)
				}
			})
		})
	}
}

// literal writes v as a literal of Cypher, for the round trip.
func literal(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(x), nil
	case int64:
		if x == math.MinInt64 {
			// Cypher reads a minus sign and then a number, and the number
			// is larger than the largest int64.
			return "(-9223372036854775807 - 1)", nil
		}
		return strconv.FormatInt(x, 10), nil
	case float64:
		s := strings.Replace(strconv.FormatFloat(x, 'g', -1, 64), "e+", "e", 1)
		if !strings.ContainsAny(s, ".e") {
			s += ".0"
		}
		return s, nil
	case string:
		r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`)
		return "'" + r.Replace(x) + "'", nil
	case dbimp.Date:
		return "date('" + x.String() + "')", nil
	case dbimp.LocalTime:
		return "localtime('" + x.String() + "')", nil
	case dbimp.OffsetTime:
		return "time('" + x.String() + "')", nil
	case dbimp.LocalDateTime:
		return "localdatetime('" + x.String() + "')", nil
	case time.Time:
		// The text form of datetime takes no seconds in an offset, and the
		// form of a map does (measured), so the literal names the instant and
		// the zone.
		tz := formatOffset(x)
		if name := x.Location().String(); name != "UTC" && name != "Local" && name != "" {
			tz = name
		}
		return fmt.Sprintf("datetime({epochSeconds: %d, nanosecond: %d, timezone: '%s'})", x.Unix(), x.Nanosecond(), tz), nil
	case dbimp.Interval:
		return "duration('" + x.String() + "')", nil
	case Point:
		if x.Dims == 3 {
			return fmt.Sprintf("point({srid: %d, x: %v, y: %v, z: %v})", x.SRID, x.X, x.Y, x.Z), nil
		}
		return fmt.Sprintf("point({srid: %d, x: %v, y: %v})", x.SRID, x.X, x.Y), nil
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			s, err := literal(e)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		var parts []string
		for k, e := range x {
			s, err := literal(e)
			if err != nil {
				return "", err
			}
			parts = append(parts, "`"+k+"`: "+s)
		}
		return "{" + strings.Join(parts, ", ") + "}", nil
	case dbimp.Vector[float32], dbimp.Vector[int64]:
		var coords []string
		var typ string
		switch c := x.(type) {
		case dbimp.Vector[float32]:
			typ = coordsFloat32
			for _, f := range c {
				coords = append(coords, strconv.FormatFloat(float64(f), 'g', -1, 32))
			}
		case dbimp.Vector[int64]:
			typ = coordsInt64
			for _, n := range c {
				coords = append(coords, strconv.FormatInt(n, 10))
			}
		default:
			return "", fmt.Errorf("writing a vector of %T: %w", c, dbimp.ErrNotSupported)
		}
		return fmt.Sprintf("vector([%s], %d, %s)", strings.Join(coords, ", "), len(coords), typ), nil
	}
	return "", fmt.Errorf("writing a %T as a literal: %w", v, dbimp.ErrNotSupported)
}

// sameTime compares two temporal values by their type, their instant and
// their offset, and by the name of the zone of a time.Time.
func sameTime(got, want any) bool {
	if reflect.TypeOf(got) != reflect.TypeOf(want) {
		return false
	}
	a, b := timeOf(got), timeOf(want)
	_, ao := a.Zone()
	_, bo := b.Zone()
	if _, ok := want.(time.Time); ok && a.Location().String() != b.Location().String() {
		return false
	}
	return a.Equal(b) && ao == bo
}

func TestIntegrationTypes(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skipf("the system has no zone Europe/Oslo: %v", err)
	}
	york, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("the system has no zone America/New_York: %v", err)
	}
	// c returns the case of a type that a property holds as it is.
	c := func(name string, values ...dbimptest.Value) dbimptest.RoundTripCase {
		l := label("t " + name)
		return dbimptest.RoundTripCase{
			Type:   name,
			Insert: "CREATE (:" + l + " {k: $1, v: $2})",
			Literal: func(key string, val any) (string, error) {
				lit, err := literal(val)
				if err != nil {
					return "", err
				}
				return "CREATE (:" + l + " {k: '" + key + "', v: " + lit + "})", nil
			},
			Select:   "MATCH (n:" + l + " {k: $1}) RETURN n.v",
			Update:   "MATCH (n:" + l + " {k: $2}) SET n.v = $1",
			Delete:   "MATCH (n:" + l + " {k: $1}) DELETE n",
			Values:   values,
			Teardown: []string{"MATCH (n:" + l + ") DETACH DELETE n"},
		}
	}
	// props returns the case of a type that the server gives for the
	// properties of a node, such as a map or the node itself. The value is a
	// map of properties, and select returns them in the form of the type.
	props := func(name, create, sel, update, del string, equal func(got, want any) bool, values ...dbimptest.Value) dbimptest.RoundTripCase {
		l, end := label("t "+name), label("t "+name+" end")
		r := strings.NewReplacer("$L", l, "$E", end)
		return dbimptest.RoundTripCase{
			Type:   name,
			Insert: r.Replace(create),
			Literal: func(key string, val any) (string, error) {
				lit, err := literal(val)
				if err != nil {
					return "", err
				}
				return strings.NewReplacer("$1", "'"+key+"'", "$2", lit).Replace(r.Replace(create)), nil
			},
			Select:   r.Replace(sel),
			Update:   r.Replace(update),
			Delete:   r.Replace(del),
			Values:   values,
			Equal:    equal,
			Teardown: []string{"MATCH (n:" + l + ") DETACH DELETE n", "MATCH (n:" + end + ") DETACH DELETE n"},
		}
	}
	// without returns m without the key k, which the statements set.
	without := func(m map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			if k != "k" {
				out[k] = v
			}
		}
		return out
	}
	propsOf := func(got any) (map[string]any, bool) {
		switch g := got.(type) {
		case map[string]any:
			return without(g), true
		case Node:
			return without(g.Props), true
		case Relationship:
			return g.Props, true
		case Path:
			if len(g.Nodes) == 2 && len(g.Relationships) == 1 {
				return g.Relationships[0].Props, true
			}
		}
		return nil, false
	}
	sameProps := func(typ reflect.Type) func(got, want any) bool {
		return func(got, want any) bool {
			m, ok := propsOf(got)
			return ok && reflect.TypeOf(got) == typ && reflect.DeepEqual(m, want)
		}
	}
	m1 := dbimptest.Value{Name: "one", In: map[string]any{"a": int64(1)}}
	m2 := dbimptest.Value{Name: "two", In: map[string]any{"s": "x", "l": []any{int64(1), int64(2)}}}
	long := strings.Repeat("0123456789", 1000)
	u7 := uuid.MustParse("0192f1c4-3b5e-7a2c-9f00-000000000001")
	date := func(y int, m time.Month, d int) dbimp.Date { return dbimp.Date{Year: y, Month: m, Day: d} }
	lt := func(h, m, s, ns int) dbimp.LocalTime {
		return dbimp.LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: ns}
	}
	zt := func(h, m, s, ns, off int) dbimp.OffsetTime {
		return dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: ns}, Offset: off}
	}
	cases := []struct {
		rt    dbimptest.RoundTripCase
		since [2]int
	}{
		{rt: c("null", dbimptest.Value{Name: "null", In: nil}, dbimptest.Value{Name: "null again", In: nil})},
		{rt: c("boolean", dbimptest.Value{Name: "true", In: true}, dbimptest.Value{Name: "false", In: false})},
		{rt: c("integer",
			dbimptest.Value{Name: "zero", In: int64(0)},
			dbimptest.Value{Name: "max", In: int64(math.MaxInt64)},
			dbimptest.Value{Name: "min", In: int64(math.MinInt64)},
		)},
		{rt: c("float",
			dbimptest.Value{Name: "fraction", In: 0.1},
			dbimptest.Value{Name: "large", In: -1.5e300},
			dbimptest.Value{Name: "whole", In: 1.0},
			dbimptest.Value{Name: "smallest", In: math.SmallestNonzeroFloat64},
		)},
		{rt: c("string",
			dbimptest.Value{Name: "empty", In: ""},
			dbimptest.Value{Name: "unicode", In: "héllo, 世界"},
			dbimptest.Value{Name: "long", In: long},
			dbimptest.Value{Name: "quotes", In: `it's "q" \ `},
		)},
		{rt: c("byte array", dbimptest.Value{Name: "two", In: []byte{0, 255}}, dbimptest.Value{Name: "long", In: bytes.Repeat([]byte{7}, 1000)})},
		{rt: c("list",
			dbimptest.Value{Name: "integers", In: []any{int64(1), int64(2)}},
			dbimptest.Value{Name: "strings", In: []any{"a", "é"}},
			dbimptest.Value{Name: "empty", In: []any{}},
		)},
		{rt: props("map", "CREATE (n:$L {k: $1}) SET n += $2", "MATCH (n:$L {k: $1}) RETURN properties(n)",
			"MATCH (n:$L {k: $2}) SET n = $1, n.k = $2", "MATCH (n:$L {k: $1}) DELETE n",
			sameProps(reflect.TypeFor[map[string]any]()), m1, m2)},
		{rt: c("date",
			dbimptest.Value{Name: "today", In: date(2026, 9, 27)},
			dbimptest.Value{Name: "max", In: date(999999999, 12, 31)},
			dbimptest.Value{Name: "min", In: date(-999999999, 1, 1)},
			dbimptest.Value{Name: "first", In: date(1, 1, 1)},
		)},
		{rt: c("local time",
			dbimptest.Value{Name: "midnight", In: lt(0, 0, 0, 0)},
			dbimptest.Value{Name: "last", In: lt(23, 59, 59, 999999999)},
			dbimptest.Value{Name: "nanos", In: lt(12, 50, 35, 1)},
		)},
		{rt: c("zoned time",
			dbimptest.Value{Name: "utc", In: zt(12, 0, 0, 0, 0)},
			dbimptest.Value{Name: "east", In: zt(0, 0, 0, 1, 3600)},
			dbimptest.Value{Name: "west", In: zt(23, 59, 59, 0, -5*3600-30*60)},
		)},
		{rt: c("local datetime",
			dbimptest.Value{Name: "now", In: dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 9, Day: 27}, Time: dbimp.LocalTime{Hour: 10, Minute: 0, Second: 0, Nanosecond: 123456789}}},
			dbimptest.Value{Name: "max", In: dbimp.LocalDateTime{Date: dbimp.Date{Year: 999999999, Month: 12, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999}}},
		)},
		{rt: c("offset datetime",
			dbimptest.Value{Name: "utc", In: time.Date(2026, 9, 27, 10, 0, 0, 1, time.UTC)},
			dbimptest.Value{Name: "offset", In: time.Date(1600, 1, 1, 0, 0, 0, 0, fixedZone(3600+30*60+15))},
		)},
		{rt: c("zoned datetime",
			dbimptest.Value{Name: "oslo", In: time.Date(2026, 9, 27, 10, 0, 0, 0, oslo)},
			dbimptest.Value{Name: "new york", In: time.Date(2026, 1, 1, 23, 59, 59, 999999999, york)},
		)},
		{rt: c("duration",
			dbimptest.Value{Name: "parts", In: dbimp.Interval{Months: 14, Days: 3, Nanoseconds: 14706007000000}},
			dbimptest.Value{Name: "zero", In: dbimp.Interval{}},
			dbimptest.Value{Name: "negative", In: dbimp.Interval{Months: -14, Days: -3, Nanoseconds: -5000000007}},
			dbimptest.Value{Name: "1ns", In: dbimp.Interval{Nanoseconds: 1}},
		)},
		{rt: c("point",
			dbimptest.Value{Name: "cartesian", In: Point{SRID: 7203, X: 1.5, Y: -2.5, Dims: 2}},
			dbimptest.Value{Name: "cartesian 3d", In: Point{SRID: 9157, X: 1, Y: 2, Z: 3, Dims: 3}},
			dbimptest.Value{Name: "wgs 84", In: Point{SRID: 4326, X: 10.7, Y: 59.9, Dims: 2}},
			dbimptest.Value{Name: "wgs 84 3d", In: Point{SRID: 4979, X: 10.7, Y: 59.9, Z: 3, Dims: 3}},
		)},
		{rt: props("node", "CREATE (n:$L {k: $1}) SET n += $2", "MATCH (n:$L {k: $1}) RETURN n",
			"MATCH (n:$L {k: $2}) SET n = $1, n.k = $2", "MATCH (n:$L {k: $1}) DELETE n",
			sameProps(reflect.TypeFor[Node]()), m1, m2)},
		{rt: props("relationship", "CREATE (:$L {k: $1})-[r:REL]->(:$E) SET r += $2", "MATCH (:$L {k: $1})-[r:REL]->() RETURN r",
			"MATCH (:$L {k: $2})-[r:REL]->() SET r = $1", "MATCH (n:$L {k: $1})-[:REL]->(e) DETACH DELETE n, e",
			sameProps(reflect.TypeFor[Relationship]()), m1, m2)},
		{rt: props("path", "CREATE (:$L {k: $1})-[r:REL]->(:$E) SET r += $2", "MATCH p = (:$L {k: $1})-[:REL]->() RETURN p",
			"MATCH (:$L {k: $2})-[r:REL]->() SET r = $1", "MATCH (n:$L {k: $1})-[:REL]->(e) DETACH DELETE n, e",
			sameProps(reflect.TypeFor[Path]()), m1, m2)},
		{rt: c("vector",
			// A vector goes and returns as a dbimp.Vector (D139).
			dbimptest.Value{Name: "float32", In: dbimp.Vector[float32]{1.5, -2.5}},
			dbimptest.Value{Name: "int64", In: dbimp.Vector[int64]{math.MaxInt64, math.MinInt64 + 1}},
		), since: [2]int{2025, 11}},
		{rt: c("uuid", dbimptest.Value{Name: "v7", In: u7}, dbimptest.Value{Name: "zero", In: uuid.UUID{}}), since: [2]int{2026, 7}},
	}
	for _, tc := range cases {
		if strings.Contains(tc.rt.Type, "time") || strings.Contains(tc.rt.Type, "date") {
			tc.rt.Equal = sameTime
		}
		t.Run(strings.ReplaceAll(tc.rt.Type, " ", "_"), func(t *testing.T) {
			forEach(t, func(t *testing.T, p principal, db *sql.DB) {
				if tc.since != [2]int{} {
					since(t, db, tc.since[0], tc.since[1], "the type "+tc.rt.Type)
				}
				dbimptest.RoundTrip(t, db, tc.rt)
			})
		})
	}
	// Neo4j has no decimal type (D63). A decimal literal is a Float, and a
	// decimal argument is refused.
	t.Run("decimal", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			equal(t, "a literal with 30 digits", query(t, db, "RETURN 1.23456789012345678901234567890 AS d"), [][]any{{1.2345678901234567}})
			_, err := queryErr(t, db, "RETURN $1 AS d", apd.New(1, 0))
			if !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("a decimal argument gave %v, want dbimp.ErrNotSupported", err)
			}
		})
	})
}

// TestIntegrationBearer sends the password as a Bearer token, with
// auth=bearer. The server takes a Bearer token only from an identity
// provider of single sign on, which the server of dbrun has not, so the
// refusal of the server reaches the caller (measured, D110).
func TestIntegrationBearer(t *testing.T) {
	cfg := config(t, admin)
	cfg.Auth = AuthBearer
	db := sql.OpenDB(NewConnector(cfg))
	defer db.Close()
	_, err := db.ExecContext(t.Context(), "RETURN 1 AS a")
	var e *ResponseError
	// The message names the scheme bearer, so the header was sent. With no
	// header, the server says that none was supplied.
	if !errors.As(err, &e) || e.HTTPStatus != http.StatusUnauthorized || code(err) != "Neo.ClientError.Security.Unauthorized" ||
		!strings.Contains(err.Error(), "bearer") {
		t.Errorf("auth=bearer gave %v, want the refusal of HTTP 401 of the scheme bearer", err)
	}
}

// TestIntegrationEarlyClose closes the rows of a long statement after its
// first row. The driver stops the statement on the server, so none runs a
// moment later. In a transaction, it stops nothing, and the transaction goes
// on (D105).
func TestIntegrationEarlyClose(t *testing.T) {
	// The first 5000 rows come at once, and the next one only after the
	// server counts to 3000000000, so the statement runs on after a Close
	// unless the driver stops it (measured). A statement that sends more rows
	// at once stops by itself, when the server cannot write the next row, and
	// the server holds back a smaller answer until the statement ends.
	const long = "UNWIND range(1, 3000000000) AS x WITH x WHERE x <= 5000 OR x = 3000000000 RETURN x"
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		rows, err := db.QueryContext(t.Context(), long)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("no first row: %v", rows.Err())
		}
		if err := rows.Close(); err != nil {
			t.Errorf("closing the rows early: %v", err)
		}
		for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(100 * time.Millisecond) {
			left := query(t, db, "SHOW TRANSACTIONS YIELD currentQuery WHERE currentQuery STARTS WITH $1 RETURN count(*)", long)
			if equalRows(left, [][]any{{int64(0)}}) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the statement still runs on the server 2 seconds after an early Close")
			}
		}
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		trows, err := tx.QueryContext(t.Context(), "UNWIND range(1, 100000) AS x RETURN x")
		if err != nil {
			t.Fatal(err)
		}
		defer trows.Close()
		if !trows.Next() {
			t.Fatalf("no first row in the transaction: %v", trows.Err())
		}
		if err := trows.Close(); err != nil {
			t.Errorf("closing the rows of the transaction early: %v", err)
		}
		var one int64
		if err := tx.QueryRowContext(t.Context(), "RETURN 1").Scan(&one); err != nil || one != 1 {
			t.Errorf("a statement after an early Close in the transaction gave %d, %v, want 1", one, err)
		}
	})
}

// equalRows reports whether got and want are deeply equal.
func equalRows(got, want [][]any) bool {
	return reflect.DeepEqual(got, want)
}

// TestIntegrationRollbackAfterTheContext ends the context of a transaction
// that holds the lock of a node. database/sql then rolls it back, and the
// driver sends the rollback, so a write from outside does not wait for the
// idle timeout of the transaction (D100).
func TestIntegrationRollbackAfterTheContext(t *testing.T) {
	db := openAs(t, admin)
	l := label("rollback")
	if _, err := db.ExecContext(t.Context(), "CREATE (:"+l+" {k: 1, v: 0})"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "MATCH (n:"+l+" {k: 1}) SET n.v = 1"); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("rolling back after the context ended: %v", err)
	}
	wctx, wcancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer wcancel()
	if _, err := db.ExecContext(wctx, "MATCH (n:"+l+" {k: 1}) SET n.v = 2"); err != nil {
		t.Errorf("writing the node of the transaction: %v, want no wait for its idle timeout", err)
	}
}

// TestIntegrationCancel holds that a statement whose context ends stops on
// the server, for cancel=tag and cancel=metadata (D67 and D95).
func TestIntegrationCancel(t *testing.T) {
	for _, how := range []string{CancelTag, CancelMetadata} {
		t.Run(how, func(t *testing.T) {
			for _, p := range principals {
				t.Run(p.name, func(t *testing.T) {
					cfg := config(t, p)
					cfg.Cancel = how
					db := sql.OpenDB(NewConnector(cfg))
					defer db.Close()
					marker := prefix + "-" + how + "-" + p.name
					stmt := "UNWIND range(1, 40000) AS a UNWIND range(1, 40000) AS b WITH a + b AS s WHERE s < 0 AND $1 <> '' RETURN count(s) AS n"
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					_, err := db.ExecContext(ctx, stmt, marker)
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("the slow statement gave %v, want context.DeadlineExceeded", err)
					}
					if err := errors.Unwrap(err); err != nil && !errors.Is(err, context.DeadlineExceeded) {
						t.Errorf("the stop on the server failed: %v", err)
					}
					// The server can list a terminated transaction for a moment,
					// so the test reads again, with a limit on the time.
					deadline := time.Now().Add(10 * time.Second)
					for {
						rows := query(t, openAs(t, p), "SHOW TRANSACTIONS YIELD currentQuery, status WHERE currentQuery CONTAINS $1 AND NOT currentQuery CONTAINS 'SHOW TRANSACTIONS' AND status = 'Running' RETURN count(*)", marker)
						if reflect.DeepEqual(rows, [][]any{{int64(0)}}) {
							break
						}
						if time.Now().After(deadline) {
							t.Fatalf("the statement still runs on the server 10 seconds after its context ended: %v", rows)
						}
						time.Sleep(100 * time.Millisecond)
					}
				})
			}
		})
	}
}

func TestIntegrationVersion(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		rows := query(t, db, "CALL dbms.components() YIELD name, versions, edition WHERE name = 'Neo4j Kernel' RETURN versions[0], edition")
		if len(rows) != 1 || rows[0][1] != "enterprise" || !strings.Contains(fmt.Sprint(rows[0][0]), ".") {
			t.Errorf("the version is %v", rows)
		}
	})
}

// TestIntegrationOptions holds the options of one statement and one
// transaction on the server (D109).
func TestIntegrationOptions(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		l := label("options")
		_, err := db.ExecContext(t.Context(), "CREATE (:"+l+")", WithReadonly(true))
		refused(t, "a write with WithReadonly", err, "Neo.ClientError.Statement.AccessMode")
		tx, err := db.BeginTx(WithOptions(t.Context(), WithReadonly(true)), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(t.Context(), "CREATE (:"+l+")")
		refused(t, "a write in a transaction with WithReadonly", err, "Neo.ClientError.Statement.AccessMode")
		_ = tx.Rollback()
		const info = "CALL db.info() YIELD name RETURN name"
		equal(t, "the database of a statement", query(t, db, info), [][]any{{"dbmeta"}})
		equal(t, "the database of a statement with WithDatabase", query(t, db, info, WithDatabase("system")), [][]any{{"system"}})
		equal(t, "a statement with WithParameter", query(t, db, "RETURN 1 AS a", WithParameter("includeCounters", true)), [][]any{{int64(1)}})
		const slow = "UNWIND range(1, 40000) AS a UNWIND range(1, 40000) AS b WITH a + b AS s WHERE s < 0 RETURN count(s) AS n"
		start := time.Now()
		_, err = db.ExecContext(t.Context(), slow, WithTimeout(500*time.Millisecond))
		if v := release(t, db); !calendarAtLeast(v, 2026, 4) {
			if !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("WithTimeout on %s gave %v, want dbimp.ErrNotSupported", v, err)
			}
			return
		}
		refused(t, "the slow statement with WithTimeout", err, "Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration")
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("the slow statement with WithTimeout of 500ms ended after %v", d)
		}
	})
}
