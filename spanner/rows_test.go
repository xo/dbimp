package spanner //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// stringField is the field of the STRING column s.
const stringField = `{"name":"s","type":{"code":"STRING"}}`

// bytesField is the field of a BYTES column.
func bytesField(name string) string {
	return `{"name":"` + name + `","type":{"code":"BYTES"}}`
}

// flush sends what the handler wrote, so that the client reads it before the handler
// goes on.
func flush(w http.ResponseWriter) {
	if err := http.NewResponseController(w).Flush(); err != nil {
		panic(err)
	}
}

// body serves a fake server that answers every statement with the text.
func body(t *testing.T, text string) *sql.DB {
	t.Helper()
	f := newFake(t, func(call) (int, string) { return http.StatusOK, text })
	return f.db()
}

// query reads every row of the query as *any values, and returns them with the
// error that ended the read.
func query(t *testing.T, db *sql.DB, q string) ([][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q)
	if err != nil {
		return nil, err
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
			t.Fatal(err)
		}
		out = append(out, vals)
	}
	return out, rows.Err()
}

// metaOf is the member metadata with the fields.
func metaOf(fields ...string) string {
	return `"metadata":{"rowType":{"fields":[` + strings.Join(fields, ",") + `]}}`
}

// TestJoinOfChunkedValues holds D191 item 11 with the shapes that the protocol
// allows: chunkedValue after the values, as the server writes it, and before them,
// a value in three pieces, a BYTES value whose first piece is not a multiple of four
// characters, and two lists that join at their last and first elements.
func TestJoinOfChunkedValues(t *testing.T) {
	t.Parallel()
	int64s, str, bytes := intField("id"), stringField, bytesField("y")
	for name, tt := range map[string]struct {
		body string
		want [][]any
	}{
		"chunkedValue after the values": {
			`[{"values":["1","ab"],` + metaOf(int64s, str) + `,"chunkedValue":true},{"values":["cd","2","x"],"last":true}]`,
			[][]any{{int64(1), "abcd"}, {int64(2), "x"}},
		},
		"chunkedValue before the values": {
			`[{"chunkedValue":true,"values":["1","ab"],` + metaOf(int64s, str) + `},{"values":["cd","2","x"]}]`,
			[][]any{{int64(1), "abcd"}, {int64(2), "x"}},
		},
		"metadata first": {
			`[{` + metaOf(int64s, str) + `,"values":["1","ab"],"chunkedValue":true},{"values":["cd"]}]`,
			[][]any{{int64(1), "abcd"}},
		},
		"three pieces": {
			`[{"values":["1","a"],` + metaOf(int64s, str) + `,"chunkedValue":true},{"values":["b"],"chunkedValue":true},{"values":["c","2","z"],"last":true}]`,
			[][]any{{int64(1), "abc"}, {int64(2), "z"}},
		},
		"a piece with a quote and a new line": {
			`[{"values":["1","a\"\n"],` + metaOf(int64s, str) + `,"chunkedValue":true},{"values":["\\b"]}]`,
			[][]any{{int64(1), "a\"\n\\b"}},
		},
		"BYTES joins before the base64 decode": {
			`[{"values":["1","YW"],` + metaOf(int64s, bytes) + `,"chunkedValue":true},{"values":["Jj"]}]`,
			[][]any{{int64(1), []byte("abc")}},
		},
		"a value that is not chunked ends its message": {
			`[{"values":["1","ab"],` + metaOf(int64s, str) + `,"chunkedValue":false},{"values":["2","cd"]}]`,
			[][]any{{int64(1), "ab"}, {int64(2), "cd"}},
		},
		"two lists": {
			`[{"values":[["a","b"]],"metadata":{"rowType":{"fields":[{"name":"l","type":{"code":"ARRAY","arrayElementType":{"code":"STRING"}}}]}},"chunkedValue":true},{"values":[["c","d"],["e"]]}]`,
			[][]any{{[]any{"a", "bc", "d"}}, {[]any{"e"}}},
		},
		"two lists that do not join": {
			`[{"values":[["a",null]],"metadata":{"rowType":{"fields":[{"name":"l","type":{"code":"ARRAY","arrayElementType":{"code":"STRING"}}}]}},"chunkedValue":true},{"values":[["c"]]}]`,
			[][]any{{[]any{"a", nil, "c"}}},
		},
	} {
		got, err := query(t, body(t, tt.body), "SELECT 1")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("%s: read %d rows, want %d", name, len(got), len(tt.want))
			continue
		}
		for i := range tt.want {
			for j := range tt.want[i] {
				if !equalValue(got[i][j], tt.want[i][j]) {
					t.Errorf("%s: row %d column %d is %#v, want %#v", name, i, j, got[i][j], tt.want[i][j])
				}
			}
		}
	}
}

