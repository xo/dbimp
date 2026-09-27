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

// decodeHex returns the bytes of s, a string of hex digits.
func decodeHex(tb testing.TB, s string) []byte {
	tb.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

// These come from Appendix A of RFC 8949.
func TestCBORHeads(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		hex   string
		major dbimp.CBORMajor
		arg   uint64
	}{
		{"00", dbimp.CBORUint, 0},
		{"17", dbimp.CBORUint, 23},
		{"1818", dbimp.CBORUint, 24},
		{"1903e8", dbimp.CBORUint, 1000},
		{"1a000f4240", dbimp.CBORUint, 1000000},
		{"1b000000e8d4a51000", dbimp.CBORUint, 1000000000000},
		{"1bffffffffffffffff", dbimp.CBORUint, math.MaxUint64},
		{"20", dbimp.CBORNegInt, 0},
		{"3903e7", dbimp.CBORNegInt, 999},
		{"6449455446", dbimp.CBORText, 4},
		{"83010203", dbimp.CBORArray, 3},
		{"a201020304", dbimp.CBORMap, 2},
		{"c11a514b67b0", dbimp.CBORTag, 1},
	} {
		d := dbimp.NewCBORDecoder(bytes.NewReader(decodeHex(t, tt.hex)))
		h, err := d.ReadHead()
		if err != nil {
			t.Errorf("reading the head of %s: %v", tt.hex, err)
			continue
		}
		if h.Major != tt.major || h.Arg != tt.arg {
			t.Errorf("the head of %s is %d %d, want %d %d", tt.hex, h.Major, h.Arg, tt.major, tt.arg)
		}
	}
}

func TestCBORFloats(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		hex  string
		want float64
	}{
		{"f90000", 0},
		{"f93c00", 1},
		{"f93e00", 1.5},
		{"f97bff", 65504},
		{"f90001", 5.960464477539063e-8},
		{"f9c400", -4},
		{"fa47c35000", 100000},
		{"fb3ff199999999999a", 1.1},
		{"fbc010666666666666", -4.1},
		{"f97c00", math.Inf(1)},
	} {
		h, err := dbimp.NewCBORDecoder(bytes.NewReader(decodeHex(t, tt.hex))).ReadHead()
		if err != nil {
			t.Fatal(err)
		}
		f, ok := h.Float()
		if !ok || f != tt.want {
			t.Errorf("%s is %v %v, want %v", tt.hex, f, ok, tt.want)
		}
	}
	h, err := dbimp.NewCBORDecoder(bytes.NewReader(decodeHex(t, "f97e00"))).ReadHead()
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := h.Float(); !ok || !math.IsNaN(f) {
		t.Errorf("f97e00 is %v %v, want NaN", f, ok)
	}
}

func TestCBORSimple(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		hex        string
		null       bool
		value, bok bool
	}{
		{"f4", false, false, true},
		{"f5", false, true, true},
		{"f6", true, false, false},
		{"f7", true, false, false},
	} {
		h, err := dbimp.NewCBORDecoder(bytes.NewReader(decodeHex(t, tt.hex))).ReadHead()
		if err != nil {
			t.Fatal(err)
		}
		v, ok := h.Bool()
		if h.Null() != tt.null || v != tt.value || ok != tt.bok {
			t.Errorf("%s: null %v, bool %v %v", tt.hex, h.Null(), v, ok)
		}
	}
}

func TestCBORStrings(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		hex  string
		want string
	}{
		{"60", ""},
		{"6449455446", "IETF"},
		{"62c3bc", "ü"},
		{"4401020304", "\x01\x02\x03\x04"},
		{"5f42010243030405ff", "\x01\x02\x03\x04\x05"},
		{"7f657374726561646d696e67ff", "streaming"},
	} {
		d := dbimp.NewCBORDecoder(bytes.NewReader(decodeHex(t, tt.hex)))
		h, err := d.ReadHead()
		if err != nil {
			t.Fatal(err)
		}
		b, err := d.ReadString(h)
		if err != nil || string(b) != tt.want {
			t.Errorf("%s is %q, %v, want %q", tt.hex, b, err, tt.want)
		}
	}
}

func TestCBORContainers(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		hex  string
		want int
	}{
		{"80", 0},
		{"83010203", 3},
		{"9f018202039f0405ffff", 3},
		{"9fff", 0},
		{"a26161016162820203", 4},
		{"bf6346756ef563416d7421ff", 4},
	} {
		d := dbimp.NewCBORDecoder(bytes.NewReader(decodeHex(t, tt.hex)))
		h, err := d.ReadHead()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for {
			more, err := d.More(h, uint64(n))
			if err != nil {
				t.Fatalf("%s: %v", tt.hex, err)
			}
			if !more {
				break
			}
			if err := d.Skip(); err != nil {
				t.Fatalf("%s: skipping item %d: %v", tt.hex, n, err)
			}
			n++
		}
		if n != tt.want {
			t.Errorf("%s holds %d items, want %d", tt.hex, n, tt.want)
		}
		if _, err := d.PeekHead(); !errors.Is(err, io.EOF) {
			t.Errorf("%s: after the container, the decoder holds more: %v", tt.hex, err)
		}
	}
}

