package avatica_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/avatica"
)

func TestDSNRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want avatica.Config
	}{
		{"avatica://localhost", avatica.Config{Host: "localhost", Port: 8765, Auth: "none"}},
		{"avatica://SA@127.0.0.1:55001", avatica.Config{Host: "127.0.0.1", Port: 55001, User: "SA", Auth: "none"}},
		{"avatica://dbmeta_user:p%21@127.0.0.1:55001/", avatica.Config{Host: "127.0.0.1", Port: 55001, User: "dbmeta_user", Password: "p!", Auth: "none"}},
		{"avatica://u:p@[::1]:8765?tls=true&auth=basic", avatica.Config{Host: "::1", Port: 8765, TLS: true, User: "u", Password: "p", Auth: "basic"}},
		{"avatica://phoenix@h:8765?auth=none", avatica.Config{Host: "h", Port: 8765, User: "phoenix", Auth: "none"}},
	} {
		got, err := avatica.ParseDSN(tt.dsn)
		if err != nil {
			t.Errorf("ParseDSN(%q): %v", tt.dsn, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("ParseDSN(%q) = %+v, want %+v", tt.dsn, *got, tt.want)
		}
		again, err := avatica.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Errorf("ParseDSN(FormatDSN(%q)) = %q: %v", tt.dsn, got.FormatDSN(), err)
			continue
		}
		if !reflect.DeepEqual(again, got) {
			t.Errorf("the round trip of %q gave %+v, want %+v", tt.dsn, *again, *got)
		}
	}
}

// TestParseDSNRefuses holds D156: a path is refused, and so is every key
// that the driver does not know, such as a key of the JDBC driver of
// Avatica.
func TestParseDSNRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dsn  string
		want error
	}{
		{"phoenix://h", dbimp.ErrScheme},
		{"http://h:8765", dbimp.ErrScheme},
		{"avatica://h/db", dbimp.ErrInvalidValue},
		{"avatica://h?serialization=json", dbimp.ErrUnknownKey},
		{"avatica://h?authentication=BASIC", dbimp.ErrUnknownKey},
		{"avatica://h?tls=0&tls=1", dbimp.ErrRepeatedKey},
		{"avatica://h?tls=maybe", dbimp.ErrInvalidValue},
		{"avatica://h?auth=bearer", dbimp.ErrInvalidValue},
		{"avatica://h?auth=", dbimp.ErrInvalidValue},
		{"avatica://h:0", dbimp.ErrInvalidValue},
		{"avatica://h:65536", dbimp.ErrInvalidValue},
		{"avatica://", dbimp.ErrInvalidValue},
	} {
		if _, err := avatica.ParseDSN(tt.dsn); !errors.Is(err, tt.want) {
			t.Errorf("ParseDSN(%q) = %v, want %v", tt.dsn, err, tt.want)
		}
	}
}

func TestParseDSNHidesThePassword(t *testing.T) {
	t.Parallel()
	_, err := avatica.ParseDSN("avatica://u:secret@bad host")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("ParseDSN of a bad host gave %v, which holds the password or no error", err)
	}
}

func FuzzParseDSN(f *testing.F) {
	for _, s := range []string{
		"avatica://localhost",
		"avatica://SA@127.0.0.1:55001",
		"avatica://u:p@[::1]:1?tls=true&auth=basic",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dsn string) {
		got, err := avatica.ParseDSN(dsn)
		if err != nil {
			return
		}
		again, err := avatica.ParseDSN(got.FormatDSN())
		if err != nil {
			t.Fatalf("ParseDSN(%q) took it, and its FormatDSN %q fails: %v", dsn, got.FormatDSN(), err)
		}
		if !reflect.DeepEqual(again, got) {
			t.Fatalf("the round trip of %q gave %+v, want %+v", dsn, *again, *got)
		}
	})
}
