package cosmos_test

import (
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"
)

// TestIntegrationCatalog runs each catalog statement (D190 item 17) against a
// server, with the resources that the test makes in its own database, which the
// run names with a prefix. The statements read the REST API, so the test makes
// the resources through it, as the other integration tests do. A resource that
// the emulator lacks skips the part with the reason (recorded). The hosted
// account has every resource, and its test needs COSMOS_HOSTED_DSN through the
// same test with that variable in COSMOS_DSN.
func TestIntegrationCatalog(t *testing.T) {
	a := getAccount(t, envEmulator)
	a.container(t, "cat_a", map[string]any{
		"defaultTtl":      -1,
		"uniqueKeyPolicy": map[string]any{"uniqueKeys": []map[string]any{{"paths": []string{"/email"}}}},
		"indexingPolicy": map[string]any{
			"indexingMode":  "consistent",
			"automatic":     true,
			"includedPaths": []map[string]any{{"path": "/*"}},
			"excludedPaths": []map[string]any{{"path": "/big/*"}},
			"compositeIndexes": [][]map[string]any{{
				{"path": "/a", "order": "ascending"},
				{"path": "/b", "order": "descending"},
			}},
		},
	})
	a.container(t, "cat_b", nil)
	a.container(t, "cat_c", map[string]any{
		"uniqueKeyPolicy": map[string]any{"uniqueKeys": []map[string]any{{"paths": []string{"/a", "/b"}}, {"paths": []string{"/c"}}}},
		"indexingPolicy": map[string]any{
			"indexingMode":  "consistent",
			"automatic":     true,
			"includedPaths": []map[string]any{{"path": "/*"}},
			"excludedPaths": []map[string]any{{"path": "/big/*"}},
			"compositeIndexes": [][]map[string]any{{
				{"path": "/a", "order": "ascending"},
				{"path": "/b", "order": "descending"},
			}},
			"spatialIndexes": []map[string]any{{"path": "/loc/*", "types": []string{"Point"}}},
		},
	})
	db := a.open(t, "cat_a")
	where := fmt.Sprintf("WHERE database = '%s'", databaseName)

	t.Run("account", func(t *testing.T) {
		cols, rows := catalog(t, db, `SELECT * FROM "$account"`)
		if len(rows) != 1 || len(cols) != 10 {
			t.Fatalf("the account has %d rows and %d columns, want 1 and 10", len(rows), len(cols))
		}
		r := rows[0]
		if regions, _ := r["writable_regions"].([]any); len(regions) == 0 {
			t.Errorf("the writable regions are %v, want some", r["writable_regions"])
		}
		if r["id"] == nil || r["default_consistency"] == nil || r["multiple_write_locations"] == nil {
			t.Errorf("the account is %v", r)
		}
	})

	t.Run("databases", func(t *testing.T) {
		_, rows := catalog(t, db, `SELECT * FROM "$databases"`)
		for _, r := range rows {
			if r["id"] == databaseName {
				if r["rid"] == nil || r["link"] == nil {
					t.Errorf("the database is %v, want a rid and a link", r)
				}
				return
			}
		}
		t.Errorf("the databases are %v, want %s in them", rows, databaseName)
	})

	t.Run("containers", func(t *testing.T) {
		_, rows := catalog(t, db, `SELECT * FROM "$containers" `+where)
		byID := map[any]map[string]any{}
		for _, r := range rows {
			byID[r["id"]] = r
		}
		if len(byID) != 3 || byID["cat_a"] == nil || byID["cat_b"] == nil || byID["cat_c"] == nil {
			t.Fatalf("the containers are %v, want cat_a, cat_b and cat_c", rows)
		}
		r := byID["cat_a"]
		for k, v := range map[string]any{
			"database": databaseName, "partition_key_paths": []any{"/pk"}, "partition_key_kind": "Hash",
			"indexing_mode": "consistent", "indexing_automatic": true, "default_ttl": int64(-1),
			"unique_keys":             []any{[]any{"/email"}},
			"indexing_included_paths": []any{"/*"},
		} {
			if !reflect.DeepEqual(r[k], v) {
				t.Errorf("cat_a: %s is %#v, want %#v", k, r[k], v)
			}
		}
		if got, _ := r["indexing_excluded_paths"].([]any); !contains(got, "/big/*") {
			t.Errorf("the excluded paths are %v, want /big/*", r["indexing_excluded_paths"])
		}
		// The emulator keeps no composite index (recorded for the container uk).
		if got, _ := r["composite_indexes"].([]any); len(got) != 1 && !a.emulator {
			t.Errorf("the composite indexes are %v, want one", r["composite_indexes"])
		}
		if s, _ := r["indexing_policy"].(string); s == "" {
			t.Errorf("the indexing policy as text is %v", r["indexing_policy"])
		}
		_, one := catalog(t, db, `SELECT * FROM "$containers" `+where+` AND container = 'cat_b'`)
		if len(one) != 1 {
			t.Fatalf("the container cat_b alone is %v, want one row", one)
		}
		for k, v := range byID["cat_b"] {
			// The list of the emulator leaves out the policies of conflict
			// resolution, the geospatial type and the unique keys, which the
			// definition of one container holds.
			if a.emulator && v == nil {
				continue
			}
			if !reflect.DeepEqual(one[0][k], v) {
				t.Errorf("cat_b alone: %s is %#v, and %#v in the list", k, one[0][k], v)
			}
		}
	})

	t.Run("stored procedures, triggers and functions", func(t *testing.T) {
		skipEmulator(t, a)
		path := collPath("cat_a")
		for kind, def := range map[string]map[string]any{
			"sprocs":   {"id": "sp1", "body": "function(){getContext().getResponse().setBody(1);}"},
			"triggers": {"id": "tr1", "body": "function(){}", "triggerOperation": "All", "triggerType": "Pre"},
			"udfs":     {"id": "plus1", "body": "function(x){return x+1;}"},
		} {
			if status, _, body := a.rest(t, http.MethodPost, path+"/"+kind, nil, def); status != http.StatusCreated {
				t.Fatalf("making %s: HTTP %d %s", kind, status, body)
			}
		}
		stmt := func(name string) string {
			return fmt.Sprintf(`SELECT * FROM "%s" WHERE database = '%s' AND container = 'cat_a'`, name, databaseName)
		}
		_, rows := catalog(t, db, stmt("$stored_procedures"))
		if len(rows) != 1 || rows[0]["id"] != "sp1" || rows[0]["body"] != "function(){getContext().getResponse().setBody(1);}" || rows[0]["link"] == nil {
			t.Errorf("the stored procedures are %v", rows)
		}
		_, rows = catalog(t, db, stmt("$triggers"))
		if len(rows) != 1 || rows[0]["id"] != "tr1" || rows[0]["trigger_type"] != "Pre" || rows[0]["trigger_operation"] != "All" {
			t.Errorf("the triggers are %v", rows)
		}
		_, rows = catalog(t, db, stmt("$functions"))
		if len(rows) != 1 || rows[0]["id"] != "plus1" || rows[0]["body"] != "function(x){return x+1;}" {
			t.Errorf("the functions are %v", rows)
		}
	})

	t.Run("index paths, unique keys, ranges and session", func(t *testing.T) {
		stmt := func(name string) string {
			return fmt.Sprintf(`SELECT * FROM "%s" WHERE database = '%s' AND container = 'cat_c'`, name, databaseName)
		}
		_, rows := catalog(t, db, stmt("$index_paths"))
		kinds := map[any]int{}
		for _, r := range rows {
			kinds[r["kind"]]++
		}
		// The emulator keeps no composite and no spatial entry (recorded for the
		// container uk).
		if kinds["included"] != 1 || kinds["excluded"] < 1 || (!a.emulator && (kinds["composite"] != 2 || kinds["spatial"] != 1)) {
			t.Errorf("the index paths are %v, want one included, an excluded, two composite and one spatial", rows)
		}
		for _, r := range rows {
			if r["kind"] == "composite" && r["path"] == "/b" && (r["path_order"] != "desc" || r["position"] != int64(1) || r["composite_group"] != int64(0)) {
				t.Errorf("the composite path /b is %v", r)
			}
			// The hosted service adds the other spatial types to a path that names
			// Point only (recorded in the run of 2026-10-11), so the test asks for
			// Point and not for the whole list.
			if types, _ := r["spatial_types"].([]any); r["kind"] == "spatial" && !slices.Contains(types, any("Point")) {
				t.Errorf("the spatial path is %v", r)
			}
		}
		_, rows = catalog(t, db, stmt("$unique_keys"))
		if len(rows) != 2 || rows[1]["position"] != int64(1) {
			t.Errorf("the unique keys are %v, want two", rows)
		}
		_, rows = catalog(t, db, stmt("$unique_key_paths"))
		if len(rows) != 3 || rows[1]["path"] != "/b" || rows[1]["position"] != int64(1) || rows[2]["key_position"] != int64(1) {
			t.Errorf("the paths of the unique keys are %v, want /a and /b of the first key and /c of the second", rows)
		}
		_, rows = catalog(t, db, stmt("$partition_key_ranges"))
		if len(rows) < 1 || rows[0]["id"] == nil || rows[0]["max_exclusive"] == nil || rows[0]["status"] != "online" {
			t.Errorf("the ranges are %v, want at least one online", rows)
		}
		_, rows = catalog(t, db, `SELECT * FROM "$session"`)
		if len(rows) != 1 || rows[0]["host"] == nil || rows[0]["database"] != databaseName || rows[0]["container"] != "cat_a" || rows[0]["tls"] == nil {
			t.Errorf("the session is %v", rows)
		}
	})

	t.Run("routines", func(t *testing.T) {
		skipEmulator(t, a)
		path := collPath("cat_c")
		for kind, def := range map[string]map[string]any{
			"sprocs": {"id": "sp1", "body": "function(){getContext().getResponse().setBody(1);}"},
			"udfs":   {"id": "plus1", "body": "function(x){return x+1;}"},
		} {
			if status, _, body := a.rest(t, http.MethodPost, path+"/"+kind, nil, def); status != http.StatusCreated {
				t.Fatalf("making %s: HTTP %d %s", kind, status, body)
			}
		}
		stmt := func(name string) string {
			return fmt.Sprintf(`SELECT * FROM "%s" WHERE database = '%s' AND container = 'cat_c'`, name, databaseName)
		}
		_, rows := catalog(t, db, stmt("$routines"))
		if len(rows) != 2 || rows[0]["kind"] != "stored_procedure" || rows[0]["id"] != "sp1" || rows[1]["kind"] != "function" || rows[1]["id"] != "plus1" {
			t.Fatalf("the routines are %v, want sp1 and plus1", rows)
		}
		for _, r := range rows {
			if ts, ok := r["timestamp"].(time.Time); !ok || ts.Location() != time.UTC || r["etag"] == nil || r["rid"] == nil {
				t.Errorf("the routine %v, want a timestamp in UTC, an etag and a rid", r)
			}
		}
	})

	t.Run("offers", func(t *testing.T) {
		// The database has the throughput that the run gave it, 400 request units a
		// second, and its containers share it (recorded: "the list of offers").
		_, rows := catalog(t, db, `SELECT * FROM "$offers" `+where)
		if len(rows) != 1 || rows[0]["scope"] != "database" || rows[0]["throughput"] != int64(400) || rows[0]["container"] != nil {
			t.Errorf("the offers are %v, want one of the database with 400", rows)
		}
	})

	t.Run("users and permissions", func(t *testing.T) {
		status, _, body := a.rest(t, http.MethodPost, "/dbs/"+databaseName+"/users", nil, map[string]any{"id": "u1"})
		if status != http.StatusCreated {
			t.Skipf("the server does not make a user: HTTP %d %s", status, body)
		}
		_, rows := catalog(t, db, `SELECT * FROM "$users" `+where)
		if len(rows) != 1 || rows[0]["id"] != "u1" || rows[0]["rid"] == nil {
			t.Errorf("the users are %v", rows)
		}
		_, conts := catalog(t, db, `SELECT * FROM "$containers" `+where+` AND container = 'cat_b'`)
		link, _ := conts[0]["link"].(string)
		perm := map[string]any{"id": "p1", "permissionMode": "Read", "resource": link}
		if status, _, body := a.rest(t, http.MethodPost, "/dbs/"+databaseName+"/users/u1/permissions", nil, perm); status != http.StatusCreated {
			t.Skipf("the server does not make a permission: HTTP %d %s", status, body)
		}
		_, rows = catalog(t, db, `SELECT * FROM "$permissions" `+where)
		if len(rows) != 1 || rows[0]["user"] != "u1" || rows[0]["id"] != "p1" || rows[0]["permission_mode"] != "Read" || rows[0]["resource_link"] != link {
			t.Errorf("the permissions are %v, want p1 of u1 on %s", rows, link)
		}
	})
}

// contains reports whether the list holds v.
func contains(list []any, v any) bool {
	return slices.Contains(list, v)
}
