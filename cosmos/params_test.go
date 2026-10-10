package cosmos_test

import (
	"database/sql"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"
)

// TestBoundValues holds D190: each Go value that a caller binds goes to the
// server as the JSON value that holds it. A decimal is a JSON number with all
// its digits, a []byte is its base64 text, a time is its RFC 3339 text, and a
// UUID is its text, because the server has no type for any of them. A list and
// a map are sent as they are.
func TestBoundValues(t *testing.T) {
	t.Parallel()
	dec, _, err := apd.NewFromString("12345678901234567890.123456789")
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	when := time.Date(2026, time.October, 10, 12, 30, 15, 123456789, time.UTC)
	var nilDecimal *apd.Decimal
	var nilUUID *uuid.UUID
	for _, tt := range []struct {
		name string
		arg  any
		want string
	}{
		{"a string", "s", `"s"`},
		{"an int", 5, `5`},
		{"an int64", int64(-9007199254740993), `-9007199254740993`},
		{"a float", 1.5, `1.5`},
		{"a bool", true, `true`},
		{"nil", nil, `null`},
		{"a decimal", dec, `12345678901234567890.123456789`},
		{"a decimal value", *dec, `12345678901234567890.123456789`},
		{"a nil decimal", nilDecimal, `null`},
		{"bytes", []byte{0, 1, 2, 0xff}, `"AAEC/w=="`},
		{"a time", when, `"2026-10-10T12:30:15.123456789Z"`},
		{"a UUID", id, `"123e4567-e89b-12d3-a456-426614174000"`},
		{"a nil UUID", nilUUID, `null`},
		{"a list", []any{1, "a", nil}, `[1,"a",null]`},
		{"a map", map[string]any{"a": []any{1}}, `{"a":[1]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := okFake()
			srv := httptest.NewServer(f)
			t.Cleanup(srv.Close)
			if err := drain(t, openFake(t, srv.URL, "c", ""), "SELECT @p AS v", sql.Named("p", tt.arg)); err != nil {
				t.Fatal(err)
			}
			body, _ := f.last(t)
			if want := `{"query":"SELECT @p AS v","parameters":[{"name":"@p","value":` + tt.want + `}]}`; body != want {
				t.Errorf("the body is %s, want %s", body, want)
			}
		})
	}
}

// TestADecimalThatIsNotAJSONNumber holds that a decimal that has no JSON form
// fails the statement before it sends anything.
func TestADecimalThatIsNotAJSONNumber(t *testing.T) {
	t.Parallel()
	f := okFake()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	for _, s := range []string{"NaN", "Infinity", "-Infinity"} {
		d, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := drain(t, openFake(t, srv.URL, "c", ""), "SELECT @p", sql.Named("p", d)); err == nil {
			t.Errorf("the decimal %s was bound, want an error", s)
		}
	}
	if f.requests() != 0 {
		t.Errorf("the driver sent %d requests, want none", f.requests())
	}
}

// TestSeveralParametersKeepTheirOrder holds that the list holds one entry for
// each argument, in the order of the arguments.
func TestSeveralParametersKeepTheirOrder(t *testing.T) {
	t.Parallel()
	f := okFake()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	if err := drain(t, openFake(t, srv.URL, "c", ""), "SELECT @b, @a", sql.Named("b", 1), sql.Named("a", 2)); err != nil {
		t.Fatal(err)
	}
	body, _ := f.last(t)
	if want := `{"query":"SELECT @b, @a","parameters":[{"name":"@b","value":1},{"name":"@a","value":2}]}`; body != want {
		t.Errorf("the body is %s, want %s", body, want)
	}
}
