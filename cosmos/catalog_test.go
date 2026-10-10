package cosmos_test

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/cosmos"
)

// The catalog statements (D190 item 17) read the REST API, and these tests
// replay the answers that step 6 recorded for GET /, /dbs, /dbs/{db}/colls,
// /dbs/{db}/colls/{c}, /offers and /dbs/{db}/users, on both servers. A resource
// that no recording holds, such as the stored procedures of a container, comes
// from a fake server whose answers have the shape that Microsoft documents.

// catalog runs a catalog statement, and returns each row as a map by the name of
// its column, with the columns.
func catalog(t *testing.T, db *sql.DB, statement string, args ...any) ([]string, []map[string]any) {
	t.Helper()
	cols, rows, err := scanAll(t.Context(), db, statement, args...)
	if err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		if len(r) != len(cols) {
			t.Fatalf("%s: a row has %d values for %d columns", statement, len(r), len(cols))
		}
		out[i] = map[string]any{}
		for j, c := range cols {
			out[i][c] = r[j]
		}
	}
	return cols, out
}

func TestCatalogAccount(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, server string, db *sql.DB) {
		t.Helper()
		_, rows := catalog(t, db, `SELECT * FROM "$account"`)
		if len(rows) != 1 {
			t.Fatalf("the account has %d rows, want 1", len(rows))
		}
		r := rows[0]
		if server == hosted {
			want := map[string]any{
				"id": "dbimp-cosmos", "writable_regions": []any{"East US"}, "readable_regions": []any{"East US"},
				"writable_endpoints":  []any{"https://dbimp-cosmos-eastus.documents.azure.com:443/"},
				"default_consistency": "Session", "multiple_write_locations": false, "continuous_backup": false,
			}
			for k, v := range want {
				if !reflect.DeepEqual(r[k], v) {
					t.Errorf("%s is %#v, want %#v", k, r[k], v)
				}
			}
			return
		}
		// The emulator leaves out the member continuousBackupEnabled, so its value
		// is nil, and its consistency is Eventual (recorded).
		if r["continuous_backup"] != nil || r["default_consistency"] != "Eventual" || r["id"] != "cosmosdev" {
			t.Errorf("the account is %v", r)
		}
		if s, _ := r["query_engine_configuration"].(string); s == "" {
			t.Errorf("the query engine configuration is %v, want JSON text", r["query_engine_configuration"])
		}
	})
}

func TestCatalogDatabases(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, server string, db *sql.DB) {
		t.Helper()
		cols, rows := catalog(t, db, `SELECT * FROM "$databases"`)
		if !reflect.DeepEqual(cols, []string{"id", "rid", "link"}) {
			t.Errorf("the columns are %q", cols)
		}
		var ids []any
		for _, r := range rows {
			ids = append(ids, r["id"])
		}
		want := []any{"dbimp_test", "dbmeta", "dbimp_it"}
		if server == emulator {
			want = []any{"dbimp_it"}
		}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("the databases are %v, want %v", ids, want)
		}
		if rows[0]["rid"] == nil || rows[0]["link"] == nil {
			t.Errorf("the first database is %v, want a rid and a link", rows[0])
		}
	})
}

func TestCatalogContainers(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	db := openFake(t, srv.URL, "kv", "")
	_, rows := catalog(t, db, `SELECT * FROM "$containers" WHERE database = 'dbimp_it'`)
	byID := map[string]map[string]any{}
	for _, r := range rows {
		id, _ := r["id"].(string)
		byID[id] = r
	}
	if len(byID) != 4 {
		t.Fatalf("the containers are %v, want nopolicy, bulk, uk and kv", rows)
	}
	kv := byID["kv"]
	want := map[string]any{
		"database": "dbimp_it", "rid": "DJB7AI1lZHo=", "link": "dbs/DJB7AA==/colls/DJB7AI1lZHo=/",
		"partition_key_paths": []any{"/pk"}, "partition_key_kind": "Hash", "partition_key_version": nil,
		"indexing_mode": "consistent", "indexing_automatic": true,
		"indexing_included_paths": []any{"/*"}, "indexing_excluded_paths": []any{`/"_etag"/?`},
		"composite_indexes": nil, "spatial_indexes": nil, "vector_indexes": nil,
		"unique_keys": nil, "default_ttl": nil, "analytical_ttl": nil,
		"conflict_resolution_mode": "LastWriterWins", "conflict_resolution_path": "/_ts", "conflict_resolution_procedure": "",
		"change_feed_retention": nil, "computed_properties": nil, "vector_embedding_policy": nil, "full_text_policy": nil,
		"geospatial_type": "Geography",
		"indexing_policy": `{"automatic":true,"excludedPaths":[{"path":"/\"_etag\"/?"}],"includedPaths":[{"path":"/*"}],"indexingMode":"consistent"}`,
	}
	for k, v := range want {
		if !reflect.DeepEqual(kv[k], v) {
			t.Errorf("kv: %s is %#v, want %#v", k, kv[k], v)
		}
	}
	uk := byID["uk"]
	for k, v := range map[string]any{
		"default_ttl": int64(-1),
		"unique_keys": []any{[]any{"/email"}},
		"composite_indexes": []any{[]any{
			map[string]any{"path": "/a", "order": "ascending"},
			map[string]any{"path": "/b", "order": "descending"},
		}},
	} {
		if !reflect.DeepEqual(uk[k], v) {
			t.Errorf("uk: %s is %#v, want %#v", k, uk[k], v)
		}
	}
	// One container has its own request, and the same columns.
	cols, one := catalog(t, db, `SELECT * FROM "$containers" WHERE database = 'dbimp_it' AND container = 'kv'`)
	if len(one) != 1 || !reflect.DeepEqual(one[0], kv) || len(cols) != 26 {
		t.Errorf("the container kv is %v (%d columns), want the row of the list", one, len(cols))
	}
}

