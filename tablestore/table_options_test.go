package tablestore //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// message is a protobuf message that a test reads without the code of the
// package, so that the tests check the bytes against the protocol and not against
// the encoder: a field number maps to the values of that field, each a uint64
// for a varint or a []byte for a field of bytes.
type message map[int][]any

// parse reads the message in b.
func parse(t *testing.T, b []byte) message {
	t.Helper()
	m := message{}
	for len(b) > 0 {
		key, n := varint(t, b)
		b = b[n:]
		field, wire := int(key>>3), key&7
		switch wire {
		case 0:
			v, n := varint(t, b)
			b = b[n:]
			m[field] = append(m[field], v)
		case 2:
			l, n := varint(t, b)
			b = b[n:]
			if uint64(len(b)) < l {
				t.Fatalf("the field %d holds %d bytes, and %d are left", field, l, len(b))
			}
			m[field] = append(m[field], append([]byte(nil), b[:l]...))
			b = b[l:]
		default:
			t.Fatalf("the field %d has the wire type %d", field, wire)
		}
	}
	return m
}

// varint reads a varint, and returns it with the count of the bytes it took.
func varint(t *testing.T, b []byte) (uint64, int) {
	t.Helper()
	var v uint64
	for i := range min(len(b), 10) {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, i + 1
		}
	}
	t.Fatalf("no varint in %x", b)
	return 0, 0
}

// sub reads the message in the one field of m with the number field.
func (m message) sub(t *testing.T, field int) message {
	t.Helper()
	if len(m[field]) != 1 {
		t.Fatalf("the message has %d values for the field %d, want 1: %v", len(m[field]), field, m)
	}
	b, ok := m[field][0].([]byte)
	if !ok {
		t.Fatalf("the field %d is not a field of bytes: %v", field, m)
	}
	return parse(t, b)
}

// int returns the one varint of the field, as a signed 64 bit integer, and false
// when the field is absent.
func (m message) int(t *testing.T, field int) (int64, bool) {
	t.Helper()
	switch len(m[field]) {
	case 0:
		return 0, false
	case 1:
		v, ok := m[field][0].(uint64)
		if !ok {
			t.Fatalf("the field %d is not a varint: %v", field, m)
		}
		return int64(v), true //nolint:gosec // G115: protobuf writes a negative int64 as its two's complement.
	}
	t.Fatalf("the field %d is repeated: %v", field, m)
	return 0, false
}

