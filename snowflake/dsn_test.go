package snowflake_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/snowflake"
)

// key is a private key that the tests make with crypto/rsa, as the text that a
// DSN holds: the base64url text of the PKCS8 DER bytes (D183).
var key = sync.OnceValue(func() string {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("making the key of the tests: " + err.Error())
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		panic("writing the key of the tests: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(der)
})

const accountHost = "org-acct.snowflakecomputing.com"

// cfg returns the configuration of a DSN with the host, the user alice and the
// key.
func cfg() snowflake.Config {
	return snowflake.Config{Host: accountHost, Port: 443, User: "alice", Password: key()}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(f func(*snowflake.Config)) snowflake.Config {
		c := cfg()
		f(&c)
		return c
	}
	k := key()
	for _, tt := range []struct {
		dsn  string
		want snowflake.Config
	}{
		{"snowflake://alice:" + k + "@" + accountHost, cfg()},
		{"snowflake://alice:" + k + "@" + accountHost + "/", cfg()},
		{"snowflake://alice:" + k + "@" + accountHost + ":443", cfg()},
		{"snowflake://alice:" + k + "@ORG-ACCT.SnowflakeComputing.com", with(func(c *snowflake.Config) { c.Host = "ORG-ACCT.SnowflakeComputing.com" })},
		{"snowflake://alice:" + k + "@" + accountHost + "/db", with(func(c *snowflake.Config) { c.Database = "db" })},
		{"snowflake://alice:" + k + "@" + accountHost + "/db/", with(func(c *snowflake.Config) { c.Database = "db" })},
		{"snowflake://alice:" + k + "@" + accountHost + "/db/public", with(func(c *snowflake.Config) { c.Database, c.Schema = "db", "public" })},
		{"snowflake://alice:" + k + "@" + accountHost + "//public", with(func(c *snowflake.Config) { c.Schema = "public" })},
		{"snowflake://alice:" + k + "@" + accountHost + "/a%20b/c%2Fd", with(func(c *snowflake.Config) { c.Database, c.Schema = "a b", "c/d" })},
		{"snowflake://alice:" + k + "@" + accountHost + "?role=ANALYST&warehouse=WH&timeout=60s&timezone=Asia%2FJakarta", with(func(c *snowflake.Config) {
			c.Role, c.Warehouse, c.Timeout, c.TimeZone = "ANALYST", "WH", 60*time.Second, "Asia/Jakarta"
		})},
		{"snowflake://alice:" + k + "@" + accountHost + "?timeout=0", cfg()},
		{"snowflake://alice:" + k + "@" + accountHost + "?timeout=1m", with(func(c *snowflake.Config) { c.Timeout = time.Minute })},
		{"snowflake://alice:" + k + "@" + accountHost + "?timeout=1500ms", with(func(c *snowflake.Config) { c.Timeout = 1500 * time.Millisecond })},
		{"snowflake://alice:" + k + "@" + accountHost + "?timezone=Local", with(func(c *snowflake.Config) { c.TimeZone = "Local" })},
		// A DSN with a port or an address is for the fake server of a test.
		{"snowflake://alice:" + k + "@localhost:8443", with(func(c *snowflake.Config) { c.Host, c.Port = "localhost", 8443 })},
		{"snowflake://alice:" + k + "@127.0.0.1", with(func(c *snowflake.Config) { c.Host = "127.0.0.1" })},
		{"snowflake://alice:" + k + "@[::1]:8443/db", with(func(c *snowflake.Config) { c.Host, c.Port, c.Database = "::1", 8443, "db" })},
		{"snowflake://alice:" + k + "@[::1]", with(func(c *snowflake.Config) { c.Host = "::1" })},
	} {
		got, err := snowflake.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", shorten(tt.dsn), err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", shorten(tt.dsn), *got, tt.want)
		}
		again, err := snowflake.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", shorten(tt.dsn), shorten(got.FormatDSN()), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", shorten(tt.dsn), *again, *got)
		}
	}
}

// shorten cuts the key out of a text, so that a failure of a test prints no
// key.
func shorten(s string) string {
	return strings.ReplaceAll(s, key(), "KEY")
}

