package tablestore //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// The two recordings of D226: the release tablestore-search holds the calls on
// search indexes and ListTable, and the release tablestore-delete holds the calls
// that show how DeleteTable and a search index work together.
const (
	releaseSearch = "tablestore-search"
	releaseDelete = "tablestore-delete"
)

// replayOf starts a fake server that answers with the recorded exchange of the
// release whose request is the same as the one it gets, whose status is status,
// and whose response holds the text has. A request that no exchange matches fails
// the test, so each test also checks the bytes that the connector sends.
func replayOf(t *testing.T, release string, status int, has string) *Connector {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		return ex.Response.Status == status && bytes.Contains(ex.Response.Content(), []byte(has)) && dbimptest.DefaultMatch(r, body, ex)
	})
	return connectorAt(t, testConfig(), srv.URL)
}

// replayPath is replayOf for a request that the test does not build byte for
// byte, such as one that the wire tests check: it matches the action and not the
// body.
func replayPath(t *testing.T, release, path string, status int, has string) *Connector {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, func(r *http.Request, _ []byte, ex *dbimptest.Exchange) bool {
		return r.URL.Path == ex.Request.Path && ex.Request.Path == path && ex.Response.Status == status && bytes.Contains(ex.Response.Content(), []byte(has))
	})
	return connectorAt(t, testConfig(), srv.URL)
}

// answering returns a connector on a fake server that answers 200 with the body.
func answering(t *testing.T, body []byte) *Connector {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return connectorAt(t, testConfig(), srv.URL)
}

// TestListTables reads the answer of ListTable, which holds the name of each table
// in the field 1 (recorded: "list the tables").
func TestListTables(t *testing.T) {
	t.Parallel()
	got, err := replayOf(t, releaseSearch, http.StatusOK, "dbimp_it_s1").ListTables(t.Context())
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if want := []string{"dbimp_it_s1", "dbimp_it_s2"}; !slices.Equal(got, want) {
		t.Errorf("ListTables = %v, want %v", got, want)
	}
}

// TestListTablesAnswer reads answers that the server did not record: no table, a
// field of another wire type, and a message that ends inside a name.
func TestListTablesAnswer(t *testing.T) {
	t.Parallel()
	names := appendString(appendString(nil, 1, "a"), 1, "bc")
	for name, tc := range map[string]struct {
		body []byte
		want []string
	}{
		"no table":                           {nil, nil},
		"two names":                          {names, []string{"a", "bc"}},
		"a varint between names":             {appendString(appendInt(appendString(nil, 1, "a"), 2, 7), 1, "bc"), []string{"a", "bc"}},
		"a field of bytes of another number": {appendString(appendString(nil, 3, "x"), 1, "a"), []string{"a"}},
	} {
		got, err := answering(t, tc.body).ListTables(t.Context())
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("ListTables of %s = %v, %v, want %v", name, got, err, tc.want)
		}
	}
	// A message that ends inside a name is an error, and no name.
	for _, body := range [][]byte{names[:len(names)-1], {0x0a, 0x05, 'a'}, {0x0a}, {0x0a, 0xff}} {
		got, err := answering(t, body).ListTables(t.Context())
		if !errors.Is(err, dbimp.ErrInvalidValue) && err == nil || got != nil {
			t.Errorf("ListTables of the cut answer %x = %v, %v, want an error and no name", body, got, err)
		}
	}
}

