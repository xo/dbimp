package cosmos_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/cosmos"
	"github.com/xo/dbimp/dbimptest"
)

// rtConnector opens connections for dbimptest.RoundTrip. A query goes to the
// driver, as the master key, and each write goes to the REST API as the same
// key, because the SQL of Cosmos DB has no INSERT, UPDATE or DELETE and the
// driver reads only (D190). The statements of a write are these: create and
// drop for the container, insert and update with the value and the key as named
// arguments, delete with the key, and literal with the key and the JSON text of
// the value in the statement, because the SQL has no literal of a write. The
// document of a row is {"id": key, "pk": key, "v": value}, and a value that
// is missing leaves out v.
type rtConnector struct {
	inner driver.Connector
	a     *account
	coll  string
}

func (rc *rtConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := rc.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &rtConn{Conn: conn, rc: rc}, nil
}

func (rc *rtConnector) Driver() driver.Driver { return rc.inner.Driver() }

// missing is a value that the document does not hold, which the server calls
// undefined. A read gives nil for it (D190).
type missing struct{}

// rtConn is a connection of rtConnector.
type rtConn struct {
	driver.Conn

	rc *rtConnector
}

// CheckNamedValue keeps every value as it is, so that a write gets the Go
// value of the test, and a query hands it to the driver.
func (c *rtConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (c *rtConn) QueryContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, fmt.Errorf("the connection runs no query: %w", dbimp.ErrNotSupported)
	}
	return q.QueryContext(ctx, statement, args)
}

// arg returns the value of the argument with the name.
func arg(args []driver.NamedValue, name string) any {
	for _, a := range args {
		if a.Name == name {
			return a.Value
		}
	}
	return nil
}

func (c *rtConn) ExecContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Result, error) {
	a, coll := c.rc.a, c.rc.coll
	verb, rest, _ := strings.Cut(statement, " ")
	var (
		status int
		body   []byte
		err    error
		want   = http.StatusOK
	)
	switch verb {
	case "create":
		want = http.StatusCreated
		def := map[string]any{"id": coll, "partitionKey": map[string]any{"paths": []string{"/pk"}, "kind": "Hash"}}
		status, _, body, err = a.raw(ctx, http.MethodPost, "/dbs/"+databaseName+"/colls", nil, def)
	case "drop":
		want = http.StatusNoContent
		status, _, body, err = a.raw(ctx, http.MethodDelete, collPath(coll), nil, nil)
	case "insert":
		want = http.StatusCreated
		key := fmt.Sprint(arg(args, "key"))
		status, body, err = c.write(ctx, http.MethodPost, collPath(coll)+"/docs", key, arg(args, "value"))
	case "literal":
		want = http.StatusCreated
		key, text, _ := strings.Cut(rest, " ")
		status, body, err = c.writeText(ctx, http.MethodPost, collPath(coll)+"/docs", key, text)
	case "update":
		key := fmt.Sprint(arg(args, "key"))
		status, body, err = c.write(ctx, http.MethodPut, collPath(coll)+"/docs/"+key, key, arg(args, "value"))
	case "delete":
		want = http.StatusNoContent
		key := fmt.Sprint(arg(args, "key"))
		status, _, body, err = a.raw(ctx, http.MethodDelete, collPath(coll)+"/docs/"+key, pk(key), nil)
	default:
		return nil, fmt.Errorf("the statement %q: %w", statement, dbimp.ErrInvalidValue)
	}
	if err != nil {
		return nil, err
	}
	if status != want {
		return nil, fmt.Errorf("%s: HTTP %d, want %d: %s", verb, status, want, body)
	}
	return driver.ResultNoRows, nil
}

// document returns the JSON text of the document of a row.
func document(key string, value any) (string, error) {
	id, err := json.Marshal(key)
	if err != nil {
		return "", fmt.Errorf("writing the key: %w", err)
	}
	if _, ok := value.(missing); ok {
		return `{"id":` + string(id) + `,"pk":` + string(id) + `}`, nil
	}
	v, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("writing the value: %w", err)
	}
	return `{"id":` + string(id) + `,"pk":` + string(id) + `,"v":` + string(v) + `}`, nil
}

