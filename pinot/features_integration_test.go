package pinot_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/pinot"
)

// These tests hold each entry of testdata/pinot/features.json (step 14a).
// The Broker takes no write (D128), so each table is made, and each row
// loaded, through the Controller.

// crudTables makes three tables through the Controller and loads their
// rows: a table of players, a table of teams with an inverted index that a
// query uses, and a table of games.
func crudTables(t *testing.T, c *controller) (string, string, string) {
	t.Helper()
	players, teams, games := prefix+"players", prefix+"teams", prefix+"games"
	c.makeTable(t, map[string]any{"schemaName": players, "enableColumnBasedNullHandling": true,
		"dimensionFieldSpecs": []any{field("id", "INT", false), field("name", "STRING", false), field("team", "STRING", false)}},
		offline(players, nil))
	c.makeTable(t, map[string]any{"schemaName": teams,
		"dimensionFieldSpecs": []any{field("team", "STRING", false), field("city", "STRING", false)}},
		offline(teams, map[string]any{"tableIndexConfig": map[string]any{"loadMode": "MMAP", "invertedIndexColumns": []any{"team"}}}))
	c.makeTable(t, map[string]any{"schemaName": games,
		"dimensionFieldSpecs": []any{field("id", "INT", false), field("home", "STRING", false)},
		"metricFieldSpecs":    []any{map[string]any{"name": "runs", "dataType": "LONG"}}},
		offline(games, nil))
	for table, rows := range map[string][]map[string]any{
		players: {{"id": 1, "name": "Ann", "team": "a"}, {"id": 2, "name": "Bo", "team": "b"}, {"id": 3}},
		teams:   {{"team": "a", "city": "Ames"}, {"team": "b", "city": "Boise"}},
		games:   {{"id": 1, "home": "a", "runs": 3}, {"id": 2, "home": "b", "runs": 5}},
	} {
		if err := c.load(t.Context(), table, "", rows); err != nil {
			t.Fatal(err)
		}
	}
	return players, teams, games
}

func TestIntegrationCRUD(t *testing.T) {
	c := newController(t)
	players, teams, games := crudTables(t, c)
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		if !readsOwnTables(t, p, db, players) {
			return
		}
		t.Run("select", func(t *testing.T) {
			got := waitRows(t, db, "SELECT id, name, team FROM "+players+" ORDER BY id", 3)
			want := [][]any{{int64(1), "Ann", "a"}, {int64(2), "Bo", "b"}, {int64(3), nil, nil}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the players are %v, want %v", got, want)
			}
			waitRows(t, db, "SELECT team FROM "+teams, 2)
			waitRows(t, db, "SELECT id FROM "+games, 2)
			got = waitRows(t, db, "SELECT p.name, t.city, g.runs FROM "+players+" p JOIN "+teams+" t ON p.team = t.team JOIN "+
				games+" g ON g.home = t.team ORDER BY p.name", 2)
			want = [][]any{{"Ann", "Ames", int64(3)}, {"Bo", "Boise", int64(5)}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the join is %v, want %v", got, want)
			}
		})
		t.Run("insert", func(t *testing.T) {
			refused(t, db, "INSERT INTO "+players+" (id, name) VALUES (9, 'x')", 150)
			refused(t, db, "INSERT INTO "+players+" (id, name) VALUES (9, 'x')", 150, pinot.WithEngine(pinot.EngineSingle))
		})
		t.Run("insert_select", func(t *testing.T) {
			refused(t, db, "INSERT INTO "+players+" SELECT * FROM "+players, 150)
		})
		t.Run("update", func(t *testing.T) {
			_, _, err := readAll(t, db, "UPDATE "+players+" SET name = 'x' WHERE id = 1")
			if code(err) == 0 {
				t.Errorf("an UPDATE gave %v, want a refusal of the server", err)
			}
		})
		t.Run("delete", func(t *testing.T) {
			_, _, err := readAll(t, db, "DELETE FROM "+players+" WHERE id = 1")
			if code(err) == 0 {
				t.Errorf("a DELETE gave %v, want a refusal of the server", err)
			}
		})
		t.Run("insert_from_file", func(t *testing.T) {
			// The Broker starts a task on a Minion, which the image runs
			// none of, and the table gets no row (measured).
			cols, got, err := readAll(t, db, "INSERT INTO "+players+" FROM FILE 's3://dbimp/none'")
			if err != nil || len(got) != 0 || !reflect.DeepEqual(cols, []string{"tableName", "taskJobName"}) {
				t.Errorf("INSERT FROM FILE gave %q, %v, %v, want the columns of a task and no row", cols, got, err)
			}
			waitRows(t, db, "SELECT id FROM "+players, 3)
		})
		t.Run("upsert", func(t *testing.T) {
			upsertRefused(t, c)
		})
		// No statement changed a row.
		if got := waitRows(t, db, "SELECT id, name FROM "+players+" ORDER BY id", 3); got[0][1] != "Ann" {
			t.Errorf("the first player is %v after the refused writes, want Ann", got[0])
		}
	})
}