// TestBrokenStreams holds that a stream that the driver cannot read to a whole
// result ends with an error: a value that stays cut, a row that stays short, a
// message that is not an object, data after the array, and a body that stops in
// the middle of a message. The error wraps dbimp.ErrIncomplete after a row.
func TestBrokenStreams(t *testing.T) {
	t.Parallel()
	int64s, str := intField("id"), stringField
	for name, tt := range map[string]struct {
		body string
		rows int
		want error
	}{
		"the last message is chunked": {
			`[{"values":["1","ab","2","c"],` + metaOf(int64s, str) + `,"chunkedValue":true}]`, 1, dbimp.ErrInvalidValue},
		"chunkedValue with no value": {
			`[{` + metaOf(int64s, str) + `,"chunkedValue":true}]`, 0, dbimp.ErrInvalidValue},
		"a row that stays short": {
			`[{"values":["1","a","2"],` + metaOf(int64s, str) + `}]`, 1, dbimp.ErrColumnCount},
		"values with no column": {
			`[{"values":["1"],"metadata":{"rowType":{}}}]`, 0, dbimp.ErrColumnCount},
		"data after the array": {
			`[{"values":["1","a"],` + metaOf(int64s, str) + `}] []`, 1, dbimp.ErrInvalidValue},
		"a message that is an array": {
			`[[]]`, 0, dbimp.ErrInvalidValue},
		"a body that is an object": {
			`{"rows":[]}`, 0, dbimp.ErrInvalidValue},
		"no metadata": {
			`[{"values":["1"]}]`, 0, dbimp.ErrInvalidValue},
		"no message": {
			`[]`, 0, dbimp.ErrInvalidValue},
		"a body that stops in a value": {
			`[{"values":["1","a","2","b`, 0, io.ErrUnexpectedEOF},
		"a body that stops after a message": {
			`[{"values":["1","a"],` + metaOf(int64s, str) + `},`, 1, io.ErrUnexpectedEOF},
		"a value that is not an INT64": {
			`[{"values":["x","a"],` + metaOf(int64s, str) + `}]`, 0, dbimp.ErrInvalidValue},
	} {
		got, err := query(t, body(t, tt.body), "SELECT 1")
		if err == nil || !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", name, err, tt.want)
			continue
		}
		// The first two rows are complete before the fault, except where the fault
		// comes first.
		if len(got) != tt.rows {
			t.Errorf("%s: read %d rows, want %d", name, len(got), tt.rows)
		}
		if tt.rows > 0 && !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: the error after %d rows is %v, want one that wraps dbimp.ErrIncomplete", name, len(got), err)
		}
		if tt.rows == 0 && errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: the error before any row wraps dbimp.ErrIncomplete: %v", name, err)
		}
	}
}

// TestErrorAfterAChunkedMessage holds D191 item 11: the error element after a
// message with chunkedValue drops the value that was cut, and the row that holds it.
func TestErrorAfterAChunkedMessage(t *testing.T) {
	t.Parallel()
	int64s, str := intField("id"), stringField
	text := `[{"values":["1","a","2","b"],` + metaOf(int64s, str) + `,"chunkedValue":true},{"error":{"code":400,"message":"division by zero","status":"OUT_OF_RANGE"}}]`
	got, err := query(t, body(t, text), "SELECT 1")
	if len(got) != 1 || got[0][0] != int64(1) {
		t.Errorf("read %v, want the row 1 only", got)
	}
	serr, ok := errors.AsType[*Error](err)
	if !ok || serr.Status != "OUT_OF_RANGE" || serr.HTTPStatus != 0 || !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want an *Error OUT_OF_RANGE with no HTTP status, that wraps dbimp.ErrIncomplete", err)
	}
}

