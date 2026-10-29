package maxcompute //nolint:testpackage // The tests read the types and the errors of the driver, which are not exported.

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// fake is a fake server that answers by the recorded exchanges of chosen
// statements. A POST that creates an instance gets the answer of the flow whose
// query it holds. A GET gets the answer of the exchange with its path and its
// query. An EXPLAIN OUTPUT that no flow holds gets the answer of the flow
// named by refuse, which is the recorded failure of EXPLAIN OUTPUT for a
// statement that is not a query.
type fake struct {
	mu      sync.Mutex
	creates map[string]*dbimptest.Exchange
	gets    map[string]*dbimptest.Exchange
	refuse  string
	// seen are the requests that the server received, as the method and the path.
	seen []string
	// puts are the bodies of the PUT requests.
	puts []string
	// ended is the exchange that answers a PUT.
	ended *dbimptest.Exchange
}

// newFake starts a fake server for the flows, and returns the connector side as a
// database.
func newFake(t *testing.T, flows ...flow) (*fake, *sql.DB) {
	t.Helper()
	f := &fake{creates: map[string]*dbimptest.Exchange{}, gets: map[string]*dbimptest.Exchange{}}
	for _, fl := range flows {
		f.add(fl)
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, open(t, recordedConfig(), srv.URL)
}

// ServeHTTP satisfies http.Handler.
func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, r.Method+" "+r.URL.Path)
	var ex *dbimptest.Exchange
	switch r.Method {
	case http.MethodPost:
		q := normalize(queryOf(string(body)))
		ex = f.creates[q]
		if ex == nil && strings.HasPrefix(q, strings.TrimSpace(explainPrefix)) {
			ex = f.creates[f.refuse]
		}
	case http.MethodPut:
		f.puts = append(f.puts, string(body))
		ex = f.ended
	default:
		ex = f.gets[r.URL.Path+"?"+r.URL.RawQuery]
	}
	if ex == nil {
		http.Error(w, "no exchange", http.StatusTeapot)
		return
	}
	res := respond(r, ex)
	maps.Copy(w.Header(), res.Header)
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}

// add adds a flow.
func (f *fake) add(fl flow) {
	f.creates[normalize(queryOf(string(fl.create.Request.Content())))] = fl.create
	f.gets[fl.status.Request.Path+"?"+fl.status.Request.Query] = fl.status
	f.gets[fl.result.Request.Path+"?"+fl.result.Request.Query] = fl.result
}

// recordedCreate returns the exchange of a statement that was created with the
// status, whose query is text.
func recordedCreate(tb testing.TB, status int, text string) *dbimptest.Exchange {
	tb.Helper()
	for n := 1; n < exchangeCount; n++ {
		ex := exchange(tb, n)
		if ex.Request.Method == http.MethodPost && ex.Response.Status == status && normalize(queryOf(string(ex.Request.Content()))) == text {
			return ex
		}
	}
	tb.Fatalf("no recorded create with the status %d for %q", status, text)
	return nil
}

// selectFlows returns the flows of a SELECT that EXPLAIN OUTPUT types, and the
// statement itself.
func selectFlows(tb testing.TB) []flow {
	tb.Helper()
	return []flow{
		recordedFlow(tb, "EXPLAIN OUTPUT SELECT 1 AS a, 'x' AS b", "Success"),
		recordedFlow(tb, "SELECT 1 AS a, 'x' AS b", "Success"),
	}
}

func TestQueryTypesTheColumns(t *testing.T) {
	t.Parallel()
	_, db := newFake(t, selectFlows(t)...)
	rows, err := db.QueryContext(t.Context(), "SELECT 1 AS a, 'x' AS b")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{types[0].DatabaseTypeName(), types[1].DatabaseTypeName()}; got[0] != "INT" || got[1] != "STRING" {
		t.Errorf("the type names are %v, want INT and STRING (D219)", got)
	}
	if got := types[0].ScanType().String(); got != "int64" {
		t.Errorf("the scan type of INT is %s, want int64", got)
	}
	var a any
	var b string
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	if err := rows.Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	if a != int64(1) || b != "x" {
		t.Errorf("the row is %v (%T) and %q, want 1 (int64) and x", a, a, b)
	}
	if rows.Next() {
		t.Error("the result has a second row")
	}
	if err := rows.Err(); err != nil {
		t.Error(err)
	}
}

func TestQueryOfAMissingTableIsTheErrorOfTheServer(t *testing.T) {
	t.Parallel()
	_, db := newFake(t,
		recordedFlow(t, "EXPLAIN OUTPUT SELECT * FROM dbimp_it_missing", "Failed"),
		recordedFlow(t, "SELECT * FROM dbimp_it_missing", "Failed"))
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM dbimp_it_missing")
	if err == nil {
		defer rows.Close()
		t.Fatalf("a query of a missing table gave rows, and the error %v", rows.Err())
	}
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("the error is %v, want an *Error", err)
	}
	if !strings.HasPrefix(e.Message, "ODPS-0130131") {
		t.Errorf("the message is %q, want the code ODPS-0130131 of a table that is not found", e.Message)
	}
	if errors.Is(err, dbimp.ErrAuthentication) || errors.Is(err, ErrCanceled) {
		t.Errorf("%v matches a sentinel that it must not match", err)
	}
}

