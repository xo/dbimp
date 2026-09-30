package neo4j

import (
	"bytes"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// wireTypes are the types of typed JSON, in the order of the type table, with
// the name of each in the survey of step 5a and its Go value (D63).
var wireTypes = []struct {
	name string
	typ  string
	goes string
}{
	{"null", "Null", "nil"},
	{"boolean", "Boolean", "bool"},
	{"integer", "Integer", "int64"},
	{"float", "Float", "float64"},
	{"string", "String", "string"},
	{"byte array", "Base64", "[]byte"},
	{"list", "List", "[]any"},
	{"map", "Map", "map[string]any"},
	{"date", "Date", "dbimp.Date"},
	{"local time", "LocalTime", "dbimp.LocalTime"},
	{"zoned time", "Time", "dbimp.OffsetTime"},
	{"local datetime", "LocalDateTime", "dbimp.LocalDateTime"},
	{"offset datetime", "OffsetDateTime", "time.Time"},
	{"zoned datetime", "ZonedDateTime", "time.Time, in the location that the zone names"},
	{"duration", "Duration", "dbimp.Interval"},
	{"point", "Point", "neo4j.Point"},
	{"node", "Node", "neo4j.Node"},
	{"relationship", "Relationship", "neo4j.Relationship"},
	{"path", "Path", "neo4j.Path"},
	{"vector", "Vector", "dbimp.Vector of the Go type of its coordinates, such as dbimp.Vector[float32]"},
	{"uuid", "UUID", "uuid.UUID"},
}

// decode returns the value of typed JSON v as its Go value (D63).
func decode(v jsontext.Value) (any, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return nil, fmt.Errorf("reading %s as a value of typed JSON: %w", v, dbimp.ErrInvalidValue)
	}
	var (
		typ string
		raw jsontext.Value
	)
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("reading a value of typed JSON: %w", err)
		}
		name := tok.String()
		val, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a value of typed JSON: %w", err)
		}
		switch name {
		case "$type":
			if typ, err = dbimp.String(val); err != nil {
				return nil, err
			}
		case "_value":
			raw = val.Clone()
		}
	}
	if typ == "" || raw == nil {
		return nil, fmt.Errorf("reading %s as a value of typed JSON: no $type or no _value: %w", v, dbimp.ErrInvalidValue)
	}
	return decodeTyped(typ, raw)
}

// decodeTyped returns the _value raw of the type typ as its Go value.
func decodeTyped(typ string, raw jsontext.Value) (any, error) {
	switch typ {
	case "Null":
		return nil, nil
	case "Boolean":
		return dbimp.Bool(raw)
	case "String":
		return dbimp.String(raw)
	case "List":
		return decodeList(raw)
	case "Map":
		return decodeMap(raw)
	case "Node":
		return decodeNode(raw)
	case "Relationship":
		return decodeRelationship(raw)
	case "Path":
		return decodePath(raw)
	case "Vector":
		return decodeVector(raw)
	case "Unsupported":
		return nil, fmt.Errorf("reading a value that typed JSON cannot write, %s: %w", raw, dbimp.ErrNotSupported)
	}
	s, err := dbimp.String(raw)
	if err != nil {
		return nil, fmt.Errorf("reading a %s: %w", typ, err)
	}
	return parseText(typ, s)
}

// parseText returns the text s of a value of the type typ, which typed JSON
// writes as a string, as its Go value.
func parseText(typ, s string) (any, error) {
	switch typ {
	case "Integer":
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, errForm(typ, s)
		}
		return i, nil
	case "Float":
		return parseFloat(s)
	case "Base64":
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, errForm(typ, s)
		}
		return b, nil
	case "UUID":
		u, err := uuid.Parse(s)
		if err != nil {
			return nil, errForm(typ, s)
		}
		return u, nil
	case "Date":
		return dbimp.ParseDate(s)
	case "LocalTime":
		return dbimp.ParseLocalTime(s)
	case "Time":
		return dbimp.ParseOffsetTime(s)
	case "LocalDateTime":
		return dbimp.ParseLocalDateTime(s)
	case "OffsetDateTime":
		return parseDateTime(typ, s, true, false)
	case "ZonedDateTime":
		return parseDateTime(typ, s, true, true)
	case "Duration":
		// A duration beyond the range of an Interval is an error (D138).
		return dbimp.ParseInterval(s)
	case "Point":
		return parsePoint(s)
	}
	return nil, fmt.Errorf("reading a value of the type %q: %w", typ, dbimp.ErrInvalidValue)
}

