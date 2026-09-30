package dbimp

import "strings"

// Affinity is how a driver of the SQLite family, such as rqlite or libSQL,
// reads the values of a column, from the type that the table declares for
// it (D140 and D147). The first five are the affinities of SQLite. BOOLEAN,
// DATE, DATETIME and TIMESTAMP are their own, because the drivers read them
// as a bool, a date and a time.
type Affinity int

// The affinities of a declared type.
const (
	// AffinityNone is the affinity of a column with no type that the drivers
	// support, such as ANY or a column of an expression. Each value reads by
	// its own storage class (D140).
	AffinityNone Affinity = iota
	AffinityInteger
	AffinityReal
	AffinityText
	AffinityBlob
	AffinityNumeric
	AffinityBoolean
	AffinityDate
	AffinityDateTime
	AffinityTimestamp
)

// AffinityOf returns the affinity of the declared type decl, such as
// "bigint" or "VARCHAR(10)" (D140). BOOLEAN, DATE, DATETIME and TIMESTAMP
// are their own affinities. Every other name follows the rules of affinity
// of SQLite, in their order: a name with INT, then CHAR, CLOB or TEXT, then
// BLOB, then REAL, FLOA or DOUB, and NUMERIC for any other. ANY and the
// empty name have none.
func AffinityOf(decl string) Affinity {
	t := strings.ToUpper(strings.TrimSpace(decl))
	switch t {
	case "", "ANY":
		return AffinityNone
	case "BOOLEAN":
		return AffinityBoolean
	case "DATE":
		return AffinityDate
	case "DATETIME":
		return AffinityDateTime
	case "TIMESTAMP":
		return AffinityTimestamp
	}
	switch {
	case strings.Contains(t, "INT"):
		return AffinityInteger
	case strings.Contains(t, "CHAR"), strings.Contains(t, "CLOB"), strings.Contains(t, "TEXT"):
		return AffinityText
	case strings.Contains(t, "BLOB"):
		return AffinityBlob
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"):
		return AffinityReal
	}
	return AffinityNumeric
}
