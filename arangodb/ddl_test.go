package arangodb //nolint:testpackage // These tests read the parser of the DDL, which is not exported.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/xo/dbimp"
)

func TestParseDDL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query string
		want  *ddl
	}{
		{"CREATE COLLECTION c", &ddl{verb: "CREATE", name: "c"}},
		{"create collection if not exists `my c` edge", &ddl{verb: "CREATE", name: "my c", ifExists: true, edge: true}},
		{"DROP COLLECTION IF EXISTS c", &ddl{verb: "DROP", drop: true, ifExists: true, name: "c"}},
		{"CREATE INDEX i ON c (a, b.c)", &ddl{verb: "CREATE", index: true, kind: "persistent", name: "i", coll: "c", fields: []string{"a", "b.c"}}},
		{"CREATE UNIQUE SPARSE INDEX IF NOT EXISTS i ON c (a)",
			&ddl{verb: "CREATE", index: true, kind: "persistent", unique: true, sparse: true, ifExists: true, name: "i", coll: "c", fields: []string{"a"}}},
		{"CREATE GEO INDEX g ON c (loc)", &ddl{verb: "CREATE", index: true, kind: "geo", name: "g", coll: "c", fields: []string{"loc"}}},
		{"CREATE INVERTED INDEX v ON c (t)", &ddl{verb: "CREATE", index: true, kind: "inverted", name: "v", coll: "c", fields: []string{"t"}}},
		{"CREATE TTL INDEX x ON c (exp) EXPIRE AFTER 3600", &ddl{verb: "CREATE", index: true, kind: "ttl", name: "x", coll: "c", fields: []string{"exp"}, expire: 3600}},
		{"DROP INDEX i ON c", &ddl{verb: "DROP", drop: true, index: true, name: "i", coll: "c"}},
		{"  DROP INDEX IF EXISTS i ON `c`", &ddl{verb: "DROP", drop: true, index: true, ifExists: true, name: "i", coll: "c"}},
		{"FOR u IN c RETURN u", nil},
		{"RETURN \"it`s\"", nil},
		{"FOR d IN c FILTER d.a == 'x`y' RETURN d", nil},
		{"INSERT {a: 1} INTO c", nil},
		{"", nil},
	} {
		got, err := parseDDL(tt.query)
		if err != nil {
			t.Errorf("parseDDL(%q): %v", tt.query, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseDDL(%q) = %+v, want %+v", tt.query, got, tt.want)
		}
	}
	for _, q := range []string{
		"CREATE TABLE t", "CREATE COLLECTION", "CREATE COLLECTION c d", "DROP VIEW v", "CREATE INDEX i ON c",
		"CREATE INDEX i ON c (a", "CREATE INDEX i c (a)", "CREATE GEO UNIQUE INDEX g ON c (a)",
		"CREATE TTL INDEX x ON c (a, b) EXPIRE AFTER 1", "CREATE TTL INDEX x ON c (a) EXPIRE AFTER soon",
		"DROP INDEX i", "CREATE COLLECTION IF EXISTS c", "CREATE COLLECTION `open",
		"DROP COLLECTION IF c", "DROP INDEX IF i ON c",
	} {
		if _, err := parseDDL(q); !errors.Is(err, dbimp.ErrInvalidValue) && !errors.Is(err, dbimp.ErrUnterminated) {
			t.Errorf("parseDDL(%q) gave %v, want an error", q, err)
		}
	}
}
