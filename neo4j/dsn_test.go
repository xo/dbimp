package neo4j_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/neo4j"
)

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want neo4j.Config
	}{
		{"neo4j://localhost", neo4j.Config{Auth: "basic", Host: "localhost", Port: 7474, Database: "neo4j", Cancel: "tag"}},
		{"neo4j://localhost/", neo4j.Config{Auth: "basic", Host: "localhost", Port: 7474, Database: "neo4j", Cancel: "tag"}},
		{
			"neo4j://neo4j:P4ss%21x@127.0.0.1:55064/dbmeta",
			neo4j.Config{Auth: "basic", Host: "127.0.0.1", Port: 55064, User: "neo4j", Password: "P4ss!x", Database: "dbmeta", Cancel: "tag"},
		},
		{"neo4j://[::1]:9000/db?tls=true", neo4j.Config{Auth: "basic", Host: "::1", Port: 9000, TLS: true, Database: "db", Cancel: "tag"}},
		{"neo4j://h?tls=true", neo4j.Config{Auth: "basic", Host: "h", Port: 7473, TLS: true, Database: "neo4j", Cancel: "tag"}},
		{"neo4j://h/a%2Fb?cancel=metadata", neo4j.Config{Auth: "basic", Host: "h", Port: 7474, Database: "a/b", Cancel: "metadata"}},
		{"neo4j://h/db?cancel=none", neo4j.Config{Auth: "basic", Host: "h", Port: 7474, Database: "db", Cancel: "none"}},
		{"neo4j://u@h/db", neo4j.Config{Auth: "basic", Host: "h", Port: 7474, User: "u", Database: "db", Cancel: "tag"}},
		{"neo4j://:tok@h/db?auth=bearer", neo4j.Config{Auth: "bearer", Host: "h", Port: 7474, Password: "tok", Database: "db", Cancel: "tag"}},
	} {
		cfg, err := neo4j.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*cfg, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *cfg, tt.want)
		}
		again, err := neo4j.ParseDSN(cfg.FormatDSN())
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
		{"bolt://localhost", dbimp.ErrScheme},
		{"neo4j+s://localhost", dbimp.ErrScheme},
		{"nj://localhost", dbimp.ErrScheme},
		{"neo4j://localhost/a/b", dbimp.ErrInvalidValue},
		{"neo4j://localhost/db/", dbimp.ErrInvalidValue},
		{"neo4j://localhost/?database=x", dbimp.ErrUnknownKey},
		{"neo4j://localhost/?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"neo4j://localhost/?tls=maybe", dbimp.ErrInvalidValue},
		{"neo4j://localhost/?cancel=kill", dbimp.ErrInvalidValue},
		{"neo4j://localhost/?auth=jwt", dbimp.ErrInvalidValue},
		{"neo4j://localhost:0/", dbimp.ErrInvalidValue},
		{"neo4j:///", dbimp.ErrInvalidValue},
		{"neo4j://::/", dbimp.ErrInvalidValue},
	} {
		if _, err := neo4j.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := neo4j.ParseDSN("neo4j://u:secret@bad host/")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"neo4j://localhost",
		"neo4j://u:p@[::1]:1/db?tls=true&cancel=none",
		"neo4j://h/a%2Fb?cancel=metadata",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		cfg, err := neo4j.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := neo4j.ParseDSN(cfg.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, cfg.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, cfg) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *cfg)
		}
	})
}
