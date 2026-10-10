package athena_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/athena"
)

// These tests need an AWS account. ATHENA_DSN is the DSN of the driver, in the
// form of D192:
// athena://key:secret@athena.us-east-1.amazonaws.com/database?workgroup=name&output=s3://bucket/prefix/
// with the access key and the secret key of an IAM user, and the key token for
// the session token of a temporary credential. A test skips when ATHENA_DSN is
// empty (hard rule 9). Athena is a hosted service that dbrun does not start, so
// a person runs these tests on an account that holds the login that dbsetup made
// for this work (dbmeta D117), and the workflow of CI has no job for them (D192
// item 2). The login is one IAM user, so each test runs as that user only, and
// there is no ordinary user to run it again as.
//
// The user needs the rights of the workgroup: athena:StartQueryExecution,
// GetQueryExecution, GetQueryResults, StopQueryExecution, BatchGetQueryExecution,
// ListQueryExecutions, GetWorkGroup, ListWorkGroups, CreatePreparedStatement,
// GetPreparedStatement and DeletePreparedStatement. It needs the rights of the
// Glue database of the DSN to make, change and drop tables, and the rights to
// read, write and list the S3 bucket of the output location. The tests make
// tables in the database of the DSN, with the name of this run for a prefix, and
// their data in the bucket, under the prefix dbimp-it/<run>/. A test that reads
// the federated catalog runs only when ATHENA_FEDERATED_CATALOG names a data
// catalog of the type LAMBDA, whose database is /aws/lambda/<catalog>, with the
// right athena:GetDataCatalog and the right to invoke its function.
//
// DROP TABLE of an external table leaves its files in S3, and the driver cannot
// delete them, because S3 is not Athena. So each run has its own prefix in S3,
// and a person removes the prefix dbimp-it/ from the bucket from time to time.
// TestMain drops the tables that a test left, and fails if it found one.

// The variables of the environment.
const (
	envDSN       = "ATHENA_DSN"
	envFederated = "ATHENA_FEDERATED_CATALOG"
)

// suffix makes the names of this run unique.
var suffix = strings.ToLower(rand.Text()[:8])

// prefix is the start of the name of each table of this run.
var prefix = "dbimp_it_" + suffix

// table returns the name of a table of this run.
func table(name string) string {
	return prefix + "_" + name
}

// dsn returns the DSN of the account, and skips the test when there is none.
func dsn(t *testing.T) string {
	t.Helper()
	v := os.Getenv(envDSN)
	if v == "" {
		t.Skipf("%s is empty, so there is no account to test against", envDSN)
	}
	return v
}

// integrationConfig returns the configuration of the account.
func integrationConfig(t *testing.T) athena.Config {
	t.Helper()
	cfg, err := athena.ParseDSN(dsn(t))
	if err != nil {
		t.Fatalf("reading %s: %v", envDSN, err)
	}
	return *cfg
}

// connect returns a database on the account.
func connect(t *testing.T) *sql.DB {
	t.Helper()
	db := sql.OpenDB(athena.NewConnector(integrationConfig(t)))
	t.Cleanup(func() { db.Close() })
	return db
}

// location returns an S3 location of this run for the data of the table name.
// It is in the bucket of the output location of the DSN, or of the workgroup when
// the DSN names none, under dbimp-it/<run>/.
func location(t *testing.T, name string) string {
	t.Helper()
	return bucket(t) + "dbimp-it/" + suffix + "/" + name + "/"
}

// bucket returns the S3 URL of the bucket of the output location, with a slash at
// its end, such as s3://bucket/.
func bucket(t *testing.T) string {
	t.Helper()
	out := integrationConfig(t).Output
	if out == "" {
		_, body, err := athena.Raw(t.Context(), integrationConfig(t), "GetWorkGroup", mustJSON(t, map[string]string{"WorkGroup": integrationConfig(t).WorkGroup}))
		if err != nil {
			t.Fatal(err)
		}
		var wg struct {
			WorkGroup struct {
				Configuration struct {
					ResultConfiguration struct{ OutputLocation string }
				}
			}
		}
		if err := json.Unmarshal(body, &wg); err != nil {
			t.Fatalf("reading the workgroup %s: %v", body, err)
		}
		out = wg.WorkGroup.Configuration.ResultConfiguration.OutputLocation
	}
	rest, ok := strings.CutPrefix(out, "s3://")
	if !ok {
		t.Skipf("the output location %q is no S3 location, so the tests have no bucket for their tables", out)
	}
	name, _, _ := strings.Cut(rest, "/")
	return "s3://" + name + "/"
}

