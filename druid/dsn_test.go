package druid_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/druid"
)

// cfg returns the configuration of a DSN with every default, and the host
// and the port.
func cfg(host string, port int) druid.Config {
	return druid.Config{Host: host, Port: port, TimeZone: "UTC"}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c druid.Config, f func(*druid.Config)) druid.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want druid.Config
	}{
		{"druid://localhost", cfg("localhost", 8888)},
		{"druid://localhost/", cfg("localhost", 8888)},
		{"druid://admin:P4ssw0rd%21x@127.0.0.1:55086", with(cfg("127.0.0.1", 55086), func(c *druid.Config) {
			c.User, c.Password = "admin", "P4ssw0rd!x"
		})},
		{"druid://[::1]:8082?tls=true&timezone=Asia%2FJakarta&timeout=1.5s", with(cfg("::1", 8082), func(c *druid.Config) {
			c.TLS, c.TimeZone, c.Timeout = true, "Asia/Jakarta", 1500*time.Millisecond
		})},
		// The port stays 8888 with tls=true (D164).
		{"druid://h?tls=true", with(cfg("h", 8888), func(c *druid.Config) { c.TLS = true })},
		{"druid://h?timezone=UTC&timeout=0s", cfg("h", 8888)},
		{"druid://h?timezone=%2B07:00", with(cfg("h", 8888), func(c *druid.Config) { c.TimeZone = "+07:00" })},
		{"druid://u@h", with(cfg("h", 8888), func(c *druid.Config) { c.User = "u" })},
		{"druid://:p@h", with(cfg("h", 8888), func(c *druid.Config) { c.Password = "p" })},
	} {
		got, err := druid.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := druid.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D164: a path is refused, because Druid has no
// database to choose, and so is every key that the driver does not know,
// such as a key of the query context.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"druids://localhost", dbimp.ErrScheme},
		{"http://localhost:8888", dbimp.ErrScheme},
		{"druid://localhost/druid", dbimp.ErrInvalidValue},
		{"druid://localhost/a/b", dbimp.ErrInvalidValue},
		{"druid://localhost?sqlTimeZone=UTC", dbimp.ErrUnknownKey},
		{"druid://localhost?sqlStringifyArrays=false", dbimp.ErrUnknownKey},
		{"druid://localhost?cancel=none", dbimp.ErrUnknownKey},
		{"druid://localhost?auth=bearer", dbimp.ErrUnknownKey},
		{"druid://localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"druid://localhost?tls=maybe", dbimp.ErrInvalidValue},
		{"druid://localhost?timezone=", dbimp.ErrInvalidValue},
		{"druid://localhost?timeout=5", dbimp.ErrInvalidValue},
		{"druid://localhost?timeout=-1s", dbimp.ErrInvalidValue},
		{"druid://localhost:0", dbimp.ErrInvalidValue},
		{"druid://localhost:65536", dbimp.ErrInvalidValue},
		{"druid://", dbimp.ErrInvalidValue},
		{"druid://::", dbimp.ErrInvalidValue},
	} {
		if _, err := druid.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := druid.ParseDSN("druid://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"druid://localhost",
		"druid://u:p@[::1]:1?tls=true&timezone=Asia%2FJakarta&timeout=1m30s",
		"druid://:p@h?timeout=1ns",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := druid.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := druid.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
