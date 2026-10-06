package druid_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/druid"
)

// These tests hold each entry of testdata/druid/features.json against a
// real server (step 14a). They need DRUID_DSN and DRUID_ORDINARY_DSN, and
// skip when one is empty.

// refused runs query through the driver, and fails the test unless the
// server refuses it with HTTP 400 and a message that holds want.
func refused(t *testing.T, db *sql.DB, query, want string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), query)
	e, ok := errors.AsType[*druid.Error](err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || !strings.Contains(e.Message, want) {
		t.Errorf("%q gave %v, want the refusal of the server with %q", query, err, want)
	}
}

// taskRefused sends query to the task API, and fails the test unless the
// server refuses it before it runs, with HTTP 400, the state FAILED and an
// error that holds want (recorded: "crud: update through the task API").
func taskRefused(t *testing.T, s *api, query, want string) {
	t.Helper()
	status, b, err := s.submit(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		State string `json:"state"`
		Error struct {
			Message string `json:"errorMessage"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &answer); err != nil || status != http.StatusBadRequest || answer.State != "FAILED" || !strings.Contains(answer.Error.Message, want) {
		t.Errorf("the task API answered %q with HTTP %d: %s, want a refusal with %q", query, status, b, want)
	}
}

// day returns the time of midnight UTC of a day of October 2026.
func day(d int) time.Time {
	return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC)
}

// overDay is the clause of a REPLACE that overwrites one day of October 2026.
func overDay(d int) string {
	return fmt.Sprintf("OVERWRITE WHERE __time >= TIMESTAMP '2026-10-%02d 00:00:00' AND __time < TIMESTAMP '2026-10-%02d 00:00:00'", d, d+1)
}

// TestIntegrationCRUD holds each statement of CRUD on three datasources. The
// SQL API refuses INSERT, UPDATE and DELETE, and the driver returns the
// refusal (D163). The administrator writes through the task API instead: an
// INSERT, a REPLACE of one day in place of an update, and a REPLACE of one
// day with no rows in place of a delete. Each principal reads the result
// through the driver. The ordinary user can run no task (measured), so the
// administrator writes for it.
func TestIntegrationCRUD(t *testing.T) {
	s := newAdminAPI(t)
	tables := []string{prefix + "crud_a", prefix + "crud_b", prefix + "crud_c"}
	selectAll := func(name string) string { return "SELECT __time, id, v FROM " + name + " ORDER BY __time" }
	t.Run("insert", func(t *testing.T) {
		for _, name := range tables {
			s.mustTask(t, "INSERT INTO "+name+" SELECT TIME_PARSE(t) AS __time, id, v FROM (VALUES "+
				"('2026-10-01T00:00:00Z', 1, 'one'), ('2026-10-02T00:00:00Z', 2, 'two')) AS x(t, id, v) PARTITIONED BY DAY CLUSTERED BY id")
		}
	})
	t.Run("select", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			for _, name := range tables {
				waitFor(t, db, selectAll(name), [][]any{{day(1), int64(1), "one"}, {day(2), int64(2), "two"}})
			}
		})
	})
	t.Run("insert through the SQL API", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refused(t, db, "INSERT INTO "+tables[0]+" SELECT TIME_PARSE('2026-10-03T00:00:00Z') AS __time, 3 AS id, 'three' AS v PARTITIONED BY DAY",
				"INSERT operations are not supported")
		})
	})
	t.Run("update", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			for _, name := range tables {
				refused(t, db, "UPDATE "+name+" SET v = 'uno' WHERE id = 1", "Unsupported SQL statement [UPDATE]")
			}
		})
	})
	t.Run("update through the task API", func(t *testing.T) {
		taskRefused(t, s, "UPDATE "+tables[0]+" SET v = 'uno' WHERE id = 1", "Unsupported SQL statement [UPDATE]")
	})
	t.Run("upsert", func(t *testing.T) {
		taskRefused(t, s, "UPSERT INTO "+tables[0]+" SELECT TIME_PARSE('2026-10-04T00:00:00Z') AS __time, 4 AS id, 'four' AS v PARTITIONED BY DAY",
			"UPSERT is not supported")
	})
	t.Run("replace", func(t *testing.T) {
		for _, name := range tables {
			s.mustTask(t, "REPLACE INTO "+name+" "+overDay(1)+" SELECT TIME_PARSE('2026-10-01T00:00:00Z') AS __time, 1 AS id, 'uno' AS v PARTITIONED BY DAY")
		}
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			for _, name := range tables {
				waitFor(t, db, selectAll(name), [][]any{{day(1), int64(1), "uno"}, {day(2), int64(2), "two"}})
			}
		})
	})
	t.Run("delete", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			for _, name := range tables {
				refused(t, db, "DELETE FROM "+name+" WHERE id = 2", "Unsupported SQL statement [DELETE]")
			}
		})
	})
	t.Run("replace a time range with no rows", func(t *testing.T) {
		for _, name := range tables {
			s.mustTask(t, "REPLACE INTO "+name+" "+overDay(2)+" SELECT __time, id, v FROM "+name+
				" WHERE __time >= TIMESTAMP '2026-10-02 00:00:00' AND __time < TIMESTAMP '2026-10-03 00:00:00' AND id = 0 PARTITIONED BY DAY")
		}
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			for _, name := range tables {
				waitFor(t, db, selectAll(name), [][]any{{day(1), int64(1), "uno"}})
			}
		})
	})
}

// TestIntegrationSchema holds the statements of a schema. A datasource has
// no DDL: a write makes it, with PARTITIONED BY and CLUSTERED BY, and the
// Coordinator drops it (recorded). The SQL API refuses every statement of
// DDL with a syntax error, and the driver returns the refusal.
func TestIntegrationSchema(t *testing.T) {
	s := newAdminAPI(t)
	name := prefix + "schema"
	listed := "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = 'druid' AND TABLE_NAME = " + sqlQuote(name)
	t.Run("datasource made by a write", func(t *testing.T) {
		s.mustTask(t, "INSERT INTO "+name+" SELECT TIME_PARSE(t) AS __time, id, v FROM (VALUES "+
			"('2026-10-01T00:00:00Z', 2, 'b'), ('2026-10-01T00:00:00Z', 1, 'a'), ('2026-10-02T00:00:00Z', 3, 'c')) AS x(t, id, v) "+
			"PARTITIONED BY DAY CLUSTERED BY id")
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			waitFor(t, db, listed, [][]any{{name}})
		})
	})
	t.Run("partitioned by", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			waitFor(t, db, `SELECT "start" FROM sys.segments WHERE datasource = `+sqlQuote(name)+` AND is_overshadowed = 0 ORDER BY "start"`,
				[][]any{{"2026-10-01T00:00:00.000Z"}, {"2026-10-02T00:00:00.000Z"}})
		})
	})
	t.Run("clustered by", func(t *testing.T) {
		// A segment keeps its rows in the order of __time and then of the
		// columns of CLUSTERED BY, and a query with no ORDER BY reads them in
		// that order (measured).
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			waitFor(t, db, "SELECT id, v FROM "+name+" WHERE __time < TIMESTAMP '2026-10-02 00:00:00'",
				[][]any{{int64(1), "a"}, {int64(2), "b"}})
		})
	})
	t.Run("information schema", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			waitFor(t, db, "SELECT COLUMN_NAME, DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = "+sqlQuote(name)+" ORDER BY ORDINAL_POSITION",
				[][]any{{"__time", "TIMESTAMP"}, {"id", "BIGINT"}, {"v", "VARCHAR"}})
		})
	})
	ddl := map[string]string{
		"create table":      "CREATE TABLE " + prefix + "x (id BIGINT)",
		"primary key":       "CREATE TABLE " + prefix + "x (id BIGINT PRIMARY KEY)",
		"unique constraint": "CREATE TABLE " + prefix + "x (id BIGINT, v VARCHAR UNIQUE)",
		"default value":     "CREATE TABLE " + prefix + "x (id BIGINT, v VARCHAR DEFAULT 'a')",
		"foreign key":       "CREATE TABLE " + prefix + "y (id BIGINT REFERENCES " + name + " (id))",
		"index":             "CREATE INDEX " + prefix + "i ON " + name + " (v)",
		"view":              "CREATE VIEW " + prefix + "v AS SELECT id FROM " + name,
		"alter table":       "ALTER TABLE " + name + " ADD COLUMN w BIGINT",
		"drop table":        "DROP TABLE " + name,
	}
	for _, sub := range []string{"create table", "primary key", "unique constraint", "default value", "foreign key", "index", "view", "alter table", "drop table"} {
		t.Run(sub, func(t *testing.T) {
			forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
				refused(t, db, ddl[sub], "Incorrect syntax")
			})
		})
	}
	t.Run("drop a datasource through the Coordinator", func(t *testing.T) {
		if err := s.drop(t.Context(), name); err != nil {
			t.Fatal(err)
		}
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			waitFor(t, db, listed, nil)
		})
	})
}

// apiAs returns the api of the server as p.
func apiAs(t *testing.T, p principal) *api {
	t.Helper()
	s, err := newAPI(dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// rawQuery sends a body to POST /druid/v2/sql as p, and returns the status
// and the body of the answer, for a result format that the driver does not
// read.
func rawQuery(t *testing.T, p principal, body map[string]any) (int, string) {
	t.Helper()
	status, b, err := apiAs(t, p).do(t.Context(), http.MethodPost, "/druid/v2/sql", body)
	if err != nil {
		t.Fatal(err)
	}
	return status, string(b)
}

// recorder is a proxy between the driver and the server. It keeps the id of
// each query that the driver sends, the id that the server names in its
// answer, and the status of each cancel.
type recorder struct {
	mu      sync.Mutex
	sent    []string
	named   []string
	cancels []int
}

// open starts the proxy to the server of p, and opens the driver on it.
func (rec *recorder) open(t *testing.T, p principal) *sql.DB {
	t.Helper()
	cfg, err := druid.ParseDSN(dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)}
	proxy := &httputil.ReverseProxy{
		Rewrite:       func(r *httputil.ProxyRequest) { r.SetURL(target) },
		FlushInterval: -1,
		// The proxy logs each request that the driver cancels.
		ErrorLog: log.New(io.Discard, "", 0),
		ModifyResponse: func(res *http.Response) error {
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if res.Request.Method == http.MethodDelete {
				rec.cancels = append(rec.cancels, res.StatusCode)
				return nil
			}
			rec.named = append(rec.named, res.Header.Get("X-Druid-Sql-Query-Id"))
			return nil
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			var body struct {
				Context struct {
					SQLQueryID string `json:"sqlQueryId"`
				} `json:"context"`
			}
			_ = json.Unmarshal(b, &body)
			rec.mu.Lock()
			rec.sent = append(rec.sent, body.Context.SQLQueryID)
			rec.mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	pcfg := *cfg
	pcfg.Host = u.Hostname()
	if pcfg.Port, err = strconv.Atoi(u.Port()); err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(druid.NewConnector(pcfg))
	t.Cleanup(func() { db.Close() })
	return db
}

// lookupName is the lookup of this run, which TestIntegrationFeatures makes.
var lookupName = prefix + "lookup"

// makeLookup makes the lookup of this run through the Coordinator, as the
// setup of step 6 did (requests.json), and removes it when the test ends.
func makeLookup(t *testing.T, s *api) {
	t.Helper()
	for _, body := range []any{
		map[string]any{},
		map[string]any{"__default": map[string]any{lookupName: map[string]any{
			"version":                "v1",
			"lookupExtractorFactory": map[string]any{"type": "map", "map": map[string]any{"a": "apple", "b": "banana"}},
		}}},
	} {
		status, b, err := s.do(t.Context(), http.MethodPost, "/druid/coordinator/v1/lookups/config", body)
		if err != nil || status >= http.StatusMultipleChoices {
			t.Fatalf("making the lookup: HTTP %d: %s, %v", status, b, err)
		}
	}
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		if _, _, err := s.do(ctx, http.MethodDelete, "/druid/coordinator/v1/lookups/config/__default/"+lookupName, nil); err != nil {
			t.Errorf("removing the lookup: %v", err)
		}
	})
}

// TestIntegrationFeatures holds each feature of features.json.
func TestIntegrationFeatures(t *testing.T) {
	types := typesDatasource(t)
	big := bigDatasource(t)
	s := newAdminAPI(t)
	t.Run("time column", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			waitFor(t, db, "SELECT TIME_FLOOR(__time, 'P1D') AS d, COUNT(*) AS c FROM "+big+" GROUP BY 1 ORDER BY 1",
				[][]any{{day(1), int64(399)}, {day(2), int64(1)}})
		})
	})
	t.Run("extern", func(t *testing.T) {
		name := prefix + "extern"
		s.mustTask(t, "REPLACE INTO "+name+` OVERWRITE ALL
SELECT TIME_PARSE(t) AS __time, v
FROM TABLE(EXTERN('{"type":"inline","data":"{\"t\":\"2026-10-01T00:00:00Z\",\"v\":1}\n{\"t\":\"2026-10-01T01:00:00Z\",\"v\":2}"}', '{"type":"json"}')) EXTEND (t VARCHAR, v BIGINT)
PARTITIONED BY DAY`)
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			waitFor(t, db, "SELECT __time, v FROM "+name+" ORDER BY __time",
				[][]any{{day(1), int64(1)}, {day(1).Add(time.Hour), int64(2)}})
		})
	})
	t.Run("query context", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT n FROM "+big+" ORDER BY __time", druid.WithParameter("context", map[string]any{"sqlOuterLimit": 5}))
			if err != nil || len(got) != 5 {
				t.Errorf("sqlOuterLimit gave %d rows, %v, want 5", len(got), err)
			}
			jakarta := time.FixedZone("", 7*3600)
			_, got, err = readAll(t, db, "SELECT __time, CAST(__time AS DATE) AS dt FROM "+types+" WHERE id = 1", druid.WithTimeZone("Asia/Jakarta"))
			want := [][]any{{time.Date(2026, 10, 1, 19, 34, 56, 789e6, jakarta), dbimp.Date{Year: 2026, Month: 10, Day: 1}}}
			if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("the time zone gave %v, %v, want %v", got, err, want)
			}
		})
	})
	t.Run("query id", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, _ *sql.DB) {
			rec := &recorder{}
			db := rec.open(t, p)
			for range 2 {
				if _, _, err := readAll(t, db, "SELECT id FROM "+types+" WHERE id = 1"); err != nil {
					t.Fatal(err)
				}
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.sent) != 2 || rec.sent[0] == rec.sent[1] || fmt.Sprint(rec.sent) != fmt.Sprint(rec.named) {
				t.Errorf("the driver sent the ids %q and the server named %q, want an id of its own for each query, which the server keeps", rec.sent, rec.named)
			}
		})
	})
	t.Run("result format object", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, _ *sql.DB) {
			status, body := rawQuery(t, p, map[string]any{"query": "SELECT id, s FROM " + types + " WHERE id = 2", "resultFormat": "object"})
			if status != http.StatusOK || strings.TrimSpace(body) != `[{"id":2,"s":""}]` {
				t.Errorf("object gave HTTP %d: %s", status, body)
			}
		})
	})
	t.Run("result format array", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, _ *sql.DB) {
			status, body := rawQuery(t, p, map[string]any{"query": "SELECT id, s FROM " + types + " WHERE id = 2", "resultFormat": "array"})
			if status != http.StatusOK || strings.TrimSpace(body) != `[[2,""]]` {
				t.Errorf("array gave HTTP %d: %s", status, body)
			}
		})
	})
	t.Run("result format objectLines", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, _ *sql.DB) {
			status, body := rawQuery(t, p, map[string]any{"query": "SELECT id, s FROM " + types + " WHERE id = 2", "resultFormat": "objectLines"})
			if status != http.StatusOK || body != "{\"id\":2,\"s\":\"\"}\n\n" {
				t.Errorf("objectLines gave HTTP %d: %q", status, body)
			}
		})
	})
	t.Run("result format arrayLines", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			status, body := rawQuery(t, p, map[string]any{"query": "SELECT id, s FROM " + types + " WHERE id = 2", "resultFormat": "arrayLines"})
			if status != http.StatusOK || body != "[2,\"\"]\n\n" {
				t.Errorf("arrayLines gave HTTP %d: %q", status, body)
			}
			// The driver reads arrayLines (D164).
			if _, got, err := readAll(t, db, "SELECT id, s FROM "+types+" WHERE id = 2"); err != nil || fmt.Sprint(got) != "[[2 ]]" {
				t.Errorf("the driver gave %v, %v", got, err)
			}
		})
	})
	t.Run("result format csv", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, _ *sql.DB) {
			status, body := rawQuery(t, p, map[string]any{"query": "SELECT id, s FROM " + types + " WHERE id = 1", "resultFormat": "csv"})
			if status != http.StatusOK || body != "1,\"é'\"\"\\ x\"\n\n" {
				t.Errorf("csv gave HTTP %d: %q", status, body)
			}
		})
	})
	t.Run("result format tsv", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			status, body := rawQuery(t, p, map[string]any{"query": "SELECT id FROM " + types, "resultFormat": "tsv"})
			if status != http.StatusBadRequest || !strings.Contains(body, "TSV") {
				t.Errorf("tsv gave HTTP %d: %s, want HTTP 400", status, body)
			}
			_, err := db.ExecContext(t.Context(), "SELECT id FROM "+types, druid.WithParameter("resultFormat", "tsv"))
			if e, ok := errors.AsType[*druid.Error](err); !ok || e.HTTPStatus != http.StatusBadRequest {
				t.Errorf("the driver with tsv gave %v, want HTTP 400", err)
			}
		})
	})
	t.Run("header", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, _ *sql.DB) {
			status, body := rawQuery(t, p, map[string]any{"query": "SELECT id, s FROM " + types + " WHERE id = 2", "resultFormat": "array", "header": true})
			if status != http.StatusOK || strings.TrimSpace(body) != `[["id","s"],[2,""]]` {
				t.Errorf("header gave HTTP %d: %s", status, body)
			}
		})
	})
	t.Run("typesHeader", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, _ *sql.DB) {
			status, body := rawQuery(t, p, map[string]any{"query": "SELECT id, s, j FROM " + types + " WHERE id = 2", "resultFormat": "array", "header": true, "typesHeader": true})
			if status != http.StatusOK || !strings.HasPrefix(body, `[["id","s","j"],["LONG","STRING","COMPLEX<json>"],`) {
				t.Errorf("typesHeader gave HTTP %d: %s", status, body)
			}
		})
	})
	t.Run("sqlTypesHeader", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			rows, err := db.QueryContext(t.Context(), "SELECT id, s, j, sa FROM "+types+" WHERE id = 2")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			cts, err := rows.ColumnTypes()
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, ct := range cts {
				names = append(names, ct.DatabaseTypeName())
			}
			if got := strings.Join(names, " "); got != "BIGINT VARCHAR OTHER ARRAY" {
				t.Errorf("the SQL types are %q, want BIGINT VARCHAR OTHER ARRAY", got)
			}
			for rows.Next() {
			}
			if err := rows.Err(); err != nil {
				t.Error(err)
			}
		})
	})
	t.Run("lookup function", func(t *testing.T) {
		makeLookup(t, s)
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			waitFor(t, db, "SELECT LOOKUP('a', "+sqlQuote(lookupName)+") AS a, LOOKUP('q', "+sqlQuote(lookupName)+") AS q", [][]any{{"apple", nil}})
		})
		t.Run("lookup table", func(t *testing.T) {
			forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
				waitFor(t, db, `SELECT k, v FROM lookup."`+lookupName+`" WHERE k = 'b'`, [][]any{{"b", "banana"}})
			})
		})
	})
	t.Run("approximate functions", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT APPROX_COUNT_DISTINCT(l) AS a, APPROX_COUNT_DISTINCT_DS_HLL(l) AS h, APPROX_QUANTILE_DS(id, 0.5) AS q FROM "+types)
			if err != nil || fmt.Sprint(got) != "[[2 2 2]]" {
				t.Errorf("the approximate functions gave %v, %v, want 2, 2 and 2", got, err)
			}
		})
	})
	t.Run("sketch", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT DS_HLL(s) AS h FROM "+types)
			if b, ok := got[0][0].([]byte); err != nil || !ok || len(b) == 0 {
				t.Errorf("DS_HLL gave %#v, %v, want the bytes of a sketch", got, err)
			}
		})
	})
	t.Run("multi-value strings", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT id, mv, MV_LENGTH(mv) AS n FROM "+types+" WHERE MV_CONTAINS(mv, 'x') OR mv = 'z' ORDER BY __time")
			want := [][]any{{int64(2), "z", int64(1)}, {int64(1), []any{"x", "y"}, int64(2)}}
			if err != nil || fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
				t.Errorf("the multi-value strings gave %#v, %v, want %#v", got, err, want)
			}
		})
	})
	t.Run("multi-value string as an array", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			for _, args := range [][]any{nil, {druid.WithParameter("context", map[string]any{"sqlStringifyArrays": false})}} {
				_, got, err := readAll(t, db, "SELECT MV_TO_ARRAY(mv) AS a FROM "+types+" WHERE id = 1", args...)
				if err != nil || fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", [][]any{{[]any{"x", "y"}}}) {
					t.Errorf("MV_TO_ARRAY gave %#v, %v", got, err)
				}
			}
		})
	})
	t.Run("sqlStringifyArrays", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			query := "SELECT sa, la, da FROM " + types + " WHERE id = 1"
			_, text, err := readAll(t, db, query)
			if err != nil {
				t.Fatal(err)
			}
			_, arrays, err := readAll(t, db, query, druid.WithParameter("context", map[string]any{"sqlStringifyArrays": false}))
			if err != nil || fmt.Sprintf("%#v", text) != fmt.Sprintf("%#v", arrays) {
				t.Errorf("the arrays as text gave %#v, and as arrays %#v, %v, want the same values", text, arrays, err)
			}
		})
	})
	t.Run("sys schema", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			waitFor(t, db, "SELECT datasource, COUNT(*) AS segments FROM sys.segments WHERE datasource = "+sqlQuote(types)+" GROUP BY datasource",
				[][]any{{types, int64(1)}})
		})
	})
	t.Run("dynamic parameters", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, got, err := readAll(t, db, "SELECT id, ? AS p FROM "+types+" WHERE __time >= ? AND s = ? AND l = ? AND d = ?",
				[]string{"a", "b"}, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), "é'\"\\ x", int64(9223372036854775807), 1.7976931348623157e308)
			want := [][]any{{int64(1), []any{"a", "b"}}}
			if err != nil || fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
				t.Errorf("the parameters gave %#v, %v, want %#v", got, err, want)
			}
		})
	})
	t.Run("named parameters", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			refused(t, db, "SELECT id FROM "+types+" WHERE id = :id", "unexpected token")
			if _, err := db.ExecContext(t.Context(), "SELECT id FROM "+types+" WHERE id = ?", sql.Named("id", 1)); !errors.Is(err, dbimp.ErrArguments) {
				t.Errorf("a named argument gave %v, want dbimp.ErrArguments", err)
			}
		})
	})
	t.Run("query timeout", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, err := db.ExecContext(t.Context(), slowQuery(big), druid.WithTimeout(time.Second))
			if e, ok := errors.AsType[*druid.Error](err); !ok || e.HTTPStatus != http.StatusGatewayTimeout || e.Category != "TIMEOUT" {
				t.Errorf("a query over its timeout gave %v, want HTTP 504 and TIMEOUT", err)
			}
		})
	})
	t.Run("cancel by query id", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, _ *sql.DB) {
			rec := &recorder{}
			db := rec.open(t, p)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			start := time.Now()
			_, err := db.ExecContext(ctx, slowQuery(big), druid.WithTimeout(time.Minute))
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 10*time.Second {
				t.Errorf("the query gave %v after %v, want context.DeadlineExceeded at once", err, time.Since(start))
			}
			rec.mu.Lock()
			sent, cancels := rec.sent, rec.cancels
			rec.mu.Unlock()
			if len(sent) != 1 || fmt.Sprint(cancels) != "[202]" {
				t.Fatalf("the driver sent the queries %q and the cancels with the answers %v, want one query and HTTP 202", sent, cancels)
			}
			// The query no longer runs, so a second cancel finds nothing.
			status, b, err := apiAs(t, p).do(t.Context(), http.MethodDelete, "/druid/v2/sql/"+url.PathEscape(sent[0]), nil)
			if err != nil || status != http.StatusNotFound {
				t.Errorf("a second cancel of %s answered HTTP %d: %s, %v, want HTTP 404", sent[0], status, b, err)
			}
		})
	})
	t.Run("transactions", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported (D164)", err)
			}
			refused(t, db, "BEGIN", "Incorrect syntax")
			refused(t, db, "COMMIT", "Incorrect syntax")
		})
	})
	statements(t, s, big)
}

// statements holds the statements API, which the driver does not use: it
// runs a query as a task, keeps one page of the result without durable
// storage, and refuses durable storage (recorded on 36.0.0 in step 7). The
// administrator runs it, because the ordinary user runs no task.
func statements(t *testing.T, s *api, big string) {
	t.Helper()
	var id string
	t.Run("statements API", func(t *testing.T) {
		status, b, err := s.do(t.Context(), http.MethodPost, "/druid/v2/sql/statements", map[string]any{
			"query": "SELECT n FROM " + big + " WHERE n <= 10", "resultFormat": "array",
			"context": map[string]any{"executionMode": "ASYNC", "rowsPerPage": 4},
		})
		var answer struct {
			QueryID string `json:"queryId"`
			State   string `json:"state"`
		}
		if err != nil || status != http.StatusOK || json.Unmarshal(b, &answer) != nil {
			t.Fatalf("the statement answered HTTP %d: %s, %v", status, b, err)
		}
		id = answer.QueryID
		for deadline := time.Now().Add(taskTimeout); answer.State != "SUCCESS"; {
			if answer.State == "FAILED" || time.Now().After(deadline) {
				t.Fatalf("the statement %s is %s", id, answer.State)
			}
			time.Sleep(time.Second)
			if err := s.getJSON(t.Context(), "/druid/v2/sql/statements/"+url.PathEscape(id), &answer); err != nil {
				t.Fatal(err)
			}
		}
		status, b, err = s.do(t.Context(), http.MethodGet, "/druid/v2/sql/statements/"+url.PathEscape(id)+"/results?page=0", nil)
		if err != nil || status != http.StatusOK || strings.TrimSpace(string(b)) != "[[1],[2],[3],[4],[5],[6],[7],[8],[9],[10]]" {
			t.Errorf("the first page answered HTTP %d: %s, %v", status, b, err)
		}
	})
	t.Run("pages of the statements API", func(t *testing.T) {
		if id == "" {
			t.Skip("the statement did not run")
		}
		status, b, err := s.do(t.Context(), http.MethodGet, "/druid/v2/sql/statements/"+url.PathEscape(id)+"/results?page=1", nil)
		if err != nil || status < http.StatusBadRequest || !strings.Contains(string(b), "out of the range") {
			t.Errorf("the second page answered HTTP %d: %s, %v, want a refusal", status, b, err)
		}
	})
	t.Run("durable storage for results", func(t *testing.T) {
		status, b, err := s.do(t.Context(), http.MethodPost, "/druid/v2/sql/statements", map[string]any{
			"query": "SELECT n FROM " + big + " WHERE n <= 10", "resultFormat": "array",
			"context": map[string]any{"executionMode": "ASYNC", "selectDestination": "durableStorage"},
		})
		if err != nil || status < http.StatusBadRequest {
			t.Errorf("durable storage answered HTTP %d: %s, %v, want a refusal", status, b, err)
		}
	})
}
