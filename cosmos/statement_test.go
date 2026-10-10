package cosmos //nolint:testpackage // The tests call the function that strips a semicolon, which is not exported.

import "testing"

func TestStripSemicolon(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, want string }{
		{"SELECT 1", "SELECT 1"},
		{"SELECT 1;", "SELECT 1"},
		{"SELECT 1 ;", "SELECT 1 "},
		{"SELECT 1;\n", "SELECT 1\n"},
		{"SELECT 1;  \t\r\n", "SELECT 1  \t\r\n"},
		// Only one semicolon goes, and the server answers the other (recorded:
		// "two statements in one request").
		{"SELECT 1;;", "SELECT 1;"},
		{"SELECT 1; SELECT 2", "SELECT 1; SELECT 2"},
		{"SELECT 1; SELECT 2;", "SELECT 1; SELECT 2"},
		{";", ""},
		{"", ""},
		// A semicolon in a string or in a comment is no token.
		{"SELECT ';'", "SELECT ';'"},
		{`SELECT ";"`, `SELECT ";"`},
		{`SELECT 'a\';'`, `SELECT 'a\';'`},
		{`SELECT 'a\'';`, `SELECT 'a\''`},
		{`SELECT c["a;"]`, `SELECT c["a;"]`},
		{"SELECT 1 -- a;", "SELECT 1 -- a;"},
		{"SELECT 1; -- a", "SELECT 1 -- a"},
		{"SELECT 1 -- a\n;", "SELECT 1 -- a\n"},
		{"SELECT 1 -- a;\n;", "SELECT 1 -- a;\n"},
		{"SELECT 1 --", "SELECT 1 --"},
		{"SELECT 1 - 2;", "SELECT 1 - 2"},
		// A block comment is no comment to the server (recorded: "a block
		// comment"), so a semicolon in it is a token, and it stays.
		{"SELECT 1 /* ; */", "SELECT 1 /* ; */"},
		{"SELECT 1 /* a */;", "SELECT 1 /* a */"},
		// A string that does not end is the server's to refuse.
		{"SELECT 'a;", "SELECT 'a;"},
		{`SELECT 'a\`, `SELECT 'a\`},
	} {
		if got := stripSemicolon(tt.in); got != tt.want {
			t.Errorf("stripSemicolon(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// FuzzStripSemicolon holds that the function removes at most one byte, and that
// the byte is a semicolon.
func FuzzStripSemicolon(f *testing.F) {
	for _, s := range []string{"SELECT 1;", "SELECT ';' -- ;\n;", `SELECT "a\"";`, "--", "'", ";;"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := stripSemicolon(in)
		switch {
		case got == in:
		case len(got) != len(in)-1:
			t.Fatalf("stripSemicolon(%q) = %q, which is not the statement or the statement with one byte less", in, got)
		default:
			i := 0
			for i < len(got) && got[i] == in[i] {
				i++
			}
			if in[i] != ';' || in[i+1:] != got[i:] {
				t.Fatalf("stripSemicolon(%q) = %q, which dropped more than a semicolon", in, got)
			}
		}
	})
}
