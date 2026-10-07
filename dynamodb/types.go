package dynamodb

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"fmt"

	"github.com/xo/dbimp"
)

// The types of the attribute values of DynamoDB. A value is an object with
// one member, whose name is its type, such as {"S": "x"} (recorded: "every
// type").
const (
	typeS    = "S"
	typeN    = "N"
	typeB    = "B"
	typeBOOL = "BOOL"
	typeNULL = "NULL"
	typeL    = "L"
	typeM    = "M"
	typeSS   = "SS"
	typeNS   = "NS"
	typeBS   = "BS"
)

// readAttribute reads one typed value from dec, and returns its Go value.
// The type of the value comes from the name of its member, as D135 says for
// a server that names no type of a column (D169). A string is a string, an
// N is an *apd.Decimal read from its text so that no digit is lost (D19 and
// D33), and a B is its bytes. A list is a []any, a map is a map[string]any,
// and a set is a []any of strings, of *apd.Decimal or of []byte. A value of
// a type that the driver does not know is an error, and never its text.
func readAttribute(dec *jsontext.Decoder) (any, error) {
	if err := expect(dec, '{'); err != nil {
		return nil, fmt.Errorf("reading a value: %w", err)
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return nil, fmt.Errorf("reading the type of a value: %w", err)
	}
	if tok.Kind() != '"' {
		return nil, fmt.Errorf("reading the type of a value: %v where a name was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
	}
	name := tok.String()
	v, err := readTyped(dec, name)
	if err != nil {
		return nil, fmt.Errorf("reading a value of the type %s: %w", name, err)
	}
	if dec.PeekKind() != '}' {
		return nil, fmt.Errorf("reading a value of the type %s: more than one type in a value: %w", name, dbimp.ErrInvalidValue)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading the end of a value: %w", err)
	}
	return v, nil
}

// readTyped reads the content of a value whose type is name.
func readTyped(dec *jsontext.Decoder, name string) (any, error) {
	switch name {
	case typeS:
		return readString(dec)
	case typeN:
		return readDecimal(dec)
	case typeB:
		return readBinary(dec)
	case typeBOOL:
		v, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		return dbimp.Bool(v)
	case typeNULL:
		v, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		if b, err := dbimp.Bool(v); err != nil || !b {
			return nil, fmt.Errorf("a NULL whose content is %s: %w", v, dbimp.ErrInvalidValue)
		}
		return nil, nil
	case typeL:
		return readList(dec)
	case typeM:
		return readMap(dec)
	case typeSS:
		return readSet(dec, readString)
	case typeNS:
		return readSet(dec, readDecimal)
	case typeBS:
		return readSet(dec, readBinary)
	}
	return nil, fmt.Errorf("the type is not one that DynamoDB has: %w", dbimp.ErrInvalidValue)
}

// readString reads a JSON string.
func readString(dec *jsontext.Decoder) (any, error) {
	v, err := dec.ReadValue()
	if err != nil {
		return nil, err
	}
	return dbimp.String(v)
}

// readDecimal reads a number that the server sends as a JSON string.
func readDecimal(dec *jsontext.Decoder) (any, error) {
	v, err := dec.ReadValue()
	if err != nil {
		return nil, err
	}
	if v.Kind() != '"' {
		return nil, fmt.Errorf("a number as %v, and not as a string: %w", v.Kind(), dbimp.ErrInvalidValue)
	}
	return dbimp.Decimal(v)
}

// readBinary reads a JSON string in base64.
func readBinary(dec *jsontext.Decoder) (any, error) {
	v, err := dec.ReadValue()
	if err != nil {
		return nil, err
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("reading base64: %w: %w", err, dbimp.ErrInvalidValue)
	}
	return b, nil
}

// readList reads a JSON array of typed values.
func readList(dec *jsontext.Decoder) (any, error) {
	if err := expect(dec, '['); err != nil {
		return nil, err
	}
	list := []any{}
	for dec.PeekKind() != ']' {
		v, err := readAttribute(dec)
		if err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}
	return list, nil
}

// readMap reads a JSON object whose values are typed values.
func readMap(dec *jsontext.Decoder) (any, error) {
	if err := expect(dec, '{'); err != nil {
		return nil, err
	}
	m := map[string]any{}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}
		key := tok.String()
		v, err := readAttribute(dec)
		if err != nil {
			return nil, fmt.Errorf("reading the key %q: %w", key, err)
		}
		m[key] = v
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}
	return m, nil
}

// readSet reads a JSON array of the values that elem reads.
func readSet(dec *jsontext.Decoder, elem func(*jsontext.Decoder) (any, error)) (any, error) {
	if err := expect(dec, '['); err != nil {
		return nil, err
	}
	set := []any{}
	for dec.PeekKind() != ']' {
		v, err := elem(dec)
		if err != nil {
			return nil, err
		}
		set = append(set, v)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}
	return set, nil
}

// expect reads one token, and returns an error if it is not the delimiter
// kind.
func expect(dec *jsontext.Decoder, kind jsontext.Kind) error {
	tok, err := dec.ReadToken()
	switch {
	case err != nil:
		return err
	case tok.Kind() != kind:
		return fmt.Errorf("%v where %v was expected: %w", tok.Kind(), kind, dbimp.ErrInvalidValue)
	}
	return nil
}
