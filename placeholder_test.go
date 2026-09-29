package dbimp_test

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

// sqlSyntax is the syntax of standard SQL, with MySQL's backquote.
var sqlSyntax = dbimp.Syntax{
	Quotes:        "'\"`",
	DashComments:  true,
	BlockComments: true,
}

func TestPlaceholders(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query string
		want  []string
	}{
		{"SELECT ?, ?", []string{"?", "?"}},
		{"SELECT @a, @b_2 FROM t WHERE x = ?", []string{"@a", "@b_2", "?"}},
		{"SELECT '?', \"@a\", `?` FROM t", nil},
		{"SELECT 'it''s ?' , ?", []string{"?"}},
		{"SELECT ? -- a ? comment\n, ?", []string{"?", "?"}},
		{"SELECT /* ? @a */ ?", []string{"?"}},
		{"SELECT @@version, @", nil},
		{"SELECT @1", nil},
		{"SELECT ? -- a comment to the end", []string{"?"}},
	} {
		ps, err := sqlSyntax.Placeholders(tt.query)
		if err != nil {
			t.Errorf("Placeholders(%q): %v", tt.query, err)
			continue
		}
		var got []string
		for _, p := range ps {
			got = append(got, tt.query[p.Offset:p.Offset+p.Len])
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("Placeholders(%q) = %q, want %q", tt.query, got, tt.want)
		}
	}
}

// TestPlaceholdersAQL holds the forms of AQL: a // comment, @@name for a
// collection, and a name that starts with a digit (D104).
func TestPlaceholdersAQL(t *testing.T) {
	t.Parallel()
	aql := dbimp.Syntax{Quotes: "'\"`", Backslash: true, BlockComments: true, SlashComments: true, DoubleAt: true, DigitNames: true}
	query := "FOR d IN @@c FILTER d.a == @1 AND d.s == '@@x and @y' // @@z @w\nRETURN /* @v */ @b"
	ps, err := aql.Placeholders(query)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range ps {
		got = append(got, fmt.Sprintf("%s %t", p.Name, p.Double))
	}
	if want := []string{"c true", "1 false", "b false"}; !slices.Equal(got, want) {
		t.Errorf("Placeholders(%q) = %q, want %q", query, got, want)
	}
}

func TestPlaceholdersUnterminated(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"SELECT 'x", "SELECT /* x", "SELECT \"x"} {
		if _, err := sqlSyntax.Placeholders(query); !errors.Is(err, dbimp.ErrUnterminated) {
			t.Errorf("Placeholders(%q) = %v, want ErrUnterminated", query, err)
		}
	}
}

func TestPlaceholdersBackslash(t *testing.T) {
	t.Parallel()
	s := dbimp.Syntax{Quotes: "'", Backslash: true, HashComments: true}
	ps, err := s.Placeholders(`SELECT 'a\'?', ? # ?`)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 {
		t.Errorf("found %d placeholders, want 1", len(ps))
	}
}

// literal writes a value as a literal of standard SQL.
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'", nil
	}
	return "", fmt.Errorf("writing %T: %w", v, dbimp.ErrNotSupported)
}

func TestBind(t *testing.T) {
	t.Parallel()
	args := []driver.NamedValue{
		{Ordinal: 1, Value: int64(1)},
		{Ordinal: 2, Name: "name", Value: "o'hara"},
		{Ordinal: 3, Value: nil},
	}
	got, err := sqlSyntax.Bind("SELECT ?, @name, '?' FROM t WHERE x = ?", args, literal)
	if err != nil {
		t.Fatal(err)
	}
	if want := "SELECT 1, 'o''hara', '?' FROM t WHERE x = NULL"; got != want {
		t.Errorf("Bind = %q, want %q", got, want)
	}
}

func TestBindMismatch(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
	}{
		{"SELECT ?, ?", []driver.NamedValue{{Ordinal: 1, Value: int64(1)}}},
		{"SELECT ?", []driver.NamedValue{{Ordinal: 1, Value: int64(1)}, {Ordinal: 2, Value: int64(2)}}},
		{"SELECT @a", nil},
		{"SELECT 1", []driver.NamedValue{{Ordinal: 1, Name: "a", Value: int64(1)}}},
	} {
		if _, err := sqlSyntax.Bind(tt.query, tt.args, literal); !errors.Is(err, dbimp.ErrArguments) {
			t.Errorf("Bind(%q) = %v, want ErrArguments", tt.query, err)
		}
	}
}

func FuzzPlaceholders(f *testing.F) {
	for _, s := range []string{"SELECT ?", "SELECT '?' -- ?\n @a /* @b */", "@@x @ ? '' \"\"", "'\\'"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, query string) {
		ps, err := sqlSyntax.Placeholders(query)
		if err != nil {
			return
		}
		last := 0
		for _, p := range ps {
			if p.Offset < last || p.Offset+p.Len > len(query) {
				t.Fatalf("placeholder %+v is out of order or out of range in %q", p, query)
			}
			if c := query[p.Offset]; c != '?' && c != '@' {
				t.Fatalf("placeholder %+v of %q starts with %q", p, query, c)
			}
			last = p.Offset + p.Len
		}
	})
}
