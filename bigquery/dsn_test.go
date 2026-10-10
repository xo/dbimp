package bigquery_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/bigquery"
)

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(f func(*bigquery.Config)) bigquery.Config {
		c := bigquery.Config{Project: "my-project"}
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want bigquery.Config
	}{
		{"bigquery://my-project", bigquery.Config{Project: "my-project"}},
		{"bigquery://my-project/", bigquery.Config{Project: "my-project"}},
		{"bigquery://my-project/ds", with(func(c *bigquery.Config) { c.Dataset = "ds" })},
		{"bigquery://my-project/ds/", with(func(c *bigquery.Config) { c.Dataset = "ds" })},
		{"bigquery://my-project/EU/ds", with(func(c *bigquery.Config) { c.Location, c.Dataset = "EU", "ds" })},
		{"bigquery://my-project/ds?location=asia-southeast1", with(func(c *bigquery.Config) { c.Location, c.Dataset = "asia-southeast1", "ds" })},
		{"bigquery://my-project/a%2Fb", with(func(c *bigquery.Config) { c.Dataset = "a/b" })},
		{"bigquery://my-project/ds?credential_file=%2Fpath%2Fkey.json", with(func(c *bigquery.Config) { c.Dataset, c.CredentialFile = "ds", "/path/key.json" })},
		{"bigquery://my-project?scopes=a%20b", with(func(c *bigquery.Config) { c.Scopes = []string{"a", "b"} })},
		{"bigquery://my-project?scopes=a,b", with(func(c *bigquery.Config) { c.Scopes = []string{"a", "b"} })},
		{"bigquery://my-project?timeout=1m&max_results=500", with(func(c *bigquery.Config) { c.Timeout, c.MaxResults = time.Minute, 500 })},
		{"bigquery://my-project?timeout=1500ms", with(func(c *bigquery.Config) { c.Timeout = 1500 * time.Millisecond })},
		{"bigquery://my-project?timeout=0&max_results=0", with(func(c *bigquery.Config) {})},
		{"bigquery://my-project?endpoint=https%3A%2F%2Fbigquery.example.com", with(func(c *bigquery.Config) { c.Endpoint = "https://bigquery.example.com" })},
		// The form that dbrun prints for the emulator: a user that nothing checks, the
		// address, and no login.
		{"bigquery://admin@dbmeta/dbmeta?endpoint=http%3A%2F%2F127.0.0.1%3A9050&disable_auth=true", bigquery.Config{Project: "dbmeta", Dataset: "dbmeta", Endpoint: "http://127.0.0.1:9050", DisableAuth: true}},
		{"bigquery://dbmeta?endpoint=http%3A%2F%2F%5B%3A%3A1%5D%3A9050&disable_auth=true", bigquery.Config{Project: "dbmeta", Endpoint: "http://[::1]:9050", DisableAuth: true}},
		{"bigquery://dbmeta?endpoint=http%3A%2F%2Flocalhost%3A9050%2F&disable_auth=1", bigquery.Config{Project: "dbmeta", Endpoint: "http://localhost:9050", DisableAuth: true}},
	} {
		got, err := bigquery.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := bigquery.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestFormatDSNLeavesTheTokenOut holds D189: the DSN never holds a secret, so an
// access token that the caller set stays out of it.
func TestFormatDSNLeavesTheTokenOut(t *testing.T) {
	t.Parallel()
	cfg := bigquery.Config{Project: "p", Dataset: "d", AccessToken: "ya29.secret-token"} //nolint:gosec // The text is a token of a test.
	if got := cfg.FormatDSN(); strings.Contains(got, "secret") || got != "bigquery://p/d" {
		t.Errorf("FormatDSN is %q, want no token", got)
	}
}

// TestParseDSNRefuses holds D189: a key that is not one of the seven is refused,
// and so is a repeated key, a scheme that is not bigquery, no project, a
// password, a path with more than a location and a dataset, a location in two
// places, and a value that is not valid.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	base := "bigquery://p"
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"bigqueries://p", dbimp.ErrScheme},
		{"https://p", dbimp.ErrScheme},
		{"bigquery://", dbimp.ErrInvalidValue},
		{"bigquery:///ds", dbimp.ErrInvalidValue},
		{"bigquery://p:8443", dbimp.ErrInvalidValue},
		{"bigquery://u:secret@p", dbimp.ErrInvalidValue},
		{"bigquery://u:@p", dbimp.ErrInvalidValue},
		{base + "/a/b/c", dbimp.ErrInvalidValue},
		{base + "/a//", dbimp.ErrInvalidValue},
		{base + "//ds", dbimp.ErrInvalidValue},
		{base + "/EU/ds?location=US", dbimp.ErrInvalidValue},
		// An error of net/url has no sentinel.
		{base + "/a/%zz", nil},
		{base + "?tls=true", dbimp.ErrUnknownKey},
		{base + "?project=x", dbimp.ErrUnknownKey},
		{base + "?credential_json=x", dbimp.ErrUnknownKey},
		{base + "?password=x", dbimp.ErrUnknownKey},
		{base + "?location=a&location=b", dbimp.ErrRepeatedKey},
		{base + "?credential_file=a&credential_file=b", dbimp.ErrRepeatedKey},
		{base + "?location=", dbimp.ErrInvalidValue},
		{base + "?credential_file=", dbimp.ErrInvalidValue},
		{base + "?endpoint=", dbimp.ErrInvalidValue},
		{base + "?endpoint=ftp%3A%2F%2Fx", dbimp.ErrInvalidValue},
		{base + "?endpoint=http%3A%2F%2F", dbimp.ErrInvalidValue},
		{base + "?endpoint=localhost%3A9050", dbimp.ErrInvalidValue},
		{base + "?endpoint=http%3A%2F%2Fu%3Ap%40x", dbimp.ErrInvalidValue},
		{base + "?endpoint=http%3A%2F%2Fx%2Fpath", dbimp.ErrInvalidValue},
		{base + "?endpoint=http%3A%2F%2Fx%3Fa%3Db", dbimp.ErrInvalidValue},
		{base + "?disable_auth=maybe", dbimp.ErrInvalidValue},
		{base + "?disable_auth=true&credential_file=k.json", dbimp.ErrInvalidValue},
		{base + "?scopes=", dbimp.ErrInvalidValue},
		{base + "?scopes=%2C", dbimp.ErrInvalidValue},
		{base + "?timeout=-1s", dbimp.ErrInvalidValue},
		{base + "?timeout=60", dbimp.ErrInvalidValue},
		{base + "?timeout=1.5", dbimp.ErrInvalidValue},
		{base + "?timeout=9999999h", dbimp.ErrInvalidValue},
		{base + "?max_results=-1", dbimp.ErrInvalidValue},
		{base + "?max_results=x", dbimp.ErrInvalidValue},
		{base + "?max_results=999999999", dbimp.ErrInvalidValue},
		{"bigquery://::", dbimp.ErrInvalidValue},
	} {
		_, err := bigquery.ParseDSN(tt.dsn)
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want an error that wraps %v", tt.dsn, err, tt.want)
		}
	}
}

