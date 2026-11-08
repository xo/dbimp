package hive_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/hive"
)

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want hive.Config
	}{
		{"hive://localhost", hive.Config{Host: "localhost", Port: 10001, HTTPPath: "cliservice"}},
		{"hive://localhost/", hive.Config{Host: "localhost", Port: 10001, HTTPPath: "cliservice"}},
		{"hive://hive:P4ss%21x@127.0.0.1:20052/sales", hive.Config{Host: "127.0.0.1", Port: 20052, User: "hive", Password: "P4ss!x", Database: "sales", HTTPPath: "cliservice"}},
		{"hive://[::1]:28000?tls=true&httpPath=cliservice", hive.Config{Host: "::1", Port: 28000, TLS: true, HTTPPath: "cliservice"}},
		{"hive://h?httpPath=a/b", hive.Config{Host: "h", Port: 10001, HTTPPath: "a/b"}},
		{"hive://u@h:1", hive.Config{Host: "h", Port: 1, User: "u", HTTPPath: "cliservice"}},
	} {
		got, err := hive.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := hive.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D234: a key that the driver does not know is
// refused, as is a key that repeats, a path that holds more than a database, and
// a scheme that is not the name of the driver.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"hive2://localhost", dbimp.ErrScheme},
		{"impala://localhost", dbimp.ErrScheme},
		{"http://localhost:10001", dbimp.ErrScheme},
		{"hive://localhost?transport=http", dbimp.ErrUnknownKey},
		{"hive://localhost?auth=PLAIN", dbimp.ErrUnknownKey},
		{"hive://localhost?service=hive", dbimp.ErrUnknownKey},
		{"hive://localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"hive://localhost?httpPath=a&httpPath=b", dbimp.ErrRepeatedKey},
		{"hive://localhost?tls=maybe", dbimp.ErrInvalidValue},
		{"hive://localhost?httpPath=", dbimp.ErrInvalidValue},
		{"hive://0?httpPath=//", dbimp.ErrInvalidValue},
		{"hive://localhost/a/b", dbimp.ErrInvalidValue},
		{"hive://localhost:0", dbimp.ErrInvalidValue},
		{"hive://localhost:65536", dbimp.ErrInvalidValue},
		{"hive://", dbimp.ErrInvalidValue},
		{"hive://::", dbimp.ErrInvalidValue},
	} {
		if _, err := hive.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := hive.ParseDSN("hive://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"hive://localhost",
		"hive://u:p@[::1]:1/db?tls=true&httpPath=x",
		"hive://h?httpPath=cliservice",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := hive.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := hive.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