// oneRequest returns a connector on a fake server that answers 200 with an empty
// body, and a function that returns the body of the one request that it got and
// the action that the request named.
func oneRequest(t *testing.T) (*Connector, func() (string, []byte)) {
	t.Helper()
	var (
		action string
		body   []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		if body, err = io.ReadAll(r.Body); err != nil {
			t.Errorf("reading the request: %v", err)
		}
		action = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return connectorAt(t, testConfig(), srv.URL), func() (string, []byte) { return action, body }
}

// tableMessage sends a table with the options, and returns the message of the
// request.
func tableMessage(t *testing.T, tbl Table, opts ...TableOption) message {
	t.Helper()
	c, got := oneRequest(t)
	if err := c.CreateTable(t.Context(), tbl, opts...); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	action, body := got()
	if action != "/CreateTable" {
		t.Fatalf("the request went to %s, want /CreateTable", action)
	}
	return parse(t, body)
}

func simpleTable() Table {
	return Table{Name: "t", Keys: []Column{{Name: "id", Type: Integer}}}
}

// TestTableDefaults checks that no option leaves the settings of D225: a time to
// live of -1, one version, zero throughput, and no other member.
func TestTableDefaults(t *testing.T) {
	t.Parallel()
	m := tableMessage(t, simpleTable())
	opts := m.sub(t, 3)
	if ttl, _ := opts.int(t, 1); ttl != -1 {
		t.Errorf("the time to live is %d, want -1", ttl)
	}
	if v, _ := opts.int(t, 2); v != 1 {
		t.Errorf("the number of versions is %d, want 1", v)
	}
	if len(opts) != 2 {
		t.Errorf("the table options are %v, want only the two members", opts)
	}
	units := m.sub(t, 2).sub(t, 1)
	if r, ok := units.int(t, 1); !ok || r != 0 {
		t.Errorf("the reserved read is %d, %v, want 0", r, ok)
	}
	if w, ok := units.int(t, 2); !ok || w != 0 {
		t.Errorf("the reserved write is %d, %v, want 0", w, ok)
	}
	for _, f := range []int{4, 5, 6, 7, 8} {
		if _, ok := m[f]; ok {
			t.Errorf("the request holds the field %d, which no option set", f)
		}
	}
}

// TestTableOptionsOnTheWire checks the bytes of each option of CreateTable
// against the numbers of the protocol (otsprotocol/table_store.proto): the time to
// live is the field 1 of TableOptions in seconds, the versions are the field 2,
// the deviation is the field 5 in seconds, allow_update is the field 6, the
// throughput is the capacity unit of the field 2 of the request, the stream is
// the field 5 of the request in hours, and the local transaction is the field 8.
func TestTableOptionsOnTheWire(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, m message, ttl, versions int64) message {
		t.Helper()
		opts := m.sub(t, 3)
		if got, _ := opts.int(t, 1); got != ttl {
			t.Errorf("the time to live is %d, want %d", got, ttl)
		}
		if got, _ := opts.int(t, 2); got != versions {
			t.Errorf("the number of versions is %d, want %d", got, versions)
		}
		return opts
	}
	t.Run("time to live", func(t *testing.T) {
		t.Parallel()
		check(t, tableMessage(t, simpleTable(), WithTimeToLive(36*time.Hour)), 129600, 1)
	})
	t.Run("versions", func(t *testing.T) {
		t.Parallel()
		check(t, tableMessage(t, simpleTable(), WithMaxVersions(5)), -1, 5)
	})
	t.Run("throughput", func(t *testing.T) {
		t.Parallel()
		units := tableMessage(t, simpleTable(), WithReservedThroughput(300, 400)).sub(t, 2).sub(t, 1)
		if r, _ := units.int(t, 1); r != 300 {
			t.Errorf("the reserved read is %d, want 300", r)
		}
		if w, _ := units.int(t, 2); w != 400 {
			t.Errorf("the reserved write is %d, want 400", w)
		}
	})
	t.Run("deviation", func(t *testing.T) {
		t.Parallel()
		opts := check(t, tableMessage(t, simpleTable(), WithDeviation(2*time.Hour)), -1, 1)
		if d, ok := opts.int(t, 5); !ok || d != 7200 {
			t.Errorf("the deviation is %d, %v, want 7200", d, ok)
		}
	})
	t.Run("allow update", func(t *testing.T) {
		t.Parallel()
		for _, allow := range []bool{false, true} {
			opts := tableMessage(t, simpleTable(), WithAllowUpdate(allow)).sub(t, 3)
			want := int64(0)
			if allow {
				want = 1
			}
			if v, ok := opts.int(t, 6); !ok || v != want {
				t.Errorf("allow_update is %d, %v for %v, want %d", v, ok, allow, want)
			}
		}
	})
	t.Run("stream", func(t *testing.T) {
		t.Parallel()
		stream := tableMessage(t, simpleTable(), WithStream(48*time.Hour)).sub(t, 5)
		if on, _ := stream.int(t, 1); on != 1 {
			t.Errorf("enable_stream is %d, want 1", on)
		}
		if h, _ := stream.int(t, 2); h != 48 {
			t.Errorf("the expiration is %d hours, want 48", h)
		}
	})
	t.Run("local transaction", func(t *testing.T) {
		t.Parallel()
		if v, ok := tableMessage(t, simpleTable(), WithLocalTransaction(true))[8]; !ok || len(v) != 1 || v[0] != uint64(1) {
			t.Errorf("enable_local_txn is %v, want 1", v)
		}
		if _, ok := tableMessage(t, simpleTable(), WithLocalTransaction(false))[8]; ok {
			t.Error("WithLocalTransaction(false) wrote enable_local_txn, want nothing")
		}
	})
	t.Run("a later option wins", func(t *testing.T) {
		t.Parallel()
		check(t, tableMessage(t, simpleTable(), WithMaxVersions(2), WithMaxVersions(7), WithTimeToLive(24*time.Hour)), 86400, 7)
	})
}

// TestTableOptionErrors holds each value that an option refuses. The call fails
// with dbimp.ErrInvalidValue and sends nothing, so a request that reached the fake
// server would fail the test.
func TestTableOptionErrors(t *testing.T) {
	t.Parallel()
	c := tablesServer(t, http.StatusOK)
	for name, opt := range map[string]TableOption{
		"a time to live of zero":      WithTimeToLive(0),
		"a negative time to live":     WithTimeToLive(-time.Second),
		"a fraction of a second":      WithTimeToLive(1500 * time.Millisecond),
		"a time to live too large":    WithTimeToLive(time.Duration(1<<31) * time.Second),
		"no version":                  WithMaxVersions(0),
		"a negative read":             WithReservedThroughput(-1, 0),
		"a negative write":            WithReservedThroughput(0, -1),
		"a deviation of zero":         WithDeviation(0),
		"a fraction of a deviation":   WithDeviation(time.Millisecond),
		"a stream of zero":            WithStream(0),
		"a fraction of an hour":       WithStream(90 * time.Minute),
		"a negative stream":           WithStream(-time.Hour),
		"a version above 32 bits":     WithMaxVersions(1 << 31),
		"a throughput above 32 bits":  WithReservedThroughput(1<<31, 0),
		"a deviation under a second ": WithDeviation(time.Second / 2),
	} {
		if err := c.CreateTable(t.Context(), simpleTable(), opt); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("CreateTable with %s gave %v, want dbimp.ErrInvalidValue", name, err)
		}
	}
}

