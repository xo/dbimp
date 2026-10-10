package spanner //nolint:testpackage // The tests read the decoder of the driver, which is not exported.

import (
	"encoding/json/jsontext"
	"errors"
	"math"
	"testing"
	"time"
	"uuid"

	"github.com/xo/dbimp"
)

// typeOf returns the type with the code, and with an element type for an array.
func typeOf(code string, elem ...string) *wireType {
	t := &wireType{Code: code}
	if len(elem) > 0 {
		t.Elem = &wireType{Code: elem[0]}
	}
	return t
}

// TestDecode holds the decoder of each type with values that the server wrote and
// values near them (D135 and docs/SPANNER.md, "Types").
func TestDecode(t *testing.T) {
	t.Parallel()
	for name, tt := range map[string]struct {
		typ  *wireType
		in   string
		want any
	}{
		"INT64":                 {typeOf(wireInt64), `"-5"`, int64(-5)},
		"INT64 as a number":     {typeOf(wireInt64), `5`, int64(5)},
		"FLOAT64":               {typeOf(wireFloat64), `1.25`, 1.25},
		"FLOAT64 is an integer": {typeOf(wireFloat64), `16777216`, 16777216.0},
		"FLOAT64 of -0":         {typeOf(wireFloat64), `-0`, math.Copysign(0, -1)},
		"FLOAT64 NaN":           {typeOf(wireFloat64), `"NaN"`, math.NaN()},
		"FLOAT64 +Inf":          {typeOf(wireFloat64), `"Infinity"`, math.Inf(1)},
		"FLOAT64 -Inf":          {typeOf(wireFloat64), `"-Infinity"`, math.Inf(-1)},
		"FLOAT32 is widened":    {typeOf(wireFloat32), `0.10000000149011612`, float64(float32(0.1))},
		"NUMERIC":               {typeOf(wireNumeric), `"0.000000001"`, dec(t, "0.000000001")},
		"NUMERIC negative":      {typeOf(wireNumeric), `"-99999999999999999999999999999.999999999"`, dec(t, "-99999999999999999999999999999.999999999")},
		"BOOL":                  {typeOf(wireBool), `false`, false},
		"STRING":                {typeOf(wireString), `"héllo 世界"`, "héllo 世界"},
		"BYTES":                 {typeOf(wireBytes), `"AP8="`, []byte{0, 0xff}},
		"BYTES empty":           {typeOf(wireBytes), `""`, []byte{}},
		"DATE":                  {typeOf(wireDate), `"2024-01-02"`, dbimp.Date{Year: 2024, Month: 1, Day: 2}},
		"TIMESTAMP":             {typeOf(wireTimestamp), `"2024-01-02T03:04:05.123456789Z"`, time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)},
		"TIMESTAMP before 1970": {typeOf(wireTimestamp), `"1969-07-20T20:17:40.123456789Z"`, time.Date(1969, 7, 20, 20, 17, 40, 123456789, time.UTC)},
		"TIMESTAMP year 1":      {typeOf(wireTimestamp), `"0001-01-01T00:00:00Z"`, time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
		"TIMESTAMP in a zone":   {typeOf(wireTimestamp), `"2024-01-02T03:04:05+05:30"`, time.Date(2024, 1, 1, 21, 34, 5, 0, time.UTC)},
		"JSON object":           {typeOf(wireJSON), `"{\"a\":1}"`, map[string]any{"a": int64(1)}},
		"JSON null is nil":      {typeOf(wireJSON), `"null"`, nil},
		"JSON string":           {typeOf(wireJSON), `"\"s\""`, "s"},
		"UUID":                  {typeOf(wireUUID), `"f47ac10b-58cc-4372-a567-0e02b2c3d479"`, uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")},
		"INTERVAL":              {typeOf(wireInterval), `"P1Y2M3DT4H5M6S"`, dbimp.Interval{Months: 14, Days: 3, Nanoseconds: (4*3600 + 5*60 + 6) * 1e9}},
		"INTERVAL negative":     {typeOf(wireInterval), `"P-1Y-2M-3DT-4H-5M-6S"`, dbimp.Interval{Months: -14, Days: -3, Nanoseconds: -(4*3600 + 5*60 + 6) * 1e9}},
		"INTERVAL fraction":     {typeOf(wireInterval), `"PT-1.5S"`, dbimp.Interval{Nanoseconds: -1500000000}},
		"INTERVAL zero":         {typeOf(wireInterval), `"P0Y"`, dbimp.Interval{}},
		"a NULL":                {typeOf(wireInt64), `null`, nil},
		"an ARRAY":              {typeOf(wireArray, wireInt64), `["1",null]`, []any{int64(1), nil}},
		"an empty ARRAY":        {typeOf(wireArray, wireString), `[]`, []any{}},
		"a NULL ARRAY":          {typeOf(wireArray, wireString), `null`, nil},
		"an ARRAY of FLOAT64":   {typeOf(wireArray, wireFloat64), `[1.5,"NaN","-Infinity"]`, []any{1.5, math.NaN(), math.Inf(-1)}},
	} {
		got, err := tt.typ.decode(jsontext.Value(tt.in))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !equalValue(got, tt.want) && (got != nil || tt.want != nil) {
			t.Errorf("%s: got %#v, want %#v", name, got, tt.want)
		}
	}
}

// TestDecodeStruct holds the STRUCT of an array: each struct is a list of the
// values of its fields, in order, and a list with another count is an error.
func TestDecodeStruct(t *testing.T) {
	t.Parallel()
	st := &wireType{Code: wireStruct, Struct: &structType{Fields: []field{
		{Name: "a", Type: wireType{Code: wireInt64}},
		{Name: "b", Type: wireType{Code: wireString}},
	}}}
	arr := &wireType{Code: wireArray, Elem: st}
	got, err := arr.decode(jsontext.Value(`[["1","x"],["2",null]]`))
	if err != nil {
		t.Fatal(err)
	}
	if !equalValue(got, []any{[]any{int64(1), "x"}, []any{int64(2), nil}}) {
		t.Errorf("got %#v", got)
	}
	for _, in := range []string{`[["1"]]`, `[["1","x","y"]]`} {
		if _, err := arr.decode(jsontext.Value(in)); !errors.Is(err, dbimp.ErrColumnCount) {
			t.Errorf("%s: the error is %v, want dbimp.ErrColumnCount", in, err)
		}
	}
	if err := arr.supported("s"); err != nil {
		t.Errorf("an ARRAY of STRUCT: %v", err)
	}
}

// TestDecodeRefuses holds that a value that the type does not hold is an error
// that wraps dbimp.ErrInvalidValue, and never a zero value.
func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	for name, tt := range map[string]struct {
		typ *wireType
		in  string
	}{
		"INT64 of text":            {typeOf(wireInt64), `"abc"`},
		"INT64 out of range":       {typeOf(wireInt64), `"9223372036854775808"`},
		"INT64 as a fraction":      {typeOf(wireInt64), `1.5`},
		"INT64 as a boolean":       {typeOf(wireInt64), `true`},
		"FLOAT64 of text":          {typeOf(wireFloat64), `"abc"`},
		"FLOAT64 as a boolean":     {typeOf(wireFloat64), `true`},
		"NUMERIC of text":          {typeOf(wireNumeric), `"abc"`},
		"NUMERIC infinite":         {typeOf(wireNumeric), `"Infinity"`},
		"NUMERIC as a number":      {typeOf(wireNumeric), `1.5`},
		"BOOL of text":             {typeOf(wireBool), `"true"`},
		"STRING as a number":       {typeOf(wireString), `1`},
		"BYTES that is not base64": {typeOf(wireBytes), `"!!!"`},
		"BYTES without padding":    {typeOf(wireBytes), `"YWJ"`},
		"DATE of text":             {typeOf(wireDate), `"yesterday"`},
		"DATE out of range":        {typeOf(wireDate), `"2024-13-01"`},
		"TIMESTAMP of text":        {typeOf(wireTimestamp), `"now"`},
		"TIMESTAMP with no zone":   {typeOf(wireTimestamp), `"2024-01-02T03:04:05"`},
		"JSON that is not JSON":    {typeOf(wireJSON), `"{"`},
		"JSON as an object":        {typeOf(wireJSON), `{}`},
		"UUID of text":             {typeOf(wireUUID), `"abc"`},
		"INTERVAL of text":         {typeOf(wireInterval), `"1 day"`},
		"ARRAY as a string":        {typeOf(wireArray, wireInt64), `"1"`},
		"ARRAY with a bad element": {typeOf(wireArray, wireInt64), `["1","x"]`},
		"STRUCT with no fields":    {typeOf(wireStruct), `[]`},
		"ARRAY with no element":    {typeOf(wireArray), `[]`},
		"a type that is not known": {typeOf("GEOGRAPHY"), `"x"`},
	} {
		got, err := tt.typ.decode(jsontext.Value(tt.in))
		if err == nil {
			t.Errorf("%s: got %#v and no error", name, got)
			continue
		}
		if !errors.Is(err, dbimp.ErrInvalidValue) && !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: the error is %v, want dbimp.ErrInvalidValue or dbimp.ErrNotSupported", name, err)
		}
	}
}

