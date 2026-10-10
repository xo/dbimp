package cosmos_test

import (
	"database/sql"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/cosmos"
)

// crudContainers makes the three containers of the CRUD tests, as a database
// with three tables has: customers, orders that refer to a customer, and items
// that refer to an order. Cosmos DB has no foreign key (TestIntegrationSchema),
// so the reference is a value that the tests compare. The orders have an index
// that a query uses, a composite index on a partition key and a total.
func crudContainers(t *testing.T, a *account) (*sql.DB, *sql.DB, *sql.DB) {
	t.Helper()
	a.container(t, "customers", nil)
	a.container(t, "orders", map[string]any{"indexingPolicy": map[string]any{
		"indexingMode":  "consistent",
		"automatic":     true,
		"includedPaths": []map[string]any{{"path": "/*"}},
		"compositeIndexes": [][]map[string]any{{
			{"path": "/pk", "order": "ascending"},
			{"path": "/total", "order": "descending"},
		}},
	}})
	a.container(t, "items", nil)
	return a.open(t, "customers"), a.open(t, "orders"), a.open(t, "items")
}

// crudRows are the documents of the CRUD tests.
var (
	crudCustomers = []map[string]any{
		{"id": "c1", "pk": "north", "name": "Ann", "tier": 1},
		{"id": "c2", "pk": "north", "name": "Bob", "tier": 2},
		{"id": "c3", "pk": "south", "name": "Cy", "tier": 3},
	}
	crudOrders = []map[string]any{
		{"id": "o1", "pk": "north", "customer": "c1", "total": 10.5},
		{"id": "o2", "pk": "north", "customer": "c1", "total": 20.25},
		{"id": "o3", "pk": "north", "customer": "c2", "total": 5},
	}
	crudItems = []map[string]any{
		{"id": "i1", "pk": "o1", "order": "o1", "sku": "a", "qty": 2},
		{"id": "i2", "pk": "o1", "order": "o1", "sku": "b", "qty": 1},
		{"id": "i3", "pk": "o2", "order": "o2", "sku": "a", "qty": 7},
	}
)

