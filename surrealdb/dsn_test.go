package surrealdb_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/surrealdb"
)

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want surrealdb.Config
	}{
		{"surrealdb://localhost/test/test", surrealdb.Config{Host: "localhost", Port: 8000, Namespace: "test", Database: "test", Auth: "root", Encoding: "cbor"}},
		{"surrealdb://root:p%21x@127.0.0.1:55063/dbmeta/dbmeta", surrealdb.Config{
			Host: "127.0.0.1", Port: 55063, User: "root", Password: "p!x", Namespace: "dbmeta", Database: "dbmeta", Auth: "root", Encoding: "cbor",
		}},
		{"surrealdb://u:p@h/ns/db?auth=database", surrealdb.Config{
			Host: "h", Port: 8000, User: "u", Password: "p", Namespace: "ns", Database: "db", Auth: "database", Encoding: "cbor",
		}},
		{"surrealdb://[::1]:9000/a%2Fb/c%20d?tls=true&auth=namespace&encoding=json", surrealdb.Config{
			Host: "::1", Port: 9000, TLS: true, Namespace: "a/b", Database: "c d", Auth: "namespace", Encoding: "json",
		}},
	} {
		cfg, err := surrealdb.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*cfg, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *cfg, tt.want)
		}
		again, err := surrealdb.ParseDSN(cfg.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, cfg.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, cfg) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *cfg)
		}
	}
}

func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"surreal://localhost/a/b", dbimp.ErrScheme},
		{"http://localhost/a/b", dbimp.ErrScheme},
		{"surrealdb://localhost", dbimp.ErrInvalidValue},
		{"surrealdb://localhost/", dbimp.ErrInvalidValue},
		{"surrealdb://localhost/ns", dbimp.ErrInvalidValue},
		{"surrealdb://localhost/ns/", dbimp.ErrInvalidValue},
		{"surrealdb://localhost//db", dbimp.ErrInvalidValue},
		{"surrealdb://localhost/a/b/c", dbimp.ErrInvalidValue},
		{"surrealdb:///a/b", dbimp.ErrInvalidValue},
		{"surrealdb://localhost/a/b?ns=x", dbimp.ErrUnknownKey},
		{"surrealdb://localhost/a/b?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"surrealdb://localhost/a/b?auth=scope", dbimp.ErrInvalidValue},
		{"surrealdb://localhost/a/b?encoding=xml", dbimp.ErrInvalidValue},
		{"surrealdb://localhost/a/b?tls=maybe", dbimp.ErrInvalidValue},
		{"surrealdb://localhost:0/a/b", dbimp.ErrInvalidValue},
	} {
		if _, err := surrealdb.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{"surrealdb://u:secret@bad host/a/b", "surrealdb://u:secret@h/a", "surrealdb://u:secret@h/a/b?auth=x"} {
		_, err := surrealdb.ParseDSN(dsn)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("ParseDSN(%q) gave %v, which holds the password or no error", dsn, err)
		}
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"surrealdb://localhost/a/b",
		"surrealdb://u:p@[::1]:1/a%2Fb/c?tls=true&auth=database",
		"surrealdb://h/n/d?encoding=json&auth=namespace",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		cfg, err := surrealdb.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := surrealdb.ParseDSN(cfg.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, cfg.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, cfg) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *cfg)
		}
	})
}
