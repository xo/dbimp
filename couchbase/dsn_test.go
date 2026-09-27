package couchbase_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/couchbase"
)

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want couchbase.Config
	}{
		{"couchbase://localhost", couchbase.Config{Host: "localhost", Port: 8093}},
		{"couchbase://u:p%21x@127.0.0.1:55059/", couchbase.Config{Host: "127.0.0.1", Port: 55059, User: "u", Password: "p!x"}},
		{"couchbase://[::1]:9000/?tls=true", couchbase.Config{Host: "::1", Port: 9000, TLS: true}},
		{"couchbase://h/?tls=true", couchbase.Config{Host: "h", Port: 18093, TLS: true}},
		{
			"couchbase://h/?query_context=default:dbmeta._default&scan_consistency=request_plus&timeout=30s&durability_level=none&txtimeout=30m",
			couchbase.Config{
				Host: "h", Port: 8093, QueryContext: "default:dbmeta._default", ScanConsistency: "request_plus",
				Timeout: 30 * time.Second, Durability: "none", TxTimeout: 30 * time.Minute,
			},
		},
	} {
		cfg, err := couchbase.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*cfg, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *cfg, tt.want)
		}
		again, err := couchbase.ParseDSN(cfg.FormatDSN())
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
		{"n1ql://localhost", dbimp.ErrScheme},
		{"couchbases://localhost", dbimp.ErrScheme},
		{"couchbase://localhost/bucket", dbimp.ErrInvalidValue},
		{"couchbase://localhost/?pool=1", dbimp.ErrUnknownKey},
		{"couchbase://localhost/?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"couchbase://localhost/?scan_consistency=at_plus", dbimp.ErrInvalidValue},
		{"couchbase://localhost/?durability_level=some", dbimp.ErrInvalidValue},
		{"couchbase://localhost/?timeout=soon", dbimp.ErrInvalidValue},
		{"couchbase:///", dbimp.ErrInvalidValue},
	} {
		if _, err := couchbase.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := couchbase.ParseDSN("couchbase://u:secret@bad host/")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"couchbase://localhost",
		"couchbase://u:p@[::1]:1/?tls=true&timeout=1s",
		"couchbase://h/?query_context=a:b.c&txtimeout=2m&durability_level=majority",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		cfg, err := couchbase.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := couchbase.ParseDSN(cfg.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, cfg.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, cfg) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *cfg)
		}
	})
}
