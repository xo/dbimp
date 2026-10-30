package tablestore //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"errors"
	"net/http"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// tablesServer starts a fake server that answers a request with the recorded
// exchange of the calls of CreateTable, CreateIndex and DeleteTable whose request
// is the same and whose status is status. A request that no exchange matches fails
// the test, so each test also checks the bytes that the connector sends.
func tablesServer(t *testing.T, status int) *Connector {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, "tablestore-tables", func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		return ex.Response.Status == status && dbimptest.DefaultMatch(r, body, ex)
	})
	return connectorAt(t, testConfig(), srv.URL)
}

// madeTable is the table of the recorded call "create a table with defined
// columns and an index".
func madeTable() Table {
	return Table{
		Name: "dbimp_it_h",
		Keys: []Column{{Name: "id", Type: String}, {Name: "n", Type: Integer}},
		Columns: []Column{
			{Name: "s", Type: String},
			{Name: "i", Type: Integer},
			{Name: "d", Type: Double},
			{Name: "b", Type: Boolean},
			{Name: "x", Type: Binary},
		},
		Indexes: []Index{{Name: "dbimp_it_h_ix", Keys: []string{"s"}, Columns: []string{"i"}}},
	}
}

// TestCreateTable sends the table with defined columns and an index, and the
// bytes are the bytes of the recorded request (recorded: "create a table with
// defined columns and an index").
func TestCreateTable(t *testing.T) {
	t.Parallel()
	if err := tablesServer(t, http.StatusOK).CreateTable(t.Context(), madeTable()); err != nil {
		t.Errorf("CreateTable: %v", err)
	}
	// A table with one key and one column, and no index.
	tbl := Table{Name: "dbimp_it_he", Keys: []Column{{Name: "id", Type: Integer}}, Columns: []Column{{Name: "s", Type: String}}}
	if err := tablesServer(t, http.StatusOK).CreateTable(t.Context(), tbl); err != nil {
		t.Errorf("CreateTable of a table with no index: %v", err)
	}
}

// TestCreateTableExists reads the answer for a table that is there, which is HTTP
// 409 with the code OTSObjectAlreadyExist (recorded: "create a table that is
// there").
func TestCreateTableExists(t *testing.T) {
	t.Parallel()
	err := tablesServer(t, http.StatusConflict).CreateTable(t.Context(), madeTable())
	e, ok := errors.AsType[*Error](err)
	if !errors.Is(err, ErrExists) || !ok || e.HTTPStatus != http.StatusConflict || e.Code != "OTSObjectAlreadyExist" {
		t.Errorf("CreateTable of a table that is there gave %v, want ErrExists from a 409", err)
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, dbimp.ErrAuthentication) {
		t.Errorf("CreateTable of a table that is there gave %v, which matches another sentinel", err)
	}
}

// TestCreateIndex sends an index on a table that is there, and reads the answers
// for an index that is there and for a table that is not (recorded: "create an
// index on a table that is there", "create an index that is there", "create an
// index on a table that is not there").
func TestCreateIndex(t *testing.T) {
	t.Parallel()
	if err := tablesServer(t, http.StatusOK).CreateIndex(t.Context(), "dbimp_it_he", Index{Name: "dbimp_it_he_ix", Keys: []string{"s"}}); err != nil {
		t.Errorf("CreateIndex: %v", err)
	}
	err := tablesServer(t, http.StatusConflict).CreateIndex(t.Context(), "dbimp_it_he", Index{Name: "dbimp_it_he_ix", Keys: []string{"s"}})
	if !errors.Is(err, ErrExists) {
		t.Errorf("CreateIndex of an index that is there gave %v, want ErrExists", err)
	}
	err = tablesServer(t, http.StatusNotFound).CreateIndex(t.Context(), "dbimp_it_hnone", Index{Name: "dbimp_it_hnone_ix", Keys: []string{"s"}})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateIndex on a table that is not there gave %v, want ErrNotFound", err)
	}
}

