package dynamodb_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/dynamodb"
)

// These tests need a server. DYNAMODB_DSN names DynamoDB Local for the
// administrator, and DYNAMODB_ORDINARY_DSN for the ordinary user (D9). Each
// is the url of dbrun, which is dynamodb://key:secret@host:port?region=r, the
// DSN of the driver. DynamoDB Local checks no key and has no users, so dbrun
// makes no ordinary user for it, and the tests as that user skip with the
// reason (the manifest says why). PartiQL has no DDL (D171), so the tests
// make their tables through the API of DynamoDB as the administrator, and
// read and write through the driver. A test skips when a variable that it
// needs is empty. Each table of the tests starts with a prefix of its own,
// which TestMain drops at the end.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each table of this run.
var prefix = "dbimp_it_" + suffix + "_"

// ready bounds the time that a table takes to be active.
const ready = 2 * time.Minute

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "DYNAMODB_DSN"}
	ordinary   = principal{"ordinary", "DYNAMODB_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// toDSN returns the url of dbrun as the DSN of the tests. A server on this
// host speaks HTTP, and the DSN turns TLS on by default (D169), so the DSN
// of a local server gets tls=false when it names none.
func toDSN(v string) string {
	u, err := url.Parse(v)
	if err != nil {
		return v
	}
	q := u.Query()
	if _, ok := q["tls"]; !ok && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
		q.Set("tls", "false")
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// dsn returns the DSN of p, or skips the test when it is empty.
func dsn(t *testing.T, p principal) string {
	t.Helper()
	v := os.Getenv(p.env)
	if v == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user. DynamoDB Local has no ordinary user, and the manifest says why", p.env, p.name)
	}
	return toDSN(v)
}

// openAs opens the server as p.
func openAs(t *testing.T, p principal) *sql.DB {
	t.Helper()
	db, err := sql.Open(dynamodb.Name, dsn(t, p))
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
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, "dropping the tables of the tests:", err)
		code = 1
	}
	os.Exit(code)
}

// cleanup drops each table of this run.
func cleanup() error {
	v := os.Getenv(admin.env)
	if v == "" {
		return nil
	}
	cfg, err := dynamodb.ParseDSN(toDSN(v))
	if err != nil {
		return err
	}
	// TestMain has no context of its own.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	status, body, err := dynamodb.Raw(ctx, *cfg, "ListTables", []byte(`{}`))
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("listing the tables: %d %s: %w", status, body, err)
	}
	var list struct{ TableNames []string }
	if err := json.Unmarshal(body, &list); err != nil {
		return err
	}
	var errs []error
	for _, name := range list.TableNames {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		b, _ := json.Marshal(map[string]any{"TableName": name})
		if status, body, err := dynamodb.Raw(ctx, *cfg, "DeleteTable", b); err != nil || status != http.StatusOK {
			errs = append(errs, fmt.Errorf("dropping %s: %d %s: %w", name, status, body, err))
		}
	}
	return errors.Join(errs...)
}

// adminConfig returns the configuration of the administrator, or skips the
// test when its DSN is empty.
func adminConfig(t *testing.T) dynamodb.Config {
	t.Helper()
	cfg, err := dynamodb.ParseDSN(dsn(t, admin))
	if err != nil {
		t.Fatal(err)
	}
	return *cfg
}

// raw sends an operation of the API as the administrator, and returns the
// status and the decoded object of the answer.
func raw(t *testing.T, op string, body any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	status, answer, err := dynamodb.Raw(t.Context(), adminConfig(t), op, b)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	var m map[string]any
	if err := json.Unmarshal(answer, &m); err != nil {
		t.Fatalf("%s: the answer is not an object: %s", op, answer)
	}
	return status, m
}

// call is raw for an operation that must succeed.
func call(t *testing.T, op string, body any) map[string]any {
	t.Helper()
	status, m := raw(t, op, body)
	if status != http.StatusOK {
		t.Fatalf("%s: HTTP %d: %v", op, status, m)
	}
	return m
}

