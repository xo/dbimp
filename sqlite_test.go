package dbimp_test

import (
	"testing"

	"github.com/xo/dbimp"
)

// TestAffinityOf holds the rules of affinity of SQLite in D140, in their
// order.
func TestAffinityOf(t *testing.T) {
	t.Parallel()
	for decl, want := range map[string]dbimp.Affinity{
		"integer":       dbimp.AffinityInteger,
		"bigint":        dbimp.AffinityInteger,
		"INT":           dbimp.AffinityInteger,
		"point":         dbimp.AffinityInteger, // POINT holds INT, and INT comes first.
		"varchar(10)":   dbimp.AffinityText,
		"clob":          dbimp.AffinityText,
		"text":          dbimp.AffinityText,
		"blob":          dbimp.AffinityBlob,
		"F32_BLOB(3)":   dbimp.AffinityBlob,
		"real":          dbimp.AffinityReal,
		"double":        dbimp.AffinityReal,
		"float":         dbimp.AffinityReal,
		"numeric":       dbimp.AffinityNumeric,
		"decimal(10,2)": dbimp.AffinityNumeric,
		"uuid":          dbimp.AffinityNumeric,
		"json":          dbimp.AffinityNumeric,
		"boolean":       dbimp.AffinityBoolean,
		"date":          dbimp.AffinityDate,
		"datetime":      dbimp.AffinityDateTime,
		"TIMESTAMP":     dbimp.AffinityTimestamp,
		"any":           dbimp.AffinityNone,
		"":              dbimp.AffinityNone,
	} {
		if got := dbimp.AffinityOf(decl); got != want {
			t.Errorf("AffinityOf(%q) = %d, want %d", decl, got, want)
		}
	}
}
