package dynamodb_test

import (
	"database/sql"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xo/dbimp/dynamodb"
)

// typeOf returns the name of the type of an error that an answer of the API
// holds, without its namespace.
func typeOf(m map[string]any) string {
	s, _ := m["__type"].(string)
	_, name, _ := strings.Cut(s, "#")
	return name
}

// statements returns the statements of a batch or a transaction.
func statements(stmts ...string) []any {
	out := make([]any, len(stmts))
	for i, s := range stmts {
		out[i] = map[string]any{"Statement": s}
	}
	return out
}

// pageTable makes a table of 300 items of 4000 bytes each, which a result of
// one statement holds in two pages of at most 1 MB (recorded: "a result larger
// than one page"). It returns the quoted name.
func pageTable(t *testing.T) string {
	t.Helper()
	name := table(t, "pages", map[string]any{
		"AttributeDefinitions": []any{map[string]any{"AttributeName": "id", "AttributeType": "N"}},
		"KeySchema":            []any{map[string]any{"AttributeName": "id", "KeyType": "HASH"}},
	})
	pad := strings.Repeat("x", 4000)
	for start := 0; start < 300; start += 25 {
		var batch []any
		for id := start; id < start+25; id++ {
			batch = append(batch, map[string]any{
				"Statement":  "INSERT INTO " + q(name) + " VALUE {'id': ?, 'pad': ?}",
				"Parameters": []any{map[string]any{"N": strconv.Itoa(id)}, map[string]any{"S": pad}},
			})
		}
		call(t, "BatchExecuteStatement", map[string]any{"Statements": batch})
	}
	return q(name)
}

// counting counts the requests that pass through a transport.
type counting struct {
	next http.RoundTripper
	n    atomic.Int32
}

func (c *counting) RoundTrip(req *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.next.RoundTrip(req)
}