// table makes the table whose name is the prefix and name, from the body of
// CreateTable, waits until it is active, and drops it when the test ends. It
// returns the name of the table.
func table(t *testing.T, name string, body map[string]any) string {
	t.Helper()
	full := prefix + name
	body["TableName"] = full
	body["BillingMode"] = "PAY_PER_REQUEST"
	call(t, "CreateTable", body)
	t.Cleanup(func() {
		// The context of the test ends before its cleanup runs.
		b, _ := json.Marshal(map[string]any{"TableName": full})
		status, answer, err := dynamodb.Raw(context.WithoutCancel(t.Context()), adminConfig(t), "DeleteTable", b)
		if err != nil || status != http.StatusOK {
			t.Errorf("dropping %s: %d %s: %v", full, status, answer, err)
		}
	})
	for deadline := time.Now().Add(ready); ; {
		d := call(t, "DescribeTable", map[string]any{"TableName": full})
		if desc, _ := d["Table"].(map[string]any); desc["TableStatus"] == "ACTIVE" {
			return full
		}
		if time.Now().After(deadline) {
			t.Fatalf("the table %s is not active after %v", full, ready)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// attrs returns the AttributeDefinitions and the KeySchema of a table whose
// key is a string hash key, with a number range key when sort is true.
func attrs(sort bool) map[string]any {
	a := []any{map[string]any{"AttributeName": "pk", "AttributeType": "S"}}
	k := []any{map[string]any{"AttributeName": "pk", "KeyType": "HASH"}}
	if sort {
		a = append(a, map[string]any{"AttributeName": "sk", "AttributeType": "N"})
		k = append(k, map[string]any{"AttributeName": "sk", "KeyType": "RANGE"})
	}
	return map[string]any{"AttributeDefinitions": a, "KeySchema": k}
}

// q quotes the name of a table for a statement.
func q(name string) string {
	return `"` + name + `"`
}

// must runs a statement that must succeed.
func must(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// rowsOf runs a query that must succeed, and returns each row as its values
// written by show and joined with a space.
func rowsOf(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	_, rows, err := read(t, db, query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.Join(r, " ")
	}
	return out
}

// refused runs a statement that the server must refuse, and checks the type
// of the error.
func refused(t *testing.T, db *sql.DB, typ, query string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), query)
	var derr *dynamodb.Error
	if !errors.As(err, &derr) || derr.Type != typ {
		t.Errorf("%s: %v, want the error %s", query, err, typ)
	}
}

// same fails the test unless got is want.
func same(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s: got\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestIntegrationPing pings the server as each principal.
func TestIntegrationPing(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

// TestIntegrationToken sends a session token, which the driver signs in the
// header X-Amz-Security-Token. DynamoDB Local checks no token, so it accepts
// the request. The test does not check the token against the service of AWS.
func TestIntegrationToken(t *testing.T) {
	v := os.Getenv(admin.env)
	if v == "" {
		t.Skipf("%s is empty, so there is no server to test with a token", admin.env)
	}
	cfg, err := dynamodb.ParseDSN(toDSN(v))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Token = "dbimp-session-token"
	status, body, err := dynamodb.Raw(t.Context(), *cfg, "ListTables", []byte(`{}`))
	if err != nil || status != http.StatusOK {
		t.Fatalf("ListTables with a token: %d %s: %v", status, body, err)
	}
	db, err := sql.Open(dynamodb.Name, cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestIntegrationVersion runs the statement that usql runs for the version,
// as each principal. DynamoDB has no such statement, and each one gets the
// error of a syntax error (recorded: "the version statement of usql").
func TestIntegrationVersion(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		_, err := db.ExecContext(t.Context(), "SELECT version();")
		var derr *dynamodb.Error
		if !errors.As(err, &derr) || derr.Type != "ValidationException" {
			t.Errorf("the version statement of usql gave %v, want a ValidationException", err)
		}
	})
}

// TestIntegrationOrdinary reads as the ordinary user, who exists on a flavor
// that checks keys only, and fails the write that it is refused.
func TestIntegrationOrdinary(t *testing.T) {
	db := openAs(t, ordinary)
	name := table(t, "ordinary", attrs(false))
	if _, err := db.ExecContext(t.Context(), "INSERT INTO "+q(name)+" VALUE {'pk': 'x'}"); err == nil {
		t.Error("the ordinary user wrote to a table")
	}
	if rows := rowsOf(t, db, "SELECT pk FROM "+q(name)); len(rows) != 0 {
		t.Errorf("the table holds %v, want no row", rows)
	}
}

// TestIntegrationCancel holds D169 and D36: a context that ends stops the
// request, the error is the error of the context, and the server stays well
// for the next statement (recorded: "a statement after the client left").
// DynamoDB has no operation that cancels a statement, and each request reads
// one page, so the server has nothing to cancel.
func TestIntegrationCancel(t *testing.T) {
	adminConfig(t)
	pages := pageTable(t)
	db := openAs(t, admin)
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT id FROM "+pages)
	if err == nil {
		_, err = ids(rows)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a query with a context that ended gave %v, want context.DeadlineExceeded", err)
	}
	same(t, "the next statement", rowsOf(t, db, "SELECT id FROM "+pages+" WHERE id = 7"), "N(7)")
}
