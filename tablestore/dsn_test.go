package tablestore_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/tablestore"
)

// base returns the configuration of a DSN with the host of an instance and the
// keys of a user.
func base(host, instance string) tablestore.Config {
	return tablestore.Config{Host: host, TLS: true, Instance: instance, User: "key", Password: "secret"}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c tablestore.Config, f func(*tablestore.Config)) tablestore.Config {
		f(&c)
		return c
	}
	const host = "dbmeta.ap-southeast-5.ots.aliyuncs.com"
	for _, tt := range []struct {
		dsn  string
		want tablestore.Config
	}{
		{"tablestore://key:secret@" + host + "/dbmeta", base(host, "dbmeta")},
		{"tablestore://key:secret@" + host + "/dbmeta?tls=true", base(host, "dbmeta")},
		{"tablestore://key:secret@" + host + "/dbmeta?tls=false", with(base(host, "dbmeta"), func(c *tablestore.Config) { c.TLS = false })},
		// A secret has characters that the URL escapes.
		{"tablestore://AKIAEXAMPLE:wJalr%2FXUtn%2BFEMI%2FK7MDENG@" + host + "/dbmeta", with(base(host, "dbmeta"), func(c *tablestore.Config) {
			c.User, c.Password = "AKIAEXAMPLE", "wJalr/XUtn+FEMI/K7MDENG"
		})},
		// A host that is not of the form of the service, a port and an address.
		{"tablestore://key:secret@localhost:8080/inst?tls=false", with(base("localhost", "inst"), func(c *tablestore.Config) { c.Port, c.TLS = 8080, false })},
		{"tablestore://key:secret@[::1]:1/inst", with(base("::1", "inst"), func(c *tablestore.Config) { c.Port = 1 })},
		// The keys can be left out, and then the connector refuses to connect.
		{"tablestore://" + host + "/dbmeta", tablestore.Config{Host: host, TLS: true, Instance: "dbmeta"}},
		{"tablestore://key:secret@" + host + "/db%20name", with(base(host, "db name"), func(c *tablestore.Config) {})},
	} {
		got, err := tablestore.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := tablestore.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D222: the path is one instance, and a key that is
// unknown or repeated is refused.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	const host = "dbmeta.ap-southeast-5.ots.aliyuncs.com"
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"ots://k:s@" + host + "/dbmeta", dbimp.ErrScheme},
		{"tablestores://k:s@" + host + "/dbmeta", dbimp.ErrScheme},
		{"https://k:s@" + host + "/dbmeta", dbimp.ErrScheme},
		{"tablestore://k:s@/dbmeta", dbimp.ErrInvalidValue},
		{"tablestore://k:s@" + host, dbimp.ErrInvalidValue},
		{"tablestore://k:s@" + host + "/", dbimp.ErrInvalidValue},
		{"tablestore://k:s@" + host + "/a/b", dbimp.ErrInvalidValue},
		{"tablestore://k:s@" + host + "/a/", dbimp.ErrInvalidValue},
		{"tablestore://k:s@" + host + ":0/dbmeta", dbimp.ErrInvalidValue},
		{"tablestore://k:s@" + host + ":65536/dbmeta", dbimp.ErrInvalidValue},
		{"tablestore://k@" + host + "/dbmeta", dbimp.ErrInvalidValue},
		{"tablestore://:s@" + host + "/dbmeta", dbimp.ErrInvalidValue},
		{"tablestore://k:s@::/dbmeta", dbimp.ErrInvalidValue},
		{"tablestore://k:s@" + host + "/dbmeta?tls=maybe", dbimp.ErrInvalidValue},
		{"tablestore://k:s@" + host + "/dbmeta?region=x", dbimp.ErrUnknownKey},
		{"tablestore://k:s@" + host + "/dbmeta?timeout=1s", dbimp.ErrUnknownKey},
		{"tablestore://k:s@" + host + "/dbmeta?secret=x", dbimp.ErrUnknownKey},
		{"tablestore://k:s@" + host + "/dbmeta?TLS=true", dbimp.ErrUnknownKey},
		{"tablestore://k:s@" + host + "/dbmeta?tls=true&tls=false", dbimp.ErrRepeatedKey},
	} {
		if _, err := tablestore.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

// TestParseDSNHidesThePassword holds that no error holds the secret (D94).
func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	const host = "dbmeta.ap-southeast-5.ots.aliyuncs.com"
	for _, dsn := range []string{
		"tablestore://k:P4ssw0rd@bad host/dbmeta",
		"tablestore://k:P4ssw0rd@" + host,
		"tablestore://k:P4ssw0rd@" + host + ":99999/dbmeta",
		"tablestore://k:P4ssw0rd@" + host + "/a/b",
		"tablestore://k:P4ssw0rd@" + host + "/dbmeta?extra=1",
		"tablestore://k:P4ssw0rd@" + host + "/dbmeta?tls=true&tls=true",
	} {
		_, err := tablestore.ParseDSN(dsn)
		if err == nil || strings.Contains(err.Error(), "P4ssw0rd") {
			t.Errorf("ParseDSN(%q) gave %v, which holds the secret or no error", dsn, err)
		}
	}
}

// TestOpenConnector holds that sql.Open takes the DSN through OpenConnector, and
// that a DSN with no keys opens no connection (D7).
func TestOpenConnector(t *testing.T) {
	t.Parallel()
	conn, err := tablestore.Driver{}.OpenConnector("tablestore://dbmeta.ap-southeast-5.ots.aliyuncs.com/dbmeta")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Connect(t.Context()); !errors.Is(err, tablestore.ErrNoCredentials) {
		t.Errorf("Connect gave %v, want ErrNoCredentials", err)
	}
	if _, err := (tablestore.Driver{}).OpenConnector("tablestore://k:s@localhost"); err == nil {
		t.Error("OpenConnector took a DSN that ParseDSN refuses")
	}
	if _, err := (tablestore.Driver{}).Open("tablestore://k:s@h/dbmeta"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("Open gave %v, want dbimp.ErrNotSupported", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"tablestore://k:s@dbmeta.ap-southeast-5.ots.aliyuncs.com/dbmeta",
		"tablestore://k:s@dbmeta.ap-southeast-5.ots.aliyuncs.com:8443/dbmeta?tls=false",
		"tablestore://%3A:%2F@h:1/%20",
		"tablestore://k:s@[::1]:1/db",
		"tablestore://k:s@h//",
		"tablestore://h/dbmeta?tls=1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := tablestore.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := tablestore.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
