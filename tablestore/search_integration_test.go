package tablestore_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/tablestore"
)

// These tests make search indexes, list them and the tables, and delete them, as
// D226 describes. They leave nothing behind, and TestMain deletes a table and the
// search indexes that a test left.

// searchTable returns a table with a string key and a column of each type that a
// search index can name, and a secondary index on the string column.
func searchTable(name string) tablestore.Table {
	return tablestore.Table{
		Name: name,
		Keys: []tablestore.Column{{Name: "id", Type: tablestore.String}},
		Columns: []tablestore.Column{
			{Name: "s", Type: tablestore.String},
			{Name: "n", Type: tablestore.Integer},
			{Name: "d", Type: tablestore.Double},
			{Name: "b", Type: tablestore.Boolean},
		},
		Indexes: []tablestore.Index{{Name: name + "_ix", Keys: []string{"s"}, Columns: []string{"n"}}},
	}
}

// createSearch makes the search index, and deletes it in a cleanup. The cleanup
// runs before the cleanup of the table, which create registered first, because the
// server refuses to delete a table that has a search index.
func createSearch(t *testing.T, c *tablestore.Connector, table string, ix tablestore.SearchIndex, opts ...tablestore.SearchIndexOption) {
	t.Helper()
	if err := retry(func() error { return c.CreateSearchIndex(t.Context(), table, ix, opts...) }); err != nil {
		t.Fatalf("creating search index %s: %v", ix.Name, err)
	}
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		_ = retry(func() error { return c.DeleteSearchIndex(ctx, table, ix.Name, tablestore.WithIfExists()) })
	})
}

// TestIntegrationSearchIndex makes a table with a secondary index and a search
// index with a field of four types, and lists them (recorded: "create a search
// index with four fields and no flags", "list the search indexes of a table with
// three", "list the tables").
func TestIntegrationSearchIndex(t *testing.T) {
	c := connector(t)
	name := table("search")
	create(t, c, searchTable(name))
	si := name + "_si"
	start := time.Now()
	createSearch(t, c, name, tablestore.SearchIndex{Name: si, Fields: []tablestore.SearchField{
		{Name: "s", Type: tablestore.SearchKeyword, Sort: true},
		{Name: "n", Type: tablestore.SearchLong},
		{Name: "d", Type: tablestore.SearchDouble},
		{Name: "b", Type: tablestore.SearchBoolean},
	}})
	t.Logf("CreateSearchIndex took %v", time.Since(start))
	names, err := c.ListSearchIndexes(t.Context(), name)
	if err != nil || !slices.Equal(names, []string{si}) {
		t.Errorf("ListSearchIndexes = %v, %v, want [%s]", names, err, si)
	}
	tables, err := c.ListTables(t.Context())
	if err != nil || !slices.Contains(tables, name) {
		t.Errorf("ListTables = %v, %v, want a list that holds %s", tables, err, name)
	}
}

// TestIntegrationSearchIndexOrder shows that DeleteTable refuses a table that has a
// search index, and that the table goes after the search index (recorded: "delete a
// table with three search indexes and an index", "delete a table whose search
// indexes are deleted").
func TestIntegrationSearchIndexOrder(t *testing.T) {
	c := connector(t)
	name := table("order")
	tbl := searchTable(name)
	create(t, c, tbl)
	si := tablestore.SearchIndex{Name: name + "_si", Fields: []tablestore.SearchField{{Name: "s", Type: tablestore.SearchKeyword}}}
	createSearch(t, c, name, si)
	err := retry(func() error { return c.CreateSearchIndex(t.Context(), name, si) })
	if !errors.Is(err, tablestore.ErrExists) {
		t.Errorf("creating a search index that is there gave %v, want ErrExists", err)
	}
	err = retry(func() error { return c.DeleteTable(t.Context(), name) })
	var e *tablestore.Error
	if !errors.As(err, &e) || e.Code != "OTSParameterInvalid" || !strings.Contains(e.Message, "at least one search index") {
		t.Fatalf("deleting a table with a search index gave %v, want OTSParameterInvalid that names the search index", err)
	}
	if err := retry(func() error { return c.DeleteSearchIndex(t.Context(), name, si.Name) }); err != nil {
		t.Fatalf("deleting the search index: %v", err)
	}
	err = retry(func() error { return c.DeleteSearchIndex(t.Context(), name, si.Name) })
	if !errors.Is(err, tablestore.ErrNotFound) {
		t.Errorf("deleting a search index twice gave %v, want ErrNotFound the second time", err)
	}
	if err := retry(func() error { return c.DeleteSearchIndex(t.Context(), name, si.Name, tablestore.WithIfExists()) }); err != nil {
		t.Errorf("deleting a search index twice with WithIfExists gave %v, want nil", err)
	}
	if names, err := c.ListSearchIndexes(t.Context(), name); err != nil || len(names) != 0 {
		t.Errorf("ListSearchIndexes after the delete = %v, %v, want an empty list", names, err)
	}
	// The secondary index goes with the table, so the same table and index can be
	// made again (recorded: "delete a table with an index and no search index").
	if err := retry(func() error { return c.DeleteTable(t.Context(), name) }); err != nil {
		t.Fatalf("deleting the table: %v", err)
	}
	tables, err := c.ListTables(t.Context())
	if err != nil || slices.Contains(tables, name) {
		t.Errorf("ListTables after the delete = %v, %v, want a list without %s", tables, err, name)
	}
	create(t, c, tbl)
}

