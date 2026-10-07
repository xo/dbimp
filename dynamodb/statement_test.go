package dynamodb //nolint:testpackage // The test reads the shape of a statement, which the package does not export.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/xo/dbimp"
)

// TestShapeOf holds D163: the columns of a result are the names that the
// statement gives, in its order, and SELECT * gives one column that holds
// each item.
func TestShapeOf(t *testing.T) {
	t.Parallel()
	whole := shape{columns: []string{""}, whole: true}
	none := shape{columns: []string{}}
	for _, tt := range []struct {
		query string
		want  shape
	}{
		{"SELECT * FROM t", whole},
		{"select * from \"t\" where pk = 'x'", whole},
		{"SELECT * FROM t -- a comment", whole},
		{"/* a comment */ SELECT /* b */ * /* c */ FROM t", whole},
		{"SELECT pk FROM t", shape{columns: []string{"pk"}}},
		// The order is the order of the statement (recorded: "a projection").
		{"SELECT s, nul, \"absent\", nint, pk FROM dbimp_types WHERE pk = 't1'", shape{columns: []string{"s", "nul", "absent", "nint", "pk"}}},
		// A path arrives under its last part (recorded: "a projection of nested paths").
		{"SELECT m.k[1], l[0], m.z FROM dbimp_types WHERE pk = 't1'", shape{columns: []string{"k[1]", "l[0]", "z"}}},
		// A name in double quotes with a dot is one top level attribute.
		{"SELECT \"m.z\" FROM t", shape{columns: []string{"m.z"}}},
		{"SELECT \"a\".\"b\" FROM t", shape{columns: []string{"b"}}},
		{"SELECT \"it\"\"s\" FROM t", shape{columns: []string{"it\"s"}}},
		{"SELECT a,b FROM t", shape{columns: []string{"a", "b"}}},
		{"SELECT a FROM \"t\".\"gsi\" WHERE g = 'FROM'", shape{columns: []string{"a"}}},
		// The word from inside a name or a literal does not end the list.
		{"SELECT \"from\" FROM t", shape{columns: []string{"from"}}},
		// A statement that writes gives no item, unless it has RETURNING.
		{"INSERT INTO t VALUE {'pk': 'a', 'v': <<'x', 'y'>>}", none},
		{"UPDATE t SET v = 1 WHERE pk = 'a'", none},
		{"DELETE FROM t WHERE pk = 'a'", none},
		{"UPDATE t SET v = 1 WHERE pk = 'a' RETURNING ALL OLD *", whole},
		{"delete from t where pk = 'a' returning modified new *", whole},
		{"UPDATE t SET v = 'RETURNING' WHERE pk = 'a'", none},
		{"UPDATE t SET v = 1 WHERE pk = 'a' AND w < 3 RETURNING ALL NEW *", whole},
		{"INSERT INTO t VALUE {'pk': 'a', 'v': {'RETURNING': 1}}", none},
		// Any other statement gives one column, whatever the server answers.
		{"EXISTS(SELECT * FROM t WHERE pk = 'a')", whole},
		{"", whole},
		{"-- only a comment", whole},
		{"SELECT", shape{columns: []string{}}},
	} {
		if got := shapeOf(tt.query); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("shapeOf(%q) = %+v, want %+v", tt.query, got, tt.want)
		}
	}
}

// TestCountPlaceholders holds that a ? in a literal, a name or a comment is no
// placeholder (recorded: "a question mark in a string").
func TestCountPlaceholders(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query string
		want  int
	}{
		{"SELECT pk FROM t", 0},
		{"SELECT pk FROM t WHERE pk = ?", 1},
		{"SELECT pk FROM t WHERE pk = '?'", 0},
		{"SELECT pk FROM t WHERE \"a?\" = ? AND b = ?", 2},
		{"SELECT pk FROM t -- ?\nWHERE pk = ?", 1},
		{"SELECT pk FROM t /* ? */ WHERE pk = ?", 1},
		{"INSERT INTO t VALUE {'pk': ?, 'v': ?}", 2},
		// DynamoDB has no @name.
		{"SELECT pk FROM t WHERE pk = @x", 0},
	} {
		got, err := countPlaceholders(tt.query)
		if err != nil || got != tt.want {
			t.Errorf("countPlaceholders(%q) = %d, %v, want %d", tt.query, got, err, tt.want)
		}
	}
	for _, query := range []string{"SELECT 'x", "SELECT \"x", "SELECT /* x"} {
		if _, err := countPlaceholders(query); !errors.Is(err, dbimp.ErrUnterminated) {
			t.Errorf("countPlaceholders(%q) = %v, want dbimp.ErrUnterminated", query, err)
		}
	}
}

func FuzzShapeOf(f *testing.F) {
	for _, s := range []string{"SELECT a, b.c[1] FROM t", "UPDATE t SET v = 1 RETURNING ALL OLD *", "SELECT \"", "<<", "SELECT [[[ FROM"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, query string) {
		sh := shapeOf(query)
		if sh.columns == nil {
			t.Fatalf("shapeOf(%q) has nil columns", query)
		}
		if sh.whole && (len(sh.columns) != 1 || sh.columns[0] != "") {
			t.Fatalf("shapeOf(%q) = %+v, want one column named empty for a whole item", query, sh)
		}
	})
}
