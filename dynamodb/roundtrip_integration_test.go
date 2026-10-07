package dynamodb_test

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/dynamodb"
)

// d returns the decimal of text.
func d(t *testing.T, text string) *apd.Decimal {
	t.Helper()
	v, _, err := apd.NewFromString(text)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// maxStatement is the most characters that the server takes in a statement. A
// longer one fails with "Member must have length less than or equal to 8192"
// (measured on 3.2.0 and 3.3.1 on 2026-10-07).
const maxStatement = 8192

// literal writes v as a literal of PartiQL. DynamoDB has no literal for a
// binary value, so a value that holds one returns an error that wraps
// dbimp.ErrNotSupported, and the round trip goes on with the argument alone.
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "null", nil
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'", nil
	case bool:
		return strconv.FormatBool(v), nil
	case *apd.Decimal:
		return v.Text('f'), nil
	case []byte:
		return "", fmt.Errorf("a binary value: PartiQL has no literal for it: %w", dbimp.ErrNotSupported)
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			s, err := literal(e)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			s, err := literal(v[k])
			if err != nil {
				return "", err
			}
			parts[i] = literal2(k) + ": " + s
		}
		return "{" + strings.Join(parts, ", ") + "}", nil
	case dynamodb.Set:
		parts := make([]string, len(v))
		for i, e := range v {
			s, err := literal(e)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "<<" + strings.Join(parts, ", ") + ">>", nil
	}
	return "", fmt.Errorf("the type %T: %w", v, dbimp.ErrNotSupported)
}

// literal2 writes a key of a map as a literal string.
func literal2(k string) string {
	s, _ := literal(k)
	return s
}

// equalValue compares a value that the driver read with the value wanted:
// decimals by their number, bytes by their content, and lists and maps by
// their elements. A []any is a list, so its order counts, and it is a set
// when unordered is true, because the server sorts a set (recorded: "every
// type").
func equalValue(got, want any, unordered bool) bool {
	switch w := want.(type) {
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case []byte:
		g, ok := got.([]byte)
		return ok && bytes.Equal(g, w)
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		if unordered {
			a, b := make([]string, len(g)), make([]string, len(w))
			for i := range g {
				a[i], b[i] = show(g[i]), show(w[i])
			}
			slices.Sort(a)
			slices.Sort(b)
			return slices.Equal(a, b)
		}
		for i := range g {
			if !equalValue(g[i], w[i], false) {
				return false
			}
		}
		return true
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for k, e := range w {
			ge, ok := g[k]
			if !ok || !equalValue(ge, e, false) {
				return false
			}
		}
		return true
	}
	return got == want
}