// TestIndexSyncOnTheWire checks the update mode of an index, which is the field 4
// of IndexMeta: 0 for the background and 1 for each write, in the index of
// CreateIndex and in the index of CreateTable.
func TestIndexSyncOnTheWire(t *testing.T) {
	t.Parallel()
	for _, sync := range []bool{false, true} {
		want := int64(0)
		if sync {
			want = 1
		}
		c, got := oneRequest(t)
		if err := c.CreateIndex(t.Context(), "t", Index{Name: "ix", Keys: []string{"s"}, Sync: sync}); err != nil {
			t.Fatalf("CreateIndex: %v", err)
		}
		_, body := got()
		if mode, ok := parse(t, body).sub(t, 2).int(t, 4); !ok || mode != want {
			t.Errorf("the update mode of CreateIndex for Sync %v is %d, %v, want %d", sync, mode, ok, want)
		}
		tbl := simpleTable()
		tbl.Columns = []Column{{Name: "s", Type: String}}
		tbl.Indexes = []Index{{Name: "ix", Keys: []string{"s"}, Sync: sync}}
		m := tableMessage(t, tbl)
		if mode, ok := m.sub(t, 7).int(t, 4); !ok || mode != want {
			t.Errorf("the update mode of CreateTable for Sync %v is %d, %v, want %d", sync, mode, ok, want)
		}
	}
}

// TestBaseDataOnTheWire checks include_base_data, the field 3 of CreateIndexRequest:
// 1 with no option, and 0 or absent when WithBaseData(false) asks for no rows.
func TestBaseDataOnTheWire(t *testing.T) {
	t.Parallel()
	c, got := oneRequest(t)
	ix := Index{Name: "ix", Keys: []string{"s"}}
	if err := c.CreateIndex(t.Context(), "t", ix); err != nil {
		t.Fatal(err)
	}
	_, body := got()
	if v, ok := parse(t, body).int(t, 3); !ok || v != 1 {
		t.Errorf("include_base_data with no option is %d, %v, want 1", v, ok)
	}
	if err := c.CreateIndex(t.Context(), "t", ix, WithBaseData(false)); err != nil {
		t.Fatal(err)
	}
	_, body = got()
	if v, ok := parse(t, body).int(t, 3); ok && v != 0 {
		t.Errorf("include_base_data with WithBaseData(false) is %d, want 0 or absent", v)
	}
	if err := c.CreateIndex(t.Context(), "t", ix, WithBaseData(false), WithBaseData(true)); err != nil {
		t.Fatal(err)
	}
	_, body = got()
	if v, ok := parse(t, body).int(t, 3); !ok || v != 1 {
		t.Errorf("include_base_data after a later WithBaseData(true) is %d, %v, want 1", v, ok)
	}
}

// TestDeleteIfExists reads the answer for a table that is not there, which is HTTP
// 404 (recorded: "delete a table that is not there"). The call fails with it, and
// WithIfExists turns it into success. Another error stays an error.
func TestDeleteIfExists(t *testing.T) {
	t.Parallel()
	if err := tablesServer(t, http.StatusNotFound).DeleteTable(t.Context(), "dbimp_it_hnone", WithIfExists()); err != nil {
		t.Errorf("DeleteTable with WithIfExists gave %v for a table that is not there, want nil", err)
	}
	if err := tablesServer(t, http.StatusNotFound).DeleteTable(t.Context(), "dbimp_it_hnone"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteTable gave %v for a table that is not there, want ErrNotFound", err)
	}
	// A refusal of the credential is no missing table.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(appendString(appendString(nil, fieldErrorCode, codeAuthFailed), fieldErrorMessage, "Signature mismatch."))
	}))
	t.Cleanup(srv.Close)
	err := connectorAt(t, testConfig(), srv.URL).DeleteTable(t.Context(), "t", WithIfExists())
	if !errors.Is(err, dbimp.ErrAuthentication) {
		t.Errorf("DeleteTable with WithIfExists gave %v for a wrong secret, want dbimp.ErrAuthentication", err)
	}
}