// upsertRefused holds that the Controller refuses a table of upserts on the
// server of the tests: an OFFLINE table cannot have one, and a REALTIME table
// needs a stream, which the image does not run (measured).
func upsertRefused(t *testing.T, c *controller) {
	t.Helper()
	name := prefix + "upsert_" + tableName(t.Name())
	if err := c.postJSON(t.Context(), "/schemas", map[string]any{"schemaName": name,
		"dimensionFieldSpecs": []any{field("k", "STRING", false)},
		"dateTimeFieldSpecs":  []any{map[string]any{"name": "ts", "dataType": "TIMESTAMP", "format": "1:MILLISECONDS:TIMESTAMP", "granularity": "1:MILLISECONDS"}},
		"primaryKeyColumns":   []any{"k"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.send(context.WithoutCancel(t.Context()), http.MethodDelete, "/schemas/"+name, nil, "")
	})
	err := c.postJSON(t.Context(), "/tables", offline(name, map[string]any{
		"segmentsConfig": map[string]any{"replication": "1", "timeColumnName": "ts"},
		"upsertConfig":   map[string]any{"mode": "FULL"}}))
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		_ = c.dropTable(context.WithoutCancel(t.Context()), name)
		t.Errorf("an OFFLINE table of upserts gave %v, want HTTP 400", err)
	}
	err = c.postJSON(t.Context(), "/tables", map[string]any{"tableName": name, "tableType": "REALTIME",
		"segmentsConfig":   map[string]any{"replication": "1", "timeColumnName": "ts"},
		"tableIndexConfig": map[string]any{"loadMode": "MMAP"}, "tenants": map[string]any{}, "metadata": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "streamConfigs") {
		t.Errorf("a REALTIME table with no stream gave %v, want a refusal that names streamConfigs", err)
	}
}

// tableName returns s with each character that a table name of Pinot cannot
// hold replaced by _, such as the name of a test.
func tableName(s string) string {
	return strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' {
			return r
		}
		return '_'
	}, s)
}