// TestUnsupportedColumns holds D191 item 10: a column of the type ENUM or PROTO
// makes the driver return an error that names the column, before any row, and so
// does a type of the PostgreSQL dialect and a type that the driver does not know.
func TestUnsupportedColumns(t *testing.T) {
	t.Parallel()
	for name, field := range map[string]string{ //nolint:gosec // G101: the names of types, which hold no secret.
		"ENUM":                     `{"name":"color","type":{"code":"ENUM","protoTypeFqn":"example.Color"}}`,
		"PROTO":                    `{"name":"msg","type":{"code":"PROTO","protoTypeFqn":"example.Msg"}}`,
		"PG_NUMERIC":               `{"name":"price","type":{"code":"NUMERIC","typeAnnotation":"PG_NUMERIC"}}`,
		"PG_JSONB":                 `{"name":"doc","type":{"code":"JSON","typeAnnotation":"PG_JSONB"}}`,
		"an array of ENUM":         `{"name":"colors","type":{"code":"ARRAY","arrayElementType":{"code":"ENUM"}}}`,
		"TOKENLIST":                `{"name":"tokens","type":{"code":"TOKENLIST"}}`,
		"a new type":               `{"name":"geo","type":{"code":"GEOGRAPHY"}}`,
		"an array with no element": `{"name":"bad","type":{"code":"ARRAY"}}`,
	} {
		db := body(t, `[{"values":["x"],`+metaOf(field)+`}]`)
		_, err := query(t, db, "SELECT 1")
		if err == nil || !errors.Is(err, dbimp.ErrNotSupported) && name != "an array with no element" {
			t.Errorf("%s: the error is %v, want one that wraps dbimp.ErrNotSupported", name, err)
			continue
		}
		col := name
		switch name {
		case "ENUM":
			col = "color"
		case "PROTO":
			col = "msg"
		case "PG_NUMERIC":
			col = "price"
		case "PG_JSONB":
			col = "doc"
		case "an array of ENUM":
			col = "colors"
		case "TOKENLIST":
			col = "tokens"
		case "a new type":
			col = "geo"
		case "an array with no element":
			col = "bad"
		}
		if !strings.Contains(err.Error(), `"`+col+`"`) {
			t.Errorf("%s: the error does not name the column %s: %v", name, col, err)
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete", name)
		}
	}
}

// TestDMLResult holds the stats of a DML statement: rowCountExact is the count
// of Exec, a statement that changes no row has 0, a partitioned DML statement has
// a lower bound, and a query has no count, which is dbimp.ErrNotSupported.
func TestDMLResult(t *testing.T) {
	t.Parallel()
	for name, tt := range map[string]struct {
		body  string
		want  int64
		known bool
	}{
		"one row":       {dmlStream("1"), 1, true},
		"no row":        {dmlStream("0"), 0, true},
		"the most":      {dmlStream("9223372036854775807"), 9223372036854775807, true},
		"a lower bound": {`[{"metadata":{"rowType":{}},"stats":{"rowCountLowerBound":"3"},"last":true}]`, 3, true},
		"a query":       {streamOf(intField("a"), `"1"`), 0, false},
		"a plan":        {`[{` + metaOf(intField("a")) + `,"stats":{"queryPlan":{"planNodes":[]}},"last":true}]`, 0, false},
		"THEN RETURN":   {`[{` + metaOf(intField("a")) + `,"values":["5"],"stats":{"rowCountExact":"1"},"last":true}]`, 1, true},
	} {
		f := newFake(t, func(c call) (int, string) {
			if c.verb == "executeStreamingSql" {
				return http.StatusOK, tt.body
			}
			return txHandler(c)
		})
		res, err := f.db().ExecContext(t.Context(), "UPDATE t SET a = 1 WHERE TRUE")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		n, err := res.RowsAffected()
		switch {
		case tt.known && (err != nil || n != tt.want):
			t.Errorf("%s: RowsAffected is %d, %v, want %d", name, n, err, tt.want)
		case !tt.known && !errors.Is(err, dbimp.ErrNotSupported):
			t.Errorf("%s: RowsAffected gave %d, %v, want dbimp.ErrNotSupported", name, n, err)
		}
	}
}

