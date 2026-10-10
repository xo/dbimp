package spanner_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/spanner"
)

// hosted returns the configuration of a DSN for the hosted service, which names
// the three parts of the path and a key file.
func hosted() spanner.Config {
	return spanner.Config{Host: "spanner.googleapis.com", Port: 443, TLS: true, Project: "p", Instance: "i", Database: "d", CredentialFile: "/etc/key.json"} //nolint:gosec // G101: a path, which is no secret.
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(f func(*spanner.Config)) spanner.Config {
		c := hosted()
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want spanner.Config
	}{
		{"spanner:///p/i/d?credential_file=/etc/key.json", hosted()},
		{"spanner://spanner.googleapis.com/p/i/d?credential_file=/etc/key.json", hosted()},
		{"spanner://spanner.googleapis.com:443/p/i/d?credential_file=%2Fetc%2Fkey.json", hosted()},
		{"spanner://:8443/p/i/d?credential_file=/etc/key.json", with(func(c *spanner.Config) { c.Port = 8443 })},
		{"spanner://example.test:8443/p/i/d?credential_file=/k", with(func(c *spanner.Config) { c.Host, c.Port, c.CredentialFile = "example.test", 8443, "/k" })},
		{"spanner:///p/i/d?credential_file=/etc/key.json&tls=true", hosted()},
		{"spanner:///example.com%3Aproj/i/d?credential_file=/etc/key.json", with(func(c *spanner.Config) { c.Project = "example.com:proj" })},
		{"spanner:///example.com:proj/i/d?credential_file=/etc/key.json", with(func(c *spanner.Config) { c.Project = "example.com:proj" })},
		{"spanner:///p/i/a%20b?credential_file=/etc/key.json", with(func(c *spanner.Config) { c.Database = "a b" })},
		{"spanner:///p/i/d?credential_file=C%3A%5Ckeys%5Ckey.json", with(func(c *spanner.Config) { c.CredentialFile = `C:\keys\key.json` })},
		// The emulator: no TLS and no credential, on the port that it serves REST.
		{"spanner://localhost:9020/p/i/d", spanner.Config{Host: "localhost", Port: 9020, Project: "p", Instance: "i", Database: "d"}},
		{"spanner://localhost/p/i/d", spanner.Config{Host: "localhost", Port: 9020, Project: "p", Instance: "i", Database: "d"}},
		{"spanner://127.0.0.1:9020/p/i/d", spanner.Config{Host: "127.0.0.1", Port: 9020, Project: "p", Instance: "i", Database: "d"}},
		{"spanner://[::1]:9020/p/i/d", spanner.Config{Host: "::1", Port: 9020, Project: "p", Instance: "i", Database: "d"}},
		{"spanner://[::1]/p/i/d", spanner.Config{Host: "::1", Port: 9020, Project: "p", Instance: "i", Database: "d"}},
		{"spanner://admin@localhost:9020/p/i/d", spanner.Config{Host: "localhost", Port: 9020, Project: "p", Instance: "i", Database: "d"}},
		{"spanner://localhost:9020/p/i/d?credential_file=/k", spanner.Config{Host: "localhost", Port: 9020, Project: "p", Instance: "i", Database: "d", CredentialFile: "/k"}},
		{"spanner://localhost:9020/p/i/d?tls=true&credential_file=/k", spanner.Config{Host: "localhost", Port: 9020, TLS: true, Project: "p", Instance: "i", Database: "d", CredentialFile: "/k"}},
		{"spanner://localhost/p/i/d?tls=true&credential_file=/k", spanner.Config{Host: "localhost", Port: 443, TLS: true, Project: "p", Instance: "i", Database: "d", CredentialFile: "/k"}},
		{"spanner://example.test/p/i/d?tls=false", spanner.Config{Host: "example.test", Port: 9020, Project: "p", Instance: "i", Database: "d"}},
	} {
		got, err := spanner.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := spanner.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestFormatDSNLeavesOutDefaults holds that FormatDSN writes the DSN that dburl
// writes: no host and no port for the hosted service, and no key tls where the
// host decides it.
func TestFormatDSNLeavesOutDefaults(t *testing.T) {
	t.Parallel()
	h := hosted()
	if got, want := h.FormatDSN(), "spanner:///p/i/d?credential_file=%2Fetc%2Fkey.json"; got != want {
		t.Errorf("FormatDSN() = %q, want %q", got, want)
	}
	emulator := spanner.Config{Host: "localhost", Port: 9020, Project: "p", Instance: "i", Database: "d"}
	if got, want := emulator.FormatDSN(), "spanner://localhost/p/i/d"; got != want {
		t.Errorf("FormatDSN() = %q, want %q", got, want)
	}
}

// TestParseDSNRefuses holds D191: a key other than the two is refused, and so is
// a repeated key, a scheme that is not spanner, a path that is not three names, a
// port that is not valid, user information, a server with TLS and no key file,
// and a value that is not valid.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	const key = "?credential_file=/k"
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"spanners:///p/i/d" + key, dbimp.ErrScheme},
		{"https:///p/i/d" + key, dbimp.ErrScheme},
		{"spanner://", dbimp.ErrInvalidValue},
		{"spanner:///" + key, dbimp.ErrInvalidValue},
		{"spanner:///p" + key, dbimp.ErrInvalidValue},
		{"spanner:///p/i" + key, dbimp.ErrInvalidValue},
		{"spanner:///p/i/d/x" + key, dbimp.ErrInvalidValue},
		{"spanner:///p//d" + key, dbimp.ErrInvalidValue},
		{"spanner:///p/i/" + key, dbimp.ErrInvalidValue},
		{"spanner:///p/i/a%2Fb" + key, dbimp.ErrInvalidValue},
		{"spanner:///p/i/d", dbimp.ErrInvalidValue},
		{"spanner://example.test/p/i/d", dbimp.ErrInvalidValue},
		{"spanner:///p/i/d?credential_file=", dbimp.ErrInvalidValue},
		{"spanner://u:secret@example.test/p/i/d" + key, dbimp.ErrInvalidValue},
		{"spanner://localhost:0/p/i/d", dbimp.ErrInvalidValue},
		{"spanner://localhost:65536/p/i/d", dbimp.ErrInvalidValue},
		{"spanner://localhost/p/i/d?tls=maybe", dbimp.ErrInvalidValue},
		{"spanner://::/p/i/d" + key, dbimp.ErrInvalidValue},
		// An error of net/url has no sentinel.
		{"spanner:///p/i/%zz" + key, nil},
		{"spanner:///p/i/d" + key + "&token=x", dbimp.ErrUnknownKey},
		{"spanner:///p/i/d" + key + "&timeout=1s", dbimp.ErrUnknownKey},
		{"spanner:///p/i/d" + key + "&password=x", dbimp.ErrUnknownKey},
		{"spanner:///p/i/d" + key + "&credential_file=/other", dbimp.ErrRepeatedKey},
		{"spanner://localhost/p/i/d?tls=true&tls=false", dbimp.ErrRepeatedKey},
	} {
		_, err := spanner.ParseDSN(tt.dsn)
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want an error that wraps %v", tt.dsn, err, tt.want)
		}
	}
}