// indexTable makes a table of every kind of index, as step 6 made it, and
// loads its 50 rows. Each test makes one of its own.
func indexTable(t *testing.T, c *controller) string {
	t.Helper()
	name := prefix + "idx_" + tableName(t.Name())
	c.makeTable(t, map[string]any{
		"schemaName": name,
		"dimensionFieldSpecs": []any{field("k", "INT", false), field("s", "STRING", false), field("t", "STRING", false),
			field("j", "JSON", false), field("f", "STRING", false), field("p", "BYTES", false), field("v", "FLOAT", true)},
		"metricFieldSpecs":   []any{map[string]any{"name": "n", "dataType": "LONG"}},
		"dateTimeFieldSpecs": []any{map[string]any{"name": "ts", "dataType": "TIMESTAMP", "format": "1:MILLISECONDS:TIMESTAMP", "granularity": "1:MILLISECONDS"}},
	}, offline(name, map[string]any{
		"segmentsConfig": map[string]any{"replication": "1", "timeColumnName": "ts"},
		"tableIndexConfig": map[string]any{"loadMode": "MMAP", "sortedColumn": []any{"k"}, "invertedIndexColumns": []any{"s"},
			"rangeIndexColumns": []any{"n"}, "bloomFilterColumns": []any{"s"}, "jsonIndexColumns": []any{"j"},
			"noDictionaryColumns":  []any{"t", "p", "v"},
			"starTreeIndexConfigs": []any{map[string]any{"dimensionsSplitOrder": []any{"s"}, "functionColumnPairs": []any{"SUM__n"}, "maxLeafRecords": 1}}},
		"fieldConfigList": []any{
			map[string]any{"name": "t", "encodingType": "RAW", "indexTypes": []any{"TEXT"}},
			map[string]any{"name": "f", "encodingType": "DICTIONARY", "indexTypes": []any{"FST"}},
			map[string]any{"name": "ts", "encodingType": "DICTIONARY", "indexTypes": []any{"TIMESTAMP"}, "timestampConfig": map[string]any{"granularities": []any{"DAY"}}},
			map[string]any{"name": "p", "encodingType": "RAW", "indexType": "H3", "properties": map[string]any{"resolutions": "5"}},
			map[string]any{"name": "v", "encodingType": "RAW", "indexType": "VECTOR", "properties": map[string]any{
				"vectorIndexType": "HNSW", "vectorDimension": "2", "vectorDistanceFunction": "EUCLIDEAN", "version": "1"}},
		},
		"ingestionConfig": map[string]any{"transformConfigs": []any{map[string]any{"columnName": "p", "transformFunction": "stPoint(lon,lat)"}}},
	}))
	var rows []map[string]any
	for k := 1; k <= 50; k++ {
		text := fmt.Sprintf("lazy dog %d", k)
		if k%2 == 1 {
			text = fmt.Sprintf("the quick brown fox %d", k)
		}
		rows = append(rows, map[string]any{"k": k, "s": fmt.Sprintf("s%d", k%5), "t": text,
			"j": map[string]any{"a": k % 3, "b": "x"}, "f": fmt.Sprintf("word%d", k),
			"lon": math.Round((-122.0+float64(k)*0.01)*100) / 100, "lat": math.Round((37.0+float64(k)*0.01)*100) / 100,
			"v": []any{float64(k), float64(k % 7)}, "n": k * 10, "ts": 1700000000000 + int64(k)*86400000})
	}
	if err := c.load(t.Context(), name, "", rows); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestIntegrationSchema(t *testing.T) {
	c := newController(t)
	idx := indexTable(t, c)
	defaults := prefix + "defaults"
	c.makeTable(t, map[string]any{"schemaName": defaults, "dimensionFieldSpecs": []any{
		field("k", "INT", false), map[string]any{"name": "v", "dataType": "INT", "defaultNullValue": -1}, field("s", "STRING", false)}},
		offline(defaults, nil))
	if err := c.load(t.Context(), defaults, "", []map[string]any{{"k": 1, "v": 5, "s": "x"}, {"k": 2}}); err != nil {
		t.Fatal(err)
	}
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		t.Run("table", func(t *testing.T) {
			refused(t, db, "CREATE TABLE "+prefix+"new (k INT)", 150)
		})
		t.Run("primary_key", func(t *testing.T) {
			refused(t, db, "CREATE TABLE "+prefix+"new (k INT PRIMARY KEY)", 150)
		})
		t.Run("foreign_key", func(t *testing.T) {
			refused(t, db, "CREATE TABLE "+prefix+"new (k INT REFERENCES baseballStats (yearID))", 150)
		})
		t.Run("unique_constraint", func(t *testing.T) {
			refused(t, db, "CREATE TABLE "+prefix+"new (k INT UNIQUE)", 150)
		})
		t.Run("view", func(t *testing.T) {
			refused(t, db, "CREATE VIEW "+prefix+"view AS SELECT * FROM baseballStats", 150)
		})
		if !readsOwnTables(t, p, db, idx) {
			return
		}
		waitRows(t, db, "SELECT k FROM "+idx, 50)
		t.Run("default_value", func(t *testing.T) {
			// A table with no null handling stores the default of a missing
			// value, and the driver cannot tell it from a value (D130).
			got := waitRows(t, db, "SELECT k, v, s FROM "+defaults+" ORDER BY k", 2)
			if want := [][]any{{int64(1), int64(5), "x"}, {int64(2), int64(-1), "null"}}; !reflect.DeepEqual(got, want) {
				t.Errorf("the defaults are %v, want %v", got, want)
			}
		})
		for _, tt := range []struct {
			name, query string
			want        [][]any
		}{
			{"time_column", "SELECT count(*) FROM " + idx + " WHERE ts >= 1702000000000", [][]any{{int64(27)}}},
			{"sorted_index", "SELECT count(*) FROM " + idx + " WHERE k = 7", [][]any{{int64(1)}}},
			{"inverted_index", "SELECT count(*) FROM " + idx + " WHERE s = 's1'", [][]any{{int64(10)}}},
			{"range_index", "SELECT count(*) FROM " + idx + " WHERE n BETWEEN 100 AND 200", [][]any{{int64(11)}}},
			{"bloom_filter", "SELECT count(*) FROM " + idx + " WHERE s = 'nothere'", [][]any{{int64(0)}}},
			{"star-tree_index", "SELECT s, SUM(n) FROM " + idx + " GROUP BY s ORDER BY s", [][]any{{"s0", 2750.0}, {"s1", 2350.0}, {"s2", 2450.0}, {"s3", 2550.0}, {"s4", 2650.0}}},
			{"text_index", "SELECT count(*) FROM " + idx + " WHERE TEXT_MATCH(t, 'fox')", [][]any{{int64(25)}}},
			{"json_index", "SELECT count(*) FROM " + idx + " WHERE JSON_MATCH(j, '\"$.a\"=1')", [][]any{{int64(17)}}},
			{"fst_index", "SELECT count(*) FROM " + idx + " WHERE REGEXP_LIKE(f, 'word1.*')", [][]any{{int64(11)}}},
			{"timestamp_index", "SELECT dateTrunc('DAY', ts) AS d, count(*) FROM " + idx + " GROUP BY d ORDER BY d LIMIT 2",
				[][]any{{time.Date(2023, 11, 15, 0, 0, 0, 0, time.UTC), int64(1)}, {time.Date(2023, 11, 16, 0, 0, 0, 0, time.UTC), int64(1)}}},
			{"geospatial_index", "SELECT count(*) FROM " + idx + " WHERE ST_Distance(p, ST_Point(-121.9, 37.1)) < 0.05", [][]any{{int64(7)}}},
			{"vector_index", "SELECT k FROM " + idx + " WHERE VECTOR_SIMILARITY(v, ARRAY[3.0, 3.0], 2) ORDER BY k LIMIT 5", [][]any{{int64(3)}, {int64(4)}}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				// Step 6 measured the indexes on the single-stage engine.
				_, got, err := readAll(t, db, tt.query, pinot.WithEngine(pinot.EngineSingle))
				if err != nil || !reflect.DeepEqual(got, tt.want) {
					t.Errorf("%q gave %v, %v, want %v", tt.query, got, err, tt.want)
				}
			})
		}
	})
}

