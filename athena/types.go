package athena

import (
	"encoding/hex"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The names of the types in the member Type of ColumnInfo, in lower case
// (recorded). The server names a Hive column of the type binary varbinary, and
// a Hive column of the type struct row.
const (
	typeTinyint    = "tinyint"
	typeSmallint   = "smallint"
	typeInteger    = "integer"
	typeBigint     = "bigint"
	typeFloat      = "float"
	typeDouble     = "double"
	typeDecimal    = "decimal"
	typeVarchar    = "varchar"
	typeChar       = "char"
	typeString     = "string"
	typeBoolean    = "boolean"
	typeDate       = "date"
	typeTime       = "time"
	typeTimeTZ     = "time with time zone"
	typeTimestamp  = "timestamp"
	typeTimestTZ   = "timestamp with time zone"
	typeVarbinary  = "varbinary"
	typeJSON       = "json"
	typeIPAddress  = "ipaddress"
	typeUUID       = "uuid"
	typeIntervalDS = "interval day to second"
	typeIntervalYM = "interval year to month"
	typeGeometry   = "geometry"
	typeArray      = "array"
	typeMap        = "map"
	typeRow        = "row"
	typeUnknown    = "unknown"
)

// maxLength is the length that database/sql names for a type of any length.
const maxLength = math.MaxInt64

// column is one entry of ColumnInfos (recorded).
type column struct {
	Name      string `json:"Name"`
	Type      string `json:"Type"`
	Precision int64  `json:"Precision"`
	Scale     int64  `json:"Scale"`
}

// wire returns the name of the type in lower case.
func (c column) wire() string {
	return strings.ToLower(c.Type)
}

// databaseType returns the name of the type in upper case, such as BIGINT or
// TIMESTAMP WITH TIME ZONE.
func (c column) databaseType() string {
	return strings.ToUpper(c.Type)
}

// decode returns the value s of a column as its Go value (D135 and D192). A
// NULL is nil and is not passed here. A type that the driver does not know
// gives the text of the server, because every value arrives as text (D192
// item 3).
func (c column) decode(s string) (any, error) {
	switch c.wire() {
	case typeTinyint, typeSmallint, typeInteger, typeBigint:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("reading %q as an integer: %w", s, dbimp.ErrInvalidValue)
		}
		return n, nil
	case typeFloat, typeDouble:
		return parseFloat(s)
	case typeDecimal:
		d, _, err := apd.NewFromString(s)
		if err != nil || d.Form != apd.Finite {
			return nil, fmt.Errorf("reading %q as a decimal: %w", s, dbimp.ErrInvalidValue)
		}
		return d, nil
	case typeBoolean:
		switch s {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("reading %q as a boolean: %w", s, dbimp.ErrInvalidValue)
	case typeDate:
		return dbimp.ParseDate(s)
	case typeTime:
		return dbimp.ParseLocalTime(s)
	case typeTimeTZ:
		return dbimp.ParseOffsetTime(s)
	case typeTimestamp:
		return dbimp.ParseLocalDateTime(s)
	case typeTimestTZ:
		return parseInstant(s)
	case typeVarbinary:
		return parseBinary(s)
	case typeJSON:
		val := jsontext.Value(s)
		if !val.IsValid() {
			return nil, fmt.Errorf("reading %q as JSON: %w", s, dbimp.ErrInvalidValue)
		}
		return dbimp.Any(val)
	case typeIPAddress:
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("reading %q as an address: %w: %w", s, dbimp.ErrInvalidValue, err)
		}
		return a, nil
	case typeUUID:
		u, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a UUID: %w: %w", s, dbimp.ErrInvalidValue, err)
		}
		return u, nil
	case typeIntervalDS:
		return parseDayToSecond(s)
	case typeIntervalYM:
		return parseYearToMonth(s)
	case typeUnknown:
		return nil, nil
	}
	return s, nil
}

// parseFloat reads a float or a double. The server writes NaN, Infinity and
// -Infinity (recorded: "the type of nan()"), which ParseFloat reads.
func parseFloat(s string) (float64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("reading %q as a float: %w", s, dbimp.ErrInvalidValue)
	}
	return f, nil
}

// parseBinary reads the text of a varbinary, which is lower case hex in pairs
// that a space separates, such as de ad be ef (recorded: "the type of
// X'DEADBEEF'"). An empty value is an empty, not nil, slice.
func parseBinary(s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return nil, fmt.Errorf("reading %q as hex pairs: %w", s, dbimp.ErrInvalidValue)
	}
	if b == nil {
		b = []byte{}
	}
	return b, nil
}

// parseInstant reads a timestamp with a time zone. The text is the date, the
// time with an optional fraction, a space, and the zone, which is an offset such
// as +07:00 or a name such as Europe/Paris, and a name has no offset (recorded:
// "a timestamp with a named zone"). A name is a zone that time.LoadLocation
// loads, from the zone database that the package time/tzdata holds (D192 item
// 7).
func parseInstant(s string) (time.Time, error) {
	// The date and the time are one word each, so the zone is the last word.
	i := strings.LastIndexByte(s, ' ')
	if i < 0 || i == len(s)-1 {
		return time.Time{}, fmt.Errorf("reading %q as a timestamp with a time zone: %w", s, dbimp.ErrInvalidValue)
	}
	local, zone := s[:i], s[i+1:]
	dt, err := dbimp.ParseLocalDateTime(local)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading %q as a timestamp with a time zone: %w", s, err)
	}
	loc, err := location(zone)
	if err != nil {
		return time.Time{}, err
	}
	return dt.In(loc), nil
}

