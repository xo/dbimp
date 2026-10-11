package cosmos_test

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/cosmos"
)

// TestCatalogIndexPaths holds the paths of the indexing policy of a recorded
// container, and of a policy with every kind of entry, from the shapes that
// Microsoft documents (not recorded: the emulator keeps no composite, spatial,
// vector or full text entry, and the hosted account has no such container).
func TestCatalogIndexPaths(t *testing.T) {
	t.Parallel()
	// The list of the containers has the policy of uk (recorded), and the answer
	// for one container has the same members.
	db := catalogFake(t, map[string]string{
		"/dbs/dbimp_it/colls/uk": `{"id":"uk","indexingPolicy":{"indexingMode":"consistent","automatic":true,"includedPaths":[{"path":"/*"}],"excludedPaths":[{"path":"/\"_etag\"/?"}],"compositeIndexes":[[{"path":"/a","order":"ascending"},{"path":"/b","order":"descending"}]]},"uniqueKeyPolicy":{"uniqueKeys":[{"paths":["/email"]}]}}`,
	})
	cols, rows := catalog(t, db, `SELECT * FROM "$index_paths" WHERE database = 'dbimp_it' AND container = 'uk'`)
	if len(cols) != 11 || len(rows) != 4 {
		t.Fatalf("columns %q rows %v, want 11 columns and 4 rows", cols, rows)
	}
	for i, want := range []map[string]any{
		{"path": "/*", "kind": "included", "path_order": nil, "position": nil, "composite_group": nil},
		{"path": `/"_etag"/?`, "kind": "excluded", "path_order": nil},
		{"path": "/a", "kind": "composite", "path_order": "asc", "position": int64(0), "composite_group": int64(0)},
		{"path": "/b", "kind": "composite", "path_order": "desc", "position": int64(1), "composite_group": int64(0), "spatial_types": nil, "dimensions": nil},
	} {
		want["database"], want["container"] = "dbimp_it", "uk"
		for k, v := range want {
			if !reflect.DeepEqual(rows[i][k], v) {
				t.Errorf("row %d: %s is %#v, want %#v", i, k, rows[i][k], v)
			}
		}
	}

	full := catalogFake(t, map[string]string{
		"/dbs/d/colls/c": `{"id":"c",
			"indexingPolicy":{"includedPaths":[{"path":"/*"}],"excludedPaths":[],
				"compositeIndexes":[[{"path":"/a","order":"ascending"},{"path":"/b","order":"descending"}],[{"path":"/c","order":"descending"},{"path":"/d","order":"ascending"}]],
				"spatialIndexes":[{"path":"/loc/*","types":["Point","Polygon"]}],
				"vectorIndexes":[{"path":"/vec","type":"diskANN"},{"path":"/other","type":"flat"}],
				"fullTextIndexes":[{"path":"/text"}]},
			"vectorEmbeddingPolicy":{"vectorEmbeddings":[{"path":"/vec","dataType":"float32","dimensions":1536,"distanceFunction":"cosine"}]}}`,
	})
	_, rows = catalog(t, full, `SELECT * FROM "$index_paths" WHERE database = 'd' AND container = 'c'`)
	if len(rows) != 9 {
		t.Fatalf("the paths are %v, want 9 rows", rows)
	}
	for _, tt := range []struct {
		row  int
		want map[string]any
	}{
		{3, map[string]any{"path": "/c", "path_order": "desc", "position": int64(0), "composite_group": int64(1)}},
		{4, map[string]any{"path": "/d", "path_order": "asc", "position": int64(1), "composite_group": int64(1)}},
		{5, map[string]any{"kind": "spatial", "path": "/loc/*", "spatial_types": []any{"Point", "Polygon"}, "path_order": nil}},
		{6, map[string]any{"kind": "vector", "path": "/vec", "vector_index_type": "diskANN", "dimensions": int64(1536)}},
		{7, map[string]any{"kind": "vector", "path": "/other", "vector_index_type": "flat", "dimensions": nil}},
		{8, map[string]any{"kind": "full_text", "path": "/text", "full_text_path": "/text", "vector_index_type": nil}},
	} {
		for k, v := range tt.want {
			if !reflect.DeepEqual(rows[tt.row][k], v) {
				t.Errorf("row %d: %s is %#v, want %#v", tt.row, k, rows[tt.row][k], v)
			}
		}
	}
	// A container with no policy has no row.
	none := catalogFake(t, map[string]string{"/dbs/d/colls/c": `{"id":"c"}`})
	if _, rows = catalog(t, none, `SELECT * FROM "$index_paths" WHERE database = 'd' AND container = 'c'`); len(rows) != 0 {
		t.Errorf("the paths are %v, want none", rows)
	}
}

