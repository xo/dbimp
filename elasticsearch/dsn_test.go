package elasticsearch_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/elasticsearch"
)

// cfg returns the configuration of a DSN with every default, and the host
// and the port.
func cfg(host string, port int) elasticsearch.Config {
	return elasticsearch.Config{Host: host, Port: port, Auth: "basic", FetchSize: 1000, TimeZone: "UTC"}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c elasticsearch.Config, f func(*elasticsearch.Config)) elasticsearch.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want elasticsearch.Config
	}{
		{"elasticsearch://localhost", cfg("localhost", 9200)},
		{"elasticsearch://localhost/", cfg("localhost", 9200)},
		{"elasticsearch://elastic:P4ssw0rd%21x@127.0.0.1:55086", with(cfg("127.0.0.1", 55086), func(c *elasticsearch.Config) {
			c.User, c.Password = "elastic", "P4ssw0rd!x"
		})},
		{"elasticsearch://[::1]:9243?tls=true&fetch_size=250&time_zone=Asia%2FJakarta&field_multi_value_leniency=true&catalog=docker-cluster",
			with(cfg("::1", 9243), func(c *elasticsearch.Config) {
				c.TLS, c.FetchSize, c.TimeZone, c.FieldMultiValueLeniency, c.Catalog = true, 250, "Asia/Jakarta", true, "docker-cluster"
			})},
		// The port stays 9200 with tls=true (D167).
		{"elasticsearch://h?tls=true", with(cfg("h", 9200), func(c *elasticsearch.Config) { c.TLS = true })},
		{"elasticsearch://h?auth=basic&fetch_size=1000&time_zone=UTC&field_multi_value_leniency=false", cfg("h", 9200)},
		{"elasticsearch://h?time_zone=%2B05:30", with(cfg("h", 9200), func(c *elasticsearch.Config) { c.TimeZone = "+05:30" })},
		{"elasticsearch://:S2V5Ok1l@h?auth=apikey", with(cfg("h", 9200), func(c *elasticsearch.Config) {
			c.Auth, c.Password = "apikey", "S2V5Ok1l"
		})},
		{"elasticsearch://u@h", with(cfg("h", 9200), func(c *elasticsearch.Config) { c.User = "u" })},
		{"elasticsearch://:p@h", with(cfg("h", 9200), func(c *elasticsearch.Config) { c.Password = "p" })},
	} {
		got, err := elasticsearch.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := elasticsearch.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D167: a path is refused, because Elasticsearch
// has no database to choose, and so is every key that the driver does not
// know, such as a key of the body of the request.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"elasticsearchs://localhost", dbimp.ErrScheme},
		{"http://localhost:9200", dbimp.ErrScheme},
		{"opensearch://localhost", dbimp.ErrScheme},
		{"elasticsearch://localhost/index", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost/a/b", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost?format=json", dbimp.ErrUnknownKey},
		{"elasticsearch://localhost?columnar=true", dbimp.ErrUnknownKey},
		{"elasticsearch://localhost?request_timeout=5s", dbimp.ErrUnknownKey},
		{"elasticsearch://localhost?page_timeout=5s", dbimp.ErrUnknownKey},
		{"elasticsearch://localhost?timeout=5s", dbimp.ErrUnknownKey},
		{"elasticsearch://localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"elasticsearch://localhost?fetch_size=1&fetch_size=2", dbimp.ErrRepeatedKey},
		{"elasticsearch://localhost?tls=maybe", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost?auth=bearer", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost?auth=apikey", dbimp.ErrInvalidValue},
		{"elasticsearch://u@localhost?auth=apikey", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost?fetch_size=0", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost?fetch_size=-5", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost?fetch_size=many", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost?time_zone=", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost?field_multi_value_leniency=2", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost?catalog=", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost:0", dbimp.ErrInvalidValue},
		{"elasticsearch://localhost:65536", dbimp.ErrInvalidValue},
		{"elasticsearch://", dbimp.ErrInvalidValue},
		{"elasticsearch://::", dbimp.ErrInvalidValue},
	} {
		if _, err := elasticsearch.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := elasticsearch.ParseDSN("elasticsearch://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
	_, err = elasticsearch.ParseDSN("elasticsearch://u:secret@h?auth=bearer")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad auth gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"elasticsearch://localhost",
		"elasticsearch://u:p@[::1]:1?tls=true&fetch_size=3&time_zone=Asia%2FJakarta&catalog=c",
		"elasticsearch://:k@h?auth=apikey&field_multi_value_leniency=true",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := elasticsearch.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := elasticsearch.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