// location returns the zone that a timestamp names: an offset such as +05:30,
// or the name of a zone such as Asia/Jakarta. A name that Go cannot load is an
// error, because the value cannot be held without it.
func location(zone string) (*time.Location, error) {
	if zone[0] == '+' || zone[0] == '-' {
		ot, err := dbimp.ParseOffsetTime("00:00" + zone)
		if err != nil {
			return nil, fmt.Errorf("the zone %q: %w", zone, dbimp.ErrInvalidValue)
		}
		if ot.Offset == 0 {
			return time.UTC, nil
		}
		return time.FixedZone("", ot.Offset), nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("the zone %q: %w: %w", zone, dbimp.ErrInvalidValue, err)
	}
	return loc, nil
}

// parseYearToMonth reads an interval of years and months, such as 1-2, with an
// optional sign that turns both parts around (recorded: "the type of INTERVAL
// '1' YEAR").
func parseYearToMonth(s string) (dbimp.Interval, error) {
	text, sign := cutSign(s)
	ys, ms, ok := strings.Cut(text, "-")
	years, err1 := strconv.ParseInt(ys, 10, 32)
	months, err2 := strconv.ParseInt(ms, 10, 32)
	total := sign * (years*12 + months)
	if !ok || err1 != nil || err2 != nil || years < 0 || months < 0 || total < math.MinInt32 || total > math.MaxInt32 {
		return dbimp.Interval{}, fmt.Errorf("reading %q as an interval of years and months: %w", s, dbimp.ErrInvalidValue)
	}
	return dbimp.Interval{Months: int32(total)}, nil
}

// parseDayToSecond reads an interval of days and time, such as 3 04:05:06.789,
// with an optional sign that turns both parts around (recorded: "the type of
// INTERVAL '1' DAY").
func parseDayToSecond(s string) (dbimp.Interval, error) {
	text, sign := cutSign(s)
	bad := fmt.Errorf("reading %q as an interval of days and time: %w", s, dbimp.ErrInvalidValue)
	ds, clock, ok := strings.Cut(text, " ")
	days, err := strconv.ParseInt(ds, 10, 32)
	if !ok || err != nil || days < 0 {
		return dbimp.Interval{}, bad
	}
	lt, err := dbimp.ParseLocalTime(clock)
	if err != nil {
		return dbimp.Interval{}, bad
	}
	nanos := ((int64(lt.Hour)*60+int64(lt.Minute))*60+int64(lt.Second))*int64(time.Second) + int64(lt.Nanosecond)
	// days is not negative and fits an int32, so the product fits.
	return dbimp.Interval{Days: int32(sign * days), Nanoseconds: sign * nanos}, nil //nolint:gosec // G115: see above.
}

// cutSign cuts the sign of an interval, and returns the text and 1 or -1.
func cutSign(s string) (string, int64) {
	switch {
	case strings.HasPrefix(s, "-"):
		return s[1:], -1
	case strings.HasPrefix(s, "+"):
		return s[1:], 1
	}
	return s, 1
}

// The scan types of the columns.
var (
	typeOfInt64    = reflect.TypeFor[int64]()
	typeOfFloat64  = reflect.TypeFor[float64]()
	typeOfDecimal  = reflect.TypeFor[*apd.Decimal]()
	typeOfString   = reflect.TypeFor[string]()
	typeOfBool     = reflect.TypeFor[bool]()
	typeOfDate     = reflect.TypeFor[dbimp.Date]()
	typeOfTime     = reflect.TypeFor[dbimp.LocalTime]()
	typeOfTimeTZ   = reflect.TypeFor[dbimp.OffsetTime]()
	typeOfDateTime = reflect.TypeFor[dbimp.LocalDateTime]()
	typeOfInstant  = reflect.TypeFor[time.Time]()
	typeOfBytes    = reflect.TypeFor[[]byte]()
	typeOfAddr     = reflect.TypeFor[netip.Addr]()
	typeOfUUID     = reflect.TypeFor[uuid.UUID]()
	typeOfInterval = reflect.TypeFor[dbimp.Interval]()
	typeOfAny      = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the column (D135 and D192). A JSON
// value and an UNKNOWN value are the decoded value, so their type is any. A type
// that the driver does not know is a string.
func (c column) scanType() reflect.Type {
	switch c.wire() {
	case typeTinyint, typeSmallint, typeInteger, typeBigint:
		return typeOfInt64
	case typeFloat, typeDouble:
		return typeOfFloat64
	case typeDecimal:
		return typeOfDecimal
	case typeBoolean:
		return typeOfBool
	case typeDate:
		return typeOfDate
	case typeTime:
		return typeOfTime
	case typeTimeTZ:
		return typeOfTimeTZ
	case typeTimestamp:
		return typeOfDateTime
	case typeTimestTZ:
		return typeOfInstant
	case typeVarbinary:
		return typeOfBytes
	case typeIPAddress:
		return typeOfAddr
	case typeUUID:
		return typeOfUUID
	case typeIntervalDS, typeIntervalYM:
		return typeOfInterval
	case typeJSON, typeUnknown:
		return typeOfAny
	}
	return typeOfString
}
