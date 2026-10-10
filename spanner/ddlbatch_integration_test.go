package spanner_test

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xo/dbimp/spanner"
)

// countingDB returns a database that reaches the server of the DSN through a
// proxy on the loopback address, and a counter of the DDL requests that went
// through it. The proxy speaks plain HTTP to the driver, and HTTPS to the
// service when the DSN has TLS. The driver sends its token to the proxy, as it
// would to the service, and the proxy hands it on.
func countingDB(t *testing.T) (*sql.DB, *atomic.Int32) {
	t.Helper()
	cfg, err := spanner.ParseDSN(dsn(t))
	if err != nil {
		t.Fatalf("reading %s: %v", envDSN, err)
	}
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	upstream := &url.URL{Scheme: scheme, Host: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))}
	proxy := &httputil.ReverseProxy{}
	proxy.Rewrite = func(r *httputil.ProxyRequest) {
		r.SetURL(upstream)
		r.Out.Host = upstream.Host
	}
	var ddls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/ddl") {
			ddls.Add(1)
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	local := *cfg
	local.Host, local.Port, local.TLS = u.Hostname(), port, false
	db := sql.OpenDB(spanner.NewConnector(local))
	t.Cleanup(func() { db.Close() })
	return db, &ddls
}

// TestIntegrationDDLBatch holds D198: one Exec with several DDL statements makes
// them all, in one updateDatabaseDdl request and one operation. The test counts
// the DDL requests at a proxy, reads the objects back from INFORMATION_SCHEMA, and
// drops them in one batch too.
func TestIntegrationDDLBatch(t *testing.T) {
	db, ddls := countingDB(t)
	parent, child, plain, idx := name("batch_parent"), name("batch_child"), name("batch_plain"), name("batch_idx")
	// Clean up whatever the test made, also when it fails half way, one object
	// at a time, because a drop of an object that is missing fails a batch.
	t.Cleanup(func() {
		for _, drop := range []string{"DROP INDEX IF EXISTS " + idx, "DROP TABLE IF EXISTS " + child, "DROP TABLE IF EXISTS " + parent, "DROP TABLE IF EXISTS " + plain} {
			if _, err := db.ExecContext(context.WithoutCancel(t.Context()), drop); err != nil {
				t.Errorf("cleaning up: %s: %v", drop, err)
			}
		}
	})
	create := "CREATE TABLE " + parent + " (id INT64 NOT NULL, note STRING(MAX) DEFAULT ('a;b')) PRIMARY KEY (id);\n" +
		"-- a comment with a semicolon; it splits nothing\n" +
		"CREATE TABLE " + child + " (id INT64 NOT NULL, parent_id INT64 NOT NULL) PRIMARY KEY (id, parent_id);\n" +
		"CREATE TABLE " + plain + " (id INT64 NOT NULL) PRIMARY KEY (id);;\n" +
		"CREATE INDEX " + idx + " ON " + parent + " (note);\n"
	start := time.Now()
	exec(t, db, create)
	took := time.Since(start)
	if n := ddls.Load(); n != 1 {
		t.Errorf("the batch of 4 statements sent %d updateDatabaseDdl requests, want 1", n)
	}
	t.Logf("the batch of 4 statements made 3 tables and 1 index in %d updateDatabaseDdl request and took %s", ddls.Load(), took.Round(time.Millisecond))
	tables := rowsOf(t, db, "SELECT table_name FROM information_schema.tables WHERE table_schema = '' AND STARTS_WITH(table_name, @p) ORDER BY table_name", sql.Named("p", name("batch_")))
	if len(tables) != 3 {
		t.Errorf("the batch made the tables %v, want 3", tables)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM information_schema.indexes WHERE table_schema = '' AND index_name = @p", sql.Named("p", idx)); n != 1 {
		t.Errorf("the batch made %d indexes named %s, want 1", n, idx)
	}
	// The default holds the semicolon, so the split did not cut the statement.
	if n := count(t, db, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = '' AND table_name = @p AND column_name = 'note' AND column_default LIKE '%a;b%'", sql.Named("p", parent)); n != 1 {
		t.Errorf("the default of the column is not 'a;b' (%d rows)", n)
	}

	before := ddls.Load()
	start = time.Now()
	exec(t, db, "DROP INDEX "+idx+"; DROP TABLE "+child+"; DROP TABLE "+parent+"; DROP TABLE "+plain)
	took = time.Since(start)
	if n := ddls.Load() - before; n != 1 {
		t.Errorf("the batch of 4 drops sent %d updateDatabaseDdl requests, want 1", n)
	}
	t.Logf("the batch of 4 drops took %s", took.Round(time.Millisecond))
	if n := len(rowsOf(t, db, "SELECT table_name FROM information_schema.tables WHERE table_schema = '' AND STARTS_WITH(table_name, @p)", sql.Named("p", name("batch_")))); n != 0 {
		t.Errorf("%d tables are left after the drops", n)
	}
}