// mustJSON returns v as JSON.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestMain drops each table of this run that a test left, and fails if it found
// one.
func TestMain(m *testing.M) {
	code := m.Run()
	if left := leftovers(); len(left) > 0 {
		fmt.Fprintf(os.Stderr, "the tests left %d tables with the prefix %s, which TestMain dropped: %v\n", len(left), prefix, left)
		code = 1
	}
	os.Exit(code)
}

// leftovers drops each table and view of this run, and returns their names. It
// returns none when there is no account.
func leftovers() []string {
	v := os.Getenv(envDSN)
	if v == "" {
		return nil
	}
	cfg, err := athena.ParseDSN(v)
	if err != nil {
		return nil
	}
	db := sql.OpenDB(athena.NewConnector(*cfg))
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	names, err := listTables(ctx, db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "looking for the tables that the tests left: %v\n", err)
		return []string{"?"}
	}
	for _, name := range names {
		// A name is a table or a view, and the statement for the other kind fails.
		for _, stmt := range []string{"DROP VIEW IF EXISTS ", "DROP TABLE IF EXISTS "} {
			_, _ = db.ExecContext(ctx, stmt+name)
		}
	}
	return names
}

// listTables returns the name of each table and view of this run.
func listTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names, rows.Err()
}

// exec runs a statement that must succeed.
func exec(t *testing.T, db *sql.DB, query string, args ...any) sql.Result {
	t.Helper()
	res, err := db.ExecContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return res
}

// affected returns the count of the rows that res changed.
func affected(t *testing.T, res sql.Result) int64 {
	t.Helper()
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("reading the rows affected: %v", err)
	}
	return n
}

// dropLater drops the table or the view name when the test ends, even when it
// failed. The context of the test ends before its cleanup runs, so the drop has
// a context of its own.
func dropLater(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
		defer cancel()
		for _, stmt := range []string{"DROP VIEW IF EXISTS ", "DROP TABLE IF EXISTS "} {
			_, _ = db.ExecContext(ctx, stmt+name)
		}
	})
}

// rowsOf runs a query and returns each row as the text of its values, which
// fmt.Sprint writes, so that a test compares what Rows.Scan returns into a *any.
func rowsOf(t *testing.T, db *sql.DB, query string, args ...any) [][]any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scanning %s: %v", query, err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

// refusal fails the test unless err is an error of the server that refused the
// request with the code MALFORMED_QUERY, which is the code of a statement that the
// parser refuses (recorded: "a syntax error"). It returns the error.
func refusal(t *testing.T, err error) *athena.Error {
	t.Helper()
	var aerr *athena.Error
	if !errors.As(err, &aerr) {
		t.Fatalf("the error is %v, want an *athena.Error with the code MALFORMED_QUERY", err)
	}
	if aerr.Code != "MALFORMED_QUERY" {
		t.Errorf("the error is %+v, want the code MALFORMED_QUERY", *aerr)
	}
	return aerr
}

// execution is the part of a query that a test reads from BatchGetQueryExecution.
type execution struct {
	ID    string `json:"QueryExecutionId"`
	Query string
	State string
	Reuse bool
}

