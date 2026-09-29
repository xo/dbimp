package databend_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/databend"
)

// cfg returns the configuration of a DSN with every default, host and port.
func cfg(host string, port int, db string) databend.Config {
	return databend.Config{Host: host, Port: port, Database: db, Cancel: "kill", Auth: "basic"}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c databend.Config, f func(*databend.Config)) databend.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want databend.Config
	}{
		{"databend://localhost", cfg("localhost", 8000, "default")},
		{"databend://localhost/", cfg("localhost", 8000, "default")},
		{"databend://root:P4ss%21x@127.0.0.1:55083/dbmeta", with(cfg("127.0.0.1", 55083, "dbmeta"), func(c *databend.Config) {
			c.User, c.Password = "root", "P4ss!x"
		})},
		{"databend://[::1]:9000/a%2Fb?tls=true&cancel=none&timezone=Asia%2FKolkata", with(cfg("::1", 9000, "a/b"), func(c *databend.Config) {
			c.TLS, c.Cancel, c.Timezone = true, "none", "Asia/Kolkata"
		})},
		// The port stays 8000 with tls=true (D117).
		{"databend://h/db?tls=true", with(cfg("h", 8000, "db"), func(c *databend.Config) { c.TLS = true })},
		{"databend://:eyJ.token@h/db?auth=bearer", with(cfg("h", 8000, "db"), func(c *databend.Config) {
			c.Password, c.Auth = "eyJ.token", "bearer"
		})},
		{"databend://u@h/db", with(cfg("h", 8000, "db"), func(c *databend.Config) { c.User = "u" })},
	} {
		got, err := databend.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := databend.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D117: every key that the driver does not know
// is an error, the keys of databend-go too.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"bend://localhost", dbimp.ErrScheme},
		{"http://localhost", dbimp.ErrScheme},
		{"databend://localhost/a/b", dbimp.ErrInvalidValue},
		{"databend://localhost/?sslmode=disable", dbimp.ErrUnknownKey},
		{"databend://localhost/?tenant=tn", dbimp.ErrUnknownKey},
		{"databend://localhost/?warehouse=wh", dbimp.ErrUnknownKey},
		{"databend://localhost/?database=x", dbimp.ErrUnknownKey},
		{"databend://localhost/?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"databend://localhost/?tls=maybe", dbimp.ErrInvalidValue},
		{"databend://localhost/?cancel=tag", dbimp.ErrInvalidValue},
		{"databend://localhost/?auth=jwt", dbimp.ErrInvalidValue},
		{"databend://localhost:0/", dbimp.ErrInvalidValue},
		{"databend:///", dbimp.ErrInvalidValue},
		{"databend://::/", dbimp.ErrInvalidValue},
	} {
		if _, err := databend.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := databend.ParseDSN("databend://u:secret@bad host/")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"databend://localhost",
		"databend://u:p@[::1]:1/db?tls=true&cancel=none&timezone=UTC",
		"databend://:tok@h/a%2Fb?auth=bearer",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := databend.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := databend.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
