package cosmos_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xo/dbimp/cosmos"
)

// The tests of this file need a hosted account, and read COSMOS_HOSTED_DSN. The
// emulator answers each query across partitions, and it throttles nothing, so
// it cannot show the refusal of the gateway or HTTP 429 (D190 item 13).

// TestIntegrationHostedCrossPartition holds D190: the driver sends a query as it
// is, and the hosted gateway refuses an aggregate, TOP, ORDER BY, OFFSET LIMIT
// and DISTINCT across partitions with HTTP 400 and the substatus 1004, which
// reaches the caller as an *Error. The driver plans and merges nothing, so the
// caller names the partition key, and the same query with a key works
// (recorded: "the count of the large container", "a query for one partition
// key").
func TestIntegrationHostedCrossPartition(t *testing.T) {
	a := getAccount(t, envHosted)
	a.container(t, "hosted_x", nil)
	a.seed(t, "hosted_x", featureDocs())
	db := a.open(t, "hosted_x")
	for _, statement := range []string{
		"SELECT VALUE COUNT(1) FROM c",
		"SELECT TOP 3 c.id FROM c",
		"SELECT c.id FROM c ORDER BY c.n",
		"SELECT c.id FROM c ORDER BY c.n OFFSET 1 LIMIT 2",
		"SELECT DISTINCT VALUE c.grp FROM c",
	} {
		_, _, err := tryQuery(t, db, statement)
		cerr, ok := errors.AsType[*cosmos.Error](err)
		if !ok || cerr.HTTPStatus != http.StatusBadRequest || cerr.SubStatus != 1004 {
			t.Errorf("%s: the error is %v, want an *Error of HTTP 400 and substatus 1004", statement, err)
			continue
		}
		if !strings.Contains(cerr.Message, "cross partition query") {
			t.Errorf("%s: the message is %q, want it to name the cross partition query", statement, cerr.Message)
		}
		if _, _, err := tryQuery(t, db, statement, cosmosKey("a")); err != nil {
			t.Errorf("%s with a partition key: %v", statement, err)
		}
	}
	// An aggregate with no VALUE, and a GROUP BY, are another refusal, with no
	// substatus (recorded: "an aggregate of every kind", "a group by").
	for _, statement := range []string{
		"SELECT COUNT(1) AS n FROM c",
		"SELECT c.grp, COUNT(1) AS n FROM c GROUP BY c.grp",
	} {
		_, _, err := tryQuery(t, db, statement)
		if cerr, ok := errors.AsType[*cosmos.Error](err); !ok || cerr.HTTPStatus != http.StatusBadRequest {
			t.Errorf("%s: the error is %v, want an *Error of HTTP 400", statement, err)
		}
	}
	// A plain SELECT across partitions works.
	if got := ids(t, db, "SELECT c.id FROM c"); len(got) != 30 {
		t.Errorf("a plain query across partitions gave %d rows, want 30", len(got))
	}
}

// testThrottling holds that the hosted account answers HTTP 429 to a burst of
// reads, that the driver sends each request once and returns the answer as an
// *Error with the wait in RetryAfter, and that the same read works after the
// wait. The container is small, with 400 request units a second shared by the
// database (recorded: "lead: a burst of reads, number 1").
func testThrottling(t *testing.T) {
	t.Helper()
	a := getAccount(t, envHosted)
	const (
		docs   = 600
		reads  = 60
		coll   = "throttle"
		burstQ = "SELECT * FROM c"
	)
	a.container(t, coll, nil)
	rows := make([]map[string]any, docs)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("r%04d", i), "pk": fmt.Sprintf("p%d", i%6), "n": i, "txt": strings.Repeat("x", 200)}
	}
	a.seed(t, coll, rows)

	cfg := a.cfg
	cfg.Database, cfg.Container = databaseName, coll
	conn := cosmos.NewConnector(cfg)
	var sent atomic.Int64
	conn.WrapTransport(func(rt http.RoundTripper) http.RoundTripper { return countTransport{next: rt, n: &sent} })
	db := sql.OpenDB(conn)
	defer db.Close()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		refused []*cosmos.Error
	)
	for range reads {
		wg.Go(func() {
			_, _, err := scanAll(t.Context(), db, burstQ, cosmos.WithPageSize(5000))
			if cerr, ok := errors.AsType[*cosmos.Error](err); ok && cerr.HTTPStatus == http.StatusTooManyRequests {
				mu.Lock()
				refused = append(refused, cerr)
				mu.Unlock()
			} else if err != nil {
				t.Errorf("a read of the burst failed with %v, want it to work or to be refused with HTTP 429", err)
			}
		})
	}
	wg.Wait()
	if n := sent.Load(); n != reads {
		t.Errorf("the driver sent %d requests for %d reads, want one for each, because it never sends a request again (D8)", n, reads)
	}
	if len(refused) == 0 {
		t.Fatalf("no read of %d at once got HTTP 429, so the test cannot show the answer", reads)
	}
	for _, e := range refused {
		if e.SubStatus != 3200 || e.RetryAfter <= 0 {
			t.Errorf("the refusal is %+v, want the substatus 3200 and a wait", *e)
		}
	}
	// The request did not run, so it can run again after the wait, and it works.
	time.Sleep(refused[0].RetryAfter)
	_, got, err := tryQuery(t, db, burstQ, cosmos.WithPageSize(5000))
	if err != nil || len(got) != docs {
		t.Errorf("the read after the wait gave %d rows and %v, want %d rows", len(got), err, docs)
	}
}