// TestListSearchIndexes reads the names of the search indexes of a table, of a
// table with none, and the answer for a table that is not there (recorded: "list
// the search indexes of a table with none", "of a table with three", "of a table
// that is not there").
func TestListSearchIndexes(t *testing.T) {
	t.Parallel()
	s1 := replayOf(t, releaseSearch, http.StatusOK, "dbimp_it_s1_a")
	got, err := s1.ListSearchIndexes(t.Context(), "dbimp_it_s1")
	if want := []string{"dbimp_it_s1_a", "dbimp_it_s1_b"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("ListSearchIndexes = %v, %v, want %v", got, err, want)
	}
	none := replayOf(t, releaseSearch, http.StatusOK, "")
	got, err = none.ListSearchIndexes(t.Context(), "dbimp_it_s1")
	if err != nil || len(got) != 0 {
		t.Errorf("ListSearchIndexes of a table with none = %v, %v, want an empty list", got, err)
	}
	_, err = replayOf(t, releaseSearch, http.StatusBadRequest, "does not exist").ListSearchIndexes(t.Context(), "dbimp_it_sgone")
	e, ok := errors.AsType[*Error](err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || e.Code != "OTSParameterInvalid" || !strings.Contains(e.Message, "does not exist") {
		t.Errorf("ListSearchIndexes of a table that is not there gave %v, want OTSParameterInvalid from a 400", err)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("ListSearchIndexes of a table that is not there gave %v, want an error that matches ErrNotFound (D226)", err)
	}
}

// TestListSearchIndexesAnswer reads an answer that holds an index of another
// table, with the members that the driver ignores.
func TestListSearchIndexesAnswer(t *testing.T) {
	t.Parallel()
	info := func(table, name string) []byte {
		return appendBytes(nil, 1, appendInt(appendString(appendString(nil, 1, table), 2, name), 3, 9))
	}
	body := append(info("t", "a"), info("t", "b")...)
	got, err := answering(t, body).ListSearchIndexes(t.Context(), "t")
	if want := []string{"a", "b"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("ListSearchIndexes = %v, %v, want %v", got, err, want)
	}
	// A message inside the answer that ends inside a name is an error.
	cut := appendBytes(nil, 1, []byte{0x0a, 0x01, 't', 0x12, 0x09, 'a'})
	if got, err := answering(t, cut).ListSearchIndexes(t.Context(), "t"); err == nil || got != nil {
		t.Errorf("ListSearchIndexes of a cut answer = %v, %v, want an error and no name", got, err)
	}
}

// TestCreateSearchIndex sends the search indexes that were recorded, and the
// bytes are the bytes of the recorded requests (recorded: "create a search index
// with the flags of a field", "create the search index that is deleted", "create
// a search index with the flags of a field and a table that refuses updates").
func TestCreateSearchIndex(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		release string
		table   string
		ix      SearchIndex
	}{
		"flags": {releaseSearch, "dbimp_it_s1", SearchIndex{Name: "dbimp_it_s1_b", Fields: []SearchField{
			{Name: "s", Type: SearchKeyword, Sort: true, Store: true},
			{Name: "n", Type: SearchLong},
		}}},
		"one keyword": {releaseSearch, "dbimp_it_s2", SearchIndex{Name: "dbimp_it_s2_ok", Fields: []SearchField{{Name: "s", Type: SearchKeyword}}}},
		"keyword and text": {releaseDelete, "dbimp_it_d2", SearchIndex{Name: "dbimp_it_d2_c", Fields: []SearchField{
			{Name: "s", Type: SearchKeyword, Sort: true, Store: true},
			{Name: "t", Type: SearchText},
		}}},
	} {
		if err := replayOf(t, tc.release, http.StatusOK, "").CreateSearchIndex(t.Context(), tc.table, tc.ix); err != nil {
			t.Errorf("CreateSearchIndex with %s: %v", name, err)
		}
	}
}

// TestCreateSearchIndexTimeToLive sends a time to live to a table that allows
// updates, which the server refuses (recorded: "create a search index with a time
// to live"), so the bytes of the option are the recorded ones.
func TestCreateSearchIndexTimeToLive(t *testing.T) {
	t.Parallel()
	ix := SearchIndex{Name: "dbimp_it_s1_c", Fields: []SearchField{{Name: "s", Type: SearchKeyword}}}
	err := replayOf(t, releaseSearch, http.StatusBadRequest, "ttl").CreateSearchIndex(t.Context(), "dbimp_it_s1", ix, WithSearchTimeToLive(24*time.Hour))
	e, ok := errors.AsType[*Error](err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || e.Code != "OTSParameterInvalid" || !strings.Contains(e.Message, "does not support ttl") {
		t.Errorf("CreateSearchIndex with a time to live on a table that allows updates gave %v, want OTSParameterInvalid from a 400", err)
	}
}

// TestCreateSearchIndexErrors reads the answers for an index that is there and for
// a table that is not (recorded: "create a search index that is there", "create a
// search index on a table that is not there"), and for a time to live under one day
// (recorded: "create a search index with a time to live of one hour").
func TestCreateSearchIndexErrors(t *testing.T) {
	t.Parallel()
	ix := SearchIndex{Name: "i", Fields: []SearchField{{Name: "s", Type: SearchKeyword}}}
	err := replayPath(t, releaseSearch, "/CreateSearchIndex", http.StatusConflict, "already exist").CreateSearchIndex(t.Context(), "t", ix)
	e, ok := errors.AsType[*Error](err)
	if !errors.Is(err, ErrExists) || !ok || e.HTTPStatus != http.StatusConflict || e.Code != "OTSObjectAlreadyExist" {
		t.Errorf("CreateSearchIndex of an index that is there gave %v, want ErrExists from a 409", err)
	}
	err = replayPath(t, releaseSearch, "/CreateSearchIndex", http.StatusBadRequest, "does not exist").CreateSearchIndex(t.Context(), "t", ix)
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "OTSParameterInvalid" || !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateSearchIndex on a table that is not there gave %v, want OTSParameterInvalid, which matches ErrNotFound (D226)", err)
	}
	err = replayPath(t, releaseDelete, "/CreateSearchIndex", http.StatusBadRequest, "86400").CreateSearchIndex(t.Context(), "t", ix, WithSearchTimeToLive(time.Hour))
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "OTSParameterInvalid" || !strings.Contains(e.Message, "86400") {
		t.Errorf("CreateSearchIndex with a time to live of one hour gave %v, want OTSParameterInvalid that names 86400", err)
	}
}

