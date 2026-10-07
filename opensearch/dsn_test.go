package opensearch_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/opensearch"
)

// cfg returns the configuration of a DSN with every default, and the host
// and the port.
func cfg(host string, port int) opensearch.Config {
	return opensearch.Config{Host: host, Port: port, FetchSize: 1000}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c opensearch.Config, f func(*opensearch.Config)) opensearch.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want opensearch.Config
	}{
		{"opensearch://localhost", cfg("localhost", 9200)},
		{"opensearch://localhost/", cfg("localhost", 9200)},
		{"opensearch://admin:P4ssw0rd%21x@127.0.0.1:55132", with(cfg("127.0.0.1", 55132), func(c *opensearch.Config) {
			c.User, c.Password = "admin", "P4ssw0rd!x"
		})},
		{"opensearch://[::1]:9243?tls=true&fetch_size=250", with(cfg("::1", 9243), func(c *opensearch.Config) {
			c.TLS, c.FetchSize = true, 250
		})},
		// The port stays 9200 with tls=true (D168).
		{"opensearch://h?tls=true", with(cfg("h", 9200), func(c *opensearch.Config) { c.TLS = true })},
		{"opensearch://h?tls=false&fetch_size=1000", cfg("h", 9200)},
		{"opensearch://u@h", with(cfg("h", 9200), func(c *opensearch.Config) { c.User = "u" })},
		{"opensearch://:p@h", with(cfg("h", 9200), func(c *opensearch.Config) { c.Password = "p" })},
		{"opensearch://h?fetch_size=10000", with(cfg("h", 9200), func(c *opensearch.Config) { c.FetchSize = 10000 })},
	} {
		got, err := opensearch.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := opensearch.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D168: a path is refused, because OpenSearch has no
// database to choose, and so is every key that the driver does not know, such
// as a key of the body of the request or a key of another driver.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"opensearchs://localhost", dbimp.ErrScheme},
		{"http://localhost:9200", dbimp.ErrScheme},
		{"elasticsearch://localhost", dbimp.ErrScheme},
		{"opensearch://localhost/index", dbimp.ErrInvalidValue},
		{"opensearch://localhost/a/b", dbimp.ErrInvalidValue},
		{"opensearch://localhost?format=csv", dbimp.ErrUnknownKey},
		{"opensearch://localhost?auth=apikey", dbimp.ErrUnknownKey},
		{"opensearch://localhost?time_zone=UTC", dbimp.ErrUnknownKey},
		{"opensearch://localhost?timeout=5s", dbimp.ErrUnknownKey},
		{"opensearch://localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"opensearch://localhost?fetch_size=1&fetch_size=2", dbimp.ErrRepeatedKey},
		{"opensearch://localhost?tls=maybe", dbimp.ErrInvalidValue},
		{"opensearch://localhost?fetch_size=0", dbimp.ErrInvalidValue},
		{"opensearch://localhost?fetch_size=-5", dbimp.ErrInvalidValue},
		{"opensearch://localhost?fetch_size=many", dbimp.ErrInvalidValue},
		{"opensearch://localhost:0", dbimp.ErrInvalidValue},
		{"opensearch://localhost:65536", dbimp.ErrInvalidValue},
		{"opensearch://", dbimp.ErrInvalidValue},
		{"opensearch://::", dbimp.ErrInvalidValue},
	} {
		if _, err := opensearch.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := opensearch.ParseDSN("opensearch://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
	_, err = opensearch.ParseDSN("opensearch://u:secret@h?fetch_size=0")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad fetch_size gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"opensearch://localhost",
		"opensearch://u:p@[::1]:1?tls=true&fetch_size=3",
		"opensearch://admin:P4ssw0rd%21x@127.0.0.1:55132",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := opensearch.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := opensearch.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