func TestCatalogContainersOfTheEmulator(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, emulator)
	db := openFake(t, srv.URL, "kv", "")
	_, rows := catalog(t, db, `SELECT * FROM "$containers" WHERE database = 'dbimp_it' AND container = 'kv'`)
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	r := rows[0]
	// The emulator writes an empty list of unique keys, and no version of the key.
	if !reflect.DeepEqual(r["unique_keys"], []any{}) || r["partition_key_kind"] != "Hash" || r["partition_key_version"] != nil {
		t.Errorf("the container is %v", r)
	}
}

func TestCatalogOffers(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	db := openFake(t, srv.URL, "kv", "")
	_, rows := catalog(t, db, `SELECT * FROM "$offers" WHERE database = 'dbimp_it'`)
	if len(rows) != 1 {
		t.Fatalf("the offers are %v, want the one of the database", rows)
	}
	r := rows[0]
	for k, v := range map[string]any{
		"scope": "database", "database": "dbimp_it", "container": nil, "throughput": int64(400),
		"autoscale_max_throughput": nil, "autoscale_increment_percent": nil, "version": "V2", "offer_type": "Invalid",
		"resource_link": "dbs/DJB7AA==/",
	} {
		if !reflect.DeepEqual(r[k], v) {
			t.Errorf("%s is %#v, want %#v", k, r[k], v)
		}
	}
	// The emulator has an offer for the database too, with a name of its own.
	esrv := replayServer(t, emulator)
	_, rows = catalog(t, openFake(t, esrv.URL, "kv", ""), `SELECT * FROM "$offers" WHERE database = 'dbimp_it'`)
	if len(rows) != 1 || rows[0]["throughput"] != int64(400) {
		t.Errorf("the offers of the emulator are %v", rows)
	}
	// A container with no offer of its own has no row.
	_, rows = catalog(t, db, `SELECT * FROM "$offers" WHERE database = 'dbimp_it' AND container = 'kv'`)
	if len(rows) != 0 {
		t.Errorf("the offers of kv are %v, want none", rows)
	}
}

func TestCatalogUsers(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, _ string, db *sql.DB) {
		t.Helper()
		cols, rows := catalog(t, db, `SELECT * FROM "$users" WHERE database = 'dbimp_it'`)
		if len(rows) != 0 || !reflect.DeepEqual(cols, []string{"database", "id", "rid", "link"}) {
			t.Errorf("columns %q rows %v, want the columns and no row", cols, rows)
		}
	})
}