func TestIntegrationFeatures(t *testing.T) {
	c := newController(t)
	idx := indexTable(t, c)
	dim := prefix + "dim"
	c.makeTable(t, map[string]any{"schemaName": dim, "dimensionFieldSpecs": []any{field("s", "STRING", false), field("label", "STRING", false)},
		"primaryKeyColumns": []any{"s"}},
		offline(dim, map[string]any{"isDimTable": true, "quota": map[string]any{"storage": "10M"},
			"segmentsConfig": map[string]any{"replication": "1", "segmentPushType": "REFRESH"}}))
	if err := c.load(t.Context(), dim, "", []map[string]any{{"s": "s0", "label": "zero"}, {"s": "s1", "label": "one"}}); err != nil {
		t.Fatal(err)
	}
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		t.Run("set_options", func(t *testing.T) {
			_, got, err := readAll(t, db, "SET timeoutMs = 5000; SELECT count(*) FROM baseballStats")
			if err != nil || !reflect.DeepEqual(got, [][]any{{int64(97889)}}) {
				t.Errorf("SET gave %v, %v", got, err)
			}
		})
		t.Run("option_clause", func(t *testing.T) {
			_, got, err := readAll(t, db, "SELECT count(*) FROM baseballStats OPTION(timeoutMs=5000)", pinot.WithEngine(pinot.EngineSingle))
			if err != nil || !reflect.DeepEqual(got, [][]any{{int64(97889)}}) {
				t.Errorf("OPTION gave %v, %v", got, err)
			}
		})
		t.Run("default_limit_of_10", func(t *testing.T) {
			_, got, err := readAll(t, db, "SELECT playerID FROM baseballStats", pinot.WithEngine(pinot.EngineSingle))
			if err != nil || len(got) != 10 {
				t.Errorf("the single-stage engine gave %d rows, %v, want 10", len(got), err)
			}
		})
		t.Run("cursor", func(t *testing.T) {
			cursor(t, p)
		})
		t.Run("null_literal", func(t *testing.T) {
			rows, err := db.QueryContext(t.Context(), "SELECT NULL AS n FROM baseballStats LIMIT 1")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			cts, err := rows.ColumnTypes()
			if err != nil || cts[0].DatabaseTypeName() != "UNKNOWN" {
				t.Errorf("the type of NULL is %v, %v, want UNKNOWN", cts, err)
			}
			var v any = 1
			if !rows.Next() || rows.Scan(&v) != nil || v != nil {
				t.Errorf("NULL read as %v, %v, want nil", v, rows.Err())
			}
		})
		t.Run("transactions", func(t *testing.T) {
			if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
			}
			refused(t, db, "BEGIN", 150)
			refused(t, db, "START TRANSACTION", 150)
		})
		t.Run("upsert_table", func(t *testing.T) {
			upsertRefused(t, c)
		})
		t.Run("real-time_table", func(t *testing.T) {
			upsertRefused(t, c)
		})
		t.Run("query_timeout", func(t *testing.T) {
			_, _, err := readAll(t, db, slow, "dbimp-"+suffix+"-feature-"+p.name, pinot.WithTimeout(500*time.Millisecond))
			if code(err) != 250 {
				t.Errorf("a timeout of 500 ms gave %v, want 250", err)
			}
		})
		t.Run("cancel_a_query", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			if _, err := db.ExecContext(ctx, slow, "dbimp-"+suffix+"-cancel-"+p.name); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("the query gave %v, want context.DeadlineExceeded", err)
			}
		})
		if !readsOwnTables(t, p, db, idx) {
			return
		}
		waitRows(t, db, "SELECT k FROM "+idx, 50)
		waitRows(t, db, "SELECT s FROM "+dim, 2)
		for _, tt := range []struct {
			name, query string
			want        [][]any
			opts        []any
		}{
			{"multi-stage_engine", "SELECT i.k, d.label FROM " + idx + " i JOIN " + dim + " d ON i.s = d.s WHERE i.k < 7 ORDER BY i.k", [][]any{{int64(1), "one"}, {int64(5), "zero"}, {int64(6), "one"}}, nil},
			{"window_function", "SELECT k, SUM(n) OVER (ORDER BY k) AS c FROM " + idx + " WHERE k < 4 ORDER BY k", [][]any{{int64(1), int64(10)}, {int64(2), int64(30)}, {int64(3), int64(60)}}, nil},
			{"lookup_join", "SELECT /*+ joinOptions(join_strategy='lookup') */ i.k, d.label FROM " + idx + " i JOIN " + dim + " d ON i.s = d.s WHERE i.k < 3 ORDER BY i.k", [][]any{{int64(1), "one"}}, nil},
			{"lookup_function", "SELECT k, LOOKUP('" + dim + "', 'label', 's', s) AS l FROM " + idx + " WHERE k < 3 ORDER BY k", [][]any{{int64(1), "one"}, {int64(2), "null"}}, single},
			{"json_extract_scalar", "SELECT k, JSON_EXTRACT_SCALAR(j, '$.a', 'INT', -1) AS a FROM " + idx + " WHERE k < 4 ORDER BY k", [][]any{{int64(1), int64(1)}, {int64(2), int64(2)}, {int64(3), int64(0)}}, nil},
			{"id_set", "SELECT ID_SET(k) FROM " + idx + " WHERE k < 4", [][]any{{"ATowAAABAAAAAAACABAAAAABAAIAAwA="}}, single},
			{"null_handling", "SELECT k, CASE WHEN k = 1 THEN NULL ELSE s END AS s FROM " + idx + " WHERE k < 3 ORDER BY k", [][]any{{int64(1), nil}, {int64(2), "s2"}}, nil},
		} {
			t.Run(tt.name, func(t *testing.T) {
				_, got, err := readAll(t, db, tt.query, tt.opts...)
				if err != nil || !reflect.DeepEqual(got, tt.want) {
					t.Errorf("%q gave %v, %v, want %v", tt.query, got, err, tt.want)
				}
			})
		}
	})
}