// TestPlanModeHasNoRows holds that a statement with queryMode PLAN, which answers
// the columns and a plan and no row, reads as a result with no row.
func TestPlanModeHasNoRows(t *testing.T) {
	t.Parallel()
	db := body(t, `[{`+metaOf(intField("a"))+`,"stats":{"queryPlan":{"planNodes":[{"index":0}]}},"last":true}]`)
	got, err := query(t, db, "SELECT a FROM t")
	if err != nil || len(got) != 0 {
		t.Errorf("read %v and the error %v, want no row and no error", got, err)
	}
	rows, err := db.QueryContext(t.Context(), "SELECT a FROM t", WithParameter("queryMode", "PLAN"))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	defer func() {
		if err := rows.Err(); err != nil {
			t.Error(err)
		}
	}()
	if cols, _ := rows.Columns(); !reflect.DeepEqual(cols, []string{"a"}) {
		t.Errorf("the columns are %q, want a", cols)
	}
}

// TestColumnsOfNoName holds that a column with no name, such as SELECT 1, has
// the name "" (recorded: "a statement with a wrong token"), and two columns can
// share a name (recorded: "columns that share a name").
func TestColumnsOfNoName(t *testing.T) {
	t.Parallel()
	db := body(t, `[{"values":["1","2"],"metadata":{"rowType":{"fields":[{"type":{"code":"INT64"}},{"name":"a","type":{"code":"INT64"}}]}}}]`)
	rows, err := db.QueryContext(t.Context(), "SELECT 1, 2 AS a")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	defer func() {
		if err := rows.Err(); err != nil {
			t.Error(err)
		}
	}()
	if cols, _ := rows.Columns(); !reflect.DeepEqual(cols, []string{"", "a"}) {
		t.Errorf("the columns are %q, want the empty name and a", cols)
	}
}

// TestCloseEarlyReadsNothingMore holds D36: rows that the caller closes before
// the end close the body and read nothing more, and a second Close is no error.
func TestCloseEarlyReadsNothingMore(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	f := newFake(t, nil)
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/sessions"):
			_, _ = io.WriteString(w, `{"name":"`+testSession+`"}`)
		default:
			_, _ = io.WriteString(w, `[{"values":["1","2"],`+metaOf(intField("a"))+`},`)
			flush(w)
			<-release
			_, _ = io.WriteString(w, `{"values":["3"]}]`)
		}
	})
	rows, err := f.db().QueryContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	//nolint:sqlclosecheck // The test closes the rows at a chosen point.
	if err := rows.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	close(release)
	if err := rows.Close(); err != nil {
		t.Errorf("the second Close: %v", err)
	}
	if rows.Next() {
		t.Error("Next after Close returned a row")
	}
}

// bigServer streams a result of many messages of 1000 rows each.
type bigServer struct {
	messages int
}

func (b *bigServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	switch {
	case r.Method == http.MethodGet:
		_, _ = io.WriteString(w, `{}`)
		return
	case strings.HasSuffix(r.URL.Path, "/sessions"):
		_, _ = io.WriteString(w, `{"name":"`+testSession+`"}`)
		return
	}
	pad := strings.Repeat("p", 90)
	_, _ = io.WriteString(w, `[{`+metaOf(intField("n"), stringField)+`,"values":[]}`)
	var sb strings.Builder
	for m := range b.messages {
		sb.Reset()
		sb.WriteString(`,{"values":[`)
		for i := range 1000 {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(`"` + strconv.Itoa(m*1000+i) + `","` + pad + `"`)
		}
		sb.WriteString(`]}`)
		if _, err := io.WriteString(w, sb.String()); err != nil {
			return
		}
	}
	_, _ = io.WriteString(w, `]`)
}