// TestIntegrationCRUD runs each statement of CRUD on three containers, and
// compares each value that a select returns with the value that the test
// wrote. The SQL of Cosmos DB has no INSERT, UPDATE or DELETE, so the writes
// are requests on a document, and the selects go through the driver.
func TestIntegrationCRUD(t *testing.T) {
	a := getAccount(t, envEmulator)
	customers, orders, items := crudContainers(t, a)
	byPK := func(key string) cosmos.Option { return cosmos.WithPartitionKey(key) }

	t.Run("insert", func(t *testing.T) {
		for coll, docs := range map[string][]map[string]any{"customers": crudCustomers, "orders": crudOrders, "items": crudItems} {
			for _, d := range docs {
				if status := a.put(t, coll, d, nil); status != http.StatusCreated {
					t.Errorf("creating %v of %s gave HTTP %d, want %d", d["id"], coll, status, http.StatusCreated)
				}
			}
		}
		// A document that exists is a conflict, so a second create is refused.
		if status, _, _ := a.rest(t, http.MethodPost, collPath("customers")+"/docs", pk("north"), crudCustomers[0]); status != http.StatusConflict {
			t.Errorf("a second create gave HTTP %d, want %d", status, http.StatusConflict)
		}
	})

	t.Run("select", func(t *testing.T) {
		cols, got := query(t, customers, "SELECT c.id, c.name, c.tier FROM c WHERE c.pk = 'north' ORDER BY c.id", byPK("north"))
		got = byName(t, cols, got, "id", "name", "tier")
		want := [][]any{{"c1", "Ann", int64(1)}, {"c2", "Bob", int64(2)}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the customers are %v, want %v", got, want)
		}
		// The orders of a customer, with a parameter and the index on the total.
		cols, got = query(t, orders, "SELECT c.id, c.total FROM c WHERE c.customer = @customer ORDER BY c.total DESC", byPK("north"), sql.Named("customer", "c1"))
		got = byName(t, cols, got, "id", "total")
		want = [][]any{{"o2", 20.25}, {"o1", 10.5}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the orders of c1 are %v, want %v", got, want)
		}
		// The items of an order, whose partition key is the order.
		got2 := ids(t, items, "SELECT c.id FROM c WHERE c[\"order\"] = 'o1' ORDER BY c.id", byPK("o1"))
		if !reflect.DeepEqual(got2, []string{"i1", "i2"}) {
			t.Errorf("the items of o1 are %v, want i1 and i2", got2)
		}
		// A read across partitions needs no key.
		if got := ids(t, customers, "SELECT c.id FROM c"); len(got) != 3 {
			t.Errorf("a query across partitions gave %v, want 3 customers", got)
		}
		if n := scalar(t, orders, "SELECT VALUE COUNT(1) FROM c", byPK("north")); n != int64(3) {
			t.Errorf("the count of the orders is %v (%T), want 3", n, n)
		}
	})

	t.Run("update", func(t *testing.T) {
		d := map[string]any{"id": "c1", "pk": "north", "name": "Anna", "tier": 5}
		if status, _, body := a.rest(t, http.MethodPut, collPath("customers")+"/docs/c1", pk("north"), d); status != http.StatusOK {
			t.Fatalf("updating c1: HTTP %d %s", status, body)
		}
		_, got := query(t, customers, "SELECT c.name, c.tier FROM c WHERE c.id = 'c1'", byPK("north"))
		if want := [][]any{{"Anna", int64(5)}}; !reflect.DeepEqual(got, want) {
			t.Errorf("c1 is %v, want %v", got, want)
		}
		if got := scalar(t, customers, "SELECT VALUE COUNT(1) FROM c WHERE c.name = 'Ann'", byPK("north")); got != int64(0) {
			t.Errorf("%v documents still have the old name, want none", got)
		}
	})

	t.Run("replace", func(t *testing.T) {
		// A replace with the etag that the document has works, and a replace
		// with another etag is refused with HTTP 412 (recorded: "replace with a
		// wrong etag").
		status, hdr, body := a.rest(t, http.MethodGet, collPath("customers")+"/docs/c2", pk("north"), nil)
		if status != http.StatusOK {
			t.Fatalf("reading c2: HTTP %d %s", status, body)
		}
		etag := hdr.Get("ETag")
		if etag == "" {
			t.Fatal("the answer has no etag")
		}
		d := map[string]any{"id": "c2", "pk": "north", "name": "Rob", "tier": 9}
		if status, _, _ := a.rest(t, http.MethodPut, collPath("customers")+"/docs/c2", merge(pk("north"), map[string]string{"If-Match": `"bogus"`}), d); status != http.StatusPreconditionFailed {
			t.Errorf("a replace with a wrong etag gave HTTP %d, want %d", status, http.StatusPreconditionFailed)
		}
		if status, _, body := a.rest(t, http.MethodPut, collPath("customers")+"/docs/c2", merge(pk("north"), map[string]string{"If-Match": etag}), d); status != http.StatusOK {
			t.Fatalf("a replace with the etag of the document: HTTP %d %s", status, body)
		}
		_, got := query(t, customers, "SELECT c.name, c.tier FROM c WHERE c.id = 'c2'", byPK("north"))
		if want := [][]any{{"Rob", int64(9)}}; !reflect.DeepEqual(got, want) {
			t.Errorf("c2 is %v, want %v", got, want)
		}
		// The etag changed, and it is a system attribute that a select returns.
		if got := scalar(t, customers, "SELECT VALUE c._etag FROM c WHERE c.id = 'c2'", byPK("north")); got == nil || got == "" {
			t.Errorf("the etag of c2 is %v", got)
		}
	})

	t.Run("upsert", func(t *testing.T) {
		up := map[string]string{"X-Ms-Documentdb-Is-Upsert": "True"}
		if status := a.put(t, "customers", map[string]any{"id": "c4", "pk": "south", "name": "Di", "tier": 1}, up); status != http.StatusCreated {
			t.Errorf("an upsert of a new document gave HTTP %d, want %d", status, http.StatusCreated)
		}
		if status := a.put(t, "customers", map[string]any{"id": "c4", "pk": "south", "name": "Dee", "tier": 2}, up); status != http.StatusOK {
			t.Errorf("an upsert of an existing document gave HTTP %d, want %d", status, http.StatusOK)
		}
		cols, got := query(t, customers, "SELECT c.id, c.name, c.tier FROM c WHERE c.pk = 'south' ORDER BY c.id", byPK("south"))
		got = byName(t, cols, got, "id", "name", "tier")
		want := [][]any{{"c3", "Cy", int64(3)}, {"c4", "Dee", int64(2)}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the south customers are %v, want %v", got, want)
		}
	})

	t.Run("patch", func(t *testing.T) {
		ops := map[string]any{"operations": []map[string]any{
			{"op": "set", "path": "/total", "value": 7},
			{"op": "incr", "path": "/total", "value": 3},
			{"op": "add", "path": "/note", "value": "patched"},
			{"op": "remove", "path": "/customer"},
		}}
		if status, _, body := a.rest(t, http.MethodPatch, collPath("orders")+"/docs/o3", pk("north"), ops); status != http.StatusOK {
			t.Fatalf("patching o3: HTTP %d %s", status, body)
		}
		cols, got := query(t, orders, "SELECT c.total, c.note, c.customer FROM c WHERE c.id = 'o3'", byPK("north"))
		// The attribute that the patch removed is not in the answer, and the
		// first row names the columns (D190).
		// The emulator sorts the keys, so the columns are compared as a set.
		if !slices.Equal(slices.Sorted(slices.Values(cols)), []string{"note", "total"}) || !reflect.DeepEqual(byName(t, cols, got, "total", "note"), [][]any{{int64(10), "patched"}}) {
			t.Errorf("o3 has the columns %v and the rows %v, want total and note, and 10 and patched", cols, got)
		}
	})

	t.Run("delete", func(t *testing.T) {
		for _, id := range []string{"i1", "i2"} {
			if status, _, body := a.rest(t, http.MethodDelete, collPath("items")+"/docs/"+id, pk("o1"), nil); status != http.StatusNoContent {
				t.Fatalf("deleting %s: HTTP %d %s", id, status, body)
			}
		}
		if got := ids(t, items, "SELECT c.id FROM c WHERE c.pk = 'o1'", byPK("o1")); len(got) != 0 {
			t.Errorf("the items of o1 after the delete are %v, want none", got)
		}
		if status, _, _ := a.rest(t, http.MethodDelete, collPath("items")+"/docs/i1", pk("o1"), nil); status != http.StatusNotFound {
			t.Errorf("a second delete gave HTTP %d, want %d", status, http.StatusNotFound)
		}
		if got := ids(t, items, "SELECT c.id FROM c"); len(got) != 1 || got[0] != "i3" {
			t.Errorf("the items that are left are %v, want i3", got)
		}
	})

	t.Run("sql dml", func(t *testing.T) {
		// The driver reads only, so Exec fails before it sends anything (D190).
		for _, s := range []string{"INSERT INTO c VALUES (1)", "UPDATE c SET c.n = 1", "DELETE FROM c"} {
			if _, err := customers.ExecContext(t.Context(), s); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("Exec(%q) = %v, want %v", s, err, dbimp.ErrNotSupported)
			}
			// The server refuses the statement too, with a syntax error, on both
			// servers (recorded: "a statement of INSERT"), so a statement that
			// reaches it as a query is refused.
			_, _, err := tryQuery(t, customers, s)
			if cerr, ok := errors.AsType[*cosmos.Error](err); !ok || cerr.HTTPStatus != http.StatusBadRequest {
				t.Errorf("the query %q gave %v, want an *Error of HTTP 400", s, err)
			}
		}
		// Nothing was written.
		if got := ids(t, customers, "SELECT c.id FROM c"); len(got) != 4 {
			t.Errorf("the customers are %v, want the 4 that the tests wrote", got)
		}
	})
}