// TestDeleteSearchIndex sends a delete, and reads the answers for an index that is
// not there and for a table that is not (recorded: "delete a search index that is
// there", "delete a search index that is not there", and the delete in the setup of
// the second recording).
func TestDeleteSearchIndex(t *testing.T) {
	t.Parallel()
	if err := replayOf(t, releaseDelete, http.StatusOK, "").DeleteSearchIndex(t.Context(), "dbimp_it_d2", "dbimp_it_d2_a"); err != nil {
		t.Errorf("DeleteSearchIndex: %v", err)
	}
	missing := func() *Connector { return replayOf(t, releaseSearch, http.StatusNotFound, "search index not exist") }
	err := missing().DeleteSearchIndex(t.Context(), "dbimp_it_s1", "dbimp_it_s1_none")
	e, ok := errors.AsType[*Error](err)
	if !errors.Is(err, ErrNotFound) || !ok || e.HTTPStatus != http.StatusNotFound || e.Code != "OTSObjectNotExist" {
		t.Errorf("DeleteSearchIndex of an index that is not there gave %v, want ErrNotFound from a 404", err)
	}
	if err := missing().DeleteSearchIndex(t.Context(), "dbimp_it_s1", "dbimp_it_s1_none", WithIfExists()); err != nil {
		t.Errorf("DeleteSearchIndex of an index that is not there with WithIfExists gave %v, want nil", err)
	}
	gone := func() *Connector { return replayOf(t, releaseDelete, http.StatusBadRequest, "does not exist") }
	err = gone().DeleteSearchIndex(t.Context(), "dbimp_it_d2", "dbimp_it_d2_a")
	if e, ok := errors.AsType[*Error](err); !ok || e.Code != "OTSParameterInvalid" || !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteSearchIndex on a table that is not there gave %v, want OTSParameterInvalid, which matches ErrNotFound (D226)", err)
	}
	if err := gone().DeleteSearchIndex(t.Context(), "dbimp_it_d2", "dbimp_it_d2_a", WithIfExists()); err == nil {
		t.Errorf("DeleteSearchIndex on a table that is not there with WithIfExists gave nil, want the error, because only a missing index is ignored")
	}
}