// decodeList reads a list of typed values.
func decodeList(raw jsontext.Value) ([]any, error) {
	var items []jsontext.Value
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("reading a List: %w", err)
	}
	list := make([]any, len(items))
	for i, item := range items {
		v, err := decode(item)
		if err != nil {
			return nil, err
		}
		list[i] = v
	}
	return list, nil
}

// decodeMap reads a map of typed values.
func decodeMap(raw jsontext.Value) (map[string]any, error) {
	var items map[string]jsontext.Value
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("reading a Map: %w", err)
	}
	m := make(map[string]any, len(items))
	for k, item := range items {
		v, err := decode(item)
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}

// decodeNode reads a Node.
func decodeNode(raw jsontext.Value) (Node, error) {
	var n struct {
		ID     string         `json:"_element_id"`
		Labels []string       `json:"_labels"`
		Props  jsontext.Value `json:"_properties"`
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		return Node{}, fmt.Errorf("reading a Node: %w", err)
	}
	props, err := decodeProps(n.Props)
	if err != nil {
		return Node{}, err
	}
	if n.Labels == nil {
		n.Labels = []string{}
	}
	return Node{ElementID: n.ID, Labels: n.Labels, Props: props}, nil
}

// decodeRelationship reads a Relationship.
func decodeRelationship(raw jsontext.Value) (Relationship, error) {
	var r struct {
		ID    string         `json:"_element_id"`
		Start string         `json:"_start_node_element_id"`
		End   string         `json:"_end_node_element_id"`
		Type  string         `json:"_type"`
		Props jsontext.Value `json:"_properties"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Relationship{}, fmt.Errorf("reading a Relationship: %w", err)
	}
	props, err := decodeProps(r.Props)
	if err != nil {
		return Relationship{}, err
	}
	return Relationship{ElementID: r.ID, StartElementID: r.Start, EndElementID: r.End, Type: r.Type, Props: props}, nil
}

// decodeProps reads the properties of a node or a relationship.
func decodeProps(raw jsontext.Value) (map[string]any, error) {
	if len(raw) == 0 || raw.Kind() == 'n' {
		return map[string]any{}, nil
	}
	return decodeMap(raw)
}

// decodePath reads a Path: a node, then a relationship and a node for each
// step of the path.
func decodePath(raw jsontext.Value) (Path, error) {
	items, err := decodeList(raw)
	if err != nil {
		return Path{}, err
	}
	p := Path{Nodes: []Node{}, Relationships: []Relationship{}}
	for i, item := range items {
		switch v := item.(type) {
		case Node:
			if i%2 == 0 {
				p.Nodes = append(p.Nodes, v)
				continue
			}
		case Relationship:
			if i%2 == 1 {
				p.Relationships = append(p.Relationships, v)
				continue
			}
		}
		return Path{}, fmt.Errorf("reading a Path: item %d is a %T: %w", i, item, dbimp.ErrInvalidValue)
	}
	if len(items)%2 == 0 {
		return Path{}, fmt.Errorf("reading a Path of %d items, which does not end with a node: %w", len(items), dbimp.ErrInvalidValue)
	}
	return p, nil
}

// decodeVector reads a Vector, whose coordinates typed JSON writes as
// strings, as a dbimp.Vector of the type of its coordinates (D139).
func decodeVector(raw jsontext.Value) (any, error) {
	var v struct {
		Type   string   `json:"coordinatesType"`
		Coords []string `json:"coordinates"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("reading a Vector: %w", err)
	}
	var (
		out any
		err error
	)
	switch v.Type {
	case coordsInt8:
		out = dbimp.Vector[int8](parseCoords[int8](v.Coords, 8, &err))
	case coordsInt16:
		out = dbimp.Vector[int16](parseCoords[int16](v.Coords, 16, &err))
	case coordsInt32:
		out = dbimp.Vector[int32](parseCoords[int32](v.Coords, 32, &err))
	case coordsInt64:
		out = dbimp.Vector[int64](parseCoords[int64](v.Coords, 64, &err))
	case coordsFloat32:
		out = dbimp.Vector[float32](parseFloatCoords[float32](v.Coords, 32, &err))
	case coordsFloat64:
		out = dbimp.Vector[float64](parseFloatCoords[float64](v.Coords, 64, &err))
	default:
		return nil, fmt.Errorf("reading a Vector of the type %q: %w", v.Type, dbimp.ErrInvalidValue)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parseCoords reads the integer coordinates of a vector, of bits bits.
func parseCoords[T int8 | int16 | int32 | int64](coords []string, bits int, errp *error) []T {
	out := make([]T, len(coords))
	for i, c := range coords {
		n, err := strconv.ParseInt(c, 10, bits)
		if err != nil {
			*errp = errForm("coordinate of a Vector", c)
			return nil
		}
		out[i] = T(n)
	}
	return out
}

// parseFloatCoords reads the float coordinates of a vector, of bits bits.
func parseFloatCoords[T float32 | float64](coords []string, bits int, errp *error) []T {
	out := make([]T, len(coords))
	for i, c := range coords {
		f, err := parseFloat(c)
		if err != nil || bits == 32 && !math.IsInf(f, 0) && !math.IsNaN(f) && math.Abs(f) > math.MaxFloat32 {
			*errp = errForm("coordinate of a Vector", c)
			return nil
		}
		out[i] = T(f)
	}
	return out
}

// The versions of typed JSON that an argument needs (D62).
const (
	// version10 is application/vnd.neo4j.query, which every release takes.
	version10 = iota
	// version11 is application/vnd.neo4j.query.v1.1, for a vector.
	version11
	// version12 is application/vnd.neo4j.query.v1.2, for a UUID.
	version12
)

// encode returns v as a value of typed JSON, and the version of typed JSON
// that it needs (D62 and D63).
func encode(v any) (jsontext.Value, int, error) {
	e := encoder{}
	if err := e.value(v); err != nil {
		return nil, 0, err
	}
	return e.b, e.version, nil
}

// encoder writes values of typed JSON.
type encoder struct {
	b       []byte
	version int
}

// typed writes a value of the type typ whose _value is a string.
func (e *encoder) typed(typ, s string) {
	e.b = append(e.b, `{"$type":"`...)
	e.b = append(e.b, typ...)
	e.b = append(e.b, `","_value":`...)
	e.b, _ = jsontext.AppendQuote(e.b, s)
	e.b = append(e.b, '}')
}

// open writes the start of a value of the type typ whose _value is the JSON
// that follows, which close ends.
func (e *encoder) open(typ string) {
	e.b = append(e.b, `{"$type":"`...)
	e.b = append(e.b, typ...)
	e.b = append(e.b, `","_value":`...)
}

func (e *encoder) close() {
	e.b = append(e.b, '}')
}

// value writes v.
func (e *encoder) value(v any) error {
	switch x := v.(type) {
	case nil:
		e.b = append(e.b, `{"$type":"Null","_value":null}`...)
		return nil
	case bool:
		e.open("Boolean")
		e.b = strconv.AppendBool(e.b, x)
		e.close()
		return nil
	case string:
		e.typed("String", x)
		return nil
	case []byte:
		if x == nil {
			return e.value(nil)
		}
		e.typed("Base64", base64.StdEncoding.EncodeToString(x))
		return nil
	case float32:
		e.typed("Float", formatFloat(float64(x), 32))
		return nil
	case float64:
		e.typed("Float", formatFloat(x, 64))
		return nil
	case time.Time:
		if name := x.Location().String(); name != "UTC" && name != "Local" && name != "" {
			e.typed("ZonedDateTime", formatDateTime(x, true))
		} else {
			e.typed("OffsetDateTime", formatDateTime(x, false))
		}
		return nil
	case dbimp.Date:
		e.typed("Date", x.String())
		return nil
	case dbimp.LocalTime:
		e.typed("LocalTime", x.String())
		return nil
	case dbimp.OffsetTime:
		e.typed("Time", x.String())
		return nil
	case dbimp.LocalDateTime:
		e.typed("LocalDateTime", x.String())
		return nil
	case dbimp.Interval:
		e.typed("Duration", x.String())
		return nil
	case time.Duration:
		e.typed("Duration", dbimp.Interval{Nanoseconds: int64(x)}.String())
		return nil
	case Point:
		e.typed("Point", x.String())
		return nil
	case dbimp.Vector[int8], dbimp.Vector[int16], dbimp.Vector[int32], dbimp.Vector[int64], dbimp.Vector[float32], dbimp.Vector[float64]:
		return e.vector(x)
	case uuid.UUID:
		e.version = max(e.version, version12)
		e.typed("UUID", x.String())
		return nil
	case Node, Relationship, Path:
		// The server refuses a node as an argument (measured).
		return fmt.Errorf("encoding a %T as an argument, which Neo4j refuses: %w", v, dbimp.ErrNotSupported)
	case *apd.Decimal, apd.Decimal:
		return fmt.Errorf("encoding a decimal: Neo4j has no decimal type (D63): %w", dbimp.ErrNotSupported)
	}
	return e.reflect(reflect.ValueOf(v))
}

// reflect writes a value of a kind that value does not name, such as an int,
// a slice or a map.
func (e *encoder) reflect(rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.typed("Integer", strconv.FormatInt(rv.Int(), 10))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if rv.Uint() > math.MaxInt64 {
			return fmt.Errorf("encoding %d, which is larger than an Integer: %w", rv.Uint(), dbimp.ErrInvalidValue)
		}
		e.typed("Integer", strconv.FormatUint(rv.Uint(), 10))
		return nil
	case reflect.Bool:
		return e.value(rv.Bool())
	case reflect.String:
		return e.value(rv.String())
	case reflect.Float32:
		return e.value(float32(rv.Float()))
	case reflect.Float64:
		return e.value(rv.Float())
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return e.value(nil)
		}
		return e.value(rv.Elem().Interface())
	case reflect.Slice:
		if rv.IsNil() {
			return e.value(nil)
		}
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return e.value(rv.Bytes())
		}
		fallthrough
	case reflect.Array:
		e.open("List")
		e.b = append(e.b, '[')
		for i := range rv.Len() {
			if i > 0 {
				e.b = append(e.b, ',')
			}
			if err := e.value(rv.Index(i).Interface()); err != nil {
				return err
			}
		}
		e.b = append(e.b, ']')
		e.close()
		return nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("encoding a map with keys of %s: %w", rv.Type().Key(), dbimp.ErrInvalidValue)
		}
		if rv.IsNil() {
			return e.value(nil)
		}
		keys := rv.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int {
			switch {
			case a.String() < b.String():
				return -1
			case a.String() > b.String():
				return 1
			}
			return 0
		})
		e.open("Map")
		e.b = append(e.b, '{')
		for i, k := range keys {
			if i > 0 {
				e.b = append(e.b, ',')
			}
			e.b, _ = jsontext.AppendQuote(e.b, k.String())
			e.b = append(e.b, ':')
			if err := e.value(rv.MapIndex(k).Interface()); err != nil {
				return err
			}
		}
		e.b = append(e.b, '}')
		e.close()
		return nil
	case reflect.Invalid:
		return e.value(nil)
	}
	return fmt.Errorf("encoding a %s as an argument: %w", rv.Type(), dbimp.ErrInvalidValue)
}

