package dbimp_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

func TestThriftRoundTrip(t *testing.T) {
	t.Parallel()
	var e dbimp.ThriftEncoder
	e.Message("OpenSession", dbimp.ThriftCall, 7)
	e.Field(dbimp.ThriftStruct, 1)
	e.Field(dbimp.ThriftI32, 1)
	e.I32(9)
	e.Field(dbimp.ThriftString, 2)
	e.String("hive")
	e.Field(dbimp.ThriftBool, 3)
	e.Bool(true)
	e.Field(dbimp.ThriftI8, 4)
	e.I8(-2)
	e.Field(dbimp.ThriftI16, 5)
	e.I16(-300)
	e.Field(dbimp.ThriftI64, 6)
	e.I64(math.MinInt64)
	e.Field(dbimp.ThriftDouble, 7)
	e.Double(1.5)
	e.Field(dbimp.ThriftString, 8)
	e.Binary([]byte{0, 1, 2})
	e.Field(dbimp.ThriftMap, 9)
	e.Map(dbimp.ThriftString, dbimp.ThriftString, 1)
	e.String("k")
	e.String("v")
	e.Field(dbimp.ThriftList, 10)
	e.List(dbimp.ThriftI32, 2)
	e.I32(1)
	e.I32(2)
	e.Stop()
	e.Stop()
	d := dbimp.NewThriftDecoder(bytes.NewReader(e.Bytes()))
	name, mt, seq, err := d.ReadMessage()
	if err != nil || name != "OpenSession" || mt != dbimp.ThriftCall || seq != 7 {
		t.Fatalf("ReadMessage() = %q, %d, %d, %v", name, mt, seq, err)
	}
	if ft, id, err := d.ReadField(); err != nil || ft != dbimp.ThriftStruct || id != 1 {
		t.Fatalf("ReadField() = %d, %d, %v", ft, id, err)
	}
	var got []any
	for {
		ft, id, err := d.ReadField()
		if err != nil {
			t.Fatalf("ReadField() error: %v", err)
		}
		if ft == dbimp.ThriftStop {
			break
		}
		var v any
		switch id {
		case 1:
			v, err = d.ReadI32()
		case 2:
			v, err = d.ReadString()
		case 3:
			v, err = d.ReadBool()
		case 4:
			v, err = d.ReadI8()
		case 5:
			v, err = d.ReadI16()
		case 6:
			v, err = d.ReadI64()
		case 7:
			v, err = d.ReadDouble()
		default:
			err = d.Skip(ft)
		}
		if err != nil {
			t.Fatalf("reading field %d: %v", id, err)
		}
		if id <= 7 {
			got = append(got, v)
		}
	}
	want := []any{int32(9), "hive", true, int8(-2), int16(-300), int64(math.MinInt64), 1.5}
	if len(got) != len(want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %v, want %v", i+1, got[i], want[i])
		}
	}
	if ft, _, err := d.ReadField(); err != nil || ft != dbimp.ThriftStop {
		t.Errorf("the end of the args = %d, %v", ft, err)
	}
	if _, err := d.ReadI8(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("ReadI8() after the end = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestThriftReadsARecordedReply reads the reply of Hive 4.2.1 to OpenSession,
// recorded on 2026-11-09.
func TestThriftReadsARecordedReply(t *testing.T) {
	t.Parallel()
	const reply = "800100020000000b4f70656e53657373696f6e00000001" +
		"0c00000c000108000100000000000800020000000" + "90c00030c00010b00010000001013edc5a849e44bdab4339fb893a1930e" +
		"0b000200000010" + "2fcf6cb743aa48428e23bb798558f9e0" + "00000d00040b0b0000000100000001" + "6b0000000176" + "0000"
	b, err := hex.DecodeString(reply)
	if err != nil {
		t.Fatal(err)
	}
	d := dbimp.NewThriftDecoder(bytes.NewReader(b))
	name, mt, _, err := d.ReadMessage()
	if err != nil || name != "OpenSession" || mt != dbimp.ThriftReply {
		t.Fatalf("ReadMessage() = %q, %d, %v", name, mt, err)
	}
	if ft, id, err := d.ReadField(); err != nil || ft != dbimp.ThriftStruct || id != 0 {
		t.Fatalf("ReadField() = %d, %d, %v", ft, id, err)
	}
	var version int32
	var guid []byte
	for {
		ft, id, err := d.ReadField()
		if err != nil {
			t.Fatalf("ReadField() error: %v", err)
		}
		if ft == dbimp.ThriftStop {
			break
		}
		switch id {
		case 2:
			version, err = d.ReadI32()
		case 3:
			guid, err = readHandleGUID(d)
		default:
			err = d.Skip(ft)
		}
		if err != nil {
			t.Fatalf("reading field %d: %v", id, err)
		}
	}
	if version != 9 {
		t.Errorf("the protocol version = %d, want 9", version)
	}
	if hex.EncodeToString(guid) != "13edc5a849e44bdab4339fb893a1930e" {
		t.Errorf("the guid = %x", guid)
	}
}

// readHandleGUID reads a session handle, which holds a handle identifier,
// which holds the guid and the secret.
func readHandleGUID(d *dbimp.ThriftDecoder) ([]byte, error) {
	var guid []byte
	for {
		ft, _, err := d.ReadField()
		if err != nil {
			return nil, err
		}
		if ft == dbimp.ThriftStop {
			return guid, nil
		}
		// The member is the identifier struct.
		for {
			it, id, err := d.ReadField()
			if err != nil {
				return nil, err
			}
			if it == dbimp.ThriftStop {
				break
			}
			if id == 1 {
				if guid, err = d.ReadBinary(); err != nil {
					return nil, err
				}
				continue
			}
			if err := d.Skip(it); err != nil {
				return nil, err
			}
		}
	}
}

func TestThriftRejectsHostileInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		hex  string
		skip dbimp.ThriftType
	}{
		{"a string that claims 2 GiB", "7fffffff61", dbimp.ThriftString},
		{"a negative string length", "ffffffff", dbimp.ThriftString},
		{"a list that claims a billion elements", "08" + "3b9aca00", dbimp.ThriftList},
		{"a struct nested too deep", strings.Repeat("0c0001", 100), dbimp.ThriftStruct},
		{"a type that does not exist", "", dbimp.ThriftType(99)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := hex.DecodeString(tt.hex)
			if err != nil {
				t.Fatal(err)
			}
			if err := dbimp.NewThriftDecoder(bytes.NewReader(b)).Skip(tt.skip); err == nil {
				t.Fatal("Skip() = nil, want an error")
			}
		})
	}
	t.Run("a version that is not strict", func(t *testing.T) {
		t.Parallel()
		_, _, _, err := dbimp.NewThriftDecoder(bytes.NewReader([]byte{0, 0, 0, 5, 'h', 'e', 'l', 'l', 'o'})).ReadMessage()
		if !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("ReadMessage() = %v, want ErrInvalidValue", err)
		}
	})
}