func TestCBORReadRawKeepsTheBytes(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"00", "3903e7", "f93e00", "fb3ff199999999999a", "f6",
		"6449455446", "5f42010243030405ff", "7f657374726561646d696e67ff",
		"83010203", "9f018202039f0405ffff", "a26161016162820203",
		"bf6346756ef563416d7421ff", "c11a514b67b0", "d82076687474703a2f2f7777772e6578616d706c652e636f6d",
	} {
		in := decodeHex(t, s)
		d := dbimp.NewCBORDecoder(bytes.NewReader(append(bytes.Clone(in), 0x01)))
		got, err := d.ReadRaw()
		if err != nil || !bytes.Equal(got, in) {
			t.Errorf("ReadRaw of %s gave %x, %v", s, got, err)
		}
		h, err := d.ReadHead()
		if err != nil || h.Arg != 1 {
			t.Errorf("after %s, the next item is %+v, %v, want 1", s, h, err)
		}
	}
}

func TestCBORRefusesMalformedInput(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		hex  string
		want error
	}{
		{"a reserved additional information", "1c", dbimp.ErrInvalidValue},
		{"an integer of indefinite length", "1f", dbimp.ErrInvalidValue},
		{"a tag of indefinite length", "df", dbimp.ErrInvalidValue},
		{"a break alone", "ff", dbimp.ErrInvalidValue},
		{"a chunk of another type", "5f6161ff", dbimp.ErrInvalidValue},
		{"a head cut short", "19", io.ErrUnexpectedEOF},
		{"a string cut short", "6449", io.ErrUnexpectedEOF},
		{"an array cut short", "8301", io.ErrUnexpectedEOF},
		{"an array of indefinite length with no break", "9f01", io.ErrUnexpectedEOF},
		{"a length that lies", "7b7fffffffffffffff61", io.ErrUnexpectedEOF},
	} {
		d := dbimp.NewCBORDecoder(bytes.NewReader(decodeHex(t, tt.hex)))
		if _, err := d.ReadRaw(); !errors.Is(err, tt.want) {
			t.Errorf("%s: ReadRaw gave %v, want %v", tt.name, err, tt.want)
		}
	}
	deep := bytes.Repeat([]byte{0x81}, 1000)
	if _, err := dbimp.NewCBORDecoder(bytes.NewReader(append(deep, 0x01))).ReadRaw(); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("1000 nested arrays gave %v, want ErrInvalidValue", err)
	}
	if _, err := dbimp.NewCBORDecoder(bytes.NewReader(nil)).ReadRaw(); !errors.Is(err, io.EOF) {
		t.Errorf("no input gave %v, want io.EOF", err)
	}
}

func TestCBOREncoder(t *testing.T) {
	t.Parallel()
	var e dbimp.CBOREncoder
	e.Int(0)
	e.Int(23)
	e.Int(24)
	e.Int(1000)
	e.Int(-1)
	e.Int(-1000)
	e.Int(math.MinInt64)
	e.Uint(math.MaxUint64)
	e.Float(1.1)
	e.Bool(true)
	e.Bool(false)
	e.Null()
	e.Text("IETF")
	e.ByteString([]byte{1, 2})
	e.Array(2)
	e.Int(1)
	e.Int(2)
	e.Map(1)
	e.Text("a")
	e.Int(1)
	e.Tag(1)
	e.Int(1363896240)
	want := "00 17 1818 1903e8 20 3903e7 3b7fffffffffffffff 1bffffffffffffffff fb3ff199999999999a f5 f4 f6 6449455446 420102 820102 a1616101 c11a514b67b0"
	if got := hex.EncodeToString(e.Bytes()); got != strings.ReplaceAll(want, " ", "") {
		t.Errorf("the encoder wrote %s, want %s", got, want)
	}
}

func FuzzCBORReadRaw(f *testing.F) {
	for _, s := range []string{"00", "9f018202039f0405ffff", "bf6346756ef563416d7421ff", "5f42010243030405ff", "c11a514b67b0", "7b7fffffffffffffff61"} {
		b, _ := hex.DecodeString(s)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		d := dbimp.NewCBORDecoder(bytes.NewReader(in))
		raw, err := d.ReadRaw()
		if err != nil {
			return
		}
		if !bytes.HasPrefix(in, raw) {
			t.Fatalf("ReadRaw of %x gave %x, which is not a prefix", in, raw)
		}
		again, err := dbimp.NewCBORDecoder(bytes.NewReader(raw)).ReadRaw()
		if err != nil || !bytes.Equal(again, raw) {
			t.Fatalf("reading %x again gave %x, %v", raw, again, err)
		}
	})
}