// TestParseDSNRefuses holds D183: a key other than the four is refused, and so
// is a repeated key, a scheme that is not snowflake, no host, no user, no key,
// a key that is not a private key, a host that is not an account host, and a
// path with more than a database and a schema.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	k := key()
	base := "snowflake://alice:" + k + "@" + accountHost
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"snowflakes://alice:" + k + "@" + accountHost, dbimp.ErrScheme},
		{"https://alice:" + k + "@" + accountHost, dbimp.ErrScheme},
		{"snowflake://", dbimp.ErrInvalidValue},
		{"snowflake://alice:" + k + "@", dbimp.ErrInvalidValue},
		{"snowflake://" + accountHost, dbimp.ErrInvalidValue},
		{"snowflake://:" + k + "@" + accountHost, dbimp.ErrInvalidValue},
		{"snowflake://alice@" + accountHost, dbimp.ErrInvalidValue},
		{"snowflake://alice:@" + accountHost, dbimp.ErrInvalidValue},
		{"snowflake://alice:notakey@" + accountHost, dbimp.ErrInvalidValue},
		{"snowflake://alice:" + base64.RawURLEncoding.EncodeToString([]byte("not DER")) + "@" + accountHost, dbimp.ErrInvalidValue},
		{"snowflake://alice:" + k + "@example.com", dbimp.ErrInvalidValue},
		{"snowflake://alice:" + k + "@snowflakecomputing.com", dbimp.ErrInvalidValue},
		{"snowflake://alice:" + k + "@" + accountHost + ":0", dbimp.ErrInvalidValue},
		{"snowflake://alice:" + k + "@" + accountHost + ":65536", dbimp.ErrInvalidValue},
		{base + "/a/b/c", dbimp.ErrInvalidValue},
		{base + "/a//", dbimp.ErrInvalidValue},
		// An error of net/url has no sentinel.
		{base + "/a/%zz", nil},
		{base + "?tls=true", dbimp.ErrUnknownKey},
		{base + "?database=db", dbimp.ErrUnknownKey},
		{base + "?auth=bearer", dbimp.ErrUnknownKey},
		{base + "?privateKey=x", dbimp.ErrUnknownKey},
		{base + "?role=a&role=b", dbimp.ErrRepeatedKey},
		{base + "?role=", dbimp.ErrInvalidValue},
		{base + "?warehouse=", dbimp.ErrInvalidValue},
		{base + "?timezone=", dbimp.ErrInvalidValue},
		{base + "?timeout=-1", dbimp.ErrInvalidValue},
		{base + "?timeout=1.5", dbimp.ErrInvalidValue},
		{base + "?timeout=60", dbimp.ErrInvalidValue},
		{base + "?timeout=-1s", dbimp.ErrInvalidValue},
		{base + "?timeout=9999999999s", dbimp.ErrInvalidValue},
		{"snowflake://alice:" + k + "@::", dbimp.ErrInvalidValue},
	} {
		_, err := snowflake.ParseDSN(tt.dsn)
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want an error that wraps %v", shorten(tt.dsn), err, tt.want)
		}
	}
}

// TestTimeoutNamesTheForm holds that a bare number is refused with a message
// that names the form of the value.
func TestTimeoutNamesTheForm(t *testing.T) {
	t.Parallel()
	_, err := snowflake.ParseDSN("snowflake://alice:" + key() + "@" + accountHost + "?timeout=60")
	if err == nil || !strings.Contains(err.Error(), "60s") {
		t.Errorf("ParseDSN with timeout=60 gave %v, want an error that names a duration such as 60s", err)
	}
}

// TestParseDSNKeepsTheSecretOut holds that an error never holds the key (D94).
func TestParseDSNKeepsTheSecretOut(t *testing.T) {
	t.Parallel()
	k := key()
	half := k[:len(k)/2]
	for _, dsn := range []string{
		"snowflake://alice:" + k + "@" + accountHost + "?bogus=1",
		"snowflakes://alice:" + k + "@" + accountHost,
		"snowflake://alice:" + k + "@" + accountHost + ":99999",
		"snowflake://alice:" + k + "@" + accountHost + "/a/b/c",
		"snowflake://alice:" + k + "@%zz",
		"snowflake://alice:" + k + "@example.com",
		"snowflake://alice:" + half + "@" + accountHost,
		"snowflake://alice:" + k + k + "@" + accountHost,
		"snowflake://alice:" + k + "@" + accountHost + "?timeout=x",
	} {
		_, err := snowflake.ParseDSN(dsn)
		if err == nil {
			t.Errorf("ParseDSN accepted %q", shorten(dsn))
			continue
		}
		if strings.Contains(err.Error(), half) || strings.Contains(err.Error(), k[:40]) {
			t.Errorf("ParseDSN(%q) gave an error that holds the key: %v", shorten(dsn), err)
		}
	}
}

// FuzzParseDSN holds that the parser never panics, and that every DSN that it
// accepts formats to a DSN that parses back to the same configuration.
func FuzzParseDSN(f *testing.F) {
	k := key()
	for _, dsn := range []string{
		"snowflake://alice:" + k + "@" + accountHost,
		"snowflake://alice:" + k + "@" + accountHost + "/db/public?role=r&warehouse=w&timeout=60s&timezone=UTC",
		"snowflake://alice:" + k + "@" + accountHost + "/a%20b/c%2Fd",
		"snowflake://alice:" + k + "@" + accountHost + "//s",
		"snowflake://alice:" + k + "@[::1]:8443/db",
		"snowflake://alice:" + k + "@localhost:1",
		"snowflake://alice:" + k + "@127.0.0.1",
		"snowflake://u@::",
		"snowflake://u@:",
		"snowflake://@h",
		"snowflake://h/%zz",
		"snowflake://h//",
		"snowflake://alice:" + k + "@" + accountHost + "?role=a&role=a",
	} {
		f.Add(dsn)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		c, err := snowflake.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := snowflake.ParseDSN(c.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(FormatDSN(%q)) = %q: %v", shorten(dsn), shorten(c.FormatDSN()), err)
		}
		if !reflect.DeepEqual(again, c) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", shorten(dsn), *again, *c)
		}
	})
}