// TestDeleteTableWithSearchIndex reads the answer of DeleteTable for a table that
// has a search index, which is HTTP 400 and OTSParameterInvalid (recorded: "delete a
// table with three search indexes and an index").
func TestDeleteTableWithSearchIndex(t *testing.T) {
	t.Parallel()
	err := replayOf(t, releaseSearch, http.StatusBadRequest, "at least one search index").DeleteTable(t.Context(), "dbimp_it_s1")
	e, ok := errors.AsType[*Error](err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || e.Code != "OTSParameterInvalid" || !strings.Contains(e.Message, "at least one search index") {
		t.Errorf("DeleteTable of a table with a search index gave %v, want OTSParameterInvalid from a 400", err)
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrExists) {
		t.Errorf("DeleteTable of a table with a search index gave %v, which matches a sentinel", err)
	}
	// With the search indexes deleted, the table goes (recorded: "delete a table
	// whose search indexes are deleted").
	if err := replayOf(t, releaseDelete, http.StatusOK, "").DeleteTable(t.Context(), "dbimp_it_d2"); err != nil {
		t.Errorf("DeleteTable of a table with no search index: %v", err)
	}
}

// TestSearchErrors holds what the connector refuses before it sends a request.
// The fake server has no exchange, so a request that reached it would fail the
// test.
func TestSearchErrors(t *testing.T) {
	t.Parallel()
	c := replayOf(t, releaseSearch, http.StatusTeapot, "never")
	one := []SearchField{{Name: "s", Type: SearchKeyword}}
	for name, tc := range map[string]struct {
		table string
		ix    SearchIndex
		opts  []SearchIndexOption
	}{
		"no table":                  {"", SearchIndex{Name: "i", Fields: one}, nil},
		"no index name":             {"t", SearchIndex{Fields: one}, nil},
		"no field":                  {"t", SearchIndex{Name: "i"}, nil},
		"a field with no name":      {"t", SearchIndex{Name: "i", Fields: []SearchField{{Type: SearchKeyword}}}, nil},
		"a field with no type":      {"t", SearchIndex{Name: "i", Fields: []SearchField{{Name: "s"}}}, nil},
		"a field of a type to come": {"t", SearchIndex{Name: "i", Fields: []SearchField{{Name: "s", Type: SearchText + 1}}}, nil},
		"a repeated field":          {"t", SearchIndex{Name: "i", Fields: []SearchField{{Name: "s", Type: SearchKeyword}, {Name: "s", Type: SearchLong}}}, nil},
		"a time to live of zero":    {"t", SearchIndex{Name: "i", Fields: one}, []SearchIndexOption{WithSearchTimeToLive(0)}},
		"a negative time to live":   {"t", SearchIndex{Name: "i", Fields: one}, []SearchIndexOption{WithSearchTimeToLive(-time.Second)}},
		"half a second":             {"t", SearchIndex{Name: "i", Fields: one}, []SearchIndexOption{WithSearchTimeToLive(1500 * time.Millisecond)}},
		"too many seconds":          {"t", SearchIndex{Name: "i", Fields: one}, []SearchIndexOption{WithSearchTimeToLive(time.Duration(1<<31) * time.Second)}},
	} {
		if err := c.CreateSearchIndex(t.Context(), tc.table, tc.ix, tc.opts...); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("CreateSearchIndex with %s gave %v, want dbimp.ErrInvalidValue", name, err)
		}
	}
	for name, args := range map[string][2]string{"no table": {"", "i"}, "no index": {"t", ""}} {
		if err := c.DeleteSearchIndex(t.Context(), args[0], args[1]); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("DeleteSearchIndex with %s gave %v, want dbimp.ErrInvalidValue", name, err)
		}
	}
	if _, err := c.ListSearchIndexes(t.Context(), ""); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("ListSearchIndexes with no table gave %v, want dbimp.ErrInvalidValue", err)
	}
}

// TestSearchNoCredentials makes each call with a Config that holds no key, which
// fails before it sends anything.
func TestSearchNoCredentials(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.User, cfg.Password = "", ""
	c := NewConnector(cfg)
	t.Cleanup(func() { _ = c.Close() })
	ix := SearchIndex{Name: "i", Fields: []SearchField{{Name: "s", Type: SearchKeyword}}}
	if err := c.CreateSearchIndex(t.Context(), "t", ix); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("CreateSearchIndex with no credentials gave %v, want ErrNoCredentials", err)
	}
	if err := c.DeleteSearchIndex(t.Context(), "t", "i"); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("DeleteSearchIndex with no credentials gave %v, want ErrNoCredentials", err)
	}
	if _, err := c.ListSearchIndexes(t.Context(), "t"); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("ListSearchIndexes with no credentials gave %v, want ErrNoCredentials", err)
	}
	if _, err := c.ListTables(t.Context()); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("ListTables with no credentials gave %v, want ErrNoCredentials", err)
	}
}

// TestSearchFieldTypeString names each type, and a type that does not exist.
func TestSearchFieldTypeString(t *testing.T) {
	t.Parallel()
	for typ, want := range map[SearchFieldType]string{
		SearchLong: "SearchLong", SearchDouble: "SearchDouble", SearchBoolean: "SearchBoolean",
		SearchKeyword: "SearchKeyword", SearchText: "SearchText", 0: "SearchFieldType(0)",
	} {
		if got := typ.String(); got != want {
			t.Errorf("SearchFieldType(%d).String() = %q, want %q", uint8(typ), got, want)
		}
	}
}
