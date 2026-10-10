package cosmos_test

import (
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/cosmos"
)

// featureDocs are the 30 documents of the feature tests, with the partition
// keys a, b and c. The document dNN has n = NN, grp = NN mod 4, two tags, and
// an object.
func featureDocs() []map[string]any {
	docs := make([]map[string]any, 30)
	for i := range docs {
		docs[i] = map[string]any{
			"id":   fmt.Sprintf("d%02d", i),
			"pk":   []string{"a", "b", "c"}[i%3],
			"n":    i,
			"grp":  i % 4,
			"tags": []any{"t" + strconv.Itoa(i%2), "u"},
			"obj":  map[string]any{"k": i % 5},
		}
	}
	return docs
}

// queryHeaders are the headers of a query that a test sends as a request of
// its own, so that it can read the headers of the answer.
func queryHeaders(extra map[string]string) map[string]string {
	return merge(map[string]string{
		"Content-Type":                               "application/query+json",
		"X-Ms-Documentdb-Isquery":                    "True",
		"X-Ms-Documentdb-Query-Enablecrosspartition": "True",
	}, extra)
}

// TestIntegrationFeatures runs each feature of the survey that is not a type or
// a schema object. A test that needs the hosted account reads COSMOS_HOSTED_DSN
// and skips when it is empty (D190 item 13). A query that the hosted gateway
// refuses across partitions names the partition key, as D190 tells a caller.
func TestIntegrationFeatures(t *testing.T) {
	a := getAccount(t, envEmulator)
	a.container(t, "feat", nil)
	a.seed(t, "feat", featureDocs())
	db := a.open(t, "feat")
	queryPath := collPath("feat") + "/docs"
	inA := cosmosKey("a")

	t.Run("request charge", func(t *testing.T) {
		status, hdr, body := a.rest(t, http.MethodPost, queryPath, queryHeaders(nil), map[string]any{"query": "SELECT c.id FROM c"})
		if status != http.StatusOK {
			t.Fatalf("the query: HTTP %d %s", status, body)
		}
		charge, err := strconv.ParseFloat(hdr.Get("X-Ms-Request-Charge"), 64)
		if err != nil || charge <= 0 {
			t.Errorf("the request charge is %q, want a number above 0", hdr.Get("X-Ms-Request-Charge"))
		}
	})

	t.Run("continuation token", func(t *testing.T) {
		status, hdr, body := a.rest(t, http.MethodPost, queryPath, queryHeaders(map[string]string{"X-Ms-Max-Item-Count": "5"}), map[string]any{"query": "SELECT c.id FROM c"})
		if status != http.StatusOK || hdr.Get("X-Ms-Continuation") == "" {
			t.Fatalf("the first page: HTTP %d, token %q, want a token: %s", status, hdr.Get("X-Ms-Continuation"), body)
		}
		// The driver follows the token to the last page, with no duplicate and
		// no gap.
		got := ids(t, db, "SELECT c.id FROM c", cosmos.WithPageSize(5))
		want := make([]string, 30)
		for i := range want {
			want[i] = fmt.Sprintf("d%02d", i)
		}
		slices.Sort(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the pages hold %v, want the 30 documents once", got)
		}
	})

	t.Run("cross partition query", func(t *testing.T) {
		// A plain SELECT across the three partitions works on both servers, and
		// without a header of a partition key (recorded: "the first page with the
		// default size").
		if got := ids(t, db, "SELECT c.id FROM c WHERE c.n < 10"); len(got) != 10 {
			t.Errorf("a query across partitions gave %d rows, want 10", len(got))
		}
	})

	t.Run("query plan", func(t *testing.T) {
		h := queryHeaders(map[string]string{
			"X-Ms-Cosmos-Is-Query-Plan-Request":    "True",
			"X-Ms-Cosmos-Supported-Query-Features": "Aggregate, CompositeAggregate, Distinct, GroupBy, MultipleOrderBy, MultipleAggregates, OffsetAndLimit, OrderBy, Top, NonValueAggregate, DCount, NonStreamingOrderBy",
		})
		status, _, body := a.rest(t, http.MethodPost, queryPath, h, map[string]any{"query": "SELECT c.id FROM c ORDER BY c.n"})
		if status != http.StatusOK {
			t.Fatalf("the plan: HTTP %d %s", status, body)
		}
		var plan struct {
			Version int `json:"partitionedQueryExecutionInfoVersion"`
			Info    struct {
				OrderBy []string `json:"orderBy"`
			} `json:"queryInfo"`
		}
		if err := json.Unmarshal(body, &plan); err != nil {
			t.Fatalf("reading the plan: %v: %s", err, body)
		}
		// The emulator answers with empty lists (recorded: "lead: the query plan of
		// an ordered query").
		if !a.emulator && !reflect.DeepEqual(plan.Info.OrderBy, []string{"Ascending"}) {
			t.Errorf("the order of the plan is %v, want Ascending", plan.Info.OrderBy)
		}
	})

	t.Run("etag and if-match", func(t *testing.T) {
		path := collPath("feat") + "/docs/d00"
		status, hdr, body := a.rest(t, http.MethodGet, path, pk("a"), nil)
		if status != http.StatusOK || hdr.Get("ETag") == "" {
			t.Fatalf("reading d00: HTTP %d, etag %q: %s", status, hdr.Get("ETag"), body)
		}
		etag := hdr.Get("ETag")
		// The hosted account answers HTTP 304 to the etag that the document has,
		// and the emulator answers HTTP 200 (recorded: "lead: read with the etag
		// that the document has").
		status, _, _ = a.rest(t, http.MethodGet, path, merge(pk("a"), map[string]string{"If-None-Match": etag}), nil)
		switch {
		case a.emulator && status != http.StatusOK:
			t.Errorf("a read with If-None-Match gave HTTP %d on the emulator, want %d", status, http.StatusOK)
		case !a.emulator && status != http.StatusNotModified:
			t.Errorf("a read with If-None-Match gave HTTP %d, want %d", status, http.StatusNotModified)
		}
		d := map[string]any{"id": "d00", "pk": "a", "n": 0, "grp": 0, "tags": []any{"t0", "u"}, "obj": map[string]any{"k": 0}}
		if status, _, body := a.rest(t, http.MethodPut, path, merge(pk("a"), map[string]string{"If-Match": etag}), d); status != http.StatusOK {
			t.Errorf("a replace with the etag: HTTP %d %s", status, body)
		}
		if status, _, _ := a.rest(t, http.MethodPut, path, merge(pk("a"), map[string]string{"If-Match": etag}), d); status != http.StatusPreconditionFailed {
			t.Errorf("a second replace with the old etag gave HTTP %d, want %d", status, http.StatusPreconditionFailed)
		}
	})

	t.Run("session token", func(t *testing.T) {
		path := collPath("feat") + "/docs/d01"
		status, hdr, body := a.rest(t, http.MethodGet, path, pk("b"), nil)
		token := hdr.Get("X-Ms-Session-Token")
		if status != http.StatusOK || token == "" {
			t.Fatalf("reading d01: HTTP %d, session token %q: %s", status, token, body)
		}
		if status, _, body := a.rest(t, http.MethodGet, path, merge(pk("b"), map[string]string{"X-Ms-Session-Token": token}), nil); status != http.StatusOK {
			t.Errorf("a read with the session token: HTTP %d %s", status, body)
		}
	})

	t.Run("select value", func(t *testing.T) {
		if got := scalar(t, db, "SELECT VALUE c.n FROM c WHERE c.id = 'd05'"); got != int64(5) {
			t.Errorf("the value is %v (%T), want 5", got, got)
		}
		cols, rows := query(t, db, "SELECT VALUE c.tags FROM c WHERE c.id = 'd05'")
		if !reflect.DeepEqual(cols, []string{"$1"}) || !reflect.DeepEqual(rows, [][]any{{[]any{"t1", "u"}}}) {
			t.Errorf("the columns %v and rows %v, want $1 and one array", cols, rows)
		}
		cols, rows = query(t, db, "SELECT VALUE c.obj FROM c WHERE c.id = 'd07'")
		if !reflect.DeepEqual(cols, []string{"k"}) || !reflect.DeepEqual(rows, [][]any{{int64(2)}}) {
			t.Errorf("the columns %v and rows %v, want the key k of the object", cols, rows)
		}
		// A value that is not there is no row.
		if _, rows := query(t, db, "SELECT VALUE c.nope FROM c WHERE c.id = 'd05'"); len(rows) != 0 {
			t.Errorf("a missing value gave %v, want no row", rows)
		}
	})

	t.Run("join inside a document", func(t *testing.T) {
		cols, rows := query(t, db, "SELECT c.id, t FROM c JOIN t IN c.tags WHERE c.id = 'd00'", inA)
		want := [][]any{{"d00", "t0"}, {"d00", "u"}}
		if len(cols) != 2 || !reflect.DeepEqual(byName(t, cols, rows, "id", "t"), want) {
			t.Errorf("the columns %v and rows %v, want id and t, and %v", cols, rows, want)
		}
	})

	t.Run("group by", func(t *testing.T) {
		cols, rows, err := tryQuery(t, db, "SELECT c.grp, COUNT(1) AS n FROM c GROUP BY c.grp", inA)
		if err != nil {
			if !a.emulator {
				t.Skipf("the hosted gateway refuses GROUP BY, also for one partition key (not measured with a key): %v", err)
			}
			t.Fatal(err)
		}
		got := map[string]int64{}
		for _, r := range byName(t, cols, rows, "grp", "n") {
			got[fmt.Sprint(r[0])], _ = r[1].(int64)
		}
		// The partition a holds the documents 0, 3, 6, ... 27, whose grp is 0, 3, 2,
		// 1, 0, 3, 2, 1, 0, 3.
		if want := map[string]int64{"0": 3, "1": 2, "2": 2, "3": 3}; !reflect.DeepEqual(got, want) {
			t.Errorf("the groups are %v, want %v", got, want)
		}
	})

	t.Run("order by", func(t *testing.T) {
		got := ids(t, db, "SELECT c.id FROM c ORDER BY c.n DESC", inA)
		want := []string{"d27", "d24", "d21", "d18", "d15", "d12", "d09", "d06", "d03", "d00"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the order is %v, want %v", got, want)
		}
	})

	t.Run("aggregate", func(t *testing.T) {
		if got := scalar(t, db, "SELECT VALUE COUNT(1) FROM c", inA); got != int64(10) {
			t.Errorf("COUNT is %v (%T), want 10", got, got)
		}
		if got := scalar(t, db, "SELECT VALUE SUM(c.n) FROM c", inA); got != int64(135) {
			t.Errorf("SUM is %v (%T), want 135", got, got)
		}
		if got := scalar(t, db, "SELECT VALUE AVG(c.n) FROM c", inA); got != 13.5 {
			t.Errorf("AVG is %v (%T), want 13.5", got, got)
		}
		if got := scalar(t, db, "SELECT VALUE MAX(c.n) FROM c", inA); got != int64(27) {
			t.Errorf("MAX is %v (%T), want 27", got, got)
		}
	})

	t.Run("top", func(t *testing.T) {
		got := ids(t, db, "SELECT TOP 3 c.id FROM c ORDER BY c.n", inA)
		if want := []string{"d00", "d03", "d06"}; !reflect.DeepEqual(got, want) {
			t.Errorf("TOP 3 gave %v, want %v", got, want)
		}
	})

	t.Run("offset and limit", func(t *testing.T) {
		got := ids(t, db, "SELECT c.id FROM c ORDER BY c.n OFFSET 2 LIMIT 3", inA)
		if want := []string{"d06", "d09", "d12"}; !reflect.DeepEqual(got, want) {
			t.Errorf("OFFSET 2 LIMIT 3 gave %v, want %v", got, want)
		}
	})

	t.Run("distinct", func(t *testing.T) {
		_, rows := query(t, db, "SELECT DISTINCT VALUE c.grp FROM c", inA)
		var got []int64
		for _, r := range rows {
			n, _ := r[0].(int64)
			got = append(got, n)
		}
		slices.Sort(got)
		if want := []int64{0, 1, 2, 3}; !reflect.DeepEqual(got, want) {
			t.Errorf("DISTINCT gave %v, want %v", got, want)
		}
	})

	t.Run("subquery", func(t *testing.T) {
		cols, rows := query(t, db, "SELECT c.id, (SELECT VALUE COUNT(1) FROM t IN c.tags) AS tagcount FROM c WHERE c.id = 'd00'", inA)
		if want := [][]any{{"d00", int64(2)}}; !reflect.DeepEqual(byName(t, cols, rows, "id", "tagcount"), want) {
			t.Errorf("the rows are %v, want %v", rows, want)
		}
	})

	t.Run("parameters", func(t *testing.T) {
		got := ids(t, db, "SELECT c.id FROM c WHERE c.n = @n AND c.pk = @pk", sql.Named("n", 5), sql.Named("pk", "c"))
		if !reflect.DeepEqual(got, []string{"d05"}) {
			t.Errorf("the parameters n and pk gave %v, want d05", got)
		}
		got = ids(t, db, "SELECT c.id FROM c WHERE ARRAY_CONTAINS(@ids, c.id) ORDER BY c.id", sql.Named("ids", []any{"d01", "d02"}), cosmosKey("b"))
		if !reflect.DeepEqual(got, []string{"d01"}) {
			t.Errorf("a list as a parameter gave %v, want d01", got)
		}
		// An argument with no name has no placeholder to bind (D190).
		if _, _, err := tryQuery(t, db, "SELECT c.id FROM c WHERE c.n = @n", 5); err == nil {
			t.Error("a positional argument was bound, want an error")
		}
	})

	t.Run("transactional batch", func(t *testing.T) {
		a.container(t, "feat_batch", nil)
		batch := func(ops ...map[string]any) (int, []byte) {
			status, _, body := a.rest(t, http.MethodPost, collPath("feat_batch")+"/docs", merge(pk("b"), map[string]string{
				"X-Ms-Cosmos-Is-Batch-Request": "True",
				"X-Ms-Cosmos-Batch-Atomic":     "True",
			}), ops)
			return status, body
		}
		create := func(id string) map[string]any {
			return map[string]any{"operationType": "Create", "resourceBody": map[string]any{"id": id, "pk": "b"}}
		}
		if status, body := batch(create("b1"), create("b2")); status != http.StatusOK {
			t.Fatalf("a batch that succeeds: HTTP %d %s", status, body)
		}
		// The second operation conflicts, so no operation of the batch runs
		// (recorded: "a batch that fails in its second operation").
		status, body := batch(create("b3"), create("b1"))
		if status != http.StatusMultiStatus {
			t.Errorf("a batch that fails: HTTP %d %s, want %d", status, body, http.StatusMultiStatus)
		}
		got := ids(t, a.open(t, "feat_batch"), "SELECT c.id FROM c ORDER BY c.id", cosmosKey("b"))
		if want := []string{"b1", "b2"}; !reflect.DeepEqual(got, want) {
			t.Errorf("the documents after the failed batch are %v, want %v", got, want)
		}
	})

	t.Run("query metrics", func(t *testing.T) {
		_, hdr, body := a.rest(t, http.MethodPost, queryPath, queryHeaders(map[string]string{"X-Ms-Documentdb-Populatequerymetrics": "True"}), map[string]any{"query": "SELECT 1"})
		metrics := hdr.Get("X-Ms-Documentdb-Query-Metrics")
		if metrics == "" {
			if a.emulator {
				t.Skip("the emulator sends no query metrics")
			}
			t.Fatalf("the answer has no query metrics: %s", body)
		}
		if !strings.Contains(metrics, "totalExecutionTimeInMs=") {
			t.Errorf("the query metrics are %q, want a list of name=value pairs", metrics)
		}
	})

	t.Run("throttling with retry after", func(t *testing.T) {
		testThrottling(t)
	})

	t.Run("signature check", func(t *testing.T) {
		checkSignature(t, a)
	})

	t.Run("gzip response", func(t *testing.T) {
		// A request that accepts gzip gets a body that is not compressed, on both
		// servers (recorded: "a request that accepts gzip"). The request names the
		// encoding itself, so the transport does not decompress the answer.
		status, hdr, body := a.rest(t, http.MethodPost, queryPath, queryHeaders(map[string]string{"Accept-Encoding": "gzip"}), map[string]any{"query": "SELECT c.id FROM c WHERE c.id = 'd00'"})
		if status != http.StatusOK || hdr.Get("Content-Encoding") != "" {
			t.Errorf("HTTP %d with the encoding %q, want an answer that is not compressed: %s", status, hdr.Get("Content-Encoding"), body)
		}
	})
}