// vector writes a dbimp.Vector, which needs version 1.1 of typed JSON (D139).
// A plain slice is a list (D63).
func (e *encoder) vector(v any) error {
	var (
		typ    string
		coords []string
	)
	switch c := v.(type) {
	case dbimp.Vector[int8]:
		typ, coords = coordsInt8, formatInts(c)
	case dbimp.Vector[int16]:
		typ, coords = coordsInt16, formatInts(c)
	case dbimp.Vector[int32]:
		typ, coords = coordsInt32, formatInts(c)
	case dbimp.Vector[int64]:
		typ, coords = coordsInt64, formatInts(c)
	case dbimp.Vector[float32]:
		typ = coordsFloat32
		for _, f := range c {
			coords = append(coords, formatFloat(float64(f), 32))
		}
	case dbimp.Vector[float64]:
		typ = coordsFloat64
		for _, f := range c {
			coords = append(coords, formatFloat(f, 64))
		}
	default:
		return fmt.Errorf("encoding a vector of %T: %w", v, dbimp.ErrInvalidValue)
	}
	e.version = max(e.version, version11)
	e.open("Vector")
	e.b = append(e.b, `{"coordinatesType":"`...)
	e.b = append(e.b, typ...)
	e.b = append(e.b, `","coordinates":[`...)
	for i, c := range coords {
		if i > 0 {
			e.b = append(e.b, ',')
		}
		e.b, _ = jsontext.AppendQuote(e.b, c)
	}
	e.b = append(e.b, "]}"...)
	e.close()
	return nil
}

// formatInts writes the integer coordinates of a vector.
func formatInts[T int8 | int16 | int32 | int64](c []T) []string {
	out := make([]string, len(c))
	for i, n := range c {
		out[i] = strconv.FormatInt(int64(n), 10)
	}
	return out
}
