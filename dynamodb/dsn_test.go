package dynamodb_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dynamodb"
)

// cfg returns the configuration of a DSN with the keys that every DSN needs.
func cfg(host string, port int) dynamodb.Config {
	return dynamodb.Config{Host: host, Port: port, TLS: true, Region: "us-east-1", User: "key", Password: "secret"}
}

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	with := func(c dynamodb.Config, f func(*dynamodb.Config)) dynamodb.Config {
		f(&c)
		return c
	}
	for _, tt := range []struct {
		dsn  string
		want dynamodb.Config
	}{
		{"dynamodb://key:secret@dynamodb.us-east-1.amazonaws.com?region=us-east-1", cfg("dynamodb.us-east-1.amazonaws.com", 0)},
		{"dynamodb://key:secret@localhost/?region=us-east-1", cfg("localhost", 0)},
		{"dynamodb://dbmeta:P4ssw0rd%21x@127.0.0.1:55135?region=us-east-1&tls=false", with(cfg("127.0.0.1", 55135), func(c *dynamodb.Config) {
			c.User, c.Password, c.TLS = "dbmeta", "P4ssw0rd!x", false
		})},
		{"dynamodb://key:secret@[::1]:8000?region=eu-west-2&tls=true", with(cfg("::1", 8000), func(c *dynamodb.Config) { c.Region = "eu-west-2" })},
		// The secret of Alternator is a hash with a slash, which the URL escapes.
		{"dynamodb://cassandra:%246%24salt%24Tw%2F8k@h:8000?region=us-east-1", with(cfg("h", 8000), func(c *dynamodb.Config) {
			c.User, c.Password = "cassandra", "$6$salt$Tw/8k"
		})},
		{"dynamodb://key:secret@h:8000?region=us-east-1&token=IQoJb3%2FJpZ%2Bk%3D", with(cfg("h", 8000), func(c *dynamodb.Config) { c.Token = "IQoJb3/JpZ+k=" })},
		{"dynamodb://key:secret@h:8000?region=us-east-1&tls=false", with(cfg("h", 8000), func(c *dynamodb.Config) { c.TLS = false })},
	} {
		got, err := dynamodb.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := dynamodb.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D169: a path is refused, the region has no
// default, tls is true by default, and every other key is refused.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"dynamodbs://k:s@localhost?region=r", dbimp.ErrScheme},
		{"godynamo://k:s@localhost?region=r", dbimp.ErrScheme},
		{"http://k:s@localhost?region=r", dbimp.ErrScheme},
		{"dynamodb://k:s@localhost", dbimp.ErrInvalidValue},
		{"dynamodb://k:s@localhost?region=", dbimp.ErrInvalidValue},
		{"dynamodb://k:s@localhost?region=us/east", dbimp.ErrInvalidValue},
		{"dynamodb://k:s@localhost/table?region=r", dbimp.ErrInvalidValue},
		{"dynamodb://k:s@localhost?region=r&Endpoint=http://x", dbimp.ErrUnknownKey},
		{"dynamodb://k:s@localhost?region=r&timeout=1s", dbimp.ErrUnknownKey},
		{"dynamodb://k:s@localhost?region=r&secret=x", dbimp.ErrUnknownKey},
		{"dynamodb://k:s@localhost?region=r&token=a&token=b", dbimp.ErrRepeatedKey},
		{"dynamodb://k:s@localhost?region=r&region=q", dbimp.ErrRepeatedKey},
		{"dynamodb://k:s@localhost?region=r&tls=true&tls=false", dbimp.ErrRepeatedKey},
		{"dynamodb://k:s@localhost?region=r&tls=maybe", dbimp.ErrInvalidValue},
		{"dynamodb://localhost?region=r", dbimp.ErrInvalidValue},
		{"dynamodb://k@localhost?region=r", dbimp.ErrInvalidValue},
		{"dynamodb://:s@localhost?region=r", dbimp.ErrInvalidValue},
		{"dynamodb://k:s@localhost:0?region=r", dbimp.ErrInvalidValue},
		{"dynamodb://k:s@localhost:65536?region=r", dbimp.ErrInvalidValue},
		{"dynamodb://k:s@?region=r", dbimp.ErrInvalidValue},
		{"dynamodb://k:s@::?region=r", dbimp.ErrInvalidValue},
	} {
		if _, err := dynamodb.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

// TestParseDSNHidesThePassword holds that no error holds the secret key or the
// session token.
func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{
		"dynamodb://k:P4ssw0rd@bad host?region=r",
		"dynamodb://k:P4ssw0rd@h:99999?region=r",
		"dynamodb://k:P4ssw0rd@h?region=r&tls=maybe",
		"dynamodb://k:P4ssw0rd@h?extra=1",
		"dynamodb://k:P4ssw0rd@h/path?region=r",
		"dynamodb://k:P4ssw0rd@h?region=r&token=T0kenValue&tls=maybe",
		"dynamodb://k:P4ssw0rd@h?region=r&token=T0kenValue&token=T0kenValue",
		"dynamodb://k:P4ssw0rd@h?region=r&token=T0kenValue&extra=1",
	} {
		_, err := dynamodb.ParseDSN(dsn)
		if err == nil || strings.Contains(err.Error(), "P4ssw0rd") || strings.Contains(err.Error(), "T0kenValue") {
			t.Errorf("ParseDSN(%q) gave %v, which holds a secret or no error", dsn, err)
		}
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"dynamodb://k:s@localhost?region=us-east-1",
		"dynamodb://k:s@[::1]:1?region=a-b&tls=false",
		"dynamodb://%3A:%2F@h?region=R",
		"dynamodb://k:s@h?region=r&token=a%2Fb%2B%3D",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := dynamodb.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := dynamodb.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