// cursor pages a result through the cursor of the Broker, which the driver
// does not use, because an answer holds every row (D133). It sends the
// requests of step 6 itself.
func cursor(t *testing.T, p principal) {
	t.Helper()
	u, err := url.Parse(dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := u.User.Password()
	send := func(method, path string, body string) map[string]any {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, "http://"+u.Host+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(u.User.Username(), pass)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		if err := json.UnmarshalRead(res.Body, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	first := send(http.MethodPost, "/query/sql?getCursor=true&numRows=3",
		`{"sql":"SELECT DISTINCT yearID FROM baseballStats ORDER BY yearID","queryOptions":"useMultistageEngine=true"}`)
	id, _ := first["requestId"].(string)
	next := send(http.MethodGet, "/responseStore/"+id+"/results?offset=3&numRows=3", "")
	rows := func(m map[string]any) any {
		rt, _ := m["resultTable"].(map[string]any)
		return rt["rows"]
	}
	want1, want2 := []any{[]any{1871.0}, []any{1872.0}, []any{1873.0}}, []any{[]any{1874.0}, []any{1875.0}, []any{1876.0}}
	if !reflect.DeepEqual(rows(first), want1) || !reflect.DeepEqual(rows(next), want2) {
		t.Errorf("the cursor gave %v and %v, want %v and %v", rows(first), rows(next), want1, want2)
	}
}

// rtConnector opens connections for dbimptest.RoundTrip. A query goes to the
// driver, and each write goes to the Controller, because the Broker takes
// none (D128). The statements of a write are these, each followed by the
// name of the table: create and drop with the type of the value, insert
// with the key and the value, update with the value and the key, and delete
// with the key. Each key is a segment of its own, so an update loads the
// segment again, and a delete drops it.
type rtConnector struct {
	inner driver.Connector
	c     *controller
}

func (rc *rtConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := rc.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &rtConn{Conn: conn, c: rc.c}, nil
}

func (rc *rtConnector) Driver() driver.Driver { return rc.inner.Driver() }

// rtConn is a connection of rtConnector.
type rtConn struct {
	driver.Conn

	c *controller
}

// CheckNamedValue keeps every value as it is, so that a write gets the Go
// value of the test.
func (rc *rtConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (rc *rtConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := rc.Conn.(driver.QueryerContext)
	if !ok {
		return nil, fmt.Errorf("the connection runs no query: %w", dbimp.ErrNotSupported)
	}
	return q.QueryContext(ctx, query, args)
}

func (rc *rtConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	words := strings.Fields(query)
	if len(words) < 2 {
		return nil, fmt.Errorf("the statement %q: %w", query, dbimp.ErrInvalidValue)
	}
	table := words[1]
	arg := func(i int) any { return args[i].Value }
	key := func(i int) string {
		s, _ := arg(i).(string)
		return s
	}
	var err error
	switch words[0] {
	case "create":
		err = rc.create(ctx, table, words[2])
	case "drop":
		err = rc.c.dropTable(ctx, table)
		if err == nil {
			err = rc.c.send(ctx, http.MethodDelete, "/schemas/"+table, nil, "")
		}
	case "insert":
		err = rc.put(ctx, table, key(0), arg(1))
	case "update":
		err = rc.put(ctx, table, key(1), arg(0))
	case "delete":
		err = rc.c.dropSegment(ctx, table, key(0))
	default:
		err = fmt.Errorf("the statement %q: %w", query, dbimp.ErrInvalidValue)
	}
	return driver.ResultNoRows, err
}

// create makes a table with a key k and a value v of the type typ.
func (rc *rtConn) create(ctx context.Context, table, typ string) error {
	schema := map[string]any{"schemaName": table, "enableColumnBasedNullHandling": true,
		"dimensionFieldSpecs": []any{field("k", "STRING", false)}}
	if typ == "MAP" {
		schema["complexFieldSpecs"] = []any{map[string]any{"name": "v", "dataType": "MAP", "fieldType": "COMPLEX",
			"childFieldSpecs": map[string]any{
				"key":   map[string]any{"name": "key", "dataType": "STRING", "fieldType": "DIMENSION"},
				"value": map[string]any{"name": "value", "dataType": "INT", "fieldType": "DIMENSION"}}}}
	} else {
		elem, isArray := strings.CutSuffix(typ, "_ARRAY")
		schema["dimensionFieldSpecs"] = []any{field("k", "STRING", false), field("v", elem, isArray)}
	}
	if err := rc.c.postJSON(ctx, "/schemas", schema); err != nil {
		return err
	}
	return rc.c.postJSON(ctx, "/tables", offline(table, nil))
}

// put loads the row of key as a segment of its own, named by the key. A
// segment whose every MAP is empty or NULL fails to load with "Index 0 out
// of bounds for length 0" (measured), so a segment of a MAP holds a second
// row, whose key no select names.
func (rc *rtConn) put(ctx context.Context, table, key string, v any) error {
	row := map[string]any{"k": key}
	if v != nil {
		row["v"] = rowValue(v)
	}
	rows := []map[string]any{row}
	if strings.HasSuffix(table, "_map") {
		rows = append(rows, map[string]any{"k": "filler", "v": map[string]any{"filler": 0}})
	}
	return rc.c.load(ctx, table, key, rows)
}

// rowValue returns v in the form of a row of JSON that the Controller loads.
func rowValue(v any) any {
	switch v := v.(type) {
	case float64:
		switch {
		case math.IsNaN(v):
			return "NaN"
		case math.IsInf(v, 1):
			return "Infinity"
		case math.IsInf(v, -1):
			return "-Infinity"
		}
	case *apd.Decimal:
		return v.Text('f')
	case time.Time:
		// The Controller refuses to load a TIMESTAMP whose number fits in
		// 32 bits, such as 0, and takes the same milliseconds as text
		// (measured).
		return strconv.FormatInt(v.UnixMilli(), 10)
	case []byte:
		return hex.EncodeToString(v)
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = rowValue(e)
		}
		return out
	}
	return v
}

// noLiteral is the Literal of every round trip: Pinot takes no insert, so a
// value has no literal of one (D128). The literals of a query are tested
// against stored values in TestReplayLiterals and TestIntegrationLiterals.
func noLiteral(string, any) (string, error) {
	return "", fmt.Errorf("Pinot takes no insert, so a value has no literal of one: %w", dbimp.ErrNotSupported)
}

// roundTrip returns the round trip of one type, in a table of its own.
func roundTrip(typ string, values []dbimptest.Value) dbimptest.RoundTripCase {
	table := prefix + "rt_" + strings.ToLower(typ)
	return dbimptest.RoundTripCase{
		Type:     typ,
		Setup:    []string{"create " + table + " " + typ},
		Teardown: []string{"drop " + table},
		Insert:   "insert " + table,
		Literal:  noLiteral,
		Select:   "SELECT v FROM " + table + " WHERE k = ?",
		Update:   "update " + table,
		Delete:   "delete " + table,
		Values:   values,
		Wait:     visible,
		Equal:    rtEqual,
	}
}

// rtEqual compares a value read back with the value wanted. A decimal
// compares by its value, and an empty array wanted
// is read back as NULL, because Pinot stores an empty array as NULL
// (measured).
func rtEqual(got, want any) bool {
	switch w := want.(type) {
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case []any:
		if len(w) == 0 {
			return got == nil
		}
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !rtEqual(g[i], w[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}

// float32s returns each v with the float64 that a FLOAT of 1.5.1 reads back
// as: the server writes the shortest text of the float32 of v, such as 0.1,
// and the driver reads that text as a float64. 1.4.0 can write more digits,
// such as 1.17549435e-38 for 1.1754944e-38 (measured), so a FLOAT compares
// as a float32, with float32Equal.
func float32s(vs ...float64) []dbimptest.Value {
	var out []dbimptest.Value
	for i, v := range vs {
		want, err := strconv.ParseFloat(strconv.FormatFloat(v, 'g', -1, 32), 64)
		if err != nil {
			panic(err)
		}
		out = append(out, dbimptest.Value{Name: strconv.Itoa(i), In: v, Want: want})
	}
	return out
}

// float32Equal compares two FLOAT values as float32.
func float32Equal(got, want any) bool {
	g, gok := got.(float64)
	w, wok := want.(float64)
	if !gok || !wok {
		return reflect.DeepEqual(got, want)
	}
	return float32(g) == float32(w)
}

func TestIntegrationRoundTrip(t *testing.T) {
	c := newController(t)
	long := strings.Repeat("é", 512)
	dec := func(s string) *apd.Decimal {
		d, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	ts := func(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
	cases := []dbimptest.RoundTripCase{
		roundTrip("INT", []dbimptest.Value{{Name: "null"}, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(math.MinInt32)}, {Name: "max", In: int64(math.MaxInt32)}}),
		roundTrip("LONG", []dbimptest.Value{{Name: "null"}, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(math.MinInt64)}, {Name: "max", In: int64(math.MaxInt64)}}),
		// A NaN that the Controller loads reads back as NULL (measured), so
		// no round trip holds one. A NaN of a query reads back as NaN
		// (TestReplayLiteralsOfSelect).
		roundTrip("FLOAT", append([]dbimptest.Value{{Name: "null"}, {Name: "inf", In: math.Inf(1)}},
			float32s(0, 1.5, -3.4e38, 1.1754944e-38, 0.1)...)),
		roundTrip("DOUBLE", []dbimptest.Value{{Name: "null"}, {Name: "zero", In: 0.0}, {Name: "step", In: 0.1}, {Name: "max", In: math.MaxFloat64},
			{Name: "smallest", In: math.SmallestNonzeroFloat64}, {Name: "-inf", In: math.Inf(-1)}}),
		roundTrip("BIG_DECIMAL", []dbimptest.Value{{Name: "null"}, {Name: "zero", In: dec("0")}, {Name: "digits", In: dec("12345678901234567890.0123456789")},
			{Name: "negative", In: dec("-1")}, {Name: "long", In: dec("-" + strings.Repeat("9", 60) + "." + strings.Repeat("1", 40))}}),
		roundTrip("BOOLEAN", []dbimptest.Value{{Name: "null"}, {Name: "true", In: true}, {Name: "false", In: false}}),
		roundTrip("TIMESTAMP", []dbimptest.Value{{Name: "null"}, {Name: "milliseconds", In: ts(1700000000123)}, {Name: "epoch", In: ts(0)},
			{Name: "zone", In: time.UnixMilli(1650000000001).In(time.FixedZone("x", -7*3600)), Want: ts(1650000000001)},
			{Name: "before the epoch", In: ts(-2208988800000)}, {Name: "far", In: ts(253402300799999)}}),
		// A STRING keeps 512 characters, its default maxLength, and the
		// server cuts a longer one to that length (measured).
		roundTrip("STRING", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: ""}, {Name: "unicode", In: "é'\"\\ x ☃"},
			{Name: "long", In: long}, {Name: "cut", In: long + "x", Want: long}}),
		roundTrip("BYTES", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: []byte{}}, {Name: "bytes", In: []byte{0x00, 0xff}},
			{Name: "long", In: []byte(strings.Repeat("\x01\xfe", 256))}}),
		roundTrip("MAP", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: map[string]any{}}, {Name: "two", In: map[string]any{"a": int64(1), "b": int64(-2)}}}),
		roundTrip("INT_ARRAY", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: []any{}}, {Name: "range", In: []any{int64(math.MinInt32), int64(0), int64(math.MaxInt32)}}}),
		roundTrip("LONG_ARRAY", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: []any{}}, {Name: "range", In: []any{int64(math.MinInt64), int64(math.MaxInt64)}}}),
		roundTrip("FLOAT_ARRAY", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: []any{}}, {Name: "values", In: []any{1.5, -0.25}}}),
		roundTrip("DOUBLE_ARRAY", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: []any{}}, {Name: "values", In: []any{0.1, math.MaxFloat64}}}),
		roundTrip("BOOLEAN_ARRAY", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: []any{}}, {Name: "values", In: []any{true, false, true}}}),
		roundTrip("STRING_ARRAY", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: []any{}}, {Name: "values", In: []any{"", "é", "x y"}}}),
		roundTrip("TIMESTAMP_ARRAY", []dbimptest.Value{{Name: "null"}, {Name: "empty", In: []any{}}, {Name: "values", In: []any{ts(1700000000123), ts(0)}}}),
	}
	// A JSON column is STRING on the multi-stage engine, so its round trip
	// runs on the single-stage engine, which names it JSON (measured).
	jsonCase := roundTrip("JSON", []dbimptest.Value{{Name: "null"}, {Name: "object", In: map[string]any{"k": []any{int64(1), nil, "s"}}},
		{Name: "array", In: []any{}, Want: []any{}}, {Name: "number", In: 1.5}, {Name: "string", In: "s"}})
	jsonCase.Equal = reflect.DeepEqual
	for i := range cases {
		if cases[i].Type == "FLOAT" {
			cases[i].Equal = float32Equal
		}
	}
	probe := prefix + "rt_probe"
	c.makeTable(t, map[string]any{"schemaName": probe, "dimensionFieldSpecs": []any{field("k", "STRING", false)}}, offline(probe, nil))
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		if !readsOwnTables(t, p, db, probe) {
			return
		}
		for _, tt := range append(cases, jsonCase) {
			t.Run(tt.Type, func(t *testing.T) {
				query := ""
				if tt.Type == "JSON" {
					query = "?engine=single"
				}
				connector, err := pinot.Driver{}.OpenConnector(dsn(t, p) + query)
				if err != nil {
					t.Fatal(err)
				}
				rt := sql.OpenDB(&rtConnector{inner: connector, c: c})
				t.Cleanup(func() { rt.Close() })
				dbimptest.RoundTrip(t, rt, tt)
			})
		}
	})
}