// TestCatalogUniqueKeys holds the unique keys and their paths.
func TestCatalogUniqueKeys(t *testing.T) {
	t.Parallel()
	db := catalogFake(t, map[string]string{
		"/dbs/dbimp_it/colls/uk": `{"id":"uk","indexingPolicy":{"indexingMode":"consistent","automatic":true,"includedPaths":[{"path":"/*"}],"excludedPaths":[{"path":"/\"_etag\"/?"}],"compositeIndexes":[[{"path":"/a","order":"ascending"},{"path":"/b","order":"descending"}]]},"uniqueKeyPolicy":{"uniqueKeys":[{"paths":["/email"]}]}}`,
	})
	cols, rows := catalog(t, db, `SELECT * FROM "$unique_keys" WHERE database = 'dbimp_it' AND container = 'uk'`)
	if !reflect.DeepEqual(cols, []string{"database", "container", "position"}) || len(rows) != 1 || rows[0]["position"] != int64(0) {
		t.Errorf("columns %q rows %v", cols, rows)
	}
	cols, rows = catalog(t, db, `SELECT * FROM "$unique_key_paths" WHERE database = 'dbimp_it' AND container = 'uk'`)
	if !reflect.DeepEqual(cols, []string{"database", "container", "key_position", "position", "path"}) || len(rows) != 1 || rows[0]["path"] != "/email" || rows[0]["key_position"] != int64(0) {
		t.Errorf("columns %q rows %v", cols, rows)
	}
	two := catalogFake(t, map[string]string{
		"/dbs/d/colls/c": `{"id":"c","uniqueKeyPolicy":{"uniqueKeys":[{"paths":["/a","/b"]},{"paths":["/c"]}]}}`,
	})
	_, rows = catalog(t, two, `SELECT * FROM "$unique_keys" WHERE database = 'd' AND container = 'c'`)
	if len(rows) != 2 || rows[1]["position"] != int64(1) {
		t.Errorf("the unique keys are %v", rows)
	}
	_, rows = catalog(t, two, `SELECT * FROM "$unique_key_paths" WHERE database = 'd' AND container = 'c'`)
	if len(rows) != 3 || rows[1]["path"] != "/b" || rows[1]["position"] != int64(1) || rows[1]["key_position"] != int64(0) || rows[2]["key_position"] != int64(1) || rows[2]["position"] != int64(0) {
		t.Errorf("the paths of the unique keys are %v", rows)
	}
}

