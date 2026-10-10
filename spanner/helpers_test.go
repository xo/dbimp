package spanner //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json/v2"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The names of the recorded exchanges, so that the paths of the fake servers
// match the recorded ones (docs/SPANNER.md). The project id in the recordings is
// the redacted name dbimp-project.
const (
	testProject  = "dbimp-project"
	testInstance = "dbimp-spanner"
	testDatabase = "dbimp_test"
)

// testDB is the full name of the database of the tests.
const testDB = "projects/" + testProject + "/instances/" + testInstance + "/databases/" + testDatabase

// testSession is the multiplexed session that the connector of a test holds, when
// the test does not make one with a fake server.
const testSession = testDB + "/sessions/test-session"

// keys is the key of the tests. A test never holds a key that came from a server
// or from a person: this one is made here, with crypto/rsa.
var keys = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("making the key of the tests: " + err.Error())
	}
	return key
})

// keyText returns the private key of the tests as the PEM text that a key file
// holds.
func keyText() string {
	der, err := x509.MarshalPKCS8PrivateKey(keys())
	if err != nil {
		panic("writing the key of the tests: " + err.Error())
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// writeKeyFile writes a key file of a service account in a folder of the test,
// whose token endpoint is tokenURI, and returns its path.
func writeKeyFile(tb testing.TB, tokenURI string) string {
	tb.Helper()
	text, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"client_email": "test@dbimp-project.iam.gserviceaccount.com",
		"private_key":  keyText(),
		"token_uri":    tokenURI,
	})
	if err != nil {
		tb.Fatal(err)
	}
	path := filepath.Join(tb.TempDir(), "key.json")
	if err := os.WriteFile(path, text, 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

// config returns the configuration of the tests, with no credential, as the DSN
// of the emulator gives it.
func config() Config {
	return Config{
		Host:     "127.0.0.1",
		Project:  testProject,
		Instance: testInstance,
		Database: testDatabase,
	}
}

// fastPoll is the interval of the poll of the tests, so that no test waits a
// fixed time.
const fastPoll = time.Millisecond

// connector returns a connector for cfg that talks to the fake server at base. A
// connector with a session sends no request before its first statement, which
// the tests of the contract need, because their fake servers answer every
// request with one body.
func connector(cfg Config, base string, withSession bool) *Connector {
	c := NewConnector(cfg)
	c.base = base
	c.pollMin, c.pollMax = fastPoll, 4*fastPoll
	if withSession {
		c.sessions[cfg.target()] = cfg.databaseName(cfg.Database) + "/sessions/test-session"
	}
	return c
}

// open returns a database on the fake server at base, with the connector of
// cfg.
func open(t *testing.T, cfg Config, base string, withSession bool) *sql.DB {
	t.Helper()
	c := connector(cfg, base, withSession)
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		db.Close()
		_ = c.Close()
	})
	return db
}
