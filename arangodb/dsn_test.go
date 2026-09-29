package arangodb_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/arangodb"
)

// cfg returns the configuration of a DSN with every default, host and port.
func cfg(host string, port int, db string) arangodb.Config {
	return arangodb.Config{Host: host, Port: port, Database: db, Cancel: "tag", Batch: 1000, Auth: "basic"}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c arangodb.Config, f func(*arangodb.Config)) arangodb.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want arangodb.Config
	}{
		{"arangodb://localhost", cfg("localhost", 8529, "_system")},
		{"arangodb://localhost/", cfg("localhost", 8529, "_system")},
		{"arangodb://root:P4ss%21x@127.0.0.1:55066/dbmeta", with(cfg("127.0.0.1", 55066, "dbmeta"), func(c *arangodb.Config) {
			c.User, c.Password = "root", "P4ss!x"
		})},
		{"arangodb://[::1]:9000/a%2Fb?tls=true&cancel=none&batch=50", with(cfg("::1", 9000, "a/b"), func(c *arangodb.Config) {
			c.TLS, c.Cancel, c.Batch = true, "none", 50
		})},
		{"arangodb://:eyJ.token@h/db?auth=bearer", with(cfg("h", 8529, "db"), func(c *arangodb.Config) {
			c.Password, c.Auth = "eyJ.token", "bearer"
		})},
		{"arangodb://u@h/db", with(cfg("h", 8529, "db"), func(c *arangodb.Config) { c.User = "u" })},
	} {
		got, err := arangodb.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := arangodb.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"arango://localhost", dbimp.ErrScheme},
		{"http://localhost", dbimp.ErrScheme},
		{"arangodb://localhost/a/b", dbimp.ErrInvalidValue},
		{"arangodb://localhost/?database=x", dbimp.ErrUnknownKey},
		{"arangodb://localhost/?jwt=x", dbimp.ErrUnknownKey},
		{"arangodb://localhost/?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"arangodb://localhost/?tls=maybe", dbimp.ErrInvalidValue},
		{"arangodb://localhost/?cancel=kill", dbimp.ErrInvalidValue},
		{"arangodb://localhost/?batch=0", dbimp.ErrInvalidValue},
		{"arangodb://localhost/?batch=many", dbimp.ErrInvalidValue},
		{"arangodb://localhost/?auth=jwt", dbimp.ErrInvalidValue},
		{"arangodb://localhost:0/", dbimp.ErrInvalidValue},
		{"arangodb:///", dbimp.ErrInvalidValue},
		{"arangodb://::/", dbimp.ErrInvalidValue},
	} {
		if _, err := arangodb.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := arangodb.ParseDSN("arangodb://u:secret@bad host/")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"arangodb://localhost",
		"arangodb://u:p@[::1]:1/db?tls=true&cancel=none&batch=2",
		"arangodb://:tok@h/a%2Fb?auth=bearer",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := arangodb.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := arangodb.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