// write sends the document of a row, with the value as a Go value.
func (c *rtConn) write(ctx context.Context, method, path, key string, value any) (int, []byte, error) {
	text, err := document(key, value)
	if err != nil {
		return 0, nil, err
	}
	status, _, body, err := c.rc.a.raw(ctx, method, path, pk(key), text)
	return status, body, err
}

// writeText sends the document of a row, with the value as JSON text, where
// "-" is a value that is missing.
func (c *rtConn) writeText(ctx context.Context, method, path, key, text string) (int, []byte, error) {
	id, err := json.Marshal(key)
	if err != nil {
		return 0, nil, fmt.Errorf("writing the key: %w", err)
	}
	doc := `{"id":` + string(id) + `,"pk":` + string(id)
	if text != "-" {
		doc += `,"v":` + text
	}
	status, _, body, err := c.rc.a.raw(ctx, method, path, pk(key), doc+"}")
	return status, body, err
}

// roundTripDB makes the container of one type, and returns a database whose
// writes go to the REST API and whose reads go to the driver.
func roundTripDB(t *testing.T, a *account, typ string) *sql.DB {
	t.Helper()
	cfg := a.cfg
	cfg.Database, cfg.Container = databaseName, "rt_"+typ
	db := sql.OpenDB(&rtConnector{inner: cosmos.NewConnector(cfg), a: a, coll: cfg.Container})
	t.Cleanup(func() { db.Close() })
	return db
}

// rtSelect reads the value of a row. A key that is missing is null, because the
// first row names the columns, and a row with no key v has no such column
// (D190), so the statement writes the value in a way that always gives a column.
const rtSelect = "SELECT IS_DEFINED(c.v) ? c.v : null AS v FROM c WHERE c.id = @key"

// rtCase returns the case of the round trip of one type.
func rtCase(typ string, values ...dbimptest.Value) dbimptest.RoundTripCase {
	return dbimptest.RoundTripCase{
		Type:     typ,
		Setup:    []string{"create"},
		Teardown: []string{"drop"},
		Insert:   "insert",
		Literal: func(key string, v any) (string, error) {
			if _, ok := v.(missing); ok {
				return "literal " + key + " -", nil
			}
			b, err := json.Marshal(v)
			if err != nil {
				return "", fmt.Errorf("writing the literal: %w", err)
			}
			return "literal " + key + " " + string(b), nil
		},
		Select: rtSelect,
		Update: "update",
		Delete: "delete",
		Values: values,
		// An index is up to date when the write returns, but a read through
		// another request can come first on a hosted account, so the test reads
		// again for a bounded time.
		Wait:  30 * time.Second,
		Named: true,
		Equal: func(got, want any) bool {
			if _, ok := want.(missing); ok {
				return got == nil
			}
			return reflect.DeepEqual(got, want)
		},
	}
}

// ints are the integers of the type number: zero, one, a negative one, the first
// integer beyond 2^53, and the ends of int64. The server keeps a stored integer
// of 64 bits exactly (recorded: "numbers beyond 2^53 and the numbers that JSON
// loses").
var ints = []int64{0, 1, -1, 9007199254740993, math.MaxInt64, math.MinInt64}

