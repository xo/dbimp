package cosmos_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/cosmos"
)

// key is a master key for the tests: the base64 text of 64 bytes, as the
// service gives it. It is not the key of any account.
const key = "dsZQi3KtZmCv1ljt3VNWNm7sQUF1y5rJfC6kv5JiwvW0EndXdDku/dkKBp8/ufDToSxLzR4y+O/0H/t4bQtVNw=="

// escapedKey is key as the password of a URL.
const escapedKey = "dsZQi3KtZmCv1ljt3VNWNm7sQUF1y5rJfC6kv5JiwvW0EndXdDku%2FdkKBp8%2FufDToSxLzR4y%2BO%2F0H%2Ft4bQtVNw%3D%3D"

// cfg returns the configuration of a DSN with the keys that every DSN needs.
func cfg(host string, port int) cosmos.Config {
	return cosmos.Config{Host: host, Port: port, TLS: true, User: "x", Key: key}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c cosmos.Config, f func(*cosmos.Config)) cosmos.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want cosmos.Config
	}{
		{"cosmos://x:" + escapedKey + "@account.documents.azure.com", cfg("account.documents.azure.com", 0)},
		{"cosmos://x:" + escapedKey + "@account.documents.azure.com:443/", cfg("account.documents.azure.com", 443)},
		{"cosmos://x:" + escapedKey + "@account.documents.azure.com/mydb", with(cfg("account.documents.azure.com", 0), func(c *cosmos.Config) {
			c.Database = "mydb"
		})},
		{"cosmos://x:" + escapedKey + "@h/mydb/mycontainer", with(cfg("h", 0), func(c *cosmos.Config) {
			c.Database, c.Container = "mydb", "mycontainer"
		})},
		{"cosmos://x:" + escapedKey + "@127.0.0.1:8081/db/c?insecure=true", with(cfg("127.0.0.1", 8081), func(c *cosmos.Config) {
			c.Database, c.Container, c.Insecure = "db", "c", true
		})},
		{"cosmos://x:" + escapedKey + "@h:8081?tls=false", with(cfg("h", 8081), func(c *cosmos.Config) { c.TLS = false })},
		{"cosmos://x:" + escapedKey + "@[::1]:8081/db/c?insecure=true&pagesize=-1", with(cfg("::1", 8081), func(c *cosmos.Config) {
			c.Database, c.Container, c.Insecure, c.PageSize = "db", "c", true, -1
		})},
		{"cosmos://x:" + escapedKey + "@h/db/c?pagesize=100&partitionkey=p%2F1", with(cfg("h", 0), func(c *cosmos.Config) {
			c.Database, c.Container, c.PageSize, c.PartitionKey = "db", "c", 100, new("p/1")
		})},
		// An empty value is a value of the partition key.
		{"cosmos://x:" + escapedKey + "@h/db/c?partitionkey=", with(cfg("h", 0), func(c *cosmos.Config) {
			c.Database, c.Container, c.PartitionKey = "db", "c", new("")
		})},
		// A name can hold characters that a URL escapes.
		{"cosmos://x:" + escapedKey + "@h/my%20db/c%25d", with(cfg("h", 0), func(c *cosmos.Config) {
			c.Database, c.Container = "my db", "c%d"
		})},
		// The user is any text, and it can be empty.
		{"cosmos://:" + escapedKey + "@h", with(cfg("h", 0), func(c *cosmos.Config) { c.User = "" })},
		{"cosmos://emulator:" + escapedKey + "@h", with(cfg("h", 0), func(c *cosmos.Config) { c.User = "emulator" })},
	} {
		got, err := cosmos.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := cosmos.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D190: the key is the password, a key of the query
// that is unknown or repeated is refused, and so is a path of more than two
// names.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	const base = "cosmos://x:" + escapedKey + "@h"
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"cosmosdb://x:" + escapedKey + "@h", dbimp.ErrScheme},
		{"gocosmos://x:" + escapedKey + "@h", dbimp.ErrScheme},
		{"https://x:" + escapedKey + "@h", dbimp.ErrScheme},
		{"cosmos://h", dbimp.ErrInvalidValue},
		{"cosmos://x@h", dbimp.ErrInvalidValue},
		{"cosmos://x:@h", dbimp.ErrInvalidValue},
		{"cosmos://x:not%20base64%21@h", dbimp.ErrInvalidValue},
		{"cosmos://x:" + escapedKey + "@", dbimp.ErrInvalidValue},
		{"cosmos://x:" + escapedKey + "@::", dbimp.ErrInvalidValue},
		{"cosmos://x:" + escapedKey + "@h:0", dbimp.ErrInvalidValue},
		{"cosmos://x:" + escapedKey + "@h:65536", dbimp.ErrInvalidValue},
		{base + "/a/b/c", dbimp.ErrInvalidValue},
		{base + "//c", dbimp.ErrInvalidValue},
		{base + "/a//", dbimp.ErrInvalidValue},
		{base + "/a%2Fb", dbimp.ErrInvalidValue},
		{base + "/a/b%5Cc", dbimp.ErrInvalidValue},
		{base + "/a/b%3Fc", dbimp.ErrInvalidValue},
		{base + "?InsecureSkipVerify=true", dbimp.ErrUnknownKey},
		{base + "?key=" + escapedKey, dbimp.ErrUnknownKey},
		{base + "?timeout=1s", dbimp.ErrUnknownKey},
		{base + "?insecure=true&insecure=false", dbimp.ErrRepeatedKey},
		{base + "?tls=true&tls=false", dbimp.ErrRepeatedKey},
		{base + "?pagesize=1&pagesize=2", dbimp.ErrRepeatedKey},
		{base + "?partitionkey=a&partitionkey=b", dbimp.ErrRepeatedKey},
		{base + "?insecure=maybe", dbimp.ErrInvalidValue},
		{base + "?tls=maybe", dbimp.ErrInvalidValue},
		{base + "?pagesize=many", dbimp.ErrInvalidValue},
		{base + "?pagesize=-2", dbimp.ErrInvalidValue},
	} {
		if _, err := cosmos.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

// TestParseDSNDefaults holds the defaults of D190: TLS is on, and the
// certificate is verified.
func TestParseDSNDefaults(t *testing.T) {
	t.Parallel()
	got, err := cosmos.ParseDSN("cosmos://x:" + escapedKey + "@h")
	if err != nil {
		t.Fatal(err)
	}
	if !got.TLS || got.Insecure || got.PageSize != 0 || got.PartitionKey != nil || got.Database != "" || got.Container != "" {
		t.Errorf("the defaults are %+v", *got)
	}
}

// TestParseDSNHidesTheKey holds that no error holds the master key (D94).
func TestParseDSNHidesTheKey(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{
		"cosmos://x:" + escapedKey + "@bad host",
		"cosmos://x:" + escapedKey + "@h:99999",
		"cosmos://x:" + escapedKey + "@h?tls=maybe",
		"cosmos://x:" + escapedKey + "@h?extra=1",
		"cosmos://x:" + escapedKey + "@h/a/b/c",
		"cosmos://x:" + escapedKey + "@h?pagesize=-9",
		"cosmos://x:" + escapedKey + "@h?tls=true&tls=false",
		"cosmosdb://x:" + escapedKey + "@h",
		"cosmos://x:P4ssw0rd%21@h",
	} {
		_, err := cosmos.ParseDSN(dsn)
		if err == nil || strings.Contains(err.Error(), "dsZQi3Ktz") || strings.Contains(err.Error(), "P4ssw0rd") {
			t.Errorf("ParseDSN(%q) gave %v, which holds the key or no error", dsn, err)
		}
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"cosmos://x:" + escapedKey + "@localhost",
		"cosmos://x:" + escapedKey + "@[::1]:1/db/c?tls=false&insecure=true",
		"cosmos://%3A:" + escapedKey + "@h/%2E/%20?pagesize=-1&partitionkey=%2F",
		"cosmos://x:AAAA@h/a?partitionkey=",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := cosmos.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := cosmos.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}

// TestTheDSNOfDbrun holds that the integration tests read the url that dbrun
// prints for the emulator, which holds the key as the user and the key
// InsecureSkipVerify, as a DSN of the driver.
func TestTheDSNOfDbrun(t *testing.T) {
	t.Parallel()
	got, err := cosmos.ParseDSN(toDSN("cosmos://" + escapedKey + "@127.0.0.1:55001/?InsecureSkipVerify=true"))
	if err != nil {
		t.Fatal(err)
	}
	want := cosmos.Config{Host: "127.0.0.1", Port: 55001, TLS: true, Insecure: true, User: "x", Key: key}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("the DSN of dbrun is %+v, want %+v", *got, want)
	}
	// The DSN of the driver passes as it is.
	got, err = cosmos.ParseDSN(toDSN("cosmos://x:" + escapedKey + "@h.documents.azure.com:443"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (cosmos.Config{Host: "h.documents.azure.com", Port: 443, TLS: true, User: "x", Key: key}); !reflect.DeepEqual(*got, want) {
		t.Errorf("the DSN of the driver is %+v, want %+v", *got, want)
	}
}
