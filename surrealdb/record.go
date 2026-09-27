package surrealdb

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// RecordID is the id of a SurrealDB record, such as person:tobie: the name of
// its table and its key (D53). The key is a string, an int64, a uuid.UUID, an
// []any or a map[string]any.
type RecordID struct {
	// Table is the name of the table.
	Table string
	// ID is the key of the record in its table.
	ID any
}

// String writes the record id as SurrealQL writes it, such as person:tobie,
// person:123 or person:['a', 1]. A name or a key that is not a plain
// identifier is quoted with backticks.
func (r RecordID) String() string {
	var key string
	switch id := r.ID.(type) {
	case string:
		key = ident(id, true)
	default:
		key = literal(id)
	}
	return ident(r.Table, false) + ":" + key
}

// ident returns name as it stands in SurrealQL: as it is if it holds only
// letters, digits and underscores, and quoted with backticks otherwise. A key
// of digits alone is quoted too, when key is true, so that it is not read as
// a number.
func ident(name string, key bool) string {
	plain := name != ""
	digits := true
	for _, c := range name {
		switch {
		case c >= '0' && c <= '9':
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			digits = false
		default:
			plain = false
		}
	}
	if plain && (!key || !digits) {
		return name
	}
	r := strings.NewReplacer("\\", "\\\\", "`", "\\`")
	return "`" + r.Replace(name) + "`"
}

// literal writes v as a literal of SurrealQL, for the key of a record id and
// the bounds of a range.
func literal(v any) string {
	switch v := v.(type) {
	case nil:
		return "NULL"
	case string:
		r := strings.NewReplacer("\\", "\\\\", "'", "\\'")
		return "'" + r.Replace(v) + "'"
	case bool:
		return strconv.FormatBool(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64) + "f"
	case *apd.Decimal:
		return v.String() + "dec"
	case uuid.UUID:
		return "u'" + v.String() + "'"
	case time.Time:
		return "d'" + v.Format(time.RFC3339Nano) + "'"
	case time.Duration:
		return formatDuration(v)
	case RecordID:
		return v.String()
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = literal(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		if len(v) == 0 {
			return "{}"
		}
		keys := slices.Sorted(maps.Keys(v))
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = ident(k, false) + ": " + literal(v[k])
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	return fmt.Sprint(v)
}

// durationUnit is a unit of a duration of SurrealQL.
type durationUnit struct {
	name string
	size time.Duration
}

// durationUnits are the units of a duration of SurrealQL, from the largest.
var durationUnits = []durationUnit{
	{"y", 365 * 24 * time.Hour},
	{"w", 7 * 24 * time.Hour},
	{"d", 24 * time.Hour},
	{"h", time.Hour},
	{"m", time.Minute},
	{"s", time.Second},
	{"ms", time.Millisecond},
	{"µs", time.Microsecond},
	{"ns", time.Nanosecond},
}

// formatDuration writes d as SurrealQL writes a duration, such as 1h30m or
// 0ns.
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0ns"
	}
	var b strings.Builder
	if d < 0 {
		b.WriteByte('-')
		d = -d
	}
	for _, u := range durationUnits {
		if n := d / u.size; n > 0 {
			b.WriteString(strconv.FormatInt(int64(n), 10))
			b.WriteString(u.name)
			d %= u.size
		}
	}
	return b.String()
}

// parseDuration reads a duration of SurrealQL, such as 1h30m or 1y2w3d. A
// duration longer than a time.Duration holds is ErrInvalidValue (D53).
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("reading the duration %q: %w", s, dbimp.ErrInvalidValue)
	}
	var total time.Duration
	for rest := s; rest != ""; {
		i := strings.IndexFunc(rest, func(c rune) bool { return c < '0' || c > '9' })
		if i <= 0 {
			return 0, fmt.Errorf("reading the duration %q: %w", s, dbimp.ErrInvalidValue)
		}
		n, err := strconv.ParseInt(rest[:i], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("reading the duration %q: %w", s, dbimp.ErrInvalidValue)
		}
		rest = rest[i:]
		unit := strings.IndexFunc(rest, func(c rune) bool { return c >= '0' && c <= '9' })
		if unit < 0 {
			unit = len(rest)
		}
		name := rest[:unit]
		rest = rest[unit:]
		if name == "us" {
			name = "µs"
		}
		k := slices.IndexFunc(durationUnits, func(u durationUnit) bool { return u.name == name })
		if k < 0 {
			return 0, fmt.Errorf("reading the duration %q: the unit %q: %w", s, name, dbimp.ErrInvalidValue)
		}
		size := durationUnits[k].size
		if n > int64((1<<63-1-total)/size) {
			return 0, fmt.Errorf("reading the duration %q: longer than a time.Duration holds: %w", s, dbimp.ErrInvalidValue)
		}
		total += time.Duration(n) * size
	}
	return total, nil
}