// TestParseDSNKeepsTheSecretOut holds D94: an error never holds the password of
// a URL, which this driver does not use, and a key file is a path, so a DSN holds
// no key text.
func TestParseDSNKeepsTheSecretOut(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{
		"spanner://u:hunter2@example.test/p/i/d?credential_file=/k",
		"spanners://u:hunter2@example.test/p/i/d",
		"spanner://u:hunter2@example.test:99999/p/i/d",
		"spanner://u:hunter2@%zz/p/i/d",
		"spanner://u:hunter2@example.test/p/i/d?bogus=1",
	} {
		_, err := spanner.ParseDSN(dsn)
		if err == nil {
			t.Errorf("ParseDSN accepted %q", dsn)
			continue
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("ParseDSN(%q) gave an error that holds the password: %v", dsn, err)
		}
	}
}

// FuzzParseDSN holds that the parser never panics, and that every DSN that it
// accepts formats to a DSN that parses back to the same configuration.
func FuzzParseDSN(f *testing.F) {
	for _, dsn := range []string{
		"spanner:///p/i/d?credential_file=/etc/key.json",
		"spanner://spanner.googleapis.com:443/p/i/d?credential_file=/k&tls=true",
		"spanner://localhost:9020/p/i/d",
		"spanner://[::1]:9020/p/i/d",
		"spanner://[::1]/p/i/d?credential_file=/k",
		"spanner:///example.com%3Aproj/i/a%20b?credential_file=C%3A%5Ck.json",
		"spanner://u@::/p/i/d",
		"spanner://u@:/p/i/d",
		"spanner://@h/p/i/d",
		"spanner://h/%zz",
		"spanner://h///",
		"spanner://localhost/p/i/d?tls=true&tls=true",
		"spanner://localhost/p/i/d?tls=false&credential_file=",
	} {
		f.Add(dsn)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		c, err := spanner.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := spanner.ParseDSN(c.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(FormatDSN(%q)) = %q: %v", dsn, c.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, c) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *c)
		}
	})
}
