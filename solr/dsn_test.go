package solr_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/solr"
)

// cfg returns the configuration of a DSN with every default, and the host
// and the port.
func cfg(host string, port int) solr.Config {
	return solr.Config{Host: host, Port: port, Mode: solr.ModeFacet}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c solr.Config, f func(*solr.Config)) solr.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want solr.Config
	}{
		{"solr://localhost", cfg("localhost", 8983)},
		{"solr://localhost/", cfg("localhost", 8983)},
		{"solr://localhost/books", with(cfg("localhost", 8983), func(c *solr.Config) { c.Collection = "books" })},
		{"solr://admin:P4ssw0rd%21x@127.0.0.1:55086/dbimp", with(cfg("127.0.0.1", 55086), func(c *solr.Config) {
			c.User, c.Password, c.Collection = "admin", "P4ssw0rd!x", "dbimp"
		})},
		{"solr://[::1]:8984/a%20b?tls=true", with(cfg("::1", 8984), func(c *solr.Config) {
			c.TLS, c.Collection = true, "a b"
		})},
		// The port stays 8983 with tls=true (D166).
		{"solr://h?tls=true", with(cfg("h", 8983), func(c *solr.Config) { c.TLS = true })},
		{"solr://h?mode=facet", cfg("h", 8983)},
		{"solr://u@h", with(cfg("h", 8983), func(c *solr.Config) { c.User = "u" })},
		{"solr://:p@h", with(cfg("h", 8983), func(c *solr.Config) { c.Password = "p" })},
	} {
		got, err := solr.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := solr.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D166: a path of more than one segment is refused,
// the mode map_reduce is refused, because it cut a GROUP BY with no sign, and
// so is every key that the driver does not know.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"solrs://localhost", dbimp.ErrScheme},
		{"http://localhost:8983", dbimp.ErrScheme},
		{"solr://localhost/a/b", dbimp.ErrInvalidValue},
		{"solr://localhost/a%2Fb", dbimp.ErrInvalidValue},
		{"solr://localhost?mode=map_reduce", dbimp.ErrNotSupported},
		{"solr://localhost?mode=other", dbimp.ErrInvalidValue},
		{"solr://localhost?mode=", dbimp.ErrInvalidValue},
		{"solr://localhost?aggregationMode=facet", dbimp.ErrUnknownKey},
		{"solr://localhost?timeout=5s", dbimp.ErrUnknownKey},
		{"solr://localhost?auth=bearer", dbimp.ErrUnknownKey},
		{"solr://localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"solr://localhost?tls=maybe", dbimp.ErrInvalidValue},
		{"solr://localhost:0", dbimp.ErrInvalidValue},
		{"solr://localhost:65536", dbimp.ErrInvalidValue},
		{"solr://", dbimp.ErrInvalidValue},
		{"solr://::", dbimp.ErrInvalidValue},
	} {
		if _, err := solr.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := solr.ParseDSN("solr://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"solr://localhost",
		"solr://u:p@[::1]:1/c?tls=true&mode=facet",
		"solr://:p@h/a%20b",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := solr.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := solr.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