// catalogFake answers each path with a body.
func catalogFake(t *testing.T, bodies map[string]string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := bodies[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"NotFound","message":"Message: {\"Errors\":[\"Resource Not Found.\"]}\r\nActivityId: x"}`))
			return
		}
		if tok := r.Header.Get("X-Ms-Continuation"); tok == "" && bodies[r.URL.Path+"#next"] != "" {
			w.Header().Set("X-Ms-Continuation", "t1")
		} else if tok == "t1" {
			b = bodies[r.URL.Path+"#next"]
		}
		_, _ = w.Write([]byte(b))
	}))
	t.Cleanup(srv.Close)
	return openFake(t, srv.URL, "c", "")
}

// TestCatalogScripts holds the stored procedures, the triggers and the functions
// of a container, from the shapes that Microsoft documents (not recorded: only
// the creation of each was). A feed that has a continuation is read to its end.
func TestCatalogScripts(t *testing.T) {
	t.Parallel()
	db := catalogFake(t, map[string]string{
		"/dbs/d/colls/c/sprocs":    `{"_rid":"r","StoredProcedures":[{"id":"sp1","body":"function(){}","_rid":"a","_self":"dbs/x/colls/y/sprocs/a/"}],"_count":1}`,
		"/dbs/d/colls/c/triggers":  `{"Triggers":[{"id":"tr1","body":"function(){}","triggerType":"Pre","triggerOperation":"All","_rid":"b","_self":"s"}]}`,
		"/dbs/d/colls/c/udfs":      `{"UserDefinedFunctions":[{"id":"f1","body":"function(x){return x+1;}"}]}`,
		"/dbs/d/colls/c/udfs#next": `{"UserDefinedFunctions":[{"id":"f2","body":"function(){}"}]}`,
	})
	cols, rows := catalog(t, db, `SELECT * FROM "$stored_procedures" WHERE database = 'd' AND container = 'c'`)
	if len(rows) != 1 || rows[0]["id"] != "sp1" || rows[0]["body"] != "function(){}" || rows[0]["link"] != "dbs/x/colls/y/sprocs/a/" || len(cols) != 6 {
		t.Errorf("columns %q rows %v", cols, rows)
	}
	cols, rows = catalog(t, db, `select * from $triggers where Container = 'c' and DATABASE = 'd'`)
	if len(rows) != 1 || rows[0]["trigger_type"] != "Pre" || rows[0]["trigger_operation"] != "All" || len(cols) != 8 {
		t.Errorf("columns %q rows %v", cols, rows)
	}
	_, rows = catalog(t, db, `SELECT * FROM "$functions" WHERE database = @d AND container = @c;`, sql.Named("d", "d"), sql.Named("c", "c"))
	if len(rows) != 2 || rows[0]["id"] != "f1" || rows[1]["id"] != "f2" || rows[0]["rid"] != nil {
		t.Errorf("the functions are %v, want f1 and f2 from two pages, and a missing rid as nil", rows)
	}
}

// TestCatalogPermissions holds the permissions of every user of a database, and
// of one user.
func TestCatalogPermissions(t *testing.T) {
	t.Parallel()
	db := catalogFake(t, map[string]string{
		"/dbs/d/users":                 `{"Users":[{"id":"u1"},{"id":"u 2"}]}`,
		"/dbs/d/users/u1/permissions":  `{"Permissions":[{"id":"p1","permissionMode":"Read","resource":"dbs/x/colls/y/","_rid":"a","_self":"s"}]}`,
		"/dbs/d/users/u 2/permissions": `{"Permissions":[{"id":"p2","permissionMode":"All","resource":"dbs/x/"}]}`,
	})
	_, rows := catalog(t, db, `SELECT * FROM "$permissions" WHERE database = 'd'`)
	if len(rows) != 2 || rows[0]["user"] != "u1" || rows[0]["permission_mode"] != "Read" || rows[1]["user"] != "u 2" || rows[1]["resource_link"] != "dbs/x/" {
		t.Errorf("the permissions are %v", rows)
	}
	_, rows = catalog(t, db, `SELECT * FROM "$permissions" WHERE database = 'd' AND user = 'u1'`)
	if len(rows) != 1 || rows[0]["id"] != "p1" {
		t.Errorf("the permissions of u1 are %v", rows)
	}
}

// TestCatalogAutoscale holds an offer with an autoscale policy, from the shape
// that Microsoft documents (not recorded), and an offer for a container.
func TestCatalogAutoscale(t *testing.T) {
	t.Parallel()
	db := catalogFake(t, map[string]string{
		"/dbs/d":       `{"id":"d","_rid":"DB"}`,
		"/dbs/d/colls": `{"DocumentCollections":[{"id":"c","_rid":"CO"},{"id":"e","_rid":"EE"}]}`,
		"/offers": `{"Offers":[
			{"id":"o1","_rid":"o1","resource":"dbs/DB/","offerResourceId":"DB","offerVersion":"V2","offerType":"Invalid","content":{"offerThroughput":4000,"offerAutopilotSettings":{"maxThroughput":4000,"autoUpgradePolicy":{"throughputPolicy":{"incrementPercent":10}}}}},
			{"id":"o2","_rid":"o2","resource":"dbs/DB/colls/CO/","offerResourceId":"CO","offerVersion":"V2","offerType":"Invalid","content":{"offerThroughput":1000}},
			{"id":"o3","offerResourceId":"OTHER","content":{"offerThroughput":1}}]}`,
	})
	_, rows := catalog(t, db, `SELECT * FROM "$offers" WHERE database = 'd'`)
	if len(rows) != 2 {
		t.Fatalf("the offers are %v, want the database and the container c", rows)
	}
	if r := rows[0]; r["scope"] != "database" || r["throughput"] != nil || r["autoscale_max_throughput"] != int64(4000) || r["autoscale_increment_percent"] != int64(10) {
		t.Errorf("the offer of the database is %v", r)
	}
	if r := rows[1]; r["scope"] != "container" || r["container"] != "c" || r["throughput"] != int64(1000) || r["autoscale_max_throughput"] != nil {
		t.Errorf("the offer of the container is %v", r)
	}
	_, rows = catalog(t, db, `SELECT * FROM "$offers" WHERE database = 'd' AND container = 'e'`)
	if len(rows) != 0 {
		t.Errorf("the offers of e are %v, want none", rows)
	}
}

// TestCatalogDefaults holds that the database and the container of the DSN fill
// what the WHERE leaves out, and that WithDatabase and WithContainer change
// them for one statement.
func TestCatalogDefaults(t *testing.T) {
	t.Parallel()
	db := catalogFake(t, map[string]string{
		"/dbs/db1/colls/c/sprocs": `{"StoredProcedures":[{"id":"one"}]}`,
		"/dbs/db2/colls/k/sprocs": `{"StoredProcedures":[{"id":"two"}]}`,
		"/dbs/d/colls/c/sprocs":   `{"StoredProcedures":[{"id":"three"}]}`,
	})
	// The DSN of the fake holds the database dbimp_it, so no path matches, and the
	// options and the WHERE give the names.
	_, rows := catalog(t, db, `SELECT * FROM "$stored_procedures"`, cosmos.WithDatabase("db1"), cosmos.WithContainer("c"))
	if len(rows) != 1 || rows[0]["id"] != "one" {
		t.Errorf("rows = %v", rows)
	}
	_, rows = catalog(t, db, `SELECT * FROM "$stored_procedures" WHERE database = 'db2'`, cosmos.WithDatabase("db1"), cosmos.WithContainer("k"))
	if len(rows) != 1 || rows[0]["id"] != "two" {
		t.Errorf("rows = %v", rows)
	}
	_, rows = catalog(t, db, `SELECT * FROM "$stored_procedures" WHERE database = 'd'`)
	if len(rows) != 1 || rows[0]["id"] != "three" {
		t.Errorf("rows = %v", rows)
	}
}

// TestCatalogRefusals holds that a catalog statement that does not follow the
// grammar fails with an error that says what is wrong, before any request, and
// that any other statement goes to the container.
func TestCatalogRefusals(t *testing.T) {
	t.Parallel()
	f := okFake()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	db, err := sql.Open(cosmos.Name, dsnFor(srv.URL, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, tt := range []struct {
		statement string
		args      []any
		want      error
	}{
		{`SELECT * FROM "$containers"`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$stored_procedures" WHERE database = 'd'`, nil, dbimp.ErrInvalidValue},
		{`SELECT id FROM "$databases"`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$databases" WHERE database = 'd'`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$containers" WHERE database = 'd' AND database = 'e'`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$containers" WHERE name = 'd'`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$containers" WHERE database = 'd' OR container = 'c'`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$containers" WHERE database = 5`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$containers" WHERE database > 'd'`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$containers" ORDER BY id`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$containers" WHERE database = @d`, nil, dbimp.ErrArguments},
		{`SELECT * FROM "$containers" WHERE database = @d`, []any{sql.Named("d", 5)}, dbimp.ErrArguments},
		{`SELECT * FROM "$containers" WHERE database = 'd'`, []any{sql.Named("x", "y")}, dbimp.ErrArguments},
		{`SELECT * FROM "$containers" WHERE database = 'a/b'`, nil, dbimp.ErrInvalidValue},
		{`SELECT * FROM "$containers" WHERE database = 'd' AND user = 'u'`, nil, dbimp.ErrInvalidValue},
	} {
		if err := drain(t, db, tt.statement, tt.args...); !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want %v", tt.statement, err, tt.want)
		}
	}
	if f.requests() != 0 {
		t.Errorf("the driver sent %d requests for statements that it refused, want none", f.requests())
	}
	// A statement that is not a catalog statement goes to the container, which
	// the path of the DSN names here through the options. A name that is not
	// reserved is one of them.
	for _, s := range []string{`SELECT * FROM "$other"`, `SELECT c.id FROM c`, `SELECT * FROM c WHERE c.database = 'd'`} {
		if err := drain(t, db, s, cosmos.WithDatabase("d"), cosmos.WithContainer("c")); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	if f.requests() != 3 {
		t.Errorf("the driver sent %d requests, want 3 queries", f.requests())
	}
}

// TestCatalogErrors holds that an error of the server reaches the caller as an
// *Error, such as the 404 of a database that is not there (recorded: "a
// database that does not exist").
func TestCatalogErrors(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, _ string, db *sql.DB) {
		t.Helper()
		err := drain(t, db, `SELECT * FROM "$offers" WHERE database = 'nope'`)
		if cerr, ok := errors.AsType[*cosmos.Error](err); !ok || cerr.HTTPStatus != http.StatusNotFound {
			t.Errorf("the error is %v, want an *Error of HTTP 404", err)
		}
	})
}