// TestIntegrationRoundTrip runs the round trip of each type of the type table
// against a container of its own: a value as a bound argument and as a literal,
// a select by the key, an update, a delete and a select to see that the row is
// gone. The container is dropped when the test ends. A type that the server has
// no type for, such as a date, runs the round trip with the string that holds
// it, because the driver gives such a value as a string (D190), and then reads
// it into the type of the caller.
func TestIntegrationRoundTrip(t *testing.T) {
	a := getAccount(t, envEmulator)

	t.Run("string", func(t *testing.T) {
		dbimptest.RoundTrip(t, roundTripDB(t, a, "string"), rtCase("string",
			dbimptest.Value{Name: "empty", In: ""},
			dbimptest.Value{Name: "ascii", In: "hello"},
			dbimptest.Value{Name: "unicode", In: "héllo wörld, 日本語, 🎉, é́"},
			dbimptest.Value{Name: "quotes", In: `a "quoted" 'text' with a \ backslash and a / slash`},
			dbimptest.Value{Name: "a number as text", In: "123"},
			dbimptest.Value{Name: "null as text", In: "null"},
			dbimptest.Value{Name: "a NUL", In: "a\x00b"},
			dbimptest.Value{Name: "long", In: strings.Repeat("0123456789abcdef", 4096)},
		))
	})

	t.Run("number", func(t *testing.T) {
		values := []dbimptest.Value{
			{Name: "fraction", In: 1.5},
			{Name: "negative fraction", In: -0.1},
			{Name: "large", In: 1e300},
			{Name: "smallest", In: math.SmallestNonzeroFloat64},
			{Name: "largest", In: math.MaxFloat64},
			{Name: "digits", In: 0.30000000000000004},
		}
		for _, i := range ints {
			values = append(values, dbimptest.Value{Name: fmt.Sprint("integer ", i), In: i})
		}
		dbimptest.RoundTrip(t, roundTripDB(t, a, "number"), rtCase("number", values...))
	})

	t.Run("boolean", func(t *testing.T) {
		dbimptest.RoundTrip(t, roundTripDB(t, a, "boolean"), rtCase("boolean",
			dbimptest.Value{Name: "true", In: true},
			dbimptest.Value{Name: "false", In: false},
		))
	})

	t.Run("null", func(t *testing.T) {
		dbimptest.RoundTrip(t, roundTripDB(t, a, "null"), rtCase("null",
			dbimptest.Value{Name: "null", In: nil},
			dbimptest.Value{Name: "null again", In: nil},
		))
	})

	t.Run("array", func(t *testing.T) {
		long := make([]any, 1000)
		for i := range long {
			long[i] = int64(i)
		}
		dbimptest.RoundTrip(t, roundTripDB(t, a, "array"), rtCase("array",
			dbimptest.Value{Name: "empty", In: []any{}},
			dbimptest.Value{Name: "mixed", In: []any{int64(1), "a", nil, true, 1.5, []any{}, map[string]any{}}},
			dbimptest.Value{Name: "nested", In: []any{[]any{[]any{int64(1)}}, map[string]any{"k": []any{"v"}}}},
			dbimptest.Value{Name: "long", In: long},
		))
	})

	t.Run("object", func(t *testing.T) {
		dbimptest.RoundTrip(t, roundTripDB(t, a, "object"), rtCase("object",
			dbimptest.Value{Name: "empty", In: map[string]any{}},
			dbimptest.Value{Name: "nested", In: map[string]any{"a": map[string]any{"b": int64(1)}, "list": []any{int64(1), "x"}, "null": nil}},
			dbimptest.Value{Name: "unicode keys", In: map[string]any{"é": "ü", "日本": int64(1), "": "empty key"}},
			dbimptest.Value{Name: "geojson", In: map[string]any{"type": "Point", "coordinates": []any{1.5, 2.5}}},
		))
	})

	t.Run("undefined", func(t *testing.T) {
		// A document with no key v has the value that the server calls undefined,
		// and the driver gives nil for it, as it does for a missing key (D190).
		dbimptest.RoundTrip(t, roundTripDB(t, a, "undefined"), rtCase("undefined",
			dbimptest.Value{Name: "missing", In: missing{}},
			dbimptest.Value{Name: "missing again", In: missing{}},
		))
	})

	// The types below have no type in Cosmos DB, so the server keeps each as a
	// string, and the driver gives a string (D190).
	t.Run("date", func(t *testing.T) {
		dbimptest.RoundTrip(t, roundTripDB(t, a, "date"), rtCase("date",
			dbimptest.Value{Name: "date", In: "2026-10-10"},
			dbimptest.Value{Name: "timestamp", In: "2026-10-10T12:30:15.123456789Z"},
			dbimptest.Value{Name: "offset", In: "2026-10-10T12:30:15+07:00"},
			dbimptest.Value{Name: "first", In: "0001-01-01T00:00:00Z"},
			dbimptest.Value{Name: "last", In: "9999-12-31T23:59:59.999999999Z"},
		))
	})

	t.Run("uuid", func(t *testing.T) {
		id := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
		// A uuid.UUID is sent as its text, and it comes back as text.
		db := roundTripDB(t, a, "uuid")
		dbimptest.RoundTrip(t, db, rtCase("uuid",
			dbimptest.Value{Name: "uuid", In: id, Want: id.String()},
			dbimptest.Value{Name: "nil uuid", In: uuid.Nil(), Want: uuid.Nil().String()},
			dbimptest.Value{Name: "max uuid", In: uuid.Max(), Want: uuid.Max().String()},
		))
	})

	t.Run("binary", func(t *testing.T) {
		// A []byte is sent as base64 text, and it comes back as that text.
		dbimptest.RoundTrip(t, roundTripDB(t, a, "binary"), rtCase("binary",
			dbimptest.Value{Name: "bytes", In: []byte{0, 1, 2, 0xff}, Want: "AAEC/w=="},
			dbimptest.Value{Name: "text", In: []byte("hello"), Want: "aGVsbG8="},
			dbimptest.Value{Name: "all bytes", In: allBytes(), Want: jsonString(t, allBytes())},
		))
	})

	t.Run("decimal", func(t *testing.T) {
		d := func(s string) *apd.Decimal {
			v, _, err := apd.NewFromString(s)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		// An *apd.Decimal is sent as text, and it comes back as text. A decimal
		// that must keep every digit is a string in Cosmos DB, because a number is
		// a double beyond 2^53.
		dbimptest.RoundTrip(t, roundTripDB(t, a, "decimal"), rtCase("decimal",
			dbimptest.Value{Name: "digits", In: d("12345678901234567890.123456789"), Want: jsonString(t, d("12345678901234567890.123456789"))},
			dbimptest.Value{Name: "negative", In: d("-0.000000000000000000001"), Want: jsonString(t, d("-0.000000000000000000001"))},
			dbimptest.Value{Name: "zero", In: d("0"), Want: jsonString(t, d("0"))},
		))
	})

	// A caller scans the strings into the types that the strings stand for
	// (D190): a UUID through its own Scan method, a date into a string that the
	// caller parses, and a binary value into a []byte that holds its base64 text.
	t.Run("scan the strings", func(t *testing.T) {
		a.container(t, "scan_strings", nil)
		id := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
		a.put(t, "scan_strings", map[string]any{"id": "1", "pk": "p", "u": id.String(), "d": "2026-10-10T12:30:15Z", "b": "AAEC/w=="}, nil)
		var (
			gotID  uuid.UUID
			gotDay string
			gotBin []byte
		)
		db := a.open(t, "scan_strings")
		// One column for each statement, because the emulator sorts the keys of a
		// projection and each column goes into its own type.
		for col, dest := range map[string]any{"u": &gotID, "d": &gotDay, "b": &gotBin} {
			if err := db.QueryRowContext(t.Context(), "SELECT c."+col+" FROM c WHERE c.id = '1'").Scan(dest); err != nil {
				t.Fatalf("scanning %s: %v", col, err)
			}
		}
		when, err := time.Parse(time.RFC3339, gotDay)
		if err != nil || gotID != id || !when.Equal(time.Date(2026, time.October, 10, 12, 30, 15, 0, time.UTC)) || string(gotBin) != "AAEC/w==" {
			t.Errorf("scanned %v, %q (%v) and %q", gotID, gotDay, err, gotBin)
		}
	})
}

// allBytes returns the 256 values of a byte, in order.
func allBytes() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// jsonString returns the text that v has as a JSON string, such as the base64
// text of a []byte and the text of an *apd.Decimal.
func jsonString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("%s is not a string: %v", b, err)
	}
	return s
}
