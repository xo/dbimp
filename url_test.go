package dbimp_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

func TestParseURL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"fake://user:pass@localhost:8093/db?tls=true", nil},
		{"fake://[::1]:8093", nil},
		{"other://localhost", dbimp.ErrScheme},
		{"n1ql://localhost", dbimp.ErrScheme},
		{"fake:opaque", dbimp.ErrInvalidValue},
		// The fuzz test of the SurrealDB DSN found this host, which net/url
		// reads as ":".
		{"fake://::/", dbimp.ErrInvalidValue},
		{"fake://[::]/", nil},
	} {
		_, err := dbimp.ParseURL("fake", tt.dsn)
		if !errors.Is(err, tt.want) {
			t.Errorf("ParseURL(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseURLHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := dbimp.ParseURL("fake", "fake://user:secret@local host:1")
	if err == nil {
		t.Fatal("ParseURL took a host with a space")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("the error %q holds the password", err)
	}
}

func TestNewQuery(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"fake://h?tls=true&timeout=5s", nil},
		{"fake://h?other=1", dbimp.ErrUnknownKey},
		{"fake://h?tls=true&tls=false", dbimp.ErrRepeatedKey},
	} {
		u, err := dbimp.ParseURL("fake", tt.dsn)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dbimp.NewQuery(u, "tls", "timeout", "name"); !errors.Is(err, tt.want) {
			t.Errorf("NewQuery(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestQueryValues(t *testing.T) {
	t.Parallel()
	u, err := dbimp.ParseURL("fake", "fake://h?tls=true&timeout=5s&n=3&name=x")
	if err != nil {
		t.Fatal(err)
	}
	q, err := dbimp.NewQuery(u, "tls", "timeout", "n", "name", "absent")
	if err != nil {
		t.Fatal(err)
	}
	if v, err := q.Bool("tls", false); err != nil || !v {
		t.Errorf("Bool = %v, %v", v, err)
	}
	if v, err := q.Duration("timeout", 0); err != nil || v != 5*time.Second {
		t.Errorf("Duration = %v, %v", v, err)
	}
	if v, err := q.Int("n", 0); err != nil || v != 3 {
		t.Errorf("Int = %v, %v", v, err)
	}
	if v := q.String("name", ""); v != "x" {
		t.Errorf("String = %q", v)
	}
	if v := q.String("absent", "def"); v != "def" || q.Has("absent") {
		t.Errorf("String of a missing key = %q", v)
	}
	if _, err := q.Int("name", 0); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("Int of %q = %v, want ErrInvalidValue", "x", err)
	}
}