// TestJoin holds the join of two pieces: two strings join as text, two lists
// join as the protocol says, and any other pair is an error.
func TestJoin(t *testing.T) {
	t.Parallel()
	for name, tt := range map[string]struct {
		a, b, want string
	}{
		"strings":               {`"ab"`, `"cd"`, `"abcd"`},
		"a string with escapes": {`"a\n"`, `"\"b"`, `"a\n\"b"`},
		"empty pieces":          {`""`, `""`, `""`},
		"lists of strings":      {`["a","b"]`, `["c"]`, `["a","bc"]`},
		"lists of numbers":      {`[1,2]`, `[3]`, `[1,2,3]`},
		"lists of lists":        {`[["a"]]`, `[["b"],["c"]]`, `[["ab"],["c"]]`},
		"an empty list":         {`[]`, `["a"]`, `["a"]`},
	} {
		got, err := join(jsontext.Value(tt.a), jsontext.Value(tt.b))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		g, w := got, jsontext.Value(tt.want)
		_ = g.Canonicalize()
		_ = w.Canonicalize()
		if string(g) != string(w) {
			t.Errorf("%s: got %s, want %s", name, got, tt.want)
		}
	}
	for name, pair := range map[string][2]string{
		"a string and a list": {`"a"`, `["b"]`},
		"two numbers":         {`1`, `2`},
		"two objects":         {`{}`, `{}`},
	} {
		if _, err := join(jsontext.Value(pair[0]), jsontext.Value(pair[1])); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: the error is %v, want dbimp.ErrNotSupported", name, err)
		}
	}
}

// TestScanTypes holds the scan type of each type: one Go type for each wire type
// in every place (D135).
func TestScanTypes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		typ  *wireType
		want string
	}{
		{typeOf(wireBool), "bool"},
		{typeOf(wireInt64), "int64"},
		{typeOf(wireFloat32), "float64"},
		{typeOf(wireFloat64), "float64"},
		{typeOf(wireNumeric), "*apd.Decimal"},
		{typeOf(wireString), "string"},
		{typeOf(wireBytes), "[]uint8"},
		{typeOf(wireDate), "dbimp.Date"},
		{typeOf(wireTimestamp), "time.Time"},
		{typeOf(wireJSON), "interface {}"},
		{typeOf(wireUUID), "uuid.UUID"},
		{typeOf(wireInterval), "dbimp.Interval"},
		{typeOf(wireArray, wireInt64), "[]interface {}"},
		{typeOf(wireStruct), "[]interface {}"},
	} {
		if got := tt.typ.scanType().String(); got != tt.want {
			t.Errorf("the scan type of %s is %s, want %s", tt.typ.Code, got, tt.want)
		}
	}
}