// TestCatalogRoutines holds the stored procedures and the functions in one set
// of rows. The first has the shape that the creation of a stored procedure
// recorded (the body, the rid, the etag and the _ts).
func TestCatalogRoutines(t *testing.T) {
	t.Parallel()
	db := catalogFake(t, map[string]string{
		"/dbs/d/colls/c/sprocs": `{"StoredProcedures":[{"id":"sp1","body":"function(){getContext().getResponse().setBody(1);}","_rid":"DJB7AI1lZHoBAAAAAAAAgA==","_self":"s","_etag":"\"6201d45e-0000-0100-0000-6ac9ff890000\"","_ts":1791623049}]}`,
		"/dbs/d/colls/c/udfs":   `{"UserDefinedFunctions":[{"id":"plus1","body":"function(x){return x+1;}"}]}`,
	})
	cols, rows := catalog(t, db, `SELECT * FROM "$routines" WHERE database = 'd' AND container = 'c'`)
	if !reflect.DeepEqual(cols, []string{"database", "container", "kind", "id", "body", "rid", "etag", "timestamp"}) || len(rows) != 2 {
		t.Fatalf("columns %q rows %v", cols, rows)
	}
	want := time.Unix(1791623049, 0).UTC()
	sp := rows[0]
	if sp["kind"] != "stored_procedure" || sp["id"] != "sp1" || sp["rid"] != "DJB7AI1lZHoBAAAAAAAAgA==" || sp["etag"] != `"6201d45e-0000-0100-0000-6ac9ff890000"` || !reflect.DeepEqual(sp["timestamp"], want) {
		t.Errorf("the stored procedure is %v, want a timestamp of %v", sp, want)
	}
	if f := rows[1]; f["kind"] != "function" || f["id"] != "plus1" || f["body"] != "function(x){return x+1;}" || f["rid"] != nil || f["etag"] != nil || f["timestamp"] != nil {
		t.Errorf("the function is %v, want nil for what the server leaves out", f)
	}
}

// TestCatalogPartitionKeyRanges holds the ranges of the recorded container kv.
func TestCatalogPartitionKeyRanges(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	db := openFake(t, srv.URL, "kv", "")
	cols, rows := catalog(t, db, `SELECT * FROM "$partition_key_ranges"`)
	if len(cols) != 7 || len(rows) != 1 {
		t.Fatalf("columns %q rows %v", cols, rows)
	}
	for k, v := range map[string]any{
		"database": "dbimp_it", "container": "kv", "id": "0", "min_inclusive": "", "max_exclusive": "FF", "status": "online", "parents": []any{},
	} {
		if !reflect.DeepEqual(rows[0][k], v) {
			t.Errorf("%s is %#v, want %#v", k, rows[0][k], v)
		}
	}
}

// TestCatalogSession holds that $session sends no request, and reads the DSN and
// the options.
func TestCatalogSession(t *testing.T) {
	t.Parallel()
	f := okFake()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	db := openFake(t, srv.URL, "c", "partitionkey=p1")
	cols, rows := catalog(t, db, `SELECT * FROM "$session"`)
	if !reflect.DeepEqual(cols, []string{"host", "port", "account", "database", "container", "tls", "partition_key"}) || len(rows) != 1 {
		t.Fatalf("columns %q rows %v", cols, rows)
	}
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	for k, v := range map[string]any{
		"host": "127.0.0.1", "port": int64(port), "account": "127.0.0.1", "database": "dbimp_it", "container": "c", "tls": false, "partition_key": "p1",
	} {
		if !reflect.DeepEqual(rows[0][k], v) {
			t.Errorf("%s is %#v, want %#v", k, rows[0][k], v)
		}
	}
	// An option replaces the container, and a DSN with no path has none.
	_, rows = catalog(t, db, `SELECT * FROM "$session"`, cosmos.WithContainer("k"), cosmos.WithPartitionKey(5))
	if rows[0]["container"] != "k" || rows[0]["partition_key"] != float64(5) {
		t.Errorf("the session is %v", rows[0])
	}
	acct, err := sql.Open(cosmos.Name, "cosmos://x:"+escapedKey+"@myacct.documents.azure.com")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { acct.Close() })
	_, rows = catalog(t, acct, `SELECT * FROM "$session"`)
	for k, v := range map[string]any{
		"host": "myacct.documents.azure.com", "port": int64(443), "account": "myacct", "database": nil, "container": nil, "tls": true, "partition_key": nil,
	} {
		if !reflect.DeepEqual(rows[0][k], v) {
			t.Errorf("%s is %#v, want %#v", k, rows[0][k], v)
		}
	}
	if err := drain(t, db, `SELECT * FROM "$session" WHERE database = 'd'`); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("the error is %v, want dbimp.ErrInvalidValue for a WHERE", err)
	}
	if f.requests() != 0 {
		t.Errorf("the driver sent %d requests for $session, want none", f.requests())
	}
}