func TestQueryOfATextStatementIsOneColumn(t *testing.T) {
	t.Parallel()
	show := recordedCreate(t, http.StatusOK, "SHOW TABLES")
	f, db := newFake(t, recordedFlow(t, "EXPLAIN OUTPUT SELECT * FROM dbimp_it_missing", "Failed"))
	f.refuse = "EXPLAIN OUTPUT SELECT * FROM dbimp_it_missing"
	f.creates["SHOW TABLES"] = show
	rows, err := db.QueryContext(t.Context(), "SHOW TABLES")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if len(cols) != 1 || cols[0] != "text" {
		t.Fatalf("the columns are %v, want one named text (D219 item 4)", cols)
	}
	var text string
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	if err := rows.Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "dbimp_it_") {
		t.Errorf("the text is %q, want the names of the tables", text)
	}
	if rows.Next() {
		t.Error("a text result has a second row")
	}
}

func TestExecOfDDLRunsAtOnce(t *testing.T) {
	t.Parallel()
	f, db := newFake(t)
	f.creates["CREATE TABLE dbimp_it_dml (id BIGINT, name STRING, score DOUBLE)"] = recordedCreate(t, http.StatusOK, "CREATE TABLE dbimp_it_dml (id BIGINT, name STRING, score DOUBLE)")
	res, err := db.ExecContext(t.Context(), "CREATE TABLE dbimp_it_dml (id BIGINT, name STRING, score DOUBLE)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected gave %v, want dbimp.ErrNotSupported (D178)", err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
	}
	for _, s := range f.seen {
		if strings.HasPrefix(s, "GET ") {
			t.Errorf("a statement that answered at once was polled: %s", s)
		}
	}
}

func TestExecOfAStatementThatDoesNotParse(t *testing.T) {
	t.Parallel()
	f, db := newFake(t)
	bad := refusedExchange(t)
	f.creates["SELEC 1"] = bad
	_, err := db.ExecContext(t.Context(), "SELEC 1")
	e, ok := errors.AsType[*Error](err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || e.Code != "ParseError" {
		t.Fatalf("the error is %v, want a 400 with the code ParseError", err)
	}
	if !strings.Contains(e.Message, "invalid token") {
		t.Errorf("the message is %q", e.Message)
	}
}

func TestExecSendsAStatementOnceAndEndsItOnTheServer(t *testing.T) {
	t.Parallel()
	running := recordedFlow(t, "MERGE INTO dbimp_it_tx t USING (SELECT 3 AS id, 'm' AS nm) s ON t.id = s.id WHEN MATCHED THEN UPDATE SET name = s.nm WHEN NOT MATCHED THEN INSERT VALUES (s.id, s.nm)", "Running")
	f, db := newFake(t, running)
	f.ended = recordedPut(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		// The poll answers Running forever, so the context ends while it waits.
		for {
			f.mu.Lock()
			n := len(f.seen)
			f.mu.Unlock()
			if n >= 3 {
				cancel()
				return
			}
		}
	}()
	_, err := db.ExecContext(ctx, "MERGE INTO dbimp_it_tx t USING (SELECT 3 AS id, 'm' AS nm) s ON t.id = s.id WHEN MATCHED THEN UPDATE SET name = s.nm WHEN NOT MATCHED THEN INSERT VALUES (s.id, s.nm)")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the error is %v, want context.Canceled (D36)", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.puts) != 1 || !strings.Contains(f.puts[0], "<Status>Terminated</Status>") {
		t.Errorf("the driver sent %v, want one PUT with the status Terminated (D220 item 7)", f.puts)
	}
	posts := 0
	for _, s := range f.seen {
		if strings.HasPrefix(s, "POST ") {
			posts++
		}
	}
	if posts != 1 {
		t.Errorf("the statement was sent %d times, want 1 (D8)", posts)
	}
}

// recordedPut returns the recorded exchange of the request that ends an instance.
func recordedPut(tb testing.TB) *dbimptest.Exchange {
	tb.Helper()
	for n := 1; n < exchangeCount; n++ {
		ex := exchange(tb, n)
		if ex.Request.Method == http.MethodPut {
			return ex
		}
	}
	tb.Fatal("no recorded PUT")
	return nil
}

func TestTransactionIsRefused(t *testing.T) {
	t.Parallel()
	_, db := newFake(t)
	if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported (D20)", err)
	}
}
