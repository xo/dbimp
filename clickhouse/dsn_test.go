package clickhouse_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/clickhouse"
)

// cfg returns the configuration of a DSN with the host and the port.
func cfg(host string, port int) clickhouse.Config {
	return clickhouse.Config{Host: host, Port: port}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c clickhouse.Config, f func(*clickhouse.Config)) clickhouse.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want clickhouse.Config
	}{
		{"clickhouse://localhost", cfg("localhost", 8123)},
		{"clickhouse://localhost/", cfg("localhost", 8123)},
		{"clickhouse://default:P4ssw0rd%21x@127.0.0.1:56034/default", with(cfg("127.0.0.1", 56034), func(c *clickhouse.Config) {
			c.User, c.Password, c.Database = "default", "P4ssw0rd!x", "default"
		})},
		{"clickhouse://u@h:9000", with(cfg("h", 9000), func(c *clickhouse.Config) { c.User = "u" })},
		{"clickhouse://:p@h", with(cfg("h", 8123), func(c *clickhouse.Config) { c.Password = "p" })},
		// The port is 8443 with tls=true and no port (D177).
		{"clickhouse://h?tls=true", with(cfg("h", 8443), func(c *clickhouse.Config) { c.TLS = true })},
		{"clickhouse://h:8123?tls=true", with(cfg("h", 8123), func(c *clickhouse.Config) { c.TLS = true })},
		{"clickhouse://h?tls=false", cfg("h", 8123)},
		{"clickhouse://u:p@[::1]:8443/db?tls=true", with(cfg("::1", 8443), func(c *clickhouse.Config) {
			c.User, c.Password, c.Database, c.TLS = "u", "p", "db", true
		})},
		{"clickhouse://h/a%20b", with(cfg("h", 8123), func(c *clickhouse.Config) { c.Database = "a b" })},
		{"clickhouse://h/a%2Fb", with(cfg("h", 8123), func(c *clickhouse.Config) { c.Database = "a/b" })},
		{"clickhouse://h/db/", with(cfg("h", 8123), func(c *clickhouse.Config) { c.Database = "db" })},
	} {
		got, err := clickhouse.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := clickhouse.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D177: a key other than tls is refused, and so is a
// repeated key, a scheme that is not clickhouse, no host, and a path with more
// than a database.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"clickhouses://localhost", dbimp.ErrScheme},
		{"http://localhost:8123", dbimp.ErrScheme},
		{"tcp://localhost:9000", dbimp.ErrScheme},
		{"clickhouse://", dbimp.ErrInvalidValue},
		{"clickhouse://u@", dbimp.ErrInvalidValue},
		{"clickhouse://localhost/a/b", dbimp.ErrInvalidValue},
		{"clickhouse://localhost//b", dbimp.ErrInvalidValue},
		{"clickhouse://localhost?secure=true", dbimp.ErrUnknownKey},
		{"clickhouse://localhost?database=default", dbimp.ErrUnknownKey},
		{"clickhouse://localhost?password=x", dbimp.ErrUnknownKey},
		{"clickhouse://localhost?timeout=1s", dbimp.ErrUnknownKey},
		{"clickhouse://localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"clickhouse://localhost?tls=yes", dbimp.ErrInvalidValue},
		{"clickhouse://localhost?tls=", dbimp.ErrInvalidValue},
		{"clickhouse://localhost:0", dbimp.ErrInvalidValue},
		{"clickhouse://localhost:65536", dbimp.ErrInvalidValue},
		{"clickhouse://localhost:http", nil},
		{"clickhouse://::", dbimp.ErrInvalidValue},
	} {
		_, err := clickhouse.ParseDSN(tt.dsn)
		// A nil want is an error of net/url that has no sentinel.
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want an error that wraps %v", tt.dsn, err, tt.want)
		}
	}
}

// TestParseDSNKeepsTheSecretOut holds that an error never holds the password
// (D94).
func TestParseDSNKeepsTheSecretOut(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{
		"clickhouse://u:s3cret@localhost?bogus=1",
		"clickhouses://u:s3cret@localhost",
		"clickhouse://u:s3cret@localhost:99999",
		"clickhouse://u:s3cret@localhost/a/b",
		"clickhouse://u:s3cret@%zz",
		"clickhouse://u:s3cret@localhost?tls=maybe",
	} {
		if _, err := clickhouse.ParseDSN(dsn); err == nil || strings.Contains(err.Error(), "s3cret") {
			t.Errorf("ParseDSN(%q) gave the error %v, want one with no password", dsn, err)
		}
	}
}

// FuzzParseDSN holds that the parser never panics, and that every DSN that it
// accepts formats to a DSN that parses back to the same configuration.
func FuzzParseDSN(f *testing.F) {
	for _, dsn := range []string{
		"clickhouse://localhost",
		"clickhouse://default:P4ssw0rd%21x@127.0.0.1:56034/default",
		"clickhouse://u:p@[::1]:8443/db?tls=true",
		"clickhouse://h/a%20b",
		"clickhouse://h/a%2Fb",
		"clickhouse://u@::",
		"clickhouse://u@:",
		"clickhouse://h?tls=true&tls=true",
		"clickhouse://@h",
		"clickhouse://h/%zz",
		"clickhouse://h//",
	} {
		f.Add(dsn)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		c, err := clickhouse.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := clickhouse.ParseDSN(c.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(FormatDSN(%q)) = %q: %v", dsn, c.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, c) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *c)
		}
	})
}
