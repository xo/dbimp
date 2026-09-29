package arangodb //nolint:testpackage // The tests reach the endpoints of the server through the connector of the driver.

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/dbimptest"
)

// These tests hold step 14a of docs/DRIVER.md: each entry of
// testdata/arangodb/features.json names one of their subtests. An operation
// that AQL does not have goes to its endpoint through call, as the driver
// does not take it (D92).

func TestIntegrationCRUD(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		cols := []string{collection(t, "crud_a_"+p.name), collection(t, "crud_b_"+p.name), collection(t, "crud_c_"+p.name)}
		t.Run("insert", func(t *testing.T) {
			for i, c := range cols {
				exec(t, db, "FOR i IN 1..3 INSERT {_key: TO_STRING(i), v: i * @m} INTO "+c, sql.Named("m", i+1))
			}
		})
		t.Run("select", func(t *testing.T) {
			same(t, cols[1], column(t, db, "FOR d IN "+cols[1]+" SORT d.v RETURN d.v"), int64(2), int64(4), int64(6))
		})
		t.Run("update", func(t *testing.T) {
			exec(t, db, "UPDATE '1' WITH {v: 10} IN "+cols[0])
			same(t, "the update", column(t, db, "RETURN DOCUMENT(@@c, '1').v", sql.Named("c", cols[0])), int64(10))
		})
		t.Run("replace", func(t *testing.T) {
			exec(t, db, "REPLACE '2' WITH {w: 1} IN "+cols[0])
			same(t, "the replace", column(t, db, "RETURN [DOCUMENT(@@c, '2').v, DOCUMENT(@@c, '2').w]", sql.Named("c", cols[0])), []any{nil, int64(1)})
		})
		t.Run("upsert", func(t *testing.T) {
			q := "UPSERT {_key: '9'} INSERT {_key: '9', v: 1} UPDATE {v: OLD.v + 1} IN " + cols[1] + " RETURN NEW.v"
			same(t, "the insert of the upsert", column(t, db, q), int64(1))
			same(t, "the update of the upsert", column(t, db, q), int64(2))
		})
		t.Run("overwrite_mode", func(t *testing.T) {
			q := "INSERT {_key: '1', x: 1} INTO " + cols[2] + " OPTIONS {overwriteMode: 'update'} RETURN NEW"
			same(t, "the overwrite", column(t, db, "LET d = FIRST("+q+") RETURN [d.v, d.x]"), []any{int64(3), int64(1)})
		})
		t.Run("delete", func(t *testing.T) {
			exec(t, db, "REMOVE '3' IN "+cols[2])
			same(t, "after the delete", column(t, db, "RETURN DOCUMENT(@@c, '3')", sql.Named("c", cols[2])), nil)
		})
	})
}