// checkSignature holds that the service checks the signature of a request, and
// that the emulator does not (recorded: "a request that the recorder sends with
// no signature"). A request with no signature is refused with HTTP 401 by the
// service. A wrong key is refused with HTTP 401 too, and a date that is out of
// the window with HTTP 403, as Microsoft says (not measured before this test).
func checkSignature(t *testing.T, a *account) {
	t.Helper()
	want := func(service int) int {
		if a.emulator {
			return http.StatusOK
		}
		return service
	}
	ctx := t.Context()
	status, _, body, err := cosmos.RawWith(ctx, a.cfg, http.MethodGet, "/dbs", nil, nil, true, time.Time{})
	if err != nil || status != want(http.StatusUnauthorized) {
		t.Errorf("a request with no signature: HTTP %d %s (%v), want %d", status, body, err, want(http.StatusUnauthorized))
	}
	wrong := a.cfg
	wrong.Key = "AAAA"
	status, _, body, err = cosmos.Raw(ctx, wrong, http.MethodGet, "/dbs", nil, nil)
	if err != nil || status != want(http.StatusUnauthorized) {
		t.Errorf("a request with a wrong key: HTTP %d %s (%v), want %d", status, body, err, want(http.StatusUnauthorized))
	}
	late := time.Now().Add(-20 * time.Minute)
	status, _, body, err = cosmos.RawWith(ctx, a.cfg, http.MethodGet, "/dbs", nil, nil, false, late)
	if err != nil || status != want(http.StatusForbidden) {
		t.Errorf("a request 20 minutes late: HTTP %d %s (%v), want %d", status, body, err, want(http.StatusForbidden))
	}
}
