package libsql_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/libsql"
)

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want libsql.Config
	}{
		{"libsql://mydb-myorg.turso.io", libsql.Config{Host: "mydb-myorg.turso.io", Port: 443, Auth: "bearer"}},
		{"libsql://:eyJ.token@mydb-myorg.turso.io/", libsql.Config{Host: "mydb-myorg.turso.io", Port: 443, Password: "eyJ.token", Auth: "bearer"}},
		{"libsql://admin:eyJ.tok@127.0.0.1:55078?tls=false", libsql.Config{Host: "127.0.0.1", Port: 55078, NoTLS: true, User: "admin", Password: "eyJ.tok", Auth: "bearer"}},
		{"libsql://u:p%21@[::1]:8080?tls=false&auth=basic", libsql.Config{Host: "::1", Port: 8080, NoTLS: true, User: "u", Password: "p!", Auth: "basic"}},
		{"libsql://h:8443?namespace=tenant1", libsql.Config{Host: "h", Port: 8443, Auth: "bearer", Namespace: "tenant1"}},
	} {
		got, err := libsql.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := libsql.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D148: a path is refused, tls=false needs a port,
// and every key that the driver does not know is refused, such as a key of
// the Go client of libSQL.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"turso://h", dbimp.ErrScheme},
		{"https://h", dbimp.ErrScheme},
		{"libsql://h/db", dbimp.ErrInvalidValue},
		{"libsql://h?tls=false", dbimp.ErrInvalidValue},
		{"libsql://h?authToken=x", dbimp.ErrUnknownKey},
		{"libsql://h?auth_token=x", dbimp.ErrUnknownKey},
		{"libsql://h?tls=0&tls=1", dbimp.ErrRepeatedKey},
		{"libsql://h?tls=maybe", dbimp.ErrInvalidValue},
		{"libsql://h?auth=jwt", dbimp.ErrInvalidValue},
		{"libsql://h?namespace=", dbimp.ErrInvalidValue},
		{"libsql://h:0", dbimp.ErrInvalidValue},
		{"libsql://h:65536", dbimp.ErrInvalidValue},
		{"libsql://", dbimp.ErrInvalidValue},
	} {
		if _, err := libsql.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := libsql.ParseDSN("libsql://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"libsql://mydb-myorg.turso.io",
		"libsql://u:p@[::1]:1?tls=false&auth=basic",
		"libsql://:tok@h:8080?tls=false&namespace=n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := libsql.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := libsql.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