// TestLargeResultHoldsLittleMemory holds D25: a result of 300 messages of 1000 rows,
// about 35 MB of JSON, never sits in memory. The heap stays within 16 MiB of where it
// started while the rows are read.
func TestLargeResultHoldsLittleMemory(t *testing.T) { //nolint:paralleltest // The test reads the heap of the process, which another test changes.
	srv := httptest.NewServer(&bigServer{messages: 300})
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL, false)
	rows, err := db.QueryContext(t.Context(), "SELECT n, s")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	runtime.GC()
	var base, now runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak uint64
	n := 0
	for rows.Next() {
		var (
			i int64
			s string
		)
		if err := rows.Scan(&i, &s); err != nil {
			t.Fatal(err)
		}
		if i != int64(n) || len(s) != 90 {
			t.Fatalf("row %d is %d and %d characters", n, i, len(s))
		}
		if n++; n%50000 == 0 {
			runtime.GC()
			runtime.ReadMemStats(&now)
			peak = max(peak, now.HeapAlloc)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("after %d rows: %v", n, err)
	}
	if n != 300000 {
		t.Errorf("read %d rows, want 300000", n)
	}
	if peak > base.HeapAlloc+16<<20 {
		t.Errorf("the heap grew by %d MiB while the driver read the rows, want less than 16 MiB (D25)", (peak-base.HeapAlloc)>>20)
	}
}

// TestNoGoroutineIsLeft holds that a connector, its queries, its transaction and
// its DDL statement leave no goroutine behind when the database closes.
func TestNoGoroutineIsLeft(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	f := newFake(t, func(c call) (int, string) {
		switch {
		case c.method == http.MethodPatch:
			return http.StatusOK, `{"name":"` + testDB + `/operations/op1","done":true}`
		case c.verb == "executeStreamingSql" && strings.Contains(string(c.body), "UPDATE"):
			return http.StatusOK, dmlStream("1")
		}
		return txHandler(c)
	})
	db := f.db()
	ctx := t.Context()
	var a int64
	if err := db.QueryRowContext(ctx, "SELECT a FROM t").Scan(&a); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE t SET a = 1"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE t SET a = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE x (id INT64) PRIMARY KEY (id)"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Err(); err != nil {
			t.Error(err)
		}
	}()
	rows.Close()
}

// TestTwoQueriesRunAtOnce holds that two queries run at the same time on one
// sql.DB: the fake server answers neither until both reached it.
func TestTwoQueriesRunAtOnce(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	arrived := 0
	both := make(chan struct{})
	f := newFake(t, func(c call) (int, string) {
		if c.verb != "executeStreamingSql" {
			return txHandler(c)
		}
		mu.Lock()
		if arrived++; arrived == 2 {
			close(both)
		}
		mu.Unlock()
		select {
		case <-both:
		case <-time.After(10 * time.Second):
		}
		return http.StatusOK, streamOf(intField("a"), `"7"`)
	})
	db := f.db()
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			var a int64
			errs <- db.QueryRowContext(t.Context(), "SELECT a FROM t").Scan(&a)
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("a query: %v", err)
		}
	}
	select {
	case <-both:
	default:
		t.Error("the two queries did not run at the same time")
	}
}

// TestValuesNeedNoSecondRead holds that a row is not delayed by the read of the
// next message: the last row of a message that is not chunked reaches the caller
// when its message ends, and the next message comes later.
func TestValuesNeedNoSecondRead(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	f := newFake(t, nil)
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/sessions"):
			_, _ = io.WriteString(w, `{"name":"`+testSession+`"}`)
		default:
			_, _ = io.WriteString(w, fmt.Sprintf(`[{"values":["1","2"],%s,"chunkedValue":false},`, metaOf(intField("a"))))
			flush(w)
			<-release
			_, _ = io.WriteString(w, `{"values":["3"]}]`)
		}
	})
	rows, err := f.db().QueryContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for want := int64(1); want <= 2; want++ {
		var a int64
		if !rows.Next() || rows.Scan(&a) != nil || a != want {
			t.Fatalf("the row %d is %d, %v", want, a, rows.Err())
		}
	}
	close(release)
	if !rows.Next() {
		t.Fatalf("the third row: %v", rows.Err())
	}
}