// TestParseDSNKeepsTheSecretOut holds that an error never holds the password of
// the URL, which D189 refuses, and so never prints it (D94).
func TestParseDSNKeepsTheSecretOut(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{
		"bigquery://u:hunter2-the-secret@p",
		"bigquery://u:hunter2-the-secret@p?bogus=1",
		"bigqueries://u:hunter2-the-secret@p",
		"bigquery://u:hunter2-the-secret@p:99999",
		"bigquery://u:hunter2-the-secret@%zz",
	} {
		_, err := bigquery.ParseDSN(dsn)
		if err == nil {
			t.Errorf("ParseDSN accepted %q", dsn)
			continue
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("ParseDSN gave an error that holds the password: %v", err)
		}
	}
}

// FuzzParseDSN holds that the parser never panics, and that every DSN that it
// accepts formats to a DSN that parses back to the same configuration.
func FuzzParseDSN(f *testing.F) {
	for _, dsn := range []string{
		"bigquery://p",
		"bigquery://p/ds",
		"bigquery://p/EU/ds?credential_file=%2Fk.json&scopes=a,b&timeout=1m&max_results=10",
		"bigquery://admin@dbmeta/dbmeta?endpoint=http%3A%2F%2F127.0.0.1%3A9050&disable_auth=true",
		"bigquery://p?endpoint=http%3A%2F%2F%5B%3A%3A1%5D%3A1",
		"bigquery://p/a%2Fb",
		"bigquery://u@::",
		"bigquery://u@:",
		"bigquery://@h",
		"bigquery://h/%zz",
		"bigquery://h//",
		"bigquery://p?location=a&location=a",
		"bigquery://p?timeout=1.5ms",
	} {
		f.Add(dsn)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		c, err := bigquery.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := bigquery.ParseDSN(c.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(FormatDSN(%q)) = %q: %v", dsn, c.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, c) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *c)
		}
	})
}
