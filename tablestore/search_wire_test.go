package tablestore //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"testing"
	"time"
)

// text returns the one field of bytes of m with the number field, as text.
func (m message) text(t *testing.T, field int) string {
	t.Helper()
	if len(m[field]) != 1 {
		t.Fatalf("the message has %d values for the field %d, want 1: %v", len(m[field]), field, m)
	}
	b, ok := m[field][0].([]byte)
	if !ok {
		t.Fatalf("the field %d is not a field of bytes: %v", field, m)
	}
	return string(b)
}

// searchMessage sends the search index with the options, and returns the message
// of the request.
func searchMessage(t *testing.T, table string, ix SearchIndex, opts ...SearchIndexOption) message {
	t.Helper()
	c, got := oneRequest(t)
	if err := c.CreateSearchIndex(t.Context(), table, ix, opts...); err != nil {
		t.Fatalf("CreateSearchIndex: %v", err)
	}
	action, body := got()
	if action != "/CreateSearchIndex" {
		t.Fatalf("the request went to %s, want /CreateSearchIndex", action)
	}
	return parse(t, body)
}

// TestSearchIndexOnTheWire checks the bytes of CreateSearchIndex against the
// numbers of the protocol (otsprotocol/search.proto): the table is the field 1 of
// the request, the name is the field 2, the schema is the field 3, and each field of
// the schema is the field 1 of it, with the name as its field 1, the type as its
// field 2, index as its field 5, sort_and_agg as its field 6 and store as its field
// 7.
func TestSearchIndexOnTheWire(t *testing.T) {
	t.Parallel()
	m := searchMessage(t, "tbl", SearchIndex{Name: "ix", Fields: []SearchField{
		{Name: "l", Type: SearchLong},
		{Name: "d", Type: SearchDouble},
		{Name: "b", Type: SearchBoolean},
		{Name: "k", Type: SearchKeyword, Sort: true},
		{Name: "x", Type: SearchText, Store: true},
		{Name: "y", Type: SearchKeyword, Sort: true, Store: true},
	}})
	if got := m.text(t, 1); got != "tbl" {
		t.Errorf("the table is %q, want tbl", got)
	}
	if got := m.text(t, 2); got != "ix" {
		t.Errorf("the name is %q, want ix", got)
	}
	if _, ok := m[5]; ok {
		t.Errorf("the request holds a time to live, which no option set: %v", m)
	}
	schema := m.sub(t, 3)
	if len(schema) != 1 {
		t.Errorf("the schema holds the members %v, want only the fields", schema)
	}
	type want struct {
		name        string
		typ         int64
		sort, store bool
	}
	fields := []want{
		{"l", 1, false, false},
		{"d", 2, false, false},
		{"b", 3, false, false},
		{"k", 4, true, false},
		{"x", 5, false, true},
		{"y", 4, true, true},
	}
	if len(schema[1]) != len(fields) {
		t.Fatalf("the schema holds %d fields, want %d", len(schema[1]), len(fields))
	}
	for i, w := range fields {
		raw, ok := schema[1][i].([]byte)
		if !ok {
			t.Fatalf("field %d is not a field of bytes: %v", i, schema[1][i])
		}
		f := parse(t, raw)
		if got := f.text(t, 1); got != w.name {
			t.Errorf("field %d is named %q, want %q", i, got, w.name)
		}
		if got, _ := f.int(t, 2); got != w.typ {
			t.Errorf("field %s has the type %d, want %d", w.name, got, w.typ)
		}
		if got, ok := f.int(t, 5); !ok || got != 1 {
			t.Errorf("field %s has index %d, %v, want 1", w.name, got, ok)
		}
		for number, on := range map[int]bool{6: w.sort, 7: w.store} {
			got, ok := f.int(t, number)
			if on && (!ok || got != 1) || !on && ok {
				t.Errorf("field %s has the member %d as %d, %v, want %v", w.name, number, got, ok, on)
			}
		}
		if len(f) != 3+btoi(w.sort)+btoi(w.store) {
			t.Errorf("field %s holds the members %v, want only the name, the type, index and the flags that are on", w.name, f)
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestSearchTimeToLiveOnTheWire checks that the option is the field 5 of the
// request, in seconds.
func TestSearchTimeToLiveOnTheWire(t *testing.T) {
	t.Parallel()
	ix := SearchIndex{Name: "ix", Fields: []SearchField{{Name: "s", Type: SearchKeyword}}}
	m := searchMessage(t, "tbl", ix, WithSearchTimeToLive(36*time.Hour))
	if got, ok := m.int(t, 5); !ok || got != 129600 {
		t.Errorf("the time to live is %d, %v, want 129600", got, ok)
	}
	// The last option wins.
	m = searchMessage(t, "tbl", ix, WithSearchTimeToLive(24*time.Hour), WithSearchTimeToLive(48*time.Hour))
	if got, _ := m.int(t, 5); got != 172800 {
		t.Errorf("the time to live of two options is %d, want 172800", got)
	}
}

// TestSearchCallsOnTheWire checks the bodies of the calls that take a name.
func TestSearchCallsOnTheWire(t *testing.T) {
	t.Parallel()
	t.Run("DeleteSearchIndex", func(t *testing.T) {
		t.Parallel()
		c, got := oneRequest(t)
		if err := c.DeleteSearchIndex(t.Context(), "tbl", "ix"); err != nil {
			t.Fatal(err)
		}
		action, body := got()
		m := parse(t, body)
		if action != "/DeleteSearchIndex" || m.text(t, 1) != "tbl" || m.text(t, 2) != "ix" || len(m) != 2 {
			t.Errorf("DeleteSearchIndex sent %s %v, want the table as the field 1 and the name as the field 2", action, m)
		}
	})
	t.Run("ListSearchIndexes", func(t *testing.T) {
		t.Parallel()
		c, got := oneRequest(t)
		if _, err := c.ListSearchIndexes(t.Context(), "tbl"); err != nil {
			t.Fatal(err)
		}
		action, body := got()
		m := parse(t, body)
		if action != "/ListSearchIndex" || m.text(t, 1) != "tbl" || len(m) != 1 {
			t.Errorf("ListSearchIndexes sent %s %v, want the table as the field 1", action, m)
		}
	})
	t.Run("ListTables", func(t *testing.T) {
		t.Parallel()
		c, got := oneRequest(t)
		if _, err := c.ListTables(t.Context()); err != nil {
			t.Fatal(err)
		}
		if action, body := got(); action != "/ListTable" || len(body) != 0 {
			t.Errorf("ListTables sent %s with %d bytes, want /ListTable with none", action, len(body))
		}
	})
}
