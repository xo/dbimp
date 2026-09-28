package influxdb_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/influxdb"
)

// cfg returns the configuration of a DSN with every default, and host and
// port.
func cfg(host string, port int) influxdb.Config {
	return influxdb.Config{
		Host: host, Port: port, SQLMode: "prefer", Version: 3, Describe: "always", Chunked: "prefer",
	}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c influxdb.Config, f func(*influxdb.Config)) influxdb.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want influxdb.Config
	}{
		{"influxdb://localhost", cfg("localhost", 8181)},
		{"influxdb://localhost/", cfg("localhost", 8181)},
		{
			"influxdb://_admin:apiv3_P4ss%21x@127.0.0.1:55073/dbmeta",
			with(cfg("127.0.0.1", 55073), func(c *influxdb.Config) {
				c.User, c.Password, c.Database = "_admin", "apiv3_P4ss!x", "dbmeta"
			}),
		},
		{
			"influxdb://admin:p@h/db?sqlmode=disable&version=1",
			with(cfg("h", 8086), func(c *influxdb.Config) {
				c.User, c.Password, c.Database, c.SQLMode, c.Version = "admin", "p", "db", "disable", 1
			}),
		},
		{
			"influxdb://h/db?version=2&sqlmode=allow&rp=autogen",
			with(cfg("h", 8086), func(c *influxdb.Config) {
				c.Database, c.Version, c.SQLMode, c.RetentionPolicy = "db", 2, "allow", "autogen"
			}),
		},
		{
			"influxdb://[::1]:9000/a%2Fb?tls=true&describe=disable&chunked=disable&sqlmode=require",
			with(cfg("::1", 9000), func(c *influxdb.Config) {
				c.Database, c.TLS, c.Describe, c.Chunked, c.SQLMode = "a/b", true, "disable", "disable", "require"
			}),
		},
		{"influxdb://u@h", with(cfg("h", 8181), func(c *influxdb.Config) { c.User = "u" })},
	} {
		got, err := influxdb.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := influxdb.ParseDSN(got.FormatDSN())
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
		{"influx://localhost", dbimp.ErrScheme},
		{"influxql://localhost", dbimp.ErrScheme},
		{"http://localhost", dbimp.ErrScheme},
		{"influxdb://localhost/a/b", dbimp.ErrInvalidValue},
		{"influxdb://localhost/?db=x", dbimp.ErrUnknownKey},
		{"influxdb://localhost/?epoch=ms", dbimp.ErrUnknownKey},
		{"influxdb://localhost/?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"influxdb://localhost/?tls=maybe", dbimp.ErrInvalidValue},
		{"influxdb://localhost/?sqlmode=verify-full", dbimp.ErrInvalidValue},
		{"influxdb://localhost/?version=4", dbimp.ErrInvalidValue},
		{"influxdb://localhost/?version=0", dbimp.ErrInvalidValue},
		{"influxdb://localhost/?version=three", dbimp.ErrInvalidValue},
		{"influxdb://localhost/?describe=csv", dbimp.ErrInvalidValue},
		{"influxdb://localhost/?chunked=true", dbimp.ErrInvalidValue},
		{"influxdb://localhost:0/", dbimp.ErrInvalidValue},
		{"influxdb:///", dbimp.ErrInvalidValue},
		{"influxdb://::/", dbimp.ErrInvalidValue},
	} {
		if _, err := influxdb.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := influxdb.ParseDSN("influxdb://u:secret@bad host/")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"influxdb://localhost",
		"influxdb://u:p@[::1]:1/db?tls=true&sqlmode=disable&version=1",
		"influxdb://h/a%2Fb?describe=disable&chunked=disable&rp=autogen",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := influxdb.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := influxdb.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
