package hive

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The type ids of TTypeId (docs/HIVE.md, Types).
const (
	typeBoolean        = 0
	typeTinyint        = 1
	typeSmallint       = 2
	typeInt            = 3
	typeBigint         = 4
	typeFloat          = 5
	typeDouble         = 6
	typeString         = 7
	typeTimestamp      = 8
	typeBinary         = 9
	typeArray          = 10
	typeMap            = 11
	typeStruct         = 12
	typeUnion          = 13
	typeDecimal        = 15
	typeNull           = 16
	typeDate           = 17
	typeVarchar        = 18
	typeChar           = 19
	typeIntervalYM     = 20
	typeIntervalDT     = 21
	typeTimestampLocal = 22
)

// typeNames are the names of the types in upper case, as SQL writes them, by
// type id.
var typeNames = map[int32]string{
	typeBoolean:        "BOOLEAN",
	typeTinyint:        "TINYINT",
	typeSmallint:       "SMALLINT",
	typeInt:            "INT",
	typeBigint:         "BIGINT",
	typeFloat:          "FLOAT",
	typeDouble:         "DOUBLE",
	typeString:         "STRING",
	typeTimestamp:      "TIMESTAMP",
	typeBinary:         "BINARY",
	typeArray:          "ARRAY",
	typeMap:            "MAP",
	typeStruct:         "STRUCT",
	typeUnion:          "UNIONTYPE",
	typeDecimal:        "DECIMAL",
	typeNull:           "NULL",
	typeDate:           "DATE",
	typeVarchar:        "VARCHAR",
	typeChar:           "CHAR",
	typeIntervalYM:     "INTERVAL_YEAR_MONTH",
	typeIntervalDT:     "INTERVAL_DAY_TIME",
	typeTimestampLocal: "TIMESTAMP WITH LOCAL TIME ZONE",
}

// typeName returns the name of the type with the id, or "" for a type that the
// driver does not know.
func typeName(id int32) string {
	return typeNames[id]
}

// The Go types of the columns (D234).
var (
	typeBool      = reflect.TypeFor[bool]()
	typeInt64     = reflect.TypeFor[int64]()
	typeFloat64   = reflect.TypeFor[float64]()
	typeDecimalGo = reflect.TypeFor[*apd.Decimal]()
	typeStringGo  = reflect.TypeFor[string]()
	typeBytes     = reflect.TypeFor[[]byte]()
	typeDateGo    = reflect.TypeFor[dbimp.Date]()
	typeLocalGo   = reflect.TypeFor[dbimp.LocalDateTime]()
	typeTimeGo    = reflect.TypeFor[time.Time]()
	typeInterval  = reflect.TypeFor[dbimp.Interval]()
	typeSlice     = reflect.TypeFor[[]any]()
	typeMapGo     = reflect.TypeFor[map[string]any]()
	typeAny       = reflect.TypeFor[any]()
)

// scanType returns the Go type of the values of the type with the id.
func scanType(id int32) reflect.Type {
	switch id {
	case typeBoolean:
		return typeBool
	case typeTinyint, typeSmallint, typeInt, typeBigint:
		return typeInt64
	case typeFloat, typeDouble:
		return typeFloat64
	case typeDecimal:
		return typeDecimalGo
	case typeBinary:
		return typeBytes
	case typeDate:
		return typeDateGo
	case typeTimestamp:
		return typeLocalGo
	case typeTimestampLocal:
		return typeTimeGo
	case typeIntervalYM, typeIntervalDT:
		return typeInterval
	case typeArray:
		return typeSlice
	case typeMap, typeStruct:
		return typeMapGo
	case typeNull:
		return typeAny
	}
	return typeStringGo
}

// decode returns the value i of the column c, which has the description d, as
// the Go value of its type (D234). A NULL is nil.
func decode(d *colDesc, c *column, i int) (driver.Value, error) {
	if c.null(i) {
		return nil, nil //nolint:nilnil // A NULL is nil (D18).
	}
	switch c.kind {
	case 1:
		return c.ints[i] != 0, nil
	case 2, 3, 4, 5:
		return c.ints[i], nil
	case 6:
		return c.floats[i], nil
	case 8:
		return c.strs[i], nil
	}
	s := string(c.strs[i])
	var err error
	var v driver.Value
	switch d.typeID {
	case typeDecimal:
		v, err = decodeDecimal(s, d.scale)
	case typeDate:
		v, err = dbimp.ParseDate(s)
	case typeTimestamp:
		v, err = dbimp.ParseLocalDateTime(s)
	case typeTimestampLocal:
		v, err = decodeZoned(s)
	case typeIntervalYM:
		v, err = decodeIntervalYM(s)
	case typeIntervalDT:
		v, err = decodeIntervalDT(s)
	case typeArray, typeMap, typeStruct:
		v, err = dbimp.Any(jsontext.Value(s))
	case typeNull:
		return nil, nil //nolint:nilnil // A NULL is nil (D18).
	default:
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %q as %s: %w", s, typeName(d.typeID), err)
	}
	return v, nil
}

