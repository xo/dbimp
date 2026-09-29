package pinot_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/pinot"
)

// cfg returns the configuration of a DSN with every default, and the host
// and the port.
func cfg(host string, port int) pinot.Config {
	return pinot.Config{Host: host, Port: port, Cancel: "kill", Engine: "multi", Auth: "basic"}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c pinot.Config, f func(*pinot.Config)) pinot.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want pinot.Config
	}{
		{"pinot://localhost", cfg("localhost", 8099)},
		{"pinot://localhost/", cfg("localhost", 8099)},
		{"pinot://admin:P4ss%21x@127.0.0.1:55080", with(cfg("127.0.0.1", 55080), func(c *pinot.Config) {
			c.User, c.Password = "admin", "P4ss!x"
		})},
		{"pinot://[::1]:8000?tls=true&cancel=none&engine=single", with(cfg("::1", 8000), func(c *pinot.Config) {
			c.TLS, c.Cancel, c.Engine = true, "none", "single"
		})},
		// The port stays 8099 with tls=true (D129).
		{"pinot://h?tls=true", with(cfg("h", 8099), func(c *pinot.Config) { c.TLS = true })},
		{"pinot://:eyJ.token@h?auth=bearer", with(cfg("h", 8099), func(c *pinot.Config) {
			c.Password, c.Auth = "eyJ.token", "bearer"
		})},
		{"pinot://u@h", with(cfg("h", 8099), func(c *pinot.Config) { c.User = "u" })},
	} {
		got, err := pinot.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := pinot.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D129: a path is refused, because Pinot has no
// databases, and so is every key that the driver does not know, such as a
// query option of the Broker.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"pi://localhost", dbimp.ErrScheme},
		{"http://localhost:8099", dbimp.ErrScheme},
		{"pinot://localhost/default", dbimp.ErrInvalidValue},
		{"pinot://localhost/a/b", dbimp.ErrInvalidValue},
		{"pinot://localhost?timeoutMs=5000", dbimp.ErrUnknownKey},
		{"pinot://localhost?useMultistageEngine=true", dbimp.ErrUnknownKey},
		{"pinot://localhost?controller=h:9000", dbimp.ErrUnknownKey},
		{"pinot://localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"pinot://localhost?tls=maybe", dbimp.ErrInvalidValue},
		{"pinot://localhost?cancel=tag", dbimp.ErrInvalidValue},
		{"pinot://localhost?engine=v2", dbimp.ErrInvalidValue},
		{"pinot://localhost?auth=jwt", dbimp.ErrInvalidValue},
		{"pinot://localhost:0", dbimp.ErrInvalidValue},
		{"pinot://localhost:65536", dbimp.ErrInvalidValue},
		{"pinot://", dbimp.ErrInvalidValue},
		{"pinot://::", dbimp.ErrInvalidValue},
	} {
		if _, err := pinot.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := pinot.ParseDSN("pinot://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"pinot://localhost",
		"pinot://u:p@[::1]:1?tls=true&cancel=none&engine=single",
		"pinot://:tok@h?auth=bearer",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := pinot.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := pinot.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
