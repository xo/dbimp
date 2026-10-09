package snowflake //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"sync"
	"testing"
	"time"
)

// testHost is the host of the account of the tests. It is written as an
// account host, so that the token holds the account that docs/SNOWFLAKE.md names.
const testHost = "org-acct.snowflakecomputing.com"

// testUser is the user of the tests, as the claims of a token hold it.
const testUser = "alice"

// keys is the key of the tests. A test never holds a key that came from a
// server or from a person: this one is made here, with crypto/rsa.
var keys = sync.OnceValues(func() (*rsa.PrivateKey, string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("making the key of the tests: " + err.Error())
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic("writing the key of the tests: " + err.Error())
	}
	return key, base64.RawURLEncoding.EncodeToString(der)
})

// testKey returns the private key of the tests, and its text as a DSN holds it.
func testKey() (*rsa.PrivateKey, string) {
	return keys()
}

// config returns the configuration of the tests.
func config() Config {
	_, text := testKey()
	return Config{Host: testHost, User: testUser, Password: text}
}

// fastPoll is the interval of the poll of the tests, so that no test waits a
// fixed time.
const fastPoll = time.Millisecond

// connector returns a connector for cfg that talks to the fake server at base
// and never sends a cancel unless stop is true. The tests of the contract use
// noStop, because their fake servers answer every request with a result.
func connector(cfg Config, base string, stop bool) *Connector {
	c := NewConnector(cfg)
	c.base = base
	c.noStop = !stop
	c.pollMin, c.pollMax = fastPoll, 4*fastPoll
	return c
}

// open returns a database on the fake server at base, with the connector of
// cfg.
func open(t *testing.T, cfg Config, base string, stop bool) *sql.DB {
	t.Helper()
	c := connector(cfg, base, stop)
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		db.Close()
		_ = c.Close()
	})
	return db
}