// decodeZoned reads a timestamp with a local time zone, as Hive writes it: the
// date and the time, and the zone as a short name, such as GMT on 4.2.1, or as the
// name of a zone, such as Etc/UTC on 4.0.1 (docs/HIVE.md, Types).
func decodeZoned(s string) (time.Time, error) {
	clock, zone, ok := strings.Cut(s, " ")
	if ok {
		var rest string
		if clock2, zone2, ok2 := strings.Cut(zone, " "); ok2 {
			clock, rest = clock+" "+clock2, zone2
		}
		zone = rest
	}
	if !ok || zone == "" {
		return time.Time{}, fmt.Errorf("reading %q as a timestamp with a time zone: %w", s, dbimp.ErrInvalidValue)
	}
	local, err := time.Parse("2006-01-02 15:04:05.999999999", clock)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading %q as a timestamp with a time zone: %w", s, dbimp.ErrInvalidValue)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		// A short name that the zone database does not know, such as PST.
		t, perr := time.Parse("2006-01-02 15:04:05.999999999 MST", s)
		if perr != nil {
			return time.Time{}, fmt.Errorf("reading %q as a timestamp with a time zone: %w", s, dbimp.ErrInvalidValue)
		}
		return t, nil
	}
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), local.Nanosecond(), loc), nil
}

// decodeDecimal reads the text of a decimal, with the scale of its column
// (D228).
func decodeDecimal(s string, scale int32) (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		return nil, fmt.Errorf("reading %q as a decimal: %w", s, dbimp.ErrInvalidValue)
	}
	return dbimp.PadScale(d, int64(scale)), nil
}

// decodeIntervalYM reads an interval of years and months, as Hive writes it,
// such as 1-2 and -1-2.
func decodeIntervalYM(s string) (dbimp.Interval, error) {
	body, neg := strings.CutPrefix(s, "-")
	y, m, ok := strings.Cut(body, "-")
	if !ok {
		return dbimp.Interval{}, fmt.Errorf("reading %q as an interval of years and months: %w", s, dbimp.ErrInvalidValue)
	}
	years, err1 := strconv.ParseInt(y, 10, 32)
	months, err2 := strconv.ParseInt(m, 10, 32)
	if err1 != nil || err2 != nil || years*12+months > math.MaxInt32 {
		return dbimp.Interval{}, fmt.Errorf("reading %q as an interval of years and months: %w", s, dbimp.ErrInvalidValue)
	}
	total := int32(years*12 + months) //nolint:gosec // G115: the test above bounds it.
	if neg {
		total = -total
	}
	return dbimp.Interval{Months: total}, nil
}

// decodeIntervalDT reads an interval of days and time, as Hive writes it, such
// as 1 02:03:04.500000000 and -1 02:03:04.5.
func decodeIntervalDT(s string) (dbimp.Interval, error) {
	bad := fmt.Errorf("reading %q as an interval of days and time: %w", s, dbimp.ErrInvalidValue)
	body, neg := strings.CutPrefix(s, "-")
	day, clock, ok := strings.Cut(body, " ")
	if !ok {
		return dbimp.Interval{}, bad
	}
	days, err := strconv.ParseInt(day, 10, 32)
	if err != nil {
		return dbimp.Interval{}, bad
	}
	hms, frac, _ := strings.Cut(clock, ".")
	parts := strings.Split(hms, ":")
	if len(parts) != 3 {
		return dbimp.Interval{}, bad
	}
	var nums [3]int64
	for i, p := range parts {
		if nums[i], err = strconv.ParseInt(p, 10, 64); err != nil {
			return dbimp.Interval{}, bad
		}
	}
	var nanos int64
	if frac != "" {
		if len(frac) > 9 {
			frac = frac[:9]
		}
		if nanos, err = strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64); err != nil {
			return dbimp.Interval{}, bad
		}
	}
	total := ((nums[0]*60+nums[1])*60+nums[2])*int64(time.Second) + nanos
	iv := dbimp.Interval{Days: int32(days), Nanoseconds: total}
	if neg {
		iv.Days, iv.Nanoseconds = -iv.Days, -iv.Nanoseconds
	}
	return iv, nil
}
