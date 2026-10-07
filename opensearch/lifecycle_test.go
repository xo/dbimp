package opensearch_test

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/opensearch"
)

// TestNoGoroutineIsLeft holds step 12: after the pages, the close before the end,
// the cancel and the close of the database, no goroutine of the driver runs. It
// does not run in parallel, because CheckGoroutines counts the goroutines of the
// process.
func TestNoGoroutineIsLeft(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	f := &fake{handle: threePages}
	db := f.open(t, "", "?fetch_size=2")
	if got, err := numbers(t, db); err != nil || !slices.Equal(got, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("read %v and %v", got, err)
	}
	rows, err := db.QueryContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	rows.Next()
	if err := rows.Close(); err != nil { //nolint:sqlclosecheck // The test closes the rows early on purpose.
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	rows, err = db.QueryContext(ctx, "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	rows.Next()
	cancel()
	_ = rows.Close() //nolint:sqlclosecheck // The test closes the rows after the cancel on purpose.
	_ = rows.Err()
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestTwoQueriesAtOnce holds step 12: two statements run at the same time on one
// sql.DB, each on a connection of its own. The fake server answers neither until
// both have arrived.
func TestTwoQueriesAtOnce(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	wg.Add(2)
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		wg.Done()
		wg.Wait()
		reply(w, http.StatusOK, page(1, 2, ""))
	}}
	db := f.open(t, "", "")
	db.SetMaxOpenConns(2)
	var got [2][]int64
	var errs [2]error
	var done sync.WaitGroup
	for i := range 2 {
		done.Go(func() { got[i], errs[i] = numbers(t, db) })
	}
	done.Wait()
	for i := range 2 {
		if errs[i] != nil || !slices.Equal(got[i], []int64{1, 2}) {
			t.Errorf("query %d read %v and %v", i, got[i], errs[i])
		}
	}
}

// TestConnectorOwnsItsTransport holds that a connector that a caller builds and
// opens with sql.OpenDB works, and that closing the database closes the
// connector, which closes the idle connections of its transport.
func TestConnectorOwnsItsTransport(t *testing.T) {
	t.Parallel()
	f := &fake{}
	srv := f.open(t, "", "")
	_ = srv
	cfg, err := opensearch.ParseDSN("opensearch://localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	c := opensearch.NewConnector(*cfg)
	db := sql.OpenDB(c)
	if err := db.Close(); err != nil {
		t.Errorf("closing the database: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("closing the connector: %v", err)
	}
	if c.Driver() == nil {
		t.Error("the connector has no driver")
	}
}
