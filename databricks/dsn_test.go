package databricks_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/databricks"
)

// token is a token that the tests make up. It is no real token.
const token = "dapiSECRET0123456789abcdef"

const (
	workspaceHost = "dbc-1234abcd-ef56.cloud.databricks.com"
	warehouse     = "1111222233334444"
)

// cfg returns the configuration of a DSN with the host, the token and the
// warehouse.
func cfg() databricks.Config {
	return databricks.Config{Host: workspaceHost, Port: 443, Token: token, Warehouse: warehouse}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(f func(*databricks.Config)) databricks.Config {
		c := cfg()
		f(&c)
		return c
	}
	base := "databricks://token:" + token + "@" + workspaceHost + "/" + warehouse
	for _, tt := range []struct {
		dsn  string
		want databricks.Config
	}{
		{base, cfg()},
		{base + "/", cfg()},
		{"databricks://:" + token + "@" + workspaceHost + "/" + warehouse, cfg()},
		{"databricks://" + "token:" + token + "@" + workspaceHost + ":443/" + warehouse, cfg()},
		{base + "?catalog=main", with(func(c *databricks.Config) { c.Catalog = "main" })},
		{base + "?catalog=main&schema=default", with(func(c *databricks.Config) { c.Catalog, c.Schema = "main", "default" })},
		{base + "?schema=sales", with(func(c *databricks.Config) { c.Schema = "sales" })},
		{base + "?catalog=a%20b&schema=c%2Fd", with(func(c *databricks.Config) { c.Catalog, c.Schema = "a b", "c/d" })},
		{base + "?timeout=5m", with(func(c *databricks.Config) { c.Timeout = 5 * time.Minute })},
		{base + "?timeout=1500ms", with(func(c *databricks.Config) { c.Timeout = 1500 * time.Millisecond })},
		{base + "?timeout=0", cfg()},
		{base + "?tls=true", cfg()},
		{base + "?tls=false", with(func(c *databricks.Config) { c.Insecure = true })},
		// The hosts of Azure and of Google Cloud have dots, and a port is allowed.
		{"databricks://token:" + token + "@adb-1234.5.azuredatabricks.net/" + warehouse, with(func(c *databricks.Config) { c.Host = "adb-1234.5.azuredatabricks.net" })},
		{"databricks://token:" + token + "@localhost:8443/" + warehouse + "?tls=false", with(func(c *databricks.Config) { c.Host, c.Port, c.Insecure = "localhost", 8443, true })},
		{"databricks://token:" + token + "@127.0.0.1/" + warehouse, with(func(c *databricks.Config) { c.Host = "127.0.0.1" })},
		{"databricks://token:" + token + "@[::1]:8443/" + warehouse, with(func(c *databricks.Config) { c.Host, c.Port = "::1", 8443 })},
		{"databricks://token:" + token + "@[::1]/" + warehouse, with(func(c *databricks.Config) { c.Host = "::1" })},
		// The token can hold characters that a URL escapes.
		{"databricks://token:p%40ss%2Fw%3Ard@" + workspaceHost + "/" + warehouse, with(func(c *databricks.Config) { c.Token = "p@ss/w:rd" })},
		{"databricks://token:" + token + "@" + workspaceHost + "/a%20b", with(func(c *databricks.Config) { c.Warehouse = "a b" })},
	} {
		got, err := databricks.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", shorten(tt.dsn), err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", shorten(tt.dsn), *got, tt.want)
		}
		again, err := databricks.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", shorten(tt.dsn), shorten(got.FormatDSN()), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", shorten(tt.dsn), *again, *got)
		}
	}
}

// shorten cuts the token out of a text, so that a failure of a test prints no
// token.
func shorten(s string) string {
	return strings.ReplaceAll(s, token, "TOKEN")
}

// TestParseDSNRefuses holds D193: a key other than the four is refused, and so is
// a repeated key, a scheme that is not databricks, no host, no token, a user name
// other than token, no warehouse, a path with more than the warehouse, and a
// value that is empty or has the wrong form.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	base := "databricks://token:" + token + "@" + workspaceHost + "/" + warehouse
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"databrick://token:" + token + "@" + workspaceHost + "/" + warehouse, dbimp.ErrScheme},
		{"https://token:" + token + "@" + workspaceHost + "/" + warehouse, dbimp.ErrScheme},
		{"databricks://", dbimp.ErrInvalidValue},
		{"databricks://token:" + token + "@/" + warehouse, dbimp.ErrInvalidValue},
		{"databricks://" + workspaceHost + "/" + warehouse, dbimp.ErrInvalidValue},
		{"databricks://token@" + workspaceHost + "/" + warehouse, dbimp.ErrInvalidValue},
		{"databricks://token:@" + workspaceHost + "/" + warehouse, dbimp.ErrInvalidValue},
		{"databricks://alice:" + token + "@" + workspaceHost + "/" + warehouse, dbimp.ErrInvalidValue},
		{"databricks://token:" + token + "@" + workspaceHost, dbimp.ErrInvalidValue},
		{"databricks://token:" + token + "@" + workspaceHost + "/", dbimp.ErrInvalidValue},
		{"databricks://token:" + token + "@" + workspaceHost + "/a%2Fb", dbimp.ErrInvalidValue},
		{"databricks://token:" + token + "@" + workspaceHost + "//", dbimp.ErrInvalidValue},
		{"databricks://token:" + token + "@" + workspaceHost + "/sql/1.0/warehouses/" + warehouse, dbimp.ErrInvalidValue},
		{"databricks://token:" + token + "@" + workspaceHost + ":0/" + warehouse, dbimp.ErrInvalidValue},
		{"databricks://token:" + token + "@" + workspaceHost + ":65536/" + warehouse, dbimp.ErrInvalidValue},
		// An error of net/url has no sentinel.
		{base + "/%zz", nil},
		{base + "?warehouse=x", dbimp.ErrUnknownKey},
		{base + "?database=db", dbimp.ErrUnknownKey},
		{base + "?auth=bearer", dbimp.ErrUnknownKey},
		{base + "?token=x", dbimp.ErrUnknownKey},
		{base + "?catalog=a&catalog=b", dbimp.ErrRepeatedKey},
		{base + "?schema=a&schema=a", dbimp.ErrRepeatedKey},
		{base + "?catalog=", dbimp.ErrInvalidValue},
		{base + "?schema=", dbimp.ErrInvalidValue},
		{base + "?timeout=-1", dbimp.ErrInvalidValue},
		{base + "?timeout=1.5", dbimp.ErrInvalidValue},
		{base + "?timeout=60", dbimp.ErrInvalidValue},
		{base + "?timeout=-1s", dbimp.ErrInvalidValue},
		{base + "?timeout=9999999h", dbimp.ErrInvalidValue},
		{base + "?timeout=x", nil},
		{base + "?tls=maybe", nil},
		{"databricks://token:" + token + "@::/" + warehouse, dbimp.ErrInvalidValue},
	} {
		_, err := databricks.ParseDSN(tt.dsn)
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want an error that wraps %v", shorten(tt.dsn), err, tt.want)
		}
	}
}