// TestIntegrationSearchIndexErrors holds the answers for a table that is not there,
// which the server gives as OTSParameterInvalid, and which matches ErrNotFound (D226).
func TestIntegrationSearchIndexErrors(t *testing.T) {
	c := connector(t)
	gone := table("gone")
	ix := tablestore.SearchIndex{Name: "ix", Fields: []tablestore.SearchField{{Name: "s", Type: tablestore.SearchKeyword}}}
	check := func(what string, err error) {
		t.Helper()
		var e *tablestore.Error
		if !errors.As(err, &e) || e.Code != "OTSParameterInvalid" || !errors.Is(err, tablestore.ErrNotFound) {
			t.Errorf("%s gave %v, want OTSParameterInvalid, which matches ErrNotFound", what, err)
		}
	}
	check("CreateSearchIndex on a table that is not there", retry(func() error { return c.CreateSearchIndex(t.Context(), gone, ix) }))
	check("DeleteSearchIndex on a table that is not there", retry(func() error { return c.DeleteSearchIndex(t.Context(), gone, "ix") }))
	_, err := c.ListSearchIndexes(t.Context(), gone)
	check("ListSearchIndexes on a table that is not there", err)
	name := table("errors")
	create(t, c, searchTable(name))
	err = retry(func() error { return c.DeleteSearchIndex(t.Context(), name, "none") })
	if !errors.Is(err, tablestore.ErrNotFound) {
		t.Errorf("DeleteSearchIndex of an index that is not there gave %v, want ErrNotFound", err)
	}
}

// TestIntegrationSearchIndexTimeToLive makes a search index with a time to live on a
// table that refuses updates, which the server accepts from one day (recorded:
// "create a search index with a time to live of one day"), and sends it to a table
// that allows updates, which the server refuses. It also makes a field of Text
// (recorded: "describe the search index with a time to live").
func TestIntegrationSearchIndexTimeToLive(t *testing.T) {
	c := connector(t)
	closed := table("closed")
	tbl := tablestore.Table{
		Name:    closed,
		Keys:    []tablestore.Column{{Name: "id", Type: tablestore.String}},
		Columns: []tablestore.Column{{Name: "s", Type: tablestore.String}, {Name: "t", Type: tablestore.String}},
	}
	create(t, c, tbl, tablestore.WithAllowUpdate(false))
	fields := []tablestore.SearchField{{Name: "s", Type: tablestore.SearchKeyword}, {Name: "t", Type: tablestore.SearchText}}
	createSearch(t, c, closed, tablestore.SearchIndex{Name: closed + "_a", Fields: fields}, tablestore.WithSearchTimeToLive(24*time.Hour))
	err := retry(func() error {
		return c.CreateSearchIndex(t.Context(), closed, tablestore.SearchIndex{Name: closed + "_b", Fields: fields}, tablestore.WithSearchTimeToLive(time.Hour))
	})
	var e *tablestore.Error
	if !errors.As(err, &e) || e.Code != "OTSParameterInvalid" || !strings.Contains(e.Message, "86400") {
		t.Errorf("a time to live of one hour gave %v, want OTSParameterInvalid that names 86400", err)
	}
	open := table("open")
	create(t, c, tablestore.Table{Name: open, Keys: tbl.Keys, Columns: tbl.Columns})
	err = retry(func() error {
		return c.CreateSearchIndex(t.Context(), open, tablestore.SearchIndex{Name: open + "_a", Fields: fields}, tablestore.WithSearchTimeToLive(24*time.Hour))
	})
	if !errors.As(err, &e) || e.Code != "OTSParameterInvalid" || !strings.Contains(e.Message, "does not support ttl") {
		t.Errorf("a time to live on a table that allows updates gave %v, want OTSParameterInvalid that names the time to live", err)
	}
}