// executionsOf returns the executions of the workgroup that ran the statement
// query, the newest first. It reads the newest 50 with ListQueryExecutions and
// BatchGetQueryExecution.
func executionsOf(t *testing.T, query string) []execution {
	t.Helper()
	cfg := integrationConfig(t)
	_, body, err := athena.Raw(t.Context(), cfg, "ListQueryExecutions", mustJSON(t, map[string]any{"WorkGroup": cfg.WorkGroup, "MaxResults": 50}))
	if err != nil {
		t.Fatal(err)
	}
	var ids struct {
		IDs []string `json:"QueryExecutionIds"`
	}
	if err := json.Unmarshal(body, &ids); err != nil || len(ids.IDs) == 0 {
		t.Fatalf("listing the executions: %s %v", body, err)
	}
	_, body, err = athena.Raw(t.Context(), cfg, "BatchGetQueryExecution", mustJSON(t, map[string]any{"QueryExecutionIds": ids.IDs}))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		QueryExecutions []struct {
			QueryExecutionID string `json:"QueryExecutionId"`
			Query            string
			Status           struct{ State string }
			Statistics       struct {
				ResultReuseInformation struct{ ReusedPreviousResult bool }
			}
		}
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("reading the executions %s: %v", body, err)
	}
	var out []execution
	for _, q := range got.QueryExecutions {
		if q.Query == query {
			out = append(out, execution{ID: q.QueryExecutionID, Query: q.Query, State: q.Status.State, Reuse: q.Statistics.ResultReuseInformation.ReusedPreviousResult})
		}
	}
	return out
}

// TestIntegrationPing checks the endpoint, the signature and the credentials with
// the ping, which sends GetWorkGroup and starts no query.
func TestIntegrationPing(t *testing.T) {
	db := connect(t)
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestIntegrationWrongSecret holds that a wrong secret key fails with the error
// of the server, an InvalidSignatureException, and that the error holds neither
// key.
func TestIntegrationWrongSecret(t *testing.T) {
	cfg := integrationConfig(t)
	cfg.Password = "wrong" + cfg.Password
	db := sql.OpenDB(athena.NewConnector(cfg))
	defer db.Close()
	err := db.PingContext(t.Context())
	var aerr *athena.Error
	if !errors.As(err, &aerr) || aerr.Type != "InvalidSignatureException" {
		t.Fatalf("the error is %v, want an InvalidSignatureException", err)
	}
	if strings.Contains(err.Error(), cfg.Password) {
		t.Error("the error holds the secret key")
	}
}

// TestIntegrationSelect reads a SELECT with a header row, and the version of the
// engine from the workgroup (D192 item 6: no query gives it).
func TestIntegrationSelect(t *testing.T) {
	db := connect(t)
	rows := rowsOf(t, db, "SELECT 1 AS a, 'x' AS b")
	if want := [][]any{{int64(1), "x"}}; !slices.EqualFunc(rows, want, slices.Equal[[]any]) {
		t.Errorf("rows %v, want %v", rows, want)
	}
	cfg := integrationConfig(t)
	if cfg.WorkGroup == "" {
		t.Skip("the DSN names no workgroup, so there is no engine version to read")
	}
	_, body, err := athena.Raw(t.Context(), cfg, "GetWorkGroup", mustJSON(t, map[string]string{"WorkGroup": cfg.WorkGroup}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Athena engine version") {
		t.Errorf("the workgroup %s names no engine version", body)
	}
}

// TestIntegrationContextDeadline holds D36: a context that ends while the query
// runs gives the error of the context, and the driver stops the query.
func TestIntegrationContextDeadline(t *testing.T) {
	db := connect(t)
	query := "SELECT count(*) FROM UNNEST(sequence(1,50000)) AS t(n) CROSS JOIN UNNEST(sequence(1,50000)) AS u(m) CROSS JOIN UNNEST(sequence(1,50000)) AS v(k) /* " + suffix + " */"
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	start := time.Now()
	_, err := db.ExecContext(ctx, query)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the error is %v after %v, want context.DeadlineExceeded", err, time.Since(start))
	}
	// The stop takes effect within a second or two. Poll for the state, with a
	// limit, and never for a fixed time.
	deadline := time.Now().Add(time.Minute)
	for {
		if execs := executionsOf(t, query); len(execs) > 0 && execs[0].State == "CANCELLED" {
			return
		} else if len(execs) > 0 && execs[0].State == "SUCCEEDED" {
			t.Fatal("the query ended in the state SUCCEEDED, so it was too short for the test")
		}
		if time.Now().After(deadline) {
			t.Fatal("the query is not CANCELLED one minute after the context ended: the driver did not stop it")
		}
		select {
		case <-t.Context().Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
