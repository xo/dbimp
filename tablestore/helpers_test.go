package tablestore //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded from Tablestore, under
// testdata/tablestore/. Each one decodes a real answer through the driver. A
// test never holds an answer that it wrote as a string, except the answers of
// the contract and of the fuzz test, which have the form of an answer.

// testdata is the folder of the recordings.
const testdata = "../testdata/tablestore"

// testConfig returns the configuration of the tests: the instance of the
// recording and a fake key pair.
func testConfig() Config {
	return Config{
		Host:     "dbmeta.ap-southeast-5.ots.aliyuncs.com",
		TLS:      true,
		Instance: "dbmeta",
		User:     "key",
		Password: "secret",
	}
}

// connectorAt returns a connector for cfg that talks to the fake server at
// rawurl, which is an http:// URL.
func connectorAt(tb testing.TB, cfg Config, rawurl string) *Connector {
	tb.Helper()
	u, err := url.Parse(rawurl)
	if err != nil {
		tb.Fatal(err)
	}
	cfg.TLS = false
	cfg.Host = u.Hostname()
	if p := u.Port(); p != "" {
		cfg.Port, err = strconv.Atoi(p)
		if err != nil {
			tb.Fatal(err)
		}
	}
	c := NewConnector(cfg)
	c.now = func() time.Time { return time.Date(2026, 10, 29, 7, 52, 17, 0, time.UTC) }
	return c
}

// open returns a database on the fake server at rawurl, with the connector of
// cfg.
func open(tb testing.TB, cfg Config, rawurl string) *sql.DB {
	tb.Helper()
	c := connectorAt(tb, cfg, rawurl)
	db := sql.OpenDB(c)
	tb.Cleanup(func() {
		db.Close()
		_ = c.Close()
	})
	return db
}

// replay returns a database on a fake server that answers each request with the
// recorded exchange whose request is the same.
func replay(t *testing.T) *sql.DB {
	t.Helper()
	srv := dbimptest.Replay(t, testdata, nil)
	return open(t, testConfig(), srv.URL)
}

// exchange returns the recorded exchange with the number n.
func exchange(tb testing.TB, n int) *dbimptest.Exchange {
	tb.Helper()
	paths, err := filepath.Glob(fmt.Sprintf("%s/tablestore-%03d-*.json", testdata, n))
	if err != nil || len(paths) != 1 {
		tb.Fatalf("finding the recorded exchange %d: %v, %d files", n, err, len(paths))
	}
	ex, err := dbimptest.ReadExchange(paths[0])
	if err != nil {
		tb.Fatal(err)
	}
	return ex
}
