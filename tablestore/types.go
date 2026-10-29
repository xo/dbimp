package tablestore

import (
	"fmt"
	"reflect"
	"time"
	_ "time/tzdata" // D223: the zone of the server is a name, and its rules need the zone database.

	"github.com/xo/dbimp"
)

// logicTimestamp is the logic type of a TIMESTAMP column. The SDK knows the
// logic types 0 to 3 only, and the server sent 4 for a TIMESTAMP column
// (recorded: "select the dates as int").
const logicTimestamp logicType = 4

// The names of the types of a column, which the type table of
// docs/TABLESTORE.md uses (D223). They are the names of the types of a mapping
// table, which DESC writes as bigint(20), double, tinyint(1), mediumtext and
// mediumblob.
const (
	typeBigint     = "BIGINT"
	typeDouble     = "DOUBLE"
	typeBool       = "BOOL"
	typeMediumtext = "MEDIUMTEXT"
	typeMediumblob = "MEDIUMBLOB"
	typeDatetime   = "DATETIME"
	typeDate       = "DATE"
	typeTimestamp  = "TIMESTAMP"
)

// serverZone is the time zone of the SQL of the server, which is Asia/Shanghai.
// A statement that writes '2026-10-29 01:02:03' into a DATETIME or a TIMESTAMP
// column stores the instant 2026-10-28 17:02:03 UTC, and a read returns that
// instant as a count of microseconds (measured on 2026-10-29: the recorded answer
// holds 1793206923000000 for the text above). The offset is +8 hours now, and it
// was +8:05:43 before 1901, which the server showed when it refused the date
// 1000-01-01, whose local midnight lies before the first date that it keeps. The
// driver reads a DATETIME as the wall clock of this zone, so that a value written
// and read back is the same, and it writes a time.Time in this zone. The zone is a
// fact of the server, and docs/TABLESTORE.md names the question that remains:
// whether another instance or region has another zone.
var serverZone = loadZone("Asia/Shanghai")

// loadZone returns the zone of the name, from the zone database that the package
// embeds with time/tzdata.
func loadZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// The package embeds the zone database, so the name always loads. The
		// fixed zone is the offset of the zone since 1991.
		return time.FixedZone(name, 8*60*60)
	}
	return loc
}

// wire returns the name of the type of c, or "" for a type that the driver does
// not know, such as a TIME column.
func (c *column) wire() string {
	switch c.data {
	case dataLong:
		return typeBigint
	case dataDouble:
		return typeDouble
	case dataBoolean:
		return typeBool
	case dataString, dataStringRLE:
		return typeMediumtext
	case dataBinary:
		return typeMediumblob
	case dataComplex:
		if c.encode != dataLong {
			return ""
		}
		switch c.logic {
		case logicDatetime:
			return typeDatetime
		case logicDate:
			return typeDate
		case logicTimestamp:
			return typeTimestamp
		}
	}
	return ""
}

// databaseType returns the name of the type of c in upper case. A type that the
// driver does not know has the name UNKNOWN.
func (c *column) databaseType() string {
	if w := c.wire(); w != "" {
		return w
	}
	return "UNKNOWN"
}

// scanTypes are the Go types of the types of a column (D135 and D223).
var scanTypes = map[string]reflect.Type{
	typeBigint:     reflect.TypeFor[int64](),
	typeDouble:     reflect.TypeFor[float64](),
	typeBool:       reflect.TypeFor[bool](),
	typeMediumtext: reflect.TypeFor[string](),
	typeMediumblob: reflect.TypeFor[[]byte](),
	typeDatetime:   reflect.TypeFor[dbimp.LocalDateTime](),
	typeDate:       reflect.TypeFor[dbimp.Date](),
	typeTimestamp:  reflect.TypeFor[time.Time](),
}

// scanType returns the Go type that the column gives for every value, a NULL
// excepted. A type that the driver does not know scans into any.
func (c *column) scanType() reflect.Type {
	if t, ok := scanTypes[c.wire()]; ok {
		return t
	}
	return reflect.TypeFor[any]()
}

// decode returns the value of the column in the row i, with the Go type of the
// type of the column. A NULL is nil, and never a zero value (hard rule 3).
func (c *column) decode(i int) (any, error) {
	w := c.wire()
	if w == "" {
		return nil, fmt.Errorf("reading the column %s: the data type %d with the logic type %d: %w", c.name, c.data, c.logic, dbimp.ErrNotSupported)
	}
	raw, err := c.value(i)
	if err != nil || raw == nil {
		return nil, err
	}
	switch w {
	case typeDatetime, typeDate, typeTimestamp:
		n, ok := raw.(int64)
		if !ok {
			return nil, fmt.Errorf("reading the column %s: a %s holds %T, want a count: %w", c.name, w, raw, dbimp.ErrInvalidValue)
		}
		switch w {
		case typeDatetime:
			return dbimp.LocalDateTimeOf(time.UnixMicro(n).In(serverZone)), nil
		case typeDate:
			return dbimp.DateOf(time.Unix(n, 0).UTC()), nil
		}
		return time.UnixMicro(n).UTC(), nil
	}
	return raw, nil
}
