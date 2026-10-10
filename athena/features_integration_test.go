package athena_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/athena"
)

// raw sends an operation of the API of Athena that the driver does not send, and
// returns its status and its body.
func raw(t *testing.T, op string, body any) (int, []byte) {
	t.Helper()
	status, out, err := athena.Raw(t.Context(), integrationConfig(t), op, mustJSON(t, body))
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return status, out
}

// stateOf returns the state of the query id.
func stateOf(t *testing.T, id string) string {
	t.Helper()
	status, body := raw(t, "GetQueryExecution", map[string]string{"QueryExecutionId": id})
	var q struct {
		QueryExecution struct{ Status struct{ State string } }
	}
	if err := json.Unmarshal(body, &q); err != nil || status != http.StatusOK {
		t.Fatalf("reading the state of %s: %d %s %v", id, status, body, err)
	}
	return q.QueryExecution.Status.State
}

// waitState polls the state of the query id until it is one of want, within the
// time limit. It never sleeps for a fixed time.
func waitState(t *testing.T, id string, limit time.Duration, want ...string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		state := stateOf(t, id)
		if slices.Contains(want, state) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the query %s is %s after %v, want one of %v", id, state, limit, want)
		}
		select {
		case <-t.Context().Done():
			t.Fatal("the test ended while it waited for a state")
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// startRaw starts a query with the token and the workgroup of the DSN, and
// returns the status and the body of the answer.
func startRaw(t *testing.T, query, token string) (int, []byte) {
	t.Helper()
	cfg := integrationConfig(t)
	body := map[string]any{"QueryString": query, "ClientRequestToken": token}
	if cfg.WorkGroup != "" {
		body["WorkGroup"] = cfg.WorkGroup
	}
	if cfg.Database != "" {
		body["QueryExecutionContext"] = map[string]string{"Database": cfg.Database}
	}
	return raw(t, "StartQueryExecution", body)
}

// startID starts a query like startRaw, and returns its id.
func startID(t *testing.T, query, token string) string {
	t.Helper()
	status, body := startRaw(t, query, token)
	var out struct {
		ID string `json:"QueryExecutionId"`
	}
	if err := json.Unmarshal(body, &out); err != nil || status != http.StatusOK || out.ID == "" {
		t.Fatalf("the start answered %d %s %v, want an id", status, body, err)
	}
	return out.ID
}

// TestIntegrationFeatures tests the features of Athena that docs/ATHENA.md
// names, and the refusals of the entries that the survey marks no. Each subtest
// is one entry of testdata/athena/features.json.
func TestIntegrationFeatures(t *testing.T) {
	db := connect(t)
	cfg := integrationConfig(t)
	ext := external(t, db, "feat_ext")
	ice := iceberg(t, db, "feat_ice")
	exec(t, db, "INSERT INTO "+ext+" VALUES (1,'a'),(2,'b'),(3,'c')")
	exec(t, db, "INSERT INTO "+ice+" VALUES (1,'a'),(2,'b'),(3,'c')")

	t.Run("workgroup", func(t *testing.T) {
		if cfg.WorkGroup == "" {
			t.Skip("the DSN names no workgroup")
		}
		status, body := raw(t, "GetWorkGroup", map[string]string{"WorkGroup": cfg.WorkGroup})
		if status != http.StatusOK || !strings.Contains(string(body), `"Name":"`+cfg.WorkGroup+`"`) {
			t.Errorf("GetWorkGroup answered %d %s", status, body)
		}
		// A workgroup that does not exist, or that the user cannot use, answers
		// with an AccessDeniedException (recorded: "a missing workgroup").
		_, err := db.ExecContext(t.Context(), "SELECT 1", athena.WithWorkGroup(cfg.WorkGroup+"_nosuch"))
		var aerr *athena.Error
		if !errors.As(err, &aerr) || aerr.Type != "AccessDeniedException" {
			t.Errorf("a missing workgroup gave %v, want an AccessDeniedException", err)
		}
	})
	t.Run("execution_parameters", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT ?, ?, ?", 1, "y", nil)
		if !equalRows(got, [][]any{{int64(1), "y", nil}}) {
			t.Errorf("the parameters came back as %v, want 1, y and NULL", got)
		}
		// Escaping: a quote, a backslash, a double quote and text outside ASCII.
		const hard = `it's a \ "test" héllo ✓ ?`
		if got := rowsOf(t, db, "SELECT ?", hard); !equalRows(got, [][]any{{hard}}) {
			t.Errorf("the string came back as %v, want %q", got, hard)
		}
		if got := rowsOf(t, db, "SELECT name FROM "+ext+" WHERE id = ?", 2); !equalRows(got, [][]any{{"b"}}) {
			t.Errorf("the parameter in a comparison gave %v, want b", got)
		}
		// Too few parameters fail the query, not the request (recorded: "too few
		// parameters").
		_, err := db.ExecContext(t.Context(), "SELECT ?, ?", 1)
		if aerr := refusalOfQuery(t, err); aerr.ErrorType != 1100 {
			t.Errorf("too few parameters gave %+v, want the type 1100", *aerr)
		}
	})
	t.Run("prepared_statement", func(t *testing.T) {
		if cfg.WorkGroup == "" {
			t.Skip("the DSN names no workgroup")
		}
		name := table("ps")
		t.Cleanup(func() {
			ctx := context.WithoutCancel(t.Context())
			_, _, _ = athena.Raw(ctx, cfg, "DeletePreparedStatement", mustJSON(t, map[string]string{"StatementName": name, "WorkGroup": cfg.WorkGroup}))
		})
		status, body := raw(t, "CreatePreparedStatement", map[string]string{"StatementName": name, "WorkGroup": cfg.WorkGroup, "QueryStatement": "SELECT ? AS v, ? AS w"})
		if status != http.StatusOK {
			t.Fatalf("CreatePreparedStatement answered %d %s", status, body)
		}
		if got := rowsOf(t, db, "EXECUTE "+name+" USING 5, 'x'"); !equalRows(got, [][]any{{int64(5), "x"}}) {
			t.Errorf("EXECUTE gave %v, want 5 and x", got)
		}
		// Too few values fail the query (recorded: "execute with too few values").
		_, err := db.ExecContext(t.Context(), "EXECUTE "+name+" USING 5")
		if aerr := refusalOfQuery(t, err); aerr.ErrorType != 1100 {
			t.Errorf("EXECUTE with too few values gave %+v, want the type 1100", *aerr)
		}
		status, body = raw(t, "DeletePreparedStatement", map[string]string{"StatementName": name, "WorkGroup": cfg.WorkGroup})
		if status != http.StatusOK {
			t.Fatalf("DeletePreparedStatement answered %d %s", status, body)
		}
		status, body = raw(t, "GetPreparedStatement", map[string]string{"StatementName": name, "WorkGroup": cfg.WorkGroup})
		if status != http.StatusBadRequest || !strings.Contains(string(body), "ResourceNotFoundException") {
			t.Errorf("GetPreparedStatement after the delete answered %d %s, want a ResourceNotFoundException", status, body)
		}
	})
	t.Run("client_request_token", func(t *testing.T) {
		token := "dbimp-it-" + suffix + "-0123456789abcdef0123456789"
		query := "SELECT 7 /* " + suffix + " */"
		first := startID(t, query, token)
		second := startID(t, query, token)
		if first != second {
			t.Errorf("the same token and the same query gave the ids %s and %s, want one", first, second)
		}
		status, body := startRaw(t, query+" ", token)
		if status != http.StatusBadRequest || !strings.Contains(string(body), "IDEMPOTENT_PARAMETER_MISMATCH") {
			t.Errorf("the same token with another query answered %d %s, want IDEMPOTENT_PARAMETER_MISMATCH", status, body)
		}
		waitState(t, first, time.Minute, "SUCCEEDED", "FAILED", "CANCELLED")
	})
	t.Run("result_paging", func(t *testing.T) {
		rows, err := db.QueryContext(t.Context(), "SELECT n FROM UNNEST(sequence(1,2500)) AS t(n)")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := int64(0)
		for rows.Next() {
			var v int64
			if err := rows.Scan(&v); err != nil {
				t.Fatal(err)
			}
			if n++; v != n {
				t.Fatalf("row %d holds %d", n, v)
			}
		}
		if err := rows.Err(); err != nil || n != 2500 {
			t.Errorf("read %d rows and %v, want 2500 rows across three pages", n, err)
		}
	})
	t.Run("header_row", func(t *testing.T) {
		if got := rowsOf(t, db, "SELECT n FROM UNNEST(sequence(1,3)) AS t(n)"); len(got) != 3 || got[0][0] != int64(1) {
			t.Errorf("the rows are %v, want 1, 2 and 3: the header row is dropped", got)
		}
		// A row of data that equals the header is data (athenadriver drops it).
		if got := rowsOf(t, db, "SELECT 'n' AS n"); !equalRows(got, [][]any{{"n"}}) {
			t.Errorf("the rows are %v, want the one value n", got)
		}
		// A SELECT that finds nothing has the header row and no more.
		if got := rowsOf(t, db, "SELECT id FROM "+ext+" WHERE id = -1"); len(got) != 0 {
			t.Errorf("the rows are %v, want none", got)
		}
		// A statement of the type UTILITY has no header row.
		for _, row := range rowsOf(t, db, "SHOW TABLES") {
			if row[0] == "tab_name" {
				t.Error("SHOW TABLES gave a header row")
			}
		}
	})
	t.Run("update_count", func(t *testing.T) {
		res := exec(t, db, "INSERT INTO "+ice+" VALUES (10,'x'),(11,'y')")
		if n := affected(t, res); n != 2 {
			t.Errorf("INSERT changed %d rows, want 2", n)
		}
		// DESCRIBE answers no UpdateCount (recorded: "describe the table").
		res = exec(t, db, "DESCRIBE "+ext)
		if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("RowsAffected of DESCRIBE gave %v, want dbimp.ErrNotSupported", err)
		}
	})
	t.Run("stop_query_execution", func(t *testing.T) {
		query := "SELECT count(*) FROM UNNEST(sequence(1,50000)) AS t(n) CROSS JOIN UNNEST(sequence(1,50000)) AS u(m) CROSS JOIN UNNEST(sequence(1,50000)) AS v(k) /* stop " + suffix + " */"
		id := startID(t, query, "dbimp-it-"+suffix+"-stop-0123456789abcdef012345")
		waitState(t, id, time.Minute, "RUNNING")
		if status, body := raw(t, "StopQueryExecution", map[string]string{"QueryExecutionId": id}); status != http.StatusOK {
			t.Fatalf("StopQueryExecution answered %d %s", status, body)
		}
		waitState(t, id, time.Minute, "CANCELLED")
		// The results of a query that was canceled are an error of the request
		// (recorded: "the results of the stopped minute statement").
		status, body := raw(t, "GetQueryResults", map[string]string{"QueryExecutionId": id})
		if status != http.StatusBadRequest || !strings.Contains(string(body), "RESULT_NOT_FOUND") {
			t.Errorf("GetQueryResults of the canceled query answered %d %s, want RESULT_NOT_FOUND", status, body)
		}
	})
	t.Run("result_reuse", func(t *testing.T) {
		query := "SELECT id FROM " + ext + " ORDER BY id"
		reuse := athena.WithParameter("ResultReuseConfiguration", map[string]any{"ResultReuseByAgeConfiguration": map[string]any{"Enabled": true, "MaxAgeInMinutes": 60}})
		first := rowsOf(t, db, query, reuse)
		second := rowsOf(t, db, query, reuse)
		if !equalRows(first, second) || len(first) != 3 {
			t.Errorf("the two reads gave %v and %v, want the same three rows", first, second)
		}
		execs := executionsOf(t, query)
		if len(execs) < 2 || execs[0].Reuse != true || execs[1].Reuse != false {
			t.Errorf("the executions are %+v, want the newer one to reuse the result of the older one", execs)
		}
	})
	t.Run("msck_repair_table", func(t *testing.T) {
		name := table("feat_msck")
		dropLater(t, db, name)
		exec(t, db, "CREATE EXTERNAL TABLE "+name+" (id int) PARTITIONED BY (p string) STORED AS PARQUET LOCATION '"+location(t, "feat_msck")+"'")
		exec(t, db, "INSERT INTO "+name+" SELECT 1, 'a'")
		if _, err := db.ExecContext(t.Context(), "MSCK REPAIR TABLE "+name); err != nil {
			t.Fatal(err)
		}
		if got := rowsOf(t, db, "SHOW PARTITIONS "+name); !equalRows(got, [][]any{{"p=a"}}) {
			t.Errorf("the partitions are %v, want p=a", got)
		}
	})
	t.Run("partition_projection", func(t *testing.T) {
		name := table("feat_proj")
		dropLater(t, db, name)
		exec(t, db, "CREATE EXTERNAL TABLE "+name+" (id int) PARTITIONED BY (p string) STORED AS PARQUET LOCATION '"+location(t, "feat_proj")+"' TBLPROPERTIES ('projection.enabled'='true', 'projection.p.type'='enum', 'projection.p.values'='a,b', 'storage.location.template'='"+location(t, "feat_proj")+"${p}/')")
		if got := rowsOf(t, db, "SELECT count(*) FROM "+name); !equalRows(got, [][]any{{int64(0)}}) {
			t.Errorf("the count is %v, want 0", got)
		}
	})
	t.Run("federated_query", func(t *testing.T) {
		catalog := os.Getenv(envFederated)
		if catalog == "" {
			t.Skipf("%s is empty, so there is no federated catalog to test against", envFederated)
		}
		got := rowsOf(t, db, "SELECT count(*) AS c FROM all_log_streams", athena.WithCatalog(catalog), athena.WithDatabase("/aws/lambda/"+catalog))
		if len(got) != 1 {
			t.Fatalf("the count is %v, want one row", got)
		}
		if _, ok := got[0][0].(int64); !ok {
			t.Errorf("the count is a %T, want int64", got[0][0])
		}
		// The three part name works with no context.
		got = rowsOf(t, db, `SELECT table_name FROM "`+catalog+`".information_schema.tables LIMIT 5`)
		if len(got) == 0 {
			t.Error("the information schema of the federated catalog has no table")
		}
	})
	t.Run("iceberg_time_travel", func(t *testing.T) {
		if got := rowsOf(t, db, "SELECT id FROM "+ice+" FOR TIMESTAMP AS OF current_timestamp ORDER BY id"); len(got) < 3 {
			t.Errorf("the table as of now holds %v, want at least the three first rows", got)
		}
		got := rowsOf(t, db, `SELECT snapshot_id FROM "`+ice+`$history"`)
		if len(got) == 0 {
			t.Fatal("the history of the table is empty")
		}
		if _, ok := got[0][0].(int64); !ok {
			t.Errorf("the snapshot id is a %T, want int64", got[0][0])
		}
		// The version is the snapshot id, a bigint, and not an integer (recorded:
		// "iceberg time travel by version").
		id, _ := got[0][0].(int64)
		// The service refuses a parameter in this clause, so the id is a literal.
		if got := rowsOf(t, db, "SELECT id FROM "+ice+" FOR VERSION AS OF "+strconv.FormatInt(id, 10)); len(got) == 0 {
			t.Errorf("the table as of the first snapshot holds no row")
		}
	})
	t.Run("information_schema", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT table_name FROM information_schema.tables WHERE table_name = ?", ext)
		if !equalRows(got, [][]any{{ext}}) {
			t.Errorf("the information schema lists %v, want %s", got, ext)
		}
		got = rowsOf(t, db, "SELECT column_name, data_type FROM information_schema.columns WHERE table_name = ? ORDER BY ordinal_position", ext)
		if !equalRows(got, [][]any{{"id", "integer"}, {"name", "varchar"}}) {
			t.Errorf("the columns are %v, want id and name", got)
		}
	})
	t.Run("show_tables", func(t *testing.T) {
		var names []any
		for _, row := range rowsOf(t, db, "SHOW TABLES") {
			names = append(names, row[0])
		}
		if !slices.Contains(names, any(ext)) || !slices.Contains(names, any(ice)) {
			t.Errorf("SHOW TABLES lists %v, want %s and %s", names, ext, ice)
		}
	})
	t.Run("describe_table", func(t *testing.T) {
		rows, err := db.QueryContext(t.Context(), "DESCRIBE "+ext)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		if !slices.Equal(cols, []string{"col_name", "data_type", "comment"}) {
			t.Errorf("the columns are %q", cols)
		}
		types, _ := rows.ColumnTypes()
		if got := types[0].DatabaseTypeName(); got != "STRING" {
			t.Errorf("the type of the first column is %s, want STRING", got)
		}
		var first string
		if !rows.Next() {
			t.Fatalf("DESCRIBE gave no row: %v", rows.Err())
		}
		var b, c any
		if err := rows.Scan(&first, &b, &c); err != nil {
			t.Fatal(err)
		}
		// The row has one value that joins the three fields with tab characters.
		if !strings.HasPrefix(first, "id") || !strings.Contains(first, "int") || b != nil || c != nil {
			t.Errorf("the first row is %q, %v and %v, want the text of the server and two NULLs", first, b, c)
		}
	})
	t.Run("show_create_table", func(t *testing.T) {
		var lines []string
		for _, row := range rowsOf(t, db, "SHOW CREATE TABLE "+ext) {
			lines = append(lines, fmt.Sprint(row[0]))
		}
		if text := strings.Join(lines, "\n"); !strings.Contains(text, "CREATE EXTERNAL TABLE") {
			t.Errorf("SHOW CREATE TABLE gave %q", text)
		}
	})
	t.Run("explain", func(t *testing.T) {
		got := rowsOf(t, db, "EXPLAIN SELECT 1")
		if len(got) == 0 || !strings.HasPrefix(fmt.Sprint(got[0][0]), "Fragment 0") {
			t.Errorf("EXPLAIN gave %v, want a plan that starts with Fragment 0, with no header row", got)
		}
	})
	t.Run("several_statements", func(t *testing.T) {
		_, err := db.ExecContext(t.Context(), "SELECT 1; SELECT 2")
		aerr := refusal(t, err)
		if !strings.Contains(aerr.Message, "Only one sql statement is allowed") {
			t.Errorf("the error is %v, want the text of the server for two statements", aerr)
		}
	})
	t.Run("transaction", func(t *testing.T) {
		if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
		}
		_, err := db.ExecContext(t.Context(), "START TRANSACTION")
		aerr := refusal(t, err)
		if !strings.Contains(aerr.Message, "Queries of this type are not supported") {
			t.Errorf("the error is %v", aerr)
		}
	})
	t.Run("use_statement", func(t *testing.T) {
		_, err := db.ExecContext(t.Context(), "USE "+cfg.Database)
		aerr := refusal(t, err)
		if !strings.Contains(aerr.Message, "Queries of this type are not supported") {
			t.Errorf("the error is %v", aerr)
		}
	})
	t.Run("version_function", func(t *testing.T) {
		// The server has no version() (recorded: "version function"), and the driver
		// answers no version request of its own (D192 item 6).
		_, err := db.ExecContext(t.Context(), "SELECT version()")
		if aerr := refusalOfQuery(t, err); aerr.ErrorType != 1303 {
			t.Errorf("SELECT version() gave %+v, want the type 1303", *aerr)
		}
	})
	t.Run("system_runtime_nodes", func(t *testing.T) {
		// The query that usql runs for the version is refused at the start.
		_, err := db.ExecContext(t.Context(), "SELECT node_version FROM system.runtime.nodes LIMIT 1")
		aerr := refusal(t, err)
		if !strings.Contains(aerr.Message, "Queries of this type are not supported") {
			t.Errorf("the error is %v", aerr)
		}
	})
}