// TestDeleteTable sends a delete, and reads the answer for a table that is not
// there, which is HTTP 404 with the code OTSObjectNotExist (recorded: "delete a
// table that is there", "delete a table that is not there").
func TestDeleteTable(t *testing.T) {
	t.Parallel()
	if err := tablesServer(t, http.StatusOK).DeleteTable(t.Context(), "dbimp_it_he"); err != nil {
		t.Errorf("DeleteTable: %v", err)
	}
	err := tablesServer(t, http.StatusNotFound).DeleteTable(t.Context(), "dbimp_it_hnone")
	e, ok := errors.AsType[*Error](err)
	if !errors.Is(err, ErrNotFound) || !ok || e.HTTPStatus != http.StatusNotFound || e.Code != "OTSObjectNotExist" {
		t.Errorf("DeleteTable of a table that is not there gave %v, want ErrNotFound from a 404", err)
	}
}

// TestTableErrors holds what the connector refuses before it sends a request.
// The fake server has no exchange, so a request that reached it would fail the
// test.
func TestTableErrors(t *testing.T) {
	t.Parallel()
	c := tablesServer(t, http.StatusOK)
	key := []Column{{Name: "id", Type: Integer}}
	for name, tbl := range map[string]Table{
		"no name":                {Keys: key},
		"no key":                 {Name: "t"},
		"a key with no name":     {Name: "t", Keys: []Column{{Type: Integer}}},
		"a key of type Double":   {Name: "t", Keys: []Column{{Name: "id", Type: Double}}},
		"a key of type Boolean":  {Name: "t", Keys: []Column{{Name: "id", Type: Boolean}}},
		"a key of no type":       {Name: "t", Keys: []Column{{Name: "id"}}},
		"a column of no type":    {Name: "t", Keys: key, Columns: []Column{{Name: "s"}}},
		"a repeated name":        {Name: "t", Keys: key, Columns: []Column{{Name: "id", Type: String}}},
		"an index on a column":   {Name: "t", Keys: key, Indexes: []Index{{Name: "ix", Keys: []string{"nope"}}}},
		"an index with no key":   {Name: "t", Keys: key, Indexes: []Index{{Name: "ix"}}},
		"an index with no name":  {Name: "t", Keys: key, Indexes: []Index{{Keys: []string{"id"}}}},
		"an index of a column ":  {Name: "t", Keys: key, Indexes: []Index{{Name: "ix", Keys: []string{"id"}, Columns: []string{"nope"}}}},
		"a repeated column name": {Name: "t", Keys: key, Columns: []Column{{Name: "s", Type: String}, {Name: "s", Type: String}}},
	} {
		if err := c.CreateTable(t.Context(), tbl); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("CreateTable with %s gave %v, want dbimp.ErrInvalidValue", name, err)
		}
	}
	if err := c.DeleteTable(t.Context(), ""); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("DeleteTable with no name gave %v, want dbimp.ErrInvalidValue", err)
	}
	if err := c.CreateIndex(t.Context(), "", Index{Name: "ix", Keys: []string{"s"}}); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("CreateIndex with no table gave %v, want dbimp.ErrInvalidValue", err)
	}
	if err := c.CreateIndex(t.Context(), "t", Index{Name: "ix"}); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("CreateIndex with no key gave %v, want dbimp.ErrInvalidValue", err)
	}
}

// TestTableNoCredentials makes a call with a Config that holds no key, which
// fails before it sends anything.
func TestTableNoCredentials(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.User, cfg.Password = "", ""
	c := NewConnector(cfg)
	t.Cleanup(func() { _ = c.Close() })
	if err := c.DeleteTable(t.Context(), "t"); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("DeleteTable with no credentials gave %v, want ErrNoCredentials", err)
	}
}

// TestColumnTypeString names each type, and a type that does not exist.
func TestColumnTypeString(t *testing.T) {
	t.Parallel()
	for typ, want := range map[ColumnType]string{Integer: "Integer", Double: "Double", Boolean: "Boolean", String: "String", Binary: "Binary", 0: "ColumnType(0)"} {
		if got := typ.String(); got != want {
			t.Errorf("ColumnType(%d).String() = %q, want %q", uint8(typ), got, want)
		}
	}
}
