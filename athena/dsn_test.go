package athena_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/athena"
)

// base returns the configuration of a DSN with the host of a region and the keys
// of a user.
func base(host, region string) athena.Config {
	return athena.Config{Host: host, TLS: true, Region: region, User: "key", Password: "secret"}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c athena.Config, f func(*athena.Config)) athena.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want athena.Config
	}{
		{"athena://key:secret@athena.us-east-1.amazonaws.com", base("athena.us-east-1.amazonaws.com", "us-east-1")},
		{"athena://key:secret@athena.us-east-1.amazonaws.com/", base("athena.us-east-1.amazonaws.com", "us-east-1")},
		{"athena://key:secret@athena.eu-west-2.amazonaws.com/dbimp_test?workgroup=dbimp", with(base("athena.eu-west-2.amazonaws.com", "eu-west-2"), func(c *athena.Config) {
			c.Database, c.WorkGroup = "dbimp_test", "dbimp"
		})},
		{"athena://key:secret@athena.us-east-1.amazonaws.com/db?output=s3%3A%2F%2Fbucket%2Fresults%2F&catalog=dbimp-cw", with(base("athena.us-east-1.amazonaws.com", "us-east-1"), func(c *athena.Config) {
			c.Database, c.Output, c.Catalog = "db", "s3://bucket/results/", "dbimp-cw"
		})},
		// A secret key has characters that the URL escapes.
		{"athena://AKIAEXAMPLE:wJalr%2FXUtn%2BFEMI%2FK7MDENG@athena.us-east-1.amazonaws.com", with(base("athena.us-east-1.amazonaws.com", "us-east-1"), func(c *athena.Config) {
			c.User, c.Password = "AKIAEXAMPLE", "wJalr/XUtn+FEMI/K7MDENG"
		})},
		{"athena://key:secret@athena.us-east-1.amazonaws.com?token=IQoJb3%2FJpZ%2Bk%3D", with(base("athena.us-east-1.amazonaws.com", "us-east-1"), func(c *athena.Config) {
			c.Token = "IQoJb3/JpZ+k="
		})},
		// A private endpoint, a FIPS endpoint and a port.
		{"athena://key:secret@vpce-0a1.athena.us-west-2.vpce.amazonaws.com:8443", with(base("vpce-0a1.athena.us-west-2.vpce.amazonaws.com", "us-west-2"), func(c *athena.Config) { c.Port = 8443 })},
		{"athena://key:secret@athena-fips.us-gov-west-1.amazonaws.com", base("athena-fips.us-gov-west-1.amazonaws.com", "us-gov-west-1")},
		{"athena://key:secret@athena.cn-north-1.amazonaws.com.cn", base("athena.cn-north-1.amazonaws.com.cn", "cn-north-1")},
		// The keys can be left out, and then the connector refuses to connect.
		{"athena://athena.us-east-1.amazonaws.com/db", athena.Config{Host: "athena.us-east-1.amazonaws.com", TLS: true, Region: "us-east-1", Database: "db"}},
		{"athena://key:secret@athena.us-east-1.amazonaws.com/db%20name", with(base("athena.us-east-1.amazonaws.com", "us-east-1"), func(c *athena.Config) { c.Database = "db name" })},
	} {
		got, err := athena.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := athena.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D192: the host holds the region, the path is one
// database, and a key that is unknown or repeated is refused.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	const host = "athena.us-east-1.amazonaws.com"
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"athenas://k:s@" + host, dbimp.ErrScheme},
		{"awsathena://k:s@" + host, dbimp.ErrScheme},
		{"s3://k:s@" + host, dbimp.ErrScheme},
		{"http://k:s@" + host, dbimp.ErrScheme},
		{"athena://k:s@", dbimp.ErrInvalidValue},
		{"athena://k:s@localhost", dbimp.ErrInvalidValue},
		{"athena://k:s@127.0.0.1:8000", dbimp.ErrInvalidValue},
		{"athena://k:s@athena.amazonaws.com", dbimp.ErrInvalidValue},
		{"athena://k:s@athena..amazonaws.com", dbimp.ErrInvalidValue},
		{"athena://k:s@athena.us_east.amazonaws.com", dbimp.ErrInvalidValue},
		{"athena://k:s@" + host + ":0", dbimp.ErrInvalidValue},
		{"athena://k:s@" + host + ":65536", dbimp.ErrInvalidValue},
		{"athena://k:s@" + host + "/db/table", dbimp.ErrInvalidValue},
		{"athena://k:s@" + host + "/db/", dbimp.ErrInvalidValue},
		{"athena://k@" + host, dbimp.ErrInvalidValue},
		{"athena://:s@" + host, dbimp.ErrInvalidValue},
		{"athena://k:s@" + host + "?region=us-east-1", dbimp.ErrUnknownKey},
		{"athena://k:s@" + host + "?tls=false", dbimp.ErrUnknownKey},
		{"athena://k:s@" + host + "?timeout=1s", dbimp.ErrUnknownKey},
		{"athena://k:s@" + host + "?secret=x", dbimp.ErrUnknownKey},
		{"athena://k:s@" + host + "?WorkGroup=x", dbimp.ErrUnknownKey},
		{"athena://k:s@" + host + "?workgroup=a&workgroup=b", dbimp.ErrRepeatedKey},
		{"athena://k:s@" + host + "?output=a&output=b", dbimp.ErrRepeatedKey},
		{"athena://k:s@" + host + "?token=a&token=b", dbimp.ErrRepeatedKey},
		{"athena://k:s@" + host + "?catalog=a&catalog=b", dbimp.ErrRepeatedKey},
	} {
		if _, err := athena.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

// TestParseDSNHidesThePassword holds that no error holds the secret key or the
// session token (D94).
func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{
		"athena://k:P4ssw0rd@bad host",
		"athena://k:P4ssw0rd@localhost",
		"athena://k:P4ssw0rd@athena.us-east-1.amazonaws.com:99999",
		"athena://k:P4ssw0rd@athena.us-east-1.amazonaws.com/a/b",
		"athena://k:P4ssw0rd@athena.us-east-1.amazonaws.com?extra=1",
		"athena://k:P4ssw0rd@athena.us-east-1.amazonaws.com?token=T0kenValue&extra=1",
		"athena://k:P4ssw0rd@athena.us-east-1.amazonaws.com?token=T0kenValue&token=T0kenValue",
		"athena://k:P4ssw0rd@athena.us-east-1.amazonaws.com?workgroup=a&workgroup=b&token=T0kenValue",
	} {
		_, err := athena.ParseDSN(dsn)
		if err == nil || strings.Contains(err.Error(), "P4ssw0rd") || strings.Contains(err.Error(), "T0kenValue") {
			t.Errorf("ParseDSN(%q) gave %v, which holds a secret or no error", dsn, err)
		}
	}
}