// TestTimeoutNamesTheForm holds that a bare number is refused with a message
// that names the form of the value.
func TestTimeoutNamesTheForm(t *testing.T) {
	t.Parallel()
	_, err := databricks.ParseDSN("databricks://token:" + token + "@" + workspaceHost + "/" + warehouse + "?timeout=60")
	if err == nil || !strings.Contains(err.Error(), "60s") {
		t.Errorf("ParseDSN with timeout=60 gave %v, want an error that names a duration such as 60s", err)
	}
}

// TestParseDSNKeepsTheSecretOut holds that an error never holds the token (D94).
func TestParseDSNKeepsTheSecretOut(t *testing.T) {
	t.Parallel()
	half := token[:len(token)/2]
	for _, dsn := range []string{
		"databricks://token:" + token + "@" + workspaceHost + "/" + warehouse + "?bogus=1",
		"databrick://token:" + token + "@" + workspaceHost + "/" + warehouse,
		"databricks://token:" + token + "@" + workspaceHost + ":99999/" + warehouse,
		"databricks://token:" + token + "@" + workspaceHost + "/a/b",
		"databricks://token:" + token + "@%zz/" + warehouse,
		"databricks://alice:" + token + "@" + workspaceHost + "/" + warehouse,
		"databricks://token:" + token + "@" + workspaceHost,
		"databricks://token:" + token + "@" + workspaceHost + "/" + warehouse + "?timeout=x",
		"databricks://token:" + token + "@" + workspaceHost + "/" + warehouse + "?tls=maybe",
	} {
		_, err := databricks.ParseDSN(dsn)
		if err == nil {
			t.Errorf("ParseDSN accepted %q", shorten(dsn))
			continue
		}
		if strings.Contains(err.Error(), half) || strings.Contains(err.Error(), token[:8]) {
			t.Errorf("ParseDSN(%q) gave an error that holds the token: %v", shorten(dsn), err)
		}
	}
}

// TestOpenConnectorKeepsTheSecretOut holds that the driver refuses a DSN that is not
// valid before it opens anything, with an error that holds no token.
func TestOpenConnectorKeepsTheSecretOut(t *testing.T) {
	t.Parallel()
	_, err := databricks.Driver{}.OpenConnector("databricks://token:" + token + "@" + workspaceHost + "/" + warehouse + "?bogus=1")
	if !errors.Is(err, dbimp.ErrUnknownKey) || strings.Contains(err.Error(), token) {
		t.Errorf("OpenConnector gave %v, want dbimp.ErrUnknownKey with no token", err)
	}
	if _, err := (databricks.Driver{}).Open("databricks://x"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("Open gave %v, want dbimp.ErrNotSupported", err)
	}
}

// FuzzParseDSN holds that the parser never panics, and that every DSN that it
// accepts formats to a DSN that parses back to the same configuration.
func FuzzParseDSN(f *testing.F) {
	for _, dsn := range []string{
		"databricks://token:" + token + "@" + workspaceHost + "/" + warehouse,
		"databricks://token:" + token + "@" + workspaceHost + "/" + warehouse + "?catalog=main&schema=default&timeout=5m&tls=false",
		"databricks://token:" + token + "@" + workspaceHost + "/a%2Fb",
		"databricks://token:" + token + "@[::1]:8443/" + warehouse,
		"databricks://token:" + token + "@localhost:1/" + warehouse,
		"databricks://token:" + token + "@127.0.0.1/" + warehouse,
		"databricks://u@::",
		"databricks://u@:",
		"databricks://@h",
		"databricks://h/%zz",
		"databricks://h//",
		"databricks://token:x@h/w?catalog=a&catalog=a",
		"databricks://token:x@h/.",
		"databricks://token:x@h/..",
		"databricks://token:p%00@h/w",
	} {
		f.Add(dsn)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		c, err := databricks.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := databricks.ParseDSN(c.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(FormatDSN(%q)) = %q: %v", dsn, c.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, c) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *c)
		}
	})
}
