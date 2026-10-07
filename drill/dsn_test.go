package drill_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/drill"
)

// cfg returns the configuration of a DSN with every default, and the host
// and the port.
func cfg(host string, port int) drill.Config {
	return drill.Config{Host: host, Port: port}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c drill.Config, f func(*drill.Config)) drill.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want drill.Config
	}{
		{"drill://localhost", cfg("localhost", 8047)},
		{"drill://localhost/", cfg("localhost", 8047)},
		{"drill://admin:P4ssw0rd%21x@127.0.0.1:55147", with(cfg("127.0.0.1", 55147), func(c *drill.Config) {
			c.User, c.Password = "admin", "P4ssw0rd!x"
		})},
		{"drill://[::1]:8047?tls=true&schema=dfs.tmp&autolimit=100", with(cfg("::1", 8047), func(c *drill.Config) {
			c.TLS, c.Schema, c.AutoLimit = true, "dfs.tmp", 100
		})},
		// The port stays 8047 with tls=true (D165).
		{"drill://h?tls=true", with(cfg("h", 8047), func(c *drill.Config) { c.TLS = true })},
		{"drill://h?tls=false", cfg("h", 8047)},
		{"drill://h?schema=cp.%60employee.json%60", with(cfg("h", 8047), func(c *drill.Config) { c.Schema = "cp.`employee.json`" })},
		{"drill://u@h", with(cfg("h", 8047), func(c *drill.Config) { c.User = "u" })},
		{"drill://:p@h", with(cfg("h", 8047), func(c *drill.Config) { c.Password = "p" })},
	} {
		got, err := drill.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := drill.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D165: a path is refused, because the schema is a
// key, and so is every key that the driver does not know.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"drills://localhost", dbimp.ErrScheme},
		{"http://localhost:8047", dbimp.ErrScheme},
		{"drill://localhost/dfs", dbimp.ErrInvalidValue},
		{"drill://localhost/a/b", dbimp.ErrInvalidValue},
		{"drill://localhost?defaultSchema=dfs", dbimp.ErrUnknownKey},
		{"drill://localhost?autoLimit=10", dbimp.ErrUnknownKey},
		{"drill://localhost?options=x", dbimp.ErrUnknownKey},
		{"drill://localhost?auth=bearer", dbimp.ErrUnknownKey},
		{"drill://localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"drill://localhost?schema=a&schema=b", dbimp.ErrRepeatedKey},
		{"drill://localhost?tls=maybe", dbimp.ErrInvalidValue},
		{"drill://localhost?schema=", dbimp.ErrInvalidValue},
		{"drill://localhost?autolimit=", dbimp.ErrInvalidValue},
		{"drill://localhost?autolimit=ten", dbimp.ErrInvalidValue},
		{"drill://localhost?autolimit=0", dbimp.ErrInvalidValue},
		{"drill://localhost?autolimit=-1", dbimp.ErrInvalidValue},
		{"drill://localhost:0", dbimp.ErrInvalidValue},
		{"drill://localhost:65536", dbimp.ErrInvalidValue},
		{"drill://", dbimp.ErrInvalidValue},
		{"drill://::", dbimp.ErrInvalidValue},
	} {
		if _, err := drill.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := drill.ParseDSN("drill://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"drill://localhost",
		"drill://u:p@[::1]:1?tls=true&schema=dfs.tmp&autolimit=100",
		"drill://:p@h?autolimit=1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := drill.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := drill.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