// TestOpenConnector holds that sql.Open takes the DSN through OpenConnector, and
// that a DSN with no keys opens no connection (D7).
func TestOpenConnector(t *testing.T) {
	t.Parallel()
	conn, err := athena.Driver{}.OpenConnector("athena://athena.us-east-1.amazonaws.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Connect(t.Context()); !errors.Is(err, athena.ErrNoCredentials) {
		t.Errorf("Connect gave %v, want ErrNoCredentials", err)
	}
	if _, err := (athena.Driver{}).OpenConnector("athena://k:s@localhost"); err == nil {
		t.Error("OpenConnector took a DSN that ParseDSN refuses")
	}
	if _, err := (athena.Driver{}).Open("athena://k:s@athena.us-east-1.amazonaws.com"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("Open gave %v, want dbimp.ErrNotSupported", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"athena://k:s@athena.us-east-1.amazonaws.com",
		"athena://k:s@athena.us-east-1.amazonaws.com/db?workgroup=w&output=s3%3A%2F%2Fb%2F&catalog=c&token=t",
		"athena://%3A:%2F@vpce-1.athena.eu-west-1.vpce.amazonaws.com:1",
		"athena://athena.a-b.c/%20",
		"athena://k:s@[::1]:1/db",
		"athena://k:s@athena-fips.us-gov-west-1.amazonaws.com//",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := athena.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := athena.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
