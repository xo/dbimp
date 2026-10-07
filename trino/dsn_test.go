package trino_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/trino"
)

// cfg returns the configuration of a DSN with every default, and the host, the
// port and the user.
func cfg(host string, port int, user string) trino.Config {
	return trino.Config{Host: host, Port: port, User: user, Source: "dbimp"}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c trino.Config, f func(*trino.Config)) trino.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want trino.Config
	}{
		{"trino://u@localhost", cfg("localhost", 8080, "u")},
		{"trino://u@localhost/", cfg("localhost", 8080, "u")},
		{"trino://trino@127.0.0.1:55037/memory/default", with(cfg("127.0.0.1", 55037, "trino"), func(c *trino.Config) {
			c.Catalog, c.Schema = "memory", "default"
		})},
		{"trino://u@h/memory", with(cfg("h", 8080, "u"), func(c *trino.Config) { c.Catalog = "memory" })},
		{"trino://u:P4ss%21x%40y@h:9000", with(cfg("h", 9000, "u"), func(c *trino.Config) { c.Password = "P4ss!x@y" })},
		// The port is 8443 with tls=true and no port (D175).
		{"trino://u@h?tls=true", with(cfg("h", 8443, "u"), func(c *trino.Config) { c.TLS = true })},
		{"trino://u@h:8080?tls=true", with(cfg("h", 8080, "u"), func(c *trino.Config) { c.TLS = true })},
		{"trino://u@[::1]:8080/c/s?flavor=presto&source=app&timezone=Asia%2FJakarta&timeout=1.5s", with(cfg("::1", 8080, "u"), func(c *trino.Config) {
			c.Catalog, c.Schema, c.Flavor, c.Source, c.TimeZone, c.Timeout = "c", "s", "presto", "app", "Asia/Jakarta", 1500*time.Millisecond
		})},
		{"trino://u@h?session.query_max_run_time=5m&session.memory.splits_per_node=4&source=dbimp&timeout=0s", with(cfg("h", 8080, "u"), func(c *trino.Config) {
			c.Session = map[string]string{"query_max_run_time": "5m", "memory.splits_per_node": "4"}
		})},
		{"trino://u@h/a%20b/c%2Fd", with(cfg("h", 8080, "u"), func(c *trino.Config) { c.Catalog, c.Schema = "a b", "c/d" })},
		{"trino://u@h?session.x=a%20b%2Bc%3D", with(cfg("h", 8080, "u"), func(c *trino.Config) { c.Session = map[string]string{"x": "a b+c="} })},
	} {
		got, err := trino.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := trino.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D175: a key that the driver does not know is
// refused, and so is a repeated key, a scheme that is not trino, no user, and a
// path with more than a catalog and a schema.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"trinos://u@localhost", dbimp.ErrScheme},
		{"presto://u@localhost", dbimp.ErrScheme},
		{"http://u@localhost:8080", dbimp.ErrScheme},
		{"trino://u@", dbimp.ErrInvalidValue},
		{"trino://localhost", dbimp.ErrInvalidValue},
		{"trino://:p@localhost", dbimp.ErrInvalidValue},
		{"trino://u@localhost/a/b/c", dbimp.ErrInvalidValue},
		{"trino://u@localhost//schema", dbimp.ErrInvalidValue},
		{"trino://u@localhost?catalog=memory", dbimp.ErrUnknownKey},
		{"trino://u@localhost?session_properties=a%3Db", dbimp.ErrUnknownKey},
		{"trino://u@localhost?password=x", dbimp.ErrUnknownKey},
		{"trino://u@localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"trino://u@localhost?session.a=1&session.a=2", dbimp.ErrRepeatedKey},
		{"trino://u@localhost?tls=yes", dbimp.ErrInvalidValue},
		{"trino://u@localhost?flavor=starburst", dbimp.ErrInvalidValue},
		{"trino://u@localhost?flavor=", dbimp.ErrInvalidValue},
		{"trino://u@localhost?source=", dbimp.ErrInvalidValue},
		{"trino://u@localhost?timezone=", dbimp.ErrInvalidValue},
		{"trino://u@localhost?timeout=-1s", dbimp.ErrInvalidValue},
		{"trino://u@localhost?timeout=soon", dbimp.ErrInvalidValue},
		{"trino://u@localhost?session.=1", dbimp.ErrInvalidValue},
		{"trino://u@localhost?session.a%3Db=1", dbimp.ErrInvalidValue},
		{"trino://u@localhost:0", dbimp.ErrInvalidValue},
		{"trino://u@localhost:65536", dbimp.ErrInvalidValue},
		{"trino://u@localhost:http", nil},
		{"trino://u@::", dbimp.ErrInvalidValue},
	} {
		_, err := trino.ParseDSN(tt.dsn)
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
		"trino://u:s3cret@localhost?bogus=1",
		"trinos://u:s3cret@localhost",
		"trino://u:s3cret@localhost:99999",
		"trino://u:s3cret@localhost/a/b/c",
		"trino://u:s3cret@%zz",
	} {
		if _, err := trino.ParseDSN(dsn); err == nil || strings.Contains(err.Error(), "s3cret") {
			t.Errorf("ParseDSN(%q) gave the error %v, want one with no password", dsn, err)
		}
	}
}

// FuzzParseDSN holds that the parser never panics, and that every DSN that it
// accepts formats to a DSN that parses back to the same configuration.
func FuzzParseDSN(f *testing.F) {
	for _, dsn := range []string{
		"trino://u@localhost",
		"trino://trino@127.0.0.1:55037/memory/default",
		"trino://u:p@[::1]:8443/c/s?tls=true&flavor=presto&source=a&timezone=UTC&timeout=1s&session.a=b",
		"trino://u@h/a%20b/c%2Fd?session.x=%20",
		"trino://u@::",
		"trino://u@:",
		"trino://u@h?tls=true&tls=true",
		"trino://@h",
		"trino://u@h/%zz",
	} {
		f.Add(dsn)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		c, err := trino.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := trino.ParseDSN(c.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(FormatDSN(%q)) = %q: %v", dsn, c.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, c) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *c)
		}
	})
}
