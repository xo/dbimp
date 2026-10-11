package exasol_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/exasol"
)

// fingerprint is a SHA-256 in hex, as keytool and openssl write it without
// the colons.
const fingerprint = "0f343b0931126a20f133d67c2b018a3b9a5cdf0e2f2e2cd2fa2b8c0c9a3ce4b1"

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	base := exasol.Config{Host: "h", Port: 8563, User: "u", FetchSize: 1024, ClientName: "dbimp", Protocol: 3}
	with := func(f func(*exasol.Config)) exasol.Config {
		c := base
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want exasol.Config
	}{
		{"exasol://u@h", base},
		{"exasol://u:p@h:9000/", with(func(c *exasol.Config) { c.Port, c.Password = 9000, "p" })},
		{"exasol://sys:exasol@127.0.0.1:55048?validateservercertificate=0", with(func(c *exasol.Config) {
			c.Host, c.Port, c.User, c.Password, c.SkipVerify = "127.0.0.1", 55048, "sys", "exasol", true
		})},
		{"exasol://u:p%40%2F@[::1]:8563/MY_SCHEMA", with(func(c *exasol.Config) { c.Host, c.Password, c.Schema = "::1", "p@/", "MY_SCHEMA" })},
		{"exasol://u@h?tls=false&compression=false&autocommit=false", with(func(c *exasol.Config) {
			c.NoTLS, c.NoCompression, c.NoAutocommit = true, true, true
		})},
		{"exasol://u@h?fetchsize=64&querytimeout=30&clientname=app&protocol=5", with(func(c *exasol.Config) {
			c.FetchSize, c.QueryTimeout, c.ClientName, c.Protocol = 64, 30, "app", 5
		})},
		{"exasol://u@h?certificatefingerprint=" + strings.ToUpper(fingerprint[:32]) + ":" + fingerprint[32:], with(func(c *exasol.Config) {
			c.CertificateFingerprint = fingerprint
		})},
	} {
		got, err := exasol.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := exasol.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D205: a key that the driver does not know is
// refused, such as a key of exasol-driver-go, and so is a value out of its range.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"exa:h:8563;user=u", dbimp.ErrScheme},
		{"https://u@h", dbimp.ErrScheme},
		{"exasol://", dbimp.ErrInvalidValue},
		{"exasol://h", dbimp.ErrInvalidValue},
		{"exasol://:p@h", dbimp.ErrInvalidValue},
		{"exasol://u@h/a/b", dbimp.ErrInvalidValue},
		{"exasol://u@h:0", dbimp.ErrInvalidValue},
		{"exasol://u@h:65536", dbimp.ErrInvalidValue},
		{"exasol://u@h?encryption=1", dbimp.ErrUnknownKey},
		{"exasol://u@h?user=x", dbimp.ErrUnknownKey},
		{"exasol://u@h?password=x", dbimp.ErrUnknownKey},
		{"exasol://u@h?tls=0&tls=1", dbimp.ErrRepeatedKey},
		{"exasol://u@h?tls=maybe", dbimp.ErrInvalidValue},
		{"exasol://u@h?fetchsize=0", dbimp.ErrInvalidValue},
		{"exasol://u@h?fetchsize=65537", dbimp.ErrInvalidValue},
		{"exasol://u@h?querytimeout=-1", dbimp.ErrInvalidValue},
		{"exasol://u@h?querytimeout=1000000", dbimp.ErrInvalidValue},
		{"exasol://u@h?protocol=2", dbimp.ErrInvalidValue},
		{"exasol://u@h?protocol=6", dbimp.ErrInvalidValue},
		{"exasol://u@h?clientname=", dbimp.ErrInvalidValue},
		{"exasol://u@h?certificatefingerprint=abc", dbimp.ErrInvalidValue},
		{"exasol://u@h?certificatefingerprint=" + strings.Repeat("zz", 32), dbimp.ErrInvalidValue},
	} {
		if _, err := exasol.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := exasol.ParseDSN("exasol://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
	_, err = exasol.ParseDSN("exasol://u:secret@h?fetchsize=x")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad key gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"exasol://u@h",
		"exasol://u:p@[::1]:1/S?tls=false&compression=false&autocommit=0",
		"exasol://u@h?fetchsize=1&querytimeout=1&clientname=c&protocol=4&validateservercertificate=false",
		"exasol://u@h?certificatefingerprint=" + fingerprint,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := exasol.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := exasol.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