// TestIntegrationLiterals holds D132 against stored values: a query with an
// argument of each type selects the row that holds it.
func TestIntegrationLiterals(t *testing.T) {
	c := newController(t)
	name := prefix + "literals"
	c.makeTable(t, map[string]any{"schemaName": name, "enableColumnBasedNullHandling": true, "dimensionFieldSpecs": []any{
		field("id", "INT", false), field("l", "LONG", false), field("d", "DOUBLE", false), field("bd", "BIG_DECIMAL", false),
		field("b", "BOOLEAN", false), field("s", "STRING", false), field("by", "BYTES", false), field("ts", "TIMESTAMP", false)}},
		offline(name, nil))
	if err := c.load(t.Context(), name, "", []map[string]any{
		{"id": 1, "l": int64(math.MaxInt64), "d": 0.1, "bd": "12345678901234567890.0123456789", "b": true, "s": "é'\"\\ x ? --", "by": "00ff", "ts": 1700000000123},
		{"id": 2, "l": 0, "d": 1.5, "bd": "-1", "b": false, "s": "", "by": "", "ts": "0"},
	}); err != nil {
		t.Fatal(err)
	}
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		if !readsOwnTables(t, p, db, name) {
			return
		}
		waitRows(t, db, "SELECT id FROM "+name, 2)
		d, _, _ := apd.NewFromString("12345678901234567890.0123456789")
		for _, tt := range []struct {
			where string
			arg   any
			want  int64
		}{
			{"l = ?", int64(math.MaxInt64), 1},
			{"l = ?", uint64(0), 2},
			{"d = ?", 0.1, 1},
			{"bd = ?", d, 1},
			{"b = ?", false, 2},
			{"s = ?", "é'\"\\ x ? --", 1},
			{"s = ?", "", 2},
			{"by = ?", []byte{0x00, 0xff}, 1},
			{"ts = ?", time.UnixMilli(1700000000123).In(time.FixedZone("x", 3600)), 1},
			{"ts = ?", time.Unix(0, 0), 2},
		} {
			var id int64
			err := db.QueryRowContext(t.Context(), "SELECT id FROM "+name+" WHERE "+tt.where, tt.arg).Scan(&id)
			if err != nil || id != tt.want {
				t.Errorf("%s with %#v selected %d, %v, want %d", tt.where, tt.arg, id, err, tt.want)
			}
		}
		var n int64
		if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+name+" WHERE s = ? OR s IS NULL", nil).Scan(&n); err != nil || n != 0 {
			t.Errorf("a NULL argument counted %d, %v, want 0", n, err)
		}
	})
}