// TestIntegrationRoundTrip runs dbimptest.RoundTrip for each type that
// features.json marks yes, and shows that the server refuses the type that it
// marks no (step 14a of docs/DRIVER.md). A column has no type in DynamoDB, so
// the table has the key pk and one attribute v, which holds each value in turn.
func TestIntegrationRoundTrip(t *testing.T) {
	adminConfig(t)
	name := q(table(t, "types", attrs(false)))
	db := openAs(t, admin)
	long := strings.Repeat("x", 50000)
	bin := make([]byte, 100000)
	for i := range bin {
		bin[i] = byte(i)
	}
	max38 := strings.Repeat("9", 38)
	num := func(text string) *apd.Decimal { return d(t, text) }
	var strs, nums []any
	for i := range 500 {
		strs = append(strs, fmt.Sprintf("s%04d", i))
		nums = append(nums, num(strconv.Itoa(i)))
	}

	run := func(typ string, unordered bool, values ...dbimptest.Value) {
		t.Run(typ, func(t *testing.T) {
			dbimptest.RoundTrip(t, db, dbimptest.RoundTripCase{
				Type:   typ,
				Insert: "INSERT INTO " + name + " VALUE {'pk': ?, 'v': ?}",
				Select: "SELECT v FROM " + name + " WHERE pk = ?",
				Update: "UPDATE " + name + " SET v = ? WHERE pk = ?",
				Delete: "DELETE FROM " + name + " WHERE pk = ?",
				Values: values,
				Equal:  func(got, want any) bool { return equalValue(got, want, unordered) },
				Literal: func(key string, v any) (string, error) {
					s, err := literal(v)
					if err != nil {
						return "", err
					}
					stmt := "INSERT INTO " + name + " VALUE {'pk': '" + key + "', 'v': " + s + "}"
					if len(stmt) > maxStatement {
						return "", fmt.Errorf("a statement of %d characters: the server takes at most %d: %w", len(stmt), maxStatement, dbimp.ErrNotSupported)
					}
					return stmt, nil
				},
			})
		})
	}
	run("S", false,
		dbimptest.Value{Name: "empty", In: ""},
		dbimptest.Value{Name: "unicode", In: "é'\"\\ 日本 😀 \n\t"},
		dbimptest.Value{Name: "a letter", In: "x"},
		dbimptest.Value{Name: "long", In: long},
	)
	run("N", false,
		dbimptest.Value{Name: "zero", In: num("0")},
		dbimptest.Value{Name: "smallest", In: num("1E-130")},
		dbimptest.Value{Name: "largest", In: num(max38 + "E+88")},
		dbimptest.Value{Name: "most negative", In: num("-" + max38 + "E+88")},
		dbimptest.Value{Name: "38 digits", In: num("12345678901234567890123456789012345678")},
		dbimptest.Value{Name: "38 digits of a fraction", In: num("0.12345678901234567890123456789012345678")},
		dbimptest.Value{Name: "a fraction", In: num("-0.5")},
	)
	run("B", false,
		dbimptest.Value{Name: "empty", In: []byte{}},
		dbimptest.Value{Name: "a zero and a byte of 255", In: []byte{0, 0xff}},
		dbimptest.Value{Name: "long", In: bin},
	)
	run("BOOL", false,
		dbimptest.Value{Name: "false", In: false},
		dbimptest.Value{Name: "true", In: true},
	)
	run("NULL", false,
		dbimptest.Value{Name: "null", In: nil},
		dbimptest.Value{Name: "null again", In: nil},
	)
	run("L", false,
		dbimptest.Value{Name: "empty", In: []any{}},
		dbimptest.Value{Name: "every type", In: []any{"a", num("1.5"), true, nil, []byte{1}, []any{"nested"}, map[string]any{"k": "v"}}},
		dbimptest.Value{Name: "long", In: nums},
	)
	run("M", false,
		dbimptest.Value{Name: "empty", In: map[string]any{}},
		dbimptest.Value{Name: "every type", In: map[string]any{"s": "x", "n": num("42"), "b": true, "nul": nil, "bin": []byte{0xff}, "l": []any{num("1")}, "m": map[string]any{}}},
		dbimptest.Value{Name: "unicode keys", In: map[string]any{"é": "ü", "日本": map[string]any{"😀": num("1")}}},
	)
	run("SS", true,
		dbimptest.Value{Name: "one", In: dynamodb.Set{"a"}, Want: []any{"a"}},
		dbimptest.Value{Name: "three", In: dynamodb.Set{"b", "é", "a"}, Want: []any{"a", "b", "é"}},
		dbimptest.Value{Name: "long", In: dynamodb.Set(strs), Want: strs},
	)
	run("NS", true,
		dbimptest.Value{Name: "one", In: dynamodb.Set{num("1")}, Want: []any{num("1")}},
		dbimptest.Value{Name: "four", In: dynamodb.Set{num("2.5"), num("1"), num("12345678901234567890123456789012345678"), num("1E-130")}, Want: []any{num("1"), num("2.5"), num("12345678901234567890123456789012345678"), num("1E-130")}},
		dbimptest.Value{Name: "long", In: dynamodb.Set(nums), Want: nums},
	)
	run("BS", true,
		dbimptest.Value{Name: "one", In: dynamodb.Set{[]byte{1}}, Want: []any{[]byte{1}}},
		dbimptest.Value{Name: "two", In: dynamodb.Set{[]byte{0, 0xff}, []byte{1}}, Want: []any{[]byte{0, 0xff}, []byte{1}}},
	)
	t.Run("DATE", func(t *testing.T) {
		// DynamoDB has no date, time, UUID or decimal type of its own. The
		// driver refuses a time, and the server refuses a date literal and a
		// cast (recorded: "lead: a date literal" and "lead: a cast").
		_, err := db.ExecContext(t.Context(), "INSERT INTO "+name+" VALUE {'pk': ?, 'v': ?}", "date", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("a time as an argument: %v, want dbimp.ErrNotSupported", err)
		}
		refused(t, db, "ValidationException", "INSERT INTO "+name+" VALUE {'pk': 'date', 'v': DATE '2026-10-01'}")
		refused(t, db, "ValidationException", "SELECT pk FROM "+name+" WHERE pk = CAST('t1' AS STRING)")
		refused(t, db, "ValidationException", "INSERT INTO "+name+" VALUE {'pk': 'empty set', 'v': <<>>}")
	})
	t.Run("nothing is left", func(t *testing.T) {
		same(t, "the table", rowsOf(t, db, "SELECT pk FROM "+name))
	})
}