// TestIntegrationFeatures uses each feature of the server that features.json
// marks yes, and sends each one that it marks no, and compares what comes
// back. A feature that the driver does not send, such as a batch and a
// transaction, goes through the API, because only the server is under test.
// The names of the subtests are the names of the entries.
func TestIntegrationFeatures(t *testing.T) {
	adminConfig(t)
	plain := q(table(t, "feat", attrs(false)))
	pages := pageTable(t)
	tx := q(table(t, "tx", attrs(false)))
	db := openAs(t, admin)

	t.Run("missing_attribute", func(t *testing.T) {
		must(t, db, "INSERT INTO "+plain+" VALUE {'pk': 'm1', 'x': 1}")
		must(t, db, "INSERT INTO "+plain+" VALUE {'pk': 'm2', 'y': 'two'}")
		got := rowsOf(t, db, "SELECT x, y FROM "+plain+" WHERE pk IN ['m1', 'm2']")
		slices.Sort(got)
		same(t, "a missing attribute and NULL are one value", got, `N(1) nil`, `nil S("two")`)
	})
	t.Run("items_with_different_attributes", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT * FROM "+plain+" WHERE pk IN ['m1', 'm2']")
		slices.Sort(got)
		same(t, "each item holds the attributes that it has", got, `{pk:S("m1") x:N(1)}`, `{pk:S("m2") y:S("two")}`)
	})
	t.Run("nested_path", func(t *testing.T) {
		must(t, db, "INSERT INTO "+plain+" VALUE {'pk': 'n', 'm': {'k': [1, 'two', null], 'z': 'last'}, 'l': [7, 8]}")
		_, rows, err := read(t, db, "SELECT m.k[1], l[0], m.z FROM "+plain+" WHERE pk = 'n'")
		if err != nil {
			t.Fatal(err)
		}
		cols, _, _ := read(t, db, "SELECT m.k[1], l[0], m.z FROM "+plain+" WHERE pk = 'n'")
		same(t, "the columns are the last parts of the paths", cols, "k[1]", "l[0]", "z")
		same(t, "the values", []string{strings.Join(rows[0], " ")}, `S("two") N(7) S("last")`)
	})
	t.Run("condition_functions", func(t *testing.T) {
		must(t, db, "INSERT INTO "+plain+" VALUE {'pk': 'cf', 'ss': <<'a', 'b'>>, 'v': 'hello'}")
		for _, where := range []string{"begins_with(pk, 'cf')", "contains(ss, 'a')", "attribute_exists(v)", "v IS NOT MISSING", "contains(v, 'ell')"} {
			same(t, where, rowsOf(t, db, "SELECT pk FROM "+plain+" WHERE "+where+" AND pk = 'cf'"), `S("cf")`)
		}
		same(t, "IS MISSING", rowsOf(t, db, "SELECT pk FROM "+plain+" WHERE w IS MISSING AND pk = 'cf'"), `S("cf")`)
		same(t, "a condition that is false", rowsOf(t, db, "SELECT pk FROM "+plain+" WHERE contains(ss, 'z') AND pk = 'cf'"))
	})
	t.Run("scan_with_no_key", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT pk FROM "+plain)
		slices.Sort(got)
		same(t, "a scan reads every item", got, `S("cf")`, `S("m1")`, `S("m2")`, `S("n")`)
	})
	t.Run("next_token_paging", func(t *testing.T) {
		cfg := adminConfig(t)
		c := dynamodb.NewConnector(cfg)
		t.Cleanup(func() {
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		})
		cnt := &counting{}
		c.WrapTransport(func(rt http.RoundTripper) http.RoundTripper { cnt.next = rt; return cnt })
		pdb := sql.OpenDB(c)
		defer pdb.Close()
		got := rowsOf(t, pdb, "SELECT id FROM "+pages)
		if len(got) != 300 {
			t.Fatalf("the result holds %d rows, want 300", len(got))
		}
		seen := map[string]bool{}
		for _, r := range got {
			seen[r] = true
		}
		if len(seen) != 300 {
			t.Errorf("the result holds %d different ids, want 300", len(seen))
		}
		if n := cnt.n.Load(); n < 2 {
			t.Errorf("the driver sent %d requests, want at least 2 pages", n)
		}
	})
	t.Run("page_of_1_MB", func(t *testing.T) {
		m := call(t, "ExecuteStatement", map[string]any{"Statement": "SELECT id FROM " + pages})
		items, _ := m["Items"].([]any)
		if m["NextToken"] == nil || len(items) == 0 || len(items) >= 300 {
			t.Errorf("the first page holds %d items and the token %v, want some of the 300 and a token", len(items), m["NextToken"])
		}
	})
	t.Run("limit_of_evaluated_items", func(t *testing.T) {
		m := call(t, "ExecuteStatement", map[string]any{"Statement": "SELECT id FROM " + pages, "Limit": 2})
		if items, _ := m["Items"].([]any); len(items) != 2 || m["NextToken"] == nil {
			t.Errorf("a limit of 2 gave %v", m)
		}
		// Limit counts the items that the server reads, so a filter that
		// matches few items gives pages with no item and a token, and the
		// driver follows each token to the end.
		ctx := dynamodb.WithOptions(t.Context(), dynamodb.WithParameter("Limit", 10))
		rows, err := db.QueryContext(ctx, "SELECT id FROM "+pages+" WHERE id > 290")
		if err != nil {
			t.Fatal(err)
		}
		got, err := ids(rows)
		if err != nil || len(got) != 9 {
			t.Errorf("a filter with a limit of 10 gave %d rows and %v, want 9 (ids 291 to 299)", len(got), err)
		}
	})
	t.Run("limit_clause", func(t *testing.T) {
		refused(t, db, "ValidationException", "SELECT id FROM "+pages+" LIMIT 5")
	})
	t.Run("consistent_read", func(t *testing.T) {
		ctx := dynamodb.WithOptions(t.Context(), dynamodb.WithParameter("ConsistentRead", true))
		rows, err := db.QueryContext(ctx, "SELECT pk FROM "+plain+" WHERE pk = 'cf'")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := ids(rows); err != nil || len(got) != 1 {
			t.Errorf("a consistent read gave %v and %v", got, err)
		}
	})
	t.Run("consumed_capacity", func(t *testing.T) {
		// DynamoDB Local sends no ConsumedCapacity (recorded: "crud: the
		// consumed capacity").
		m := call(t, "ExecuteStatement", map[string]any{"Statement": "SELECT pk FROM " + plain + " WHERE pk = 'cf'", "ReturnConsumedCapacity": "TOTAL"})
		if _, ok := m["ConsumedCapacity"]; ok {
			t.Errorf("the server sent ConsumedCapacity: %v, so the verdict no is wrong", m)
		}
		ctx := dynamodb.WithOptions(t.Context(), dynamodb.WithParameter("ReturnConsumedCapacity", "TOTAL"))
		rows, err := db.QueryContext(ctx, "SELECT pk FROM "+plain+" WHERE pk = 'cf'")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := ids(rows); err != nil || len(got) != 1 {
			t.Errorf("a read with ReturnConsumedCapacity gave %v and %v", got, err)
		}
	})
	t.Run("values_on_a_failed_condition", func(t *testing.T) {
		status, m := raw(t, "ExecuteStatement", map[string]any{
			"Statement":                           "UPDATE " + plain + " SET v = 'x' WHERE pk = 'cf' AND v = 'nomatch'",
			"ReturnValuesOnConditionCheckFailure": "ALL_OLD",
		})
		item, _ := m["Item"].(map[string]any)
		if status != http.StatusBadRequest || typeOf(m) != "ConditionalCheckFailedException" || item["pk"] == nil {
			t.Errorf("HTTP %d: %v, want the item in a ConditionalCheckFailedException", status, m)
		}
	})
	t.Run("positional_parameters", func(t *testing.T) {
		same(t, "a ? for each argument", rowsOf(t, db, "SELECT pk, v FROM "+plain+" WHERE pk = ? AND v = ?", "cf", "hello"), `S("cf") S("hello")`)
		same(t, "a ? in a string", rowsOf(t, db, "SELECT pk FROM "+plain+" WHERE pk = '?'"))
	})
	t.Run("named_parameters", func(t *testing.T) {
		status, m := raw(t, "ExecuteStatement", map[string]any{
			"Statement":  "SELECT pk FROM " + plain + " WHERE pk = :p",
			"Parameters": []any{map[string]any{"S": "cf"}},
		})
		if status != http.StatusBadRequest {
			t.Errorf("HTTP %d: %v, want the refusal of :p", status, m)
		}
	})
	t.Run("batch_of_statements", func(t *testing.T) {
		m := call(t, "BatchExecuteStatement", map[string]any{"Statements": statements(
			"SELECT pk FROM "+plain+" WHERE pk = 'cf'",
			"SELECT pk FROM "+q(prefix+"none")+" WHERE pk = 'c'",
		)})
		rs, _ := m["Responses"].([]any)
		if len(rs) != 2 {
			t.Fatalf("a batch of 2 statements gave %v", m)
		}
		first, _ := rs[0].(map[string]any)
		second, _ := rs[1].(map[string]any)
		if first["Item"] == nil || second["Error"] == nil {
			t.Errorf("a batch with one failure gave %v, want an Item and an Error, with HTTP 200", m)
		}
	})
	t.Run("transaction", func(t *testing.T) {
		call(t, "ExecuteTransaction", map[string]any{"TransactStatements": statements(
			"INSERT INTO "+tx+" VALUE {'pk': 'a', 'v': 1}",
			"INSERT INTO "+tx+" VALUE {'pk': 'b', 'v': 2}",
		)})
		got := rowsOf(t, db, "SELECT pk, v FROM "+tx)
		slices.Sort(got)
		same(t, "after a transaction", got, `S("a") N(1)`, `S("b") N(2)`)
		status, m := raw(t, "ExecuteTransaction", map[string]any{"TransactStatements": statements(
			"INSERT INTO "+tx+" VALUE {'pk': 'c', 'v': 3}",
			"UPDATE "+tx+" SET v = 9 WHERE pk = 'none'",
		)})
		if status != http.StatusBadRequest || typeOf(m) != "TransactionCanceledException" {
			t.Errorf("HTTP %d: %v, want TransactionCanceledException", status, m)
		}
		same(t, "a transaction that fails keeps nothing", rowsOf(t, db, "SELECT pk FROM "+tx+" WHERE pk = 'c'"))
	})
	t.Run("transaction_of_reads", func(t *testing.T) {
		m := call(t, "ExecuteTransaction", map[string]any{"TransactStatements": statements(
			"SELECT * FROM "+tx+" WHERE pk = 'a'",
			"SELECT * FROM "+tx+" WHERE pk = 'none'",
		)})
		rs, _ := m["Responses"].([]any)
		if len(rs) != 2 {
			t.Fatalf("a transaction of 2 reads gave %v", m)
		}
		first, _ := rs[0].(map[string]any)
		second, _ := rs[1].(map[string]any)
		if first["Item"] == nil || len(second) != 0 {
			t.Errorf("a transaction of reads gave %v, want an Item and {}", m)
		}
	})
	t.Run("transaction_of_reads_and_writes", func(t *testing.T) {
		status, m := raw(t, "ExecuteTransaction", map[string]any{"TransactStatements": statements(
			"SELECT * FROM "+tx+" WHERE pk = 'a'",
			"INSERT INTO "+tx+" VALUE {'pk': 'd'}",
		)})
		if status != http.StatusBadRequest || typeOf(m) != "ValidationException" {
			t.Errorf("HTTP %d: %v, want a ValidationException", status, m)
		}
	})
	t.Run("exists_in_a_transaction", func(t *testing.T) {
		call(t, "ExecuteTransaction", map[string]any{"TransactStatements": statements(
			"EXISTS(SELECT * FROM "+tx+" WHERE pk = 'a' AND v = 1)",
			"UPDATE "+tx+" SET v = 10 WHERE pk = 'b'",
		)})
		same(t, "a condition that holds", rowsOf(t, db, "SELECT v FROM "+tx+" WHERE pk = 'b'"), "N(10)")
		status, m := raw(t, "ExecuteTransaction", map[string]any{"TransactStatements": statements(
			"EXISTS(SELECT * FROM "+tx+" WHERE pk = 'a' AND v = 99)",
			"UPDATE "+tx+" SET v = 11 WHERE pk = 'b'",
		)})
		if status != http.StatusBadRequest || typeOf(m) != "TransactionCanceledException" {
			t.Errorf("HTTP %d: %v, want TransactionCanceledException", status, m)
		}
		same(t, "a condition that fails", rowsOf(t, db, "SELECT v FROM "+tx+" WHERE pk = 'b'"), "N(10)")
	})
	t.Run("idempotency_token", func(t *testing.T) {
		// A token makes a transaction run once, and the server remembers it
		// for minutes, so each run uses a token of its own.
		token := "dbimp-" + suffix
		first := map[string]any{"TransactStatements": statements("UPDATE " + tx + " SET v = 14 WHERE pk = 'b'"), "ClientRequestToken": token}
		call(t, "ExecuteTransaction", first)
		call(t, "ExecuteTransaction", first)
		status, m := raw(t, "ExecuteTransaction", map[string]any{"TransactStatements": statements("UPDATE " + tx + " SET v = 15 WHERE pk = 'b'"), "ClientRequestToken": token})
		if status != http.StatusBadRequest || typeOf(m) != "IdempotentParameterMismatchException" {
			t.Errorf("HTTP %d: %v, want IdempotentParameterMismatchException", status, m)
		}
		same(t, "the transaction ran once", rowsOf(t, db, "SELECT v FROM "+tx+" WHERE pk = 'b'"), "N(14)")
	})
	t.Run("transaction_across_requests", func(t *testing.T) {
		refused(t, db, "ValidationException", "BEGIN TRANSACTION")
		if _, err := db.BeginTx(t.Context(), nil); err == nil {
			t.Error("BeginTx returned a transaction")
		}
	})
	t.Run("parallel_scan_segments", func(t *testing.T) {
		// ExecuteStatement ignores Segment and TotalSegments, so a scan in
		// two segments reads every item twice.
		all := call(t, "ExecuteStatement", map[string]any{"Statement": "SELECT pk FROM " + plain})
		seg := call(t, "ExecuteStatement", map[string]any{"Statement": "SELECT pk FROM " + plain, "Segment": 0, "TotalSegments": 2})
		a, _ := all["Items"].([]any)
		b, _ := seg["Items"].([]any)
		if len(a) == 0 || len(a) != len(b) {
			t.Errorf("a scan gave %d items, and a scan of segment 0 of 2 gave %d, want the same count", len(a), len(b))
		}
	})
	t.Run("cancel_a_statement", func(t *testing.T) {
		status, m := raw(t, "CancelStatement", map[string]any{})
		if status != http.StatusBadRequest || typeOf(m) != "UnknownOperationException" {
			t.Errorf("HTTP %d: %v, want UnknownOperationException", status, m)
		}
	})
	skipAlternator(t, "alternator_scan_through_the_API", "alternator_batch_of_statements", "alternator_transaction")
}
