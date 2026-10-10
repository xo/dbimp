package cosmos_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"
)

// listed reports whether the answer to a list, such as GET /dbs, holds a
// resource with the id, under the member key.
func listed(t *testing.T, body []byte, key, id string) bool {
	t.Helper()
	var list map[string]jsontext.Value
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("reading the list: %v: %s", err, body)
	}
	var items []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(list[key], &items); err != nil {
		t.Fatalf("reading the member %s: %v: %s", key, err, body)
	}
	return slices.ContainsFunc(items, func(i struct {
		ID string `json:"id"`
	}) bool {
		return i.ID == id
	})
}

// TestIntegrationSchema runs each schema feature of the survey. The SQL of
// Cosmos DB has no DDL, because a database and a container are resources of the
// REST API (D190), so the tests make them there, and the driver reads the
// documents. A feature that only the hosted account has skips on the emulator
// with the reason (recorded).
func TestIntegrationSchema(t *testing.T) {
	a := getAccount(t, envEmulator)

	t.Run("database", func(t *testing.T) {
		status, _, body := a.rest(t, http.MethodGet, "/dbs", nil, nil)
		if status != http.StatusOK || !listed(t, body, "Databases", databaseName) {
			t.Errorf("the list of databases is HTTP %d %s, want the database %s in it", status, body, databaseName)
		}
		if status, _, body := a.rest(t, http.MethodGet, "/dbs/"+databaseName, nil, nil); status != http.StatusOK {
			t.Errorf("reading the database: HTTP %d %s", status, body)
		}
		if status, _, _ := a.rest(t, http.MethodPost, "/dbs", nil, map[string]any{"id": databaseName}); status != http.StatusConflict {
			t.Errorf("a second database with the same name gave HTTP %d, want %d", status, http.StatusConflict)
		}
		// The database of another name has a name of its own, which a
		// statement can name with WithDatabase, and it is not there.
		if status, _, _ := a.rest(t, http.MethodGet, "/dbs/"+databaseName+"_nope", nil, nil); status != http.StatusNotFound {
			t.Errorf("a database that does not exist gave HTTP %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("container", func(t *testing.T) {
		a.container(t, "schema_c", nil)
		status, _, body := a.rest(t, http.MethodGet, "/dbs/"+databaseName+"/colls", nil, nil)
		if status != http.StatusOK || !listed(t, body, "DocumentCollections", "schema_c") {
			t.Errorf("the list of containers is HTTP %d %s, want schema_c in it", status, body)
		}
		if status, _ := a.tryContainer(t, "schema_c", nil); status != http.StatusConflict {
			t.Errorf("a second container with the same name gave HTTP %d, want %d", status, http.StatusConflict)
		}
		if status, _ := a.tryContainer(t, "bad/name", nil); status != http.StatusBadRequest {
			t.Errorf("a container with a slash in its name gave HTTP %d, want %d", status, http.StatusBadRequest)
		}
		// The driver reads the container, and an empty one has no row and no
		// column.
		cols, rows := query(t, a.open(t, "schema_c"), "SELECT * FROM c")
		if len(cols) != 0 || len(rows) != 0 {
			t.Errorf("an empty container gave the columns %q and %d rows, want none", cols, len(rows))
		}
	})

	t.Run("partition key", func(t *testing.T) {
		a.container(t, "schema_pk", nil)
		path := collPath("schema_pk") + "/docs"
		// A key that is not the value of the attribute is refused (recorded: "a
		// document with the wrong partition key").
		if status, _, _ := a.rest(t, http.MethodPost, path, pk("b"), map[string]any{"id": "1", "pk": "a"}); status != http.StatusBadRequest {
			t.Errorf("a document with the wrong partition key gave HTTP %d, want %d", status, http.StatusBadRequest)
		}
		// A create with no header is refused by the hosted account, and the
		// emulator takes the key from the document (recorded: "a document with
		// no partition key header").
		status, _, _ := a.rest(t, http.MethodPost, path, nil, map[string]any{"id": "2", "pk": "a"})
		switch {
		case a.emulator && status != http.StatusCreated:
			t.Errorf("a create with no header gave HTTP %d on the emulator, want %d", status, http.StatusCreated)
		case !a.emulator && status != http.StatusBadRequest:
			t.Errorf("a create with no header gave HTTP %d, want %d", status, http.StatusBadRequest)
		}
		a.put(t, "schema_pk", map[string]any{"id": "1", "pk": "a", "n": 1}, nil)
		a.put(t, "schema_pk", map[string]any{"id": "3", "pk": "b", "n": 2}, nil)
		db := a.open(t, "schema_pk")
		if got := ids(t, db, "SELECT c.id FROM c WHERE c.pk = 'b'", cosmosKey("b")); !reflect.DeepEqual(got, []string{"3"}) {
			t.Errorf("a query for the key b gave %v, want 3", got)
		}
	})

	t.Run("hierarchical partition key", func(t *testing.T) {
		// The hosted account refuses a container with two paths at the version
		// 2018-12-31 of the API (recorded: "setup: create the container hier").
		status, body := a.tryContainer(t, "schema_hier", map[string]any{
			"partitionKey": map[string]any{"paths": []string{"/a", "/b"}, "kind": "MultiHash", "version": 2},
		})
		if a.emulator {
			t.Skipf("the emulator takes a container with two partition key paths (HTTP %d), and the hosted account refuses it", status)
		}
		if status != http.StatusBadRequest {
			t.Errorf("a container with two paths gave HTTP %d %s, want %d", status, body, http.StatusBadRequest)
		}
	})

	t.Run("unique key", func(t *testing.T) {
		a.container(t, "schema_uk", map[string]any{"uniqueKeyPolicy": map[string]any{"uniqueKeys": []map[string]any{{"paths": []string{"/email"}}}}})
		a.put(t, "schema_uk", map[string]any{"id": "1", "pk": "p", "email": "a@b"}, nil)
		// The same value in the same partition breaks the key, with the same
		// status as a document that exists (recorded: "a document that breaks the
		// unique key").
		if status, _, _ := a.rest(t, http.MethodPost, collPath("schema_uk")+"/docs", pk("p"), map[string]any{"id": "2", "pk": "p", "email": "a@b"}); status != http.StatusConflict {
			t.Errorf("a document that breaks the unique key gave HTTP %d, want %d", status, http.StatusConflict)
		}
		// A key is unique inside one partition, so the same value in another
		// partition is allowed.
		a.put(t, "schema_uk", map[string]any{"id": "3", "pk": "q", "email": "a@b"}, nil)
		if got := ids(t, a.open(t, "schema_uk"), "SELECT c.id FROM c WHERE c.email = 'a@b'"); len(got) != 2 {
			t.Errorf("the documents with the email are %v, want 2", got)
		}
	})

	t.Run("indexing policy", func(t *testing.T) {
		policy := func(excluded string) map[string]any {
			return map[string]any{"indexingMode": "consistent", "automatic": true,
				"includedPaths": []map[string]any{{"path": "/*"}},
				"excludedPaths": []map[string]any{{"path": excluded}}}
		}
		a.container(t, "schema_ix", map[string]any{"indexingPolicy": policy("/big/*")})
		read := func() []string {
			status, _, body := a.rest(t, http.MethodGet, collPath("schema_ix"), nil, nil)
			if status != http.StatusOK {
				t.Fatalf("reading the container: HTTP %d %s", status, body)
			}
			var def struct {
				Policy struct {
					Excluded []struct {
						Path string `json:"path"`
					} `json:"excludedPaths"`
				} `json:"indexingPolicy"`
			}
			if err := json.Unmarshal(body, &def); err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, e := range def.Policy.Excluded {
				out = append(out, e.Path)
			}
			return out
		}
		if got := read(); !slices.Contains(got, "/big/*") {
			t.Errorf("the excluded paths are %v, want /big/*", got)
		}
		// A policy replaces the policy of a container that has no unique key
		// (recorded: "replace the indexing policy").
		def := map[string]any{"id": "schema_ix", "partitionKey": map[string]any{"paths": []string{"/pk"}, "kind": "Hash"}, "indexingPolicy": policy("/huge/*")}
		if status, _, body := a.rest(t, http.MethodPut, collPath("schema_ix"), nil, def); status != http.StatusOK {
			t.Fatalf("replacing the policy: HTTP %d %s", status, body)
		}
		if got := read(); !slices.Contains(got, "/huge/*") || slices.Contains(got, "/big/*") {
			t.Errorf("the excluded paths after the replace are %v, want /huge/* and not /big/*", got)
		}
	})

	t.Run("composite index", func(t *testing.T) {
		a.container(t, "schema_ci", map[string]any{"indexingPolicy": map[string]any{
			"indexingMode":  "consistent",
			"automatic":     true,
			"includedPaths": []map[string]any{{"path": "/*"}},
			"compositeIndexes": [][]map[string]any{{
				{"path": "/a", "order": "ascending"},
				{"path": "/b", "order": "descending"},
			}},
		}})
		a.seed(t, "schema_ci", []map[string]any{
			{"id": "1", "pk": "p", "a": 1, "b": 1},
			{"id": "2", "pk": "p", "a": 1, "b": 2},
			{"id": "3", "pk": "p", "a": 0, "b": 5},
		})
		// An ORDER BY of two properties needs the composite index, and it uses
		// the order of the policy.
		got := ids(t, a.open(t, "schema_ci"), "SELECT c.id FROM c ORDER BY c.a ASC, c.b DESC", cosmosKey("p"))
		if want := []string{"3", "2", "1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("the order is %v, want %v", got, want)
		}
	})

	t.Run("time to live", func(t *testing.T) {
		a.container(t, "schema_ttl", map[string]any{"defaultTtl": -1})
		a.put(t, "schema_ttl", map[string]any{"id": "gone", "pk": "p", "ttl": 1}, nil)
		a.put(t, "schema_ttl", map[string]any{"id": "stays", "pk": "p"}, nil)
		// The server deletes the document some time after its time to live ends.
		// The test reads again until it is gone, for a bounded time, and never
		// sleeps for a fixed time.
		db := a.open(t, "schema_ttl")
		deadline := time.Now().Add(90 * time.Second)
		for {
			got := ids(t, db, "SELECT c.id FROM c WHERE c.pk = 'p'", cosmosKey("p"))
			if !slices.Contains(got, "gone") {
				if !slices.Contains(got, "stays") {
					t.Errorf("the documents are %v, want stays", got)
				}
				return
			}
			if time.Now().After(deadline) {
				if a.emulator {
					t.Skip("the emulator did not delete the document in 90 seconds")
				}
				t.Fatal("the document was not deleted in 90 seconds")
			}
			time.Sleep(time.Second)
		}
	})

	t.Run("change feed", func(t *testing.T) {
		a.container(t, "schema_cf", nil)
		a.put(t, "schema_cf", map[string]any{"id": "1", "pk": "p"}, nil)
		a.put(t, "schema_cf", map[string]any{"id": "2", "pk": "p"}, nil)
		status, hdr, body := a.rest(t, http.MethodGet, collPath("schema_cf")+"/docs", map[string]string{
			"A-Im":                                "Incremental feed",
			"X-Ms-Documentdb-Partitionkeyrangeid": "0",
		}, nil)
		if a.emulator && status != http.StatusOK {
			t.Skipf("the emulator answers the change feed with HTTP %d", status)
		}
		if status != http.StatusOK {
			t.Fatalf("the change feed: HTTP %d %s", status, body)
		}
		var feed struct {
			Documents []struct {
				ID string `json:"id"`
			} `json:"Documents"`
		}
		if err := json.Unmarshal(body, &feed); err != nil || len(feed.Documents) != 2 {
			t.Errorf("the change feed is %s (%v), want the 2 documents", body, err)
		}
		if hdr.Get("ETag") == "" {
			t.Error("the change feed has no etag to continue from")
		}
	})

	t.Run("stored procedure", func(t *testing.T) {
		skipEmulator(t, a)
		a.container(t, "schema_sp", nil)
		def := map[string]any{"id": "sp1", "body": "function(){getContext().getResponse().setBody(1);}"}
		if status, _, body := a.rest(t, http.MethodPost, collPath("schema_sp")+"/sprocs", nil, def); status != http.StatusCreated {
			t.Fatalf("making the procedure: HTTP %d %s", status, body)
		}
		status, _, body := a.rest(t, http.MethodPost, collPath("schema_sp")+"/sprocs/sp1", pk("a"), "[]")
		if status != http.StatusOK || string(body) != "1" {
			t.Errorf("the call of the procedure: HTTP %d %s, want 1", status, body)
		}
	})

	t.Run("trigger", func(t *testing.T) {
		skipEmulator(t, a)
		a.container(t, "schema_tr", nil)
		def := map[string]any{"id": "tr1", "body": "function(){}", "triggerOperation": "All", "triggerType": "Pre"}
		if status, _, body := a.rest(t, http.MethodPost, collPath("schema_tr")+"/triggers", nil, def); status != http.StatusCreated {
			t.Fatalf("making the trigger: HTTP %d %s", status, body)
		}
		// A trigger fires only when a write names it.
		a.put(t, "schema_tr", map[string]any{"id": "1", "pk": "a"}, map[string]string{"X-Ms-Documentdb-Pre-Trigger-Include": "tr1"})
	})

	t.Run("user defined function", func(t *testing.T) {
		skipEmulator(t, a)
		a.container(t, "schema_udf", nil)
		def := map[string]any{"id": "plus1", "body": "function(x){return x+1;}"}
		if status, _, body := a.rest(t, http.MethodPost, collPath("schema_udf")+"/udfs", nil, def); status != http.StatusCreated {
			t.Fatalf("making the function: HTTP %d %s", status, body)
		}
		// The driver sends the call as a query (recorded: "a query that calls a
		// user defined function").
		if got := scalar(t, a.open(t, "schema_udf"), "SELECT VALUE udf.plus1(1)"); got != int64(2) {
			t.Errorf("udf.plus1(1) is %v (%T), want 2", got, got)
		}
	})

	t.Run("view", func(t *testing.T) {
		// Cosmos DB has no view, and the path of a view is not a path (recorded:
		// "a view").
		if status, _, _ := a.rest(t, http.MethodGet, "/dbs/"+databaseName+"/views", nil, nil); status < 400 {
			t.Errorf("the path of a view gave HTTP %d, want an error", status)
		}
	})

	t.Run("default value", func(t *testing.T) {
		// A container takes the member defaultValues and ignores it (recorded:
		// "setup: create the container nopolicy").
		a.container(t, "schema_dv", map[string]any{"defaultValues": map[string]any{"/x": 1}})
		a.put(t, "schema_dv", map[string]any{"id": "1", "pk": "p"}, nil)
		cols, _ := query(t, a.open(t, "schema_dv"), "SELECT c.id, c.x FROM c")
		if !reflect.DeepEqual(cols, []string{"id"}) {
			t.Errorf("the columns are %q, want id only, because the server wrote no default value", cols)
		}
	})

	t.Run("foreign key", func(t *testing.T) {
		// A container takes the member foreignKeyPolicy and ignores it, so a
		// document that refers to nothing is created (recorded: "setup: create
		// the container nopolicy").
		a.container(t, "schema_fk", map[string]any{"foreignKeyPolicy": map[string]any{"foreignKeys": []map[string]any{{"paths": []string{"/ref"}, "references": "schema_dv"}}}})
		if status := a.put(t, "schema_fk", map[string]any{"id": "1", "pk": "p", "ref": "no such document"}, nil); status != http.StatusCreated {
			t.Errorf("a document with a reference to nothing gave HTTP %d, want %d", status, http.StatusCreated)
		}
	})
}

// skipEmulator skips the test on the emulator, which runs no stored procedure,
// no trigger and no user defined function (recorded).
func skipEmulator(t *testing.T, a *account) {
	t.Helper()
	if a.emulator {
		t.Skip("the emulator runs no stored procedure, no trigger and no user defined function (recorded)")
	}
}