func TestIntegrationSchema(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		c := prefix + "schema_" + p.name
		t.Run("document_collection", func(t *testing.T) { exec(t, db, "CREATE COLLECTION "+c) })
		t.Run("edge_collection", func(t *testing.T) { exec(t, db, "CREATE COLLECTION "+c+"_e EDGE") })
		t.Run("persistent_index", func(t *testing.T) { exec(t, db, "CREATE INDEX p ON "+c+" (a)") })
		t.Run("geo_index", func(t *testing.T) { exec(t, db, "CREATE GEO INDEX g ON "+c+" (loc)") })
		t.Run("inverted_index", func(t *testing.T) { exec(t, db, "CREATE INVERTED INDEX v ON "+c+" (t)") })
		t.Run("ttl_index", func(t *testing.T) { exec(t, db, "CREATE TTL INDEX x ON "+c+" (exp) EXPIRE AFTER 60") })
		t.Run("fulltext_index", func(t *testing.T) {
			if err := call(t, p, http.MethodPost, "index?collection="+c, map[string]any{"type": "fulltext", "fields": []string{"f"}}); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("vector_index", func(t *testing.T) {
			body := map[string]any{"type": "vector", "fields": []string{"vec"}, "params": map[string]any{"metric": "cosine", "dimension": 2, "nLists": 1}}
			// The server refuses it, because --vector-index is off by
			// default (measured).
			if err := call(t, p, http.MethodPost, "index?collection="+c, body); err == nil {
				t.Error("a vector index gave no error on a server with --vector-index off")
			}
		})
		t.Run("unique_constraint", func(t *testing.T) {
			exec(t, db, "CREATE UNIQUE INDEX u ON "+c+" (u)")
			exec(t, db, "INSERT {u: 1} INTO "+c)
			if e := refused(t, db, "INSERT {u: 1} INTO "+c); e != nil && e.Num != 1210 {
				t.Errorf("a duplicate gave %d, want 1210", e.Num)
			}
		})
		t.Run("primary_key", func(t *testing.T) {
			exec(t, db, "INSERT {_key: 'k'} INTO "+c)
			if e := refused(t, db, "INSERT {_key: 'k'} INTO "+c); e != nil && e.Num != 1210 {
				t.Errorf("a duplicate key gave %d, want 1210", e.Num)
			}
		})
		t.Run("foreign_key", func(t *testing.T) {
			// An edge to a document that does not exist is stored, so there
			// is no foreign key (measured).
			same(t, "the edge", column(t, db, "INSERT {_from: 'nothere/1', _to: 'nothere/2'} INTO "+c+"_e RETURN NEW._from"), "nothere/1")
		})
		t.Run("default_value", func(t *testing.T) {
			dc := collection(t, "default_"+p.name)
			same(t, "the attributes", column(t, db, "LET d = FIRST(INSERT {x: 1} INTO "+dc+" RETURN NEW) RETURN ATTRIBUTES(d, true)"), []any{"x"})
		})
		t.Run("schema_validation", func(t *testing.T) {
			rule := map[string]any{"schema": map[string]any{"rule": map[string]any{"type": "object", "properties": map[string]any{"s": map[string]any{"type": "string"}}}, "level": "moderate", "message": "s is a string"}}
			if err := call(t, p, http.MethodPut, "collection/"+c+"/properties", rule); err != nil {
				t.Fatal(err)
			}
			if e := refused(t, db, "INSERT {s: 1} INTO "+c); e != nil && e.Num != 1620 {
				t.Errorf("a document that breaks the schema gave %d, want 1620", e.Num)
			}
		})
		t.Run("arangosearch_view", func(t *testing.T) {
			body := map[string]any{"name": c + "_view", "type": "arangosearch", "links": map[string]any{c: map[string]any{"includeAllFields": true}}}
			if err := call(t, p, http.MethodPost, "view", body); err != nil {
				t.Fatal(err)
			}
			undo(t, p, "view/"+c+"_view")
		})
		t.Run("search_alias_view", func(t *testing.T) {
			if err := call(t, p, http.MethodPost, "view", map[string]any{"name": c + "_alias", "type": "search-alias"}); err != nil {
				t.Fatal(err)
			}
			undo(t, p, "view/"+c+"_alias")
		})
		t.Run("named_graph", func(t *testing.T) {
			body := map[string]any{"name": c + "_graph", "edgeDefinitions": []any{map[string]any{"collection": c + "_e", "from": []string{c}, "to": []string{c}}}}
			if err := call(t, p, http.MethodPost, "gharial", body); err != nil {
				t.Fatal(err)
			}
			undo(t, p, "gharial/"+c+"_graph")
		})
		t.Run("analyzer", func(t *testing.T) {
			if err := call(t, p, http.MethodPost, "analyzer", map[string]any{"name": c + "_an", "type": "identity"}); err != nil {
				t.Fatal(err)
			}
			undo(t, p, "analyzer/"+c+"_an")
		})
		t.Run("create_database", func(t *testing.T) {
			sys := p
			err := callIn(t, sys, "_system", http.MethodPost, "database", map[string]any{"name": c + "_db"})
			if p == ordinary {
				// The ordinary user has no access to _system (measured).
				if err == nil {
					t.Error("the ordinary user made a database")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
		t.Run("drop_database", func(t *testing.T) {
			if p == ordinary {
				t.Skip("the ordinary user has no access to _system, and made no database (measured)")
			}
			if err := callIn(t, p, "_system", http.MethodDelete, "database/"+c+"_db", nil); err != nil {
				t.Fatal(err)
			}
		})
	})
}

// undo deletes path as p when the test ends. The context of a test ends
// before its cleanup runs, so the delete takes the context without its end.
func undo(t *testing.T, p principal, path string) {
	t.Helper()
	c := NewConnector(config(t, p))
	t.Cleanup(func() {
		defer c.transport.CloseIdleConnections()
		if err := c.call(context.WithoutCancel(t.Context()), http.MethodDelete, path, nil, nil, ""); err != nil {
			t.Errorf("deleting %s at the end of the test: %v", path, err)
		}
	})
}

// callIn is call in the database db.
func callIn(t *testing.T, p principal, db, method, path string, body any) error {
	t.Helper()
	cfg := config(t, p)
	cfg.Database = db
	c := NewConnector(cfg)
	defer c.transport.CloseIdleConnections()
	return c.call(t.Context(), method, path, body, nil, "")
}

func TestIntegrationFeatures(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		v := collection(t, "graph_v_"+p.name, `{_key: "a", n: 1}`, `{_key: "b", n: 2}`, `{_key: "c", n: 3}`)
		e := prefix + "graph_e_" + p.name
		exec(t, db, "CREATE COLLECTION IF NOT EXISTS "+e+" EDGE")
		exec(t, db, "FOR x IN [['a', 'b'], ['b', 'c']] INSERT {_from: CONCAT(@v, '/', x[0]), _to: CONCAT(@v, '/', x[1])} INTO "+e, sql.Named("v", v))
		start := v + "/a"
		t.Run("traversal", func(t *testing.T) {
			same(t, "the traversal", column(t, db, "FOR x IN 1..2 OUTBOUND @s "+e+" RETURN x._key", sql.Named("s", start)), "b", "c")
		})
		t.Run("shortest_path", func(t *testing.T) {
			same(t, "the path", column(t, db, "FOR x IN OUTBOUND SHORTEST_PATH @s TO @t "+e+" RETURN x._key",
				sql.Named("s", start), sql.Named("t", v+"/c")), "a", "b", "c")
		})
		t.Run("prune", func(t *testing.T) {
			same(t, "the pruned traversal", column(t, db, "FOR x IN 1..3 OUTBOUND @s "+e+" PRUNE x._key == 'b' RETURN x._key", sql.Named("s", start)), "b")
		})
		t.Run("collect_aggregate", func(t *testing.T) {
			same(t, "the sum", column(t, db, "FOR d IN "+v+" COLLECT AGGREGATE s = SUM(d.n) RETURN s"), int64(6))
		})
		t.Run("collect_with_count", func(t *testing.T) {
			same(t, "the count", column(t, db, "FOR d IN "+v+" COLLECT WITH COUNT INTO c RETURN c"), int64(3))
		})
		t.Run("window", func(t *testing.T) {
			same(t, "the window", column(t, db, "FOR d IN "+v+" SORT d.n WINDOW {preceding: 1} AGGREGATE s = SUM(d.n) RETURN s"), int64(1), int64(3), int64(5))
		})
		t.Run("subquery", func(t *testing.T) {
			same(t, "the subquery", column(t, db, "LET m = (FOR d IN "+v+" RETURN d.n) RETURN MAX(m)"), int64(3))
		})
		t.Run("array_expansion", func(t *testing.T) {
			same(t, "the expansion", column(t, db, "RETURN (FOR d IN "+v+" SORT d.n RETURN d)[*].n"), []any{int64(1), int64(2), int64(3)})
		})
		t.Run("collection_parameter", func(t *testing.T) {
			same(t, "the count", column(t, db, "RETURN LENGTH(@@c)", sql.Named("c", v)), int64(3))
		})
		t.Run("full_count", func(t *testing.T) {
			// The driver streams, and fullCount does not work with a stream
			// (D90). COUNT of a subquery gives the same count.
			same(t, "the count", column(t, db, "RETURN COUNT(FOR d IN "+v+" RETURN 1)"), int64(3))
		})
		t.Run("stream_cursor", func(t *testing.T) {
			bdb := openWith(t, p, func(cfg *Config) { cfg.Batch = 1 })
			same(t, "the stream", column(t, bdb, "FOR d IN "+v+" SORT d.n RETURN d.n"), int64(1), int64(2), int64(3))
		})
		t.Run("max_runtime", func(t *testing.T) {
			// maxRuntime is an option of the cursor API, which the driver does
			// not send, so the test sends it.
			err := call(t, p, http.MethodPost, "cursor", map[string]any{"query": "RETURN SLEEP(1)", "options": map[string]any{"maxRuntime": 0.1}})
			if e, ok := errors.AsType[*Error](err); !ok || e.Num != 1500 {
				t.Errorf("a query past maxRuntime gave %v, want error 1500 (measured)", err)
			}
		})
		t.Run("search", func(t *testing.T) {
			view := v + "_view"
			body := map[string]any{"name": view, "type": "arangosearch", "links": map[string]any{v: map[string]any{"includeAllFields": true}}}
			if err := call(t, p, http.MethodPost, "view", body); err != nil {
				t.Fatal(err)
			}
			undo(t, p, "view/"+view)
			deadline := time.Now().Add(10 * time.Second)
			for got := column(t, db, "FOR d IN "+view+" SEARCH d.n == 2 OPTIONS {waitForSync: true} RETURN d._key"); len(got) == 0; {
				if time.Now().After(deadline) {
					t.Fatal("the view found nothing in 10 seconds")
				}
				time.Sleep(100 * time.Millisecond)
				got = column(t, db, "FOR d IN "+view+" SEARCH d.n == 2 OPTIONS {waitForSync: true} RETURN d._key")
			}
		})
		t.Run("geo_functions", func(t *testing.T) {
			got := column(t, db, "RETURN ROUND(GEO_DISTANCE([0, 0], [0, 1]) / 1000)")
			same(t, "the distance in km", got, int64(111))
		})
		t.Run("query_cache", func(t *testing.T) {
			var props struct {
				Mode string `json:"mode"`
			}
			c := NewConnector(config(t, p))
			defer c.transport.CloseIdleConnections()
			if err := c.call(t.Context(), http.MethodGet, "query-cache/properties", nil, &props, ""); err != nil {
				t.Fatal(err)
			}
			if props.Mode == "" {
				t.Error("the query cache names no mode")
			}
		})
		t.Run("stream_transactions", func(t *testing.T) {
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(t.Context(), "INSERT {_key: 'tx'} INTO "+v); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			same(t, "the committed write", column(t, db, "RETURN DOCUMENT(@@c, 'tx')._key", sql.Named("c", v)), "tx")
		})
		t.Run("explain", func(t *testing.T) {
			var plan map[string]any
			c := NewConnector(config(t, p))
			defer c.transport.CloseIdleConnections()
			if err := c.call(t.Context(), http.MethodPost, "explain", map[string]any{"query": "FOR d IN " + v + " RETURN d"}, &plan, ""); err != nil {
				t.Fatal(err)
			}
			if _, ok := plan["plan"]; !ok {
				t.Errorf("explain gave %v, want a plan", plan)
			}
		})
		t.Run("profile", func(t *testing.T) {
			var res map[string]any
			c := NewConnector(config(t, p))
			defer c.transport.CloseIdleConnections()
			if err := c.call(t.Context(), http.MethodPost, "cursor", map[string]any{"query": "RETURN 1", "options": map[string]any{"profile": true}}, &res, ""); err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(res["extra"])
			if err != nil || !strings.Contains(string(b), "profile") {
				t.Errorf("profile gave %s, want a profile", b)
			}
		})
	})
}

// roundTrip returns the round trip of one type into the collection c.
func roundTrip(typ, c string, values []dbimptest.Value) dbimptest.RoundTripCase {
	return dbimptest.RoundTripCase{
		Type:   typ,
		Named:  true,
		Values: values,
		Insert: "INSERT {_key: @key, v: @value} INTO " + c,
		Literal: func(key string, v any) (string, error) {
			k, err := json.Marshal(key)
			if err != nil {
				return "", err
			}
			lit, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return "INSERT {_key: " + string(k) + ", v: " + string(lit) + "} INTO " + c, nil
		},
		// An object that a query returns gives its keys as the columns
		// (D89), so the value goes in an object of one key.
		Select: "FOR d IN " + c + " FILTER d._key == @key RETURN {v: d.v}",
		Update: "UPDATE @key WITH {v: @value} IN " + c + " OPTIONS {mergeObjects: false}",
		Delete: "REMOVE @key IN " + c,
	}
}

func TestIntegrationRoundTrip(t *testing.T) {
	long := strings.Repeat("xé", 5000)
	for _, tt := range []struct {
		typ    string
		values []dbimptest.Value
	}{
		{"null", []dbimptest.Value{{Name: "null", In: nil}, {Name: "one", In: int64(1)}}},
		{"boolean", []dbimptest.Value{{Name: "null", In: nil}, {Name: "false", In: false}, {Name: "true", In: true}}},
		{"integer", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "zero", In: int64(0)}, {Name: "smallest", In: int64(-9007199254740991)},
			{Name: "largest", In: int64(math.MaxInt64)}, {Name: "above 2^53", In: int64(9007199254740993)},
		}},
		{"double", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "half", In: 0.5}, {Name: "smallest", In: -math.MaxFloat64},
			{Name: "largest", In: math.MaxFloat64}, {Name: "smallest step", In: math.SmallestNonzeroFloat64},
			{Name: "last digit", In: 0.1 + 0.2},
		}},
		{"string", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "empty", In: ""}, {Name: "long", In: long},
			{Name: "unicode", In: "é 日本 🙂"}, {Name: "quotes", In: `say "hi" \ bye`},
		}},
		{"array", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "empty", In: []any{}}, {Name: "mixed", In: []any{int64(1), "a", nil, []any{true}}},
		}},
		{"object", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "empty", In: map[string]any{}},
			{Name: "nested", In: map[string]any{"a": int64(1), "b": map[string]any{"c": "é"}}},
		}},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			forEach(t, func(t *testing.T, p principal, db *sql.DB) {
				c := collection(t, "rt_"+tt.typ+"_"+p.name)
				dbimptest.RoundTrip(t, db, roundTrip(tt.typ, c, tt.values))
			})
		})
	}
}
