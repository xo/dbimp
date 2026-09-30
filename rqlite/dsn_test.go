package rqlite_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/rqlite"
)

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want rqlite.Config
	}{
		{"rqlite://localhost", rqlite.Config{Host: "localhost", Port: 4001}},
		{"rqlite://localhost/", rqlite.Config{Host: "localhost", Port: 4001}},
		{"rqlite://admin:P4ss%21x@127.0.0.1:55077", rqlite.Config{Host: "127.0.0.1", Port: 55077, User: "admin", Password: "P4ss!x"}},
		{"rqlite://[::1]:4001?tls=true&level=strong", rqlite.Config{Host: "::1", Port: 4001, TLS: true, Level: "strong"}},
		{"rqlite://h?level=none&freshness=1m30s", rqlite.Config{Host: "h", Port: 4001, Level: "none", Freshness: 90 * time.Second}},
		{"rqlite://u@h", rqlite.Config{Host: "h", Port: 4001, User: "u"}},
	} {
		got, err := rqlite.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := rqlite.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D141: a path is refused, because rqlite has one
// database, and so is every key that the driver does not know, such as a key
// of the API of the server.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"rq://localhost", dbimp.ErrScheme},
		{"http://localhost:4001", dbimp.ErrScheme},
		{"rqlite://localhost/main", dbimp.ErrInvalidValue},
		{"rqlite://localhost?transaction", dbimp.ErrUnknownKey},
		{"rqlite://localhost?db_timeout=1s", dbimp.ErrUnknownKey},
		{"rqlite://localhost?redirect", dbimp.ErrUnknownKey},
		{"rqlite://localhost?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"rqlite://localhost?tls=maybe", dbimp.ErrInvalidValue},
		{"rqlite://localhost?level=bogus", dbimp.ErrInvalidValue},
		{"rqlite://localhost?level=WEAK", dbimp.ErrInvalidValue},
		{"rqlite://localhost?freshness=soon", dbimp.ErrInvalidValue},
		{"rqlite://localhost?freshness=-1s", dbimp.ErrInvalidValue},
		{"rqlite://localhost:0", dbimp.ErrInvalidValue},
		{"rqlite://localhost:65536", dbimp.ErrInvalidValue},
		{"rqlite://", dbimp.ErrInvalidValue},
		{"rqlite://::", dbimp.ErrInvalidValue},
	} {
		if _, err := rqlite.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := rqlite.ParseDSN("rqlite://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"rqlite://localhost",
		"rqlite://u:p@[::1]:1?tls=true&level=linearizable",
		"rqlite://h?level=none&freshness=5s",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := rqlite.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := rqlite.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
