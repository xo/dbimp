package solr_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/solr"
)

// The tests of this file hold the entries of testdata/solr/features.json. Each
// subtest has the name of its entry, with an underscore for each space, and
// runs as each principal where the principal can do what it tests.

// postForm sends a form to the path, as the HTTP API does for the handlers that
// the driver does not read, such as /sql with a statement that the driver
// binds.
func (a *api) postForm(ctx context.Context, path string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(a.user, a.pass)
	res, err := a.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return res.StatusCode, b, err
}

// tuples returns the tuples of the answer of /sql or /stream, and the
// exception of the tuple EOF.
func tuples(t *testing.T, body []byte) ([]map[string]any, string) {
	t.Helper()
	var res struct {
		ResultSet struct {
			Docs []map[string]any `json:"docs"`
		} `json:"result-set"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("reading the answer %q: %v", body, err)
	}
	exc := ""
	for _, d := range res.ResultSet.Docs {
		if s, ok := d["EXCEPTION"].(string); ok {
			exc = s
		}
	}
	return res.ResultSet.Docs, exc
}

// rawSQL sends the form to /sql on the main collection, as p, with no help of
// the driver.
func rawSQL(t *testing.T, p principal, form url.Values) (int, []byte) {
	t.Helper()
	status, body, err := apiAs(t, p).postForm(t.Context(), "/solr/"+collMain+"/sql", form)
	if err != nil {
		t.Fatal(err)
	}
	return status, body
}

// idsOf runs a statement and returns the first column of its rows.
func idsOf(t *testing.T, db *sql.DB, stmt string, args ...any) []string {
	t.Helper()
	_, rows := query(t, db, stmt, args...)
	return ids(rows)
}

// expectRefused runs a statement that Solr refuses, and checks that the error
// is an exception of the server with HTTP 200 that names the statement.
func expectRefused(t *testing.T, db *sql.DB, stmt, mention string) {
	t.Helper()
	_, _, err := tryQuery(t.Context(), db, stmt)
	serr := refusal(t, err)
	if serr.HTTPStatus != http.StatusOK || !strings.Contains(serr.Message, mention) {
		t.Errorf("%q gave %v, want an exception of HTTP 200 that holds %q", stmt, err, mention)
	}
}

// removeDocs removes the documents of the collections that match the query, in
// a cleanup, so that the fixture of the main collection stays.
func removeDocs(t *testing.T, a *api, q string, collections ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		for _, c := range collections {
			if err := a.update(ctx, c, map[string]any{"delete": map[string]any{"query": q}}); err != nil {
				t.Errorf("removing the documents of %s: %v", c, err)
			}
		}
	})
}

func TestIntegrationCRUD(t *testing.T) {
	a := apiAs(t, admin)
	type doc = map[string]any
	crudID := func(p principal) string { return "crud-" + p.name }

	t.Run("insert", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			expectRefused(t, db, "INSERT INTO "+collMain+" (id, n_s) VALUES ('"+crudID(p)+"', 'inserted')", "INSERT")
			if got := idsOf(t, db, "SELECT id FROM "+collMain+" WHERE id = '"+crudID(p)+"' LIMIT 1"); len(got) != 0 {
				t.Errorf("the refused INSERT left the document %v", got)
			}
		})
	})
	t.Run("select", func(t *testing.T) {
		removeDocs(t, a, "id:crud-*", collMain)
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			id := crudID(p)
			if err := a.update(t.Context(), collMain, []doc{{"id": id, "n_s": "inserted", "n_i": 1}}); err != nil {
				t.Fatal(err)
			}
			cols, rows := query(t, db, "SELECT id, n_s, n_i FROM "+collMain+" WHERE id = ? LIMIT 1", id)
			if want := [][]any{{id, "inserted", int64(1)}}; !reflect.DeepEqual(rows, want) || !slices.Equal(cols, []string{"id", "n_s", "n_i"}) {
				t.Errorf("read %v %v, want %v", cols, rows, want)
			}
		})
	})
	t.Run("update", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			expectRefused(t, db, "UPDATE "+collMain+" SET n_s = 'updated' WHERE id = '"+crudID(p)+"'", "UPDATE")
		})
	})
	t.Run("delete", func(t *testing.T) {
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			expectRefused(t, db, "DELETE FROM "+collMain+" WHERE id = '"+crudID(p)+"'", "DELETE")
		})
	})
	// The update handler is not SQL, and the driver does not send to it. The
	// tests write through the API, and read the result through the driver.
	t.Run("update handler add", func(t *testing.T) {
		removeDocs(t, a, "id:crud-*", collMain)
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			id := crudID(p) + "-add"
			if err := a.update(t.Context(), collMain, []doc{{"id": id, "n_s": "inserted", "n_i": 1}}); err != nil {
				t.Fatal(err)
			}
			_, rows := query(t, db, "SELECT id, n_s, n_i FROM "+collMain+" WHERE id = '"+id+"' LIMIT 1")
			if want := [][]any{{id, "inserted", int64(1)}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v", rows, want)
			}
		})
	})
	t.Run("update handler atomic update", func(t *testing.T) {
		removeDocs(t, a, "id:crud-*", collMain)
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			id := crudID(p) + "-atomic"
			if err := a.update(t.Context(), collMain, []doc{{"id": id, "n_s": "inserted", "n_i": 1}}); err != nil {
				t.Fatal(err)
			}
			if err := a.update(t.Context(), collMain, []doc{{"id": id, "n_s": doc{"set": "updated"}, "n_i": doc{"inc": 1}, "n_is": doc{"add": []int{7}}}}); err != nil {
				t.Fatal(err)
			}
			_, rows := query(t, db, "SELECT id, n_s, n_i, n_is FROM "+collMain+" WHERE id = '"+id+"' LIMIT 1")
			if want := [][]any{{id, "updated", int64(2), []any{int64(7)}}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v", rows, want)
			}
		})
	})
	t.Run("update handler replace", func(t *testing.T) {
		removeDocs(t, a, "id:crud-*", collMain)
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			id := crudID(p) + "-replace"
			if err := a.update(t.Context(), collMain, []doc{{"id": id, "n_s": "inserted", "n_i": 1}}); err != nil {
				t.Fatal(err)
			}
			if err := a.update(t.Context(), collMain, []doc{{"id": id, "n_s": "replaced"}}); err != nil {
				t.Fatal(err)
			}
			_, rows := query(t, db, "SELECT id, n_s, n_i FROM "+collMain+" WHERE id = '"+id+"' LIMIT 1")
			if want := [][]any{{id, "replaced", nil}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v: a document that replaces another keeps no field of it", rows, want)
			}
		})
	})
	t.Run("update handler delete by id", func(t *testing.T) {
		removeDocs(t, a, "id:crud-*", collMain)
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			id := crudID(p) + "-delete"
			if err := a.update(t.Context(), collMain, []doc{{"id": id, "n_s": "x"}}); err != nil {
				t.Fatal(err)
			}
			if got := idsOf(t, db, "SELECT id FROM "+collMain+" WHERE id = '"+id+"' LIMIT 1"); !reflect.DeepEqual(got, []string{id}) {
				t.Fatalf("the document is %v before the delete", got)
			}
			if err := a.update(t.Context(), collMain, doc{"delete": doc{"id": id}}); err != nil {
				t.Fatal(err)
			}
			if got := idsOf(t, db, "SELECT id FROM "+collMain+" WHERE id = '"+id+"' LIMIT 1"); len(got) != 0 {
				t.Errorf("the document %v is still there", got)
			}
		})
	})
	t.Run("update handler delete by query", func(t *testing.T) {
		removeDocs(t, a, "id:crud-*", collMain)
		forEach(t, func(t *testing.T, p principal, db *sql.DB) {
			tag := "by query " + p.name
			id1, id2 := crudID(p)+"-q1", crudID(p)+"-q2"
			if err := a.update(t.Context(), collMain, []doc{{"id": id1, "n_s": tag}, {"id": id2, "n_s": tag}}); err != nil {
				t.Fatal(err)
			}
			if got := idsOf(t, db, "SELECT id FROM "+collMain+" WHERE id = '"+id1+"' OR id = '"+id2+"' ORDER BY id LIMIT 5"); !reflect.DeepEqual(got, []string{id1, id2}) {
				t.Fatalf("the documents are %v before the delete", got)
			}
			if err := a.update(t.Context(), collMain, doc{"delete": doc{"query": fmt.Sprintf("n_s:%q", tag)}}); err != nil {
				t.Fatal(err)
			}
			if got := idsOf(t, db, "SELECT id FROM "+collMain+" WHERE id = '"+id1+"' OR id = '"+id2+"' LIMIT 5"); len(got) != 0 {
				t.Errorf("the documents %v are still there", got)
			}
		})
	})
	// The ordinary user has the role search, and the update handler refuses
	// it (recorded: "crud: insert").
	t.Run("update handler as the ordinary user", func(t *testing.T) {
		removeDocs(t, a, "id:crud-*", collMain)
		for _, p := range principals {
			t.Run(p.name, func(t *testing.T) {
				pa := apiAs(t, p)
				status, body, err := pa.do(t.Context(), http.MethodPost, "/solr/"+collMain+"/update?commit=true", []doc{{"id": crudID(p) + "-as", "n_s": "x"}})
				if err != nil {
					t.Fatal(err)
				}
				want := http.StatusOK
				if p == ordinary {
					want = http.StatusForbidden
				}
				if status != want {
					t.Errorf("the update handler answered HTTP %d, want %d: %s", status, want, body)
				}
			})
		}
	})
	// Three collections hold a small catalog, where a book refers to an
	// author and a review to a book. Solr checks no reference, so the test
	// holds the form that Solr has in place of a foreign key: a field that
	// names the id of another document, and a join on it.
	t.Run("three collections", func(t *testing.T) {
		removeDocs(t, a, "*:*", collAuthors, collBooks, collReviews)
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			ctx := t.Context()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(a.update(ctx, collAuthors, []doc{{"id": "a1", "name": "Ursula"}, {"id": "a2", "name": "Terry"}}))
			must(a.update(ctx, collBooks, []doc{
				{"id": "b1", "title": "The Dispossessed", "author_id": "a1", "pubyear": 1974},
				{"id": "b2", "title": "Guards! Guards!", "author_id": "a2", "pubyear": 1989},
				{"id": "b3", "title": "Mort", "author_id": "a2", "pubyear": 1987},
			}))
			must(a.update(ctx, collReviews, []doc{
				{"id": "r1", "book_id": "b1", "stars": 5, "body": "great book"},
				{"id": "r2", "book_id": "b2", "stars": 4, "body": "funny"},
				{"id": "r3", "book_id": "b3", "stars": 3, "body": "ok"},
			}))
			check := func(label, stmt string, want [][]any) {
				t.Helper()
				if _, got := query(t, db, stmt); !reflect.DeepEqual(got, want) {
					t.Errorf("%s: read %v, want %v", label, got, want)
				}
			}
			check("authors", "SELECT id, name FROM "+collAuthors+" ORDER BY id LIMIT 10", [][]any{{"a1", "Ursula"}, {"a2", "Terry"}})
			check("books", "SELECT id, title, author_id, pubyear FROM "+collBooks+" ORDER BY id LIMIT 10", [][]any{
				{"b1", "The Dispossessed", "a1", int64(1974)}, {"b2", "Guards! Guards!", "a2", int64(1989)}, {"b3", "Mort", "a2", int64(1987)}})
			check("reviews", "SELECT id, book_id, stars FROM "+collReviews+" ORDER BY id LIMIT 10", [][]any{
				{"r1", "b1", int64(5)}, {"r2", "b2", int64(4)}, {"r3", "b3", int64(3)}})
			// The field author_id is indexed, and a filter on it reads the
			// index.
			check("books of an author", "SELECT id FROM "+collBooks+" WHERE author_id = 'a2' ORDER BY id LIMIT 10", [][]any{{"b2"}, {"b3"}})
			check("count", "SELECT author_id, count(*) AS n FROM "+collBooks+" GROUP BY author_id ORDER BY author_id", [][]any{{"a1", int64(1)}, {"a2", int64(2)}})
			// Update: a field of one document, a number, and a whole document.
			must(a.update(ctx, collBooks, []doc{{"id": "b1", "pubyear": doc{"set": 1975}}}))
			must(a.update(ctx, collReviews, []doc{{"id": "r2", "stars": doc{"inc": 1}}}))
			must(a.update(ctx, collAuthors, []doc{{"id": "a1", "name": "Ursula K. Le Guin"}}))
			check("updated books", "SELECT id, pubyear FROM "+collBooks+" WHERE id = 'b1' LIMIT 1", [][]any{{"b1", int64(1975)}})
			check("updated reviews", "SELECT id, stars FROM "+collReviews+" WHERE id = 'r2' LIMIT 1", [][]any{{"r2", int64(5)}})
			check("updated authors", "SELECT id, name FROM "+collAuthors+" ORDER BY id LIMIT 10", [][]any{{"a1", "Ursula K. Le Guin"}, {"a2", "Terry"}})
			// Delete: by id, and by query.
			must(a.update(ctx, collReviews, doc{"delete": doc{"id": "r1"}}))
			must(a.update(ctx, collBooks, doc{"delete": doc{"query": "author_id:a2"}}))
			check("reviews after the delete", "SELECT id FROM "+collReviews+" ORDER BY id LIMIT 10", [][]any{{"r2"}, {"r3"}})
			check("books after the delete", "SELECT id FROM "+collBooks+" ORDER BY id LIMIT 10", [][]any{{"b1"}})
			must(a.update(ctx, collReviews, doc{"delete": doc{"query": "*:*"}}))
			must(a.update(ctx, collBooks, doc{"delete": doc{"query": "*:*"}}))
			must(a.update(ctx, collAuthors, doc{"delete": doc{"query": "*:*"}}))
			for _, c := range []string{collAuthors, collBooks, collReviews} {
				check("count of "+c, "SELECT count(*) AS n FROM "+c, [][]any{{int64(0)}})
			}
		})
	})
}

func TestIntegrationSchema(t *testing.T) {
	a := apiAs(t, admin)
	listPath := "/solr/admin/collections?action=LIST"
	// status returns the HTTP status of a GET as the principal.
	status := func(t *testing.T, p principal, path string) (int, []byte) {
		t.Helper()
		s, b, err := apiAs(t, p).do(t.Context(), http.MethodGet, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		return s, b
	}
	t.Run("collection", func(t *testing.T) {
		// A collection is a table, and only the administrator lists them.
		s, b := status(t, admin, listPath)
		if s != http.StatusOK || !strings.Contains(string(b), `"`+collMain+`"`) {
			t.Errorf("the list of collections is HTTP %d %s, want the collection %s", s, b, collMain)
		}
		if s, _ := status(t, ordinary, listPath); s != http.StatusForbidden {
			t.Errorf("the ordinary user got HTTP %d for the list of collections, want 403", s)
		}
	})
	t.Run("create table", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "CREATE TABLE t (id VARCHAR PRIMARY KEY, p VARCHAR REFERENCES u (id), UNIQUE (p))", "CREATE")
		})
	})
	t.Run("unique key", func(t *testing.T) {
		s, b := status(t, admin, "/solr/"+collMain+"/schema/uniquekey")
		if s != http.StatusOK || !strings.Contains(string(b), `"uniqueKey":"id"`) {
			t.Errorf("the unique key is HTTP %d %s, want id", s, b)
		}
		if s, _ := status(t, ordinary, "/solr/"+collMain+"/schema/uniquekey"); s != http.StatusForbidden {
			t.Errorf("the ordinary user got HTTP %d for the unique key, want 403", s)
		}
	})
	t.Run("foreign key", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "CREATE TABLE t (id VARCHAR PRIMARY KEY, p VARCHAR REFERENCES u (id))", "CREATE")
		})
	})
	t.Run("unique constraint", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "CREATE TABLE t (id VARCHAR, UNIQUE (id))", "CREATE")
		})
	})
	t.Run("index", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "CREATE INDEX i ON "+collMain+" (n_s)", "CREATE")
		})
	})
	t.Run("view", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "CREATE VIEW v AS SELECT id FROM "+collMain, "CREATE")
		})
	})
	// An alias of a collection stands for a view, and a statement on it reads
	// the collection (recorded: "a statement on an alias").
	t.Run("collection alias", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			var viaAlias, direct int64
			if err := db.QueryRowContext(t.Context(), "SELECT count(*) AS n FROM "+collAlias).Scan(&viaAlias); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(t.Context(), "SELECT count(*) AS n FROM "+collMain).Scan(&direct); err != nil {
				t.Fatal(err)
			}
			if viaAlias != direct || direct < 303 {
				t.Errorf("the alias counts %d and the collection %d, want the same count, at least 303", viaAlias, direct)
			}
			// The driver reads the types of the alias through the luke
			// handler of the alias.
			_, rows := query(t, db, "SELECT id, n_b FROM "+collAlias+" WHERE id = '1' LIMIT 1")
			if want := [][]any{{"1", true}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v through the alias, want %v", rows, want)
			}
		})
	})
	t.Run("default value", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT id, n_def FROM "+collMain+" WHERE id = '1' LIMIT 1")
			if want := [][]any{{"1", "the default"}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v", rows, want)
			}
		})
	})
	t.Run("field type", func(t *testing.T) {
		s, b := status(t, admin, "/solr/"+collMain+"/schema/fieldtypes/uuid")
		if s != http.StatusOK || !strings.Contains(string(b), "solr.UUIDField") {
			t.Errorf("the field type uuid is HTTP %d %s, want the class solr.UUIDField", s, b)
		}
	})
	t.Run("dynamic field", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT id, n_x_s FROM "+collMain+" WHERE id = '1' LIMIT 1")
			if want := [][]any{{"1", "a dynamic field"}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v", rows, want)
			}
		})
	})
	t.Run("copy field", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT id, n_cp FROM "+collMain+" WHERE id = '1' LIMIT 1")
			if want := [][]any{{"1", []any{"héllo wörld 日本 🙂"}}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v", rows, want)
			}
		})
	})
	t.Run("the schema as the ordinary user", func(t *testing.T) {
		if s, _ := status(t, admin, "/solr/"+collMain+"/schema/fields"); s != http.StatusOK {
			t.Errorf("the administrator got HTTP %d for the fields, want 200", s)
		}
		if s, _ := status(t, ordinary, "/solr/"+collMain+"/schema/fields"); s != http.StatusForbidden {
			t.Errorf("the ordinary user got HTTP %d for the fields, want 403", s)
		}
		// The luke handler names the types of the fields, and the ordinary
		// user can read it. The driver reads its types from it (D166).
		if s, _ := status(t, ordinary, "/solr/"+collMain+"/admin/luke?show=schema&numTerms=0"); s != http.StatusOK {
			t.Errorf("the ordinary user got HTTP %d for the luke handler, want 200", s)
		}
	})
	t.Run("metadata tables", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT tableSchem, tableName, tableType FROM metadata.TABLES")
			names := map[string]string{}
			for _, r := range rows {
				names[r[1].(string)], _ = r[2].(string)
			}
			if names[collMain] != "TABLE" || names[collAlias] != "TABLE" {
				t.Errorf("the tables are %v, want %s and %s as TABLE", names, collMain, collAlias)
			}
		})
	})
	t.Run("metadata columns", func(t *testing.T) {
		forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT columnName, dataType FROM metadata.COLUMNS WHERE tableName = ?", collMain)
			types := map[string]int64{}
			for _, r := range rows {
				name, _ := r[0].(string)
				types[name], _ = r[1].(int64)
			}
			want := map[string]int64{"id": 12, "n_i": -5, "n_l": -5, "n_f": 8, "n_d": 8, "n_b": 12, "n_dt": 93, "n_is": 2000, "n_bin": 12, "n_u": 12, "n_x_s": 12, "score": 8}
			for k, v := range want {
				if types[k] != v {
					t.Errorf("the column %s has the code %d, want %d", k, types[k], v)
				}
			}
		})
	})
	_ = a
}

func TestIntegrationFeatures(t *testing.T) {
	a := apiAs(t, admin)
	main := collMain
	// each runs f as each principal, on the main collection.
	each := forEach
	t.Run("full text search", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if got := idsOf(t, db, "SELECT id, n_t FROM "+main+" WHERE n_t = 'quick' LIMIT 5"); !reflect.DeepEqual(got, []string{"1"}) {
				t.Errorf("found %v, want 1", got)
			}
		})
	})
	t.Run("lucene query", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if got := idsOf(t, db, "SELECT id FROM "+main+" WHERE _query_ = 'n_t:fox' LIMIT 5"); !reflect.DeepEqual(got, []string{"1"}) {
				t.Errorf("found %v, want 1", got)
			}
		})
	})
	t.Run("lucene query with spaces", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "SELECT id FROM "+main+" WHERE _query_ = 'n_t:fox AND n_i:[0 TO *]' LIMIT 5", "Cannot parse")
		})
	})
	t.Run("score", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT id, score FROM "+main+" WHERE n_t = 'quick' ORDER BY score DESC LIMIT 5")
			if len(rows) != 1 || rows[0][0] != "1" {
				t.Fatalf("read %v, want the document 1", rows)
			}
			if s, ok := rows[0][1].(float64); !ok || s <= 0 {
				t.Errorf("the score is %#v, want a float64 above 0", rows[0][1])
			}
		})
	})
	t.Run("score with no limit", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "SELECT id, score FROM "+main, "score")
		})
	})
	t.Run("wildcard in equality", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if got := idsOf(t, db, "SELECT id FROM "+main+" WHERE n_s = 'h*' LIMIT 5"); !reflect.DeepEqual(got, []string{"1"}) {
				t.Errorf("found %v, want 1", got)
			}
		})
	})
	group := "SELECT n_s, count(*) AS c FROM " + main + " WHERE id = '1' OR id = '2' OR id = '3' GROUP BY n_s"
	t.Run("group by facet", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if _, rows := query(t, db, group); !reflect.DeepEqual(rows, [][]any{{"héllo wörld 日本 🙂", int64(1)}}) {
				t.Errorf("read %v, want the group of the one document that has a value", rows)
			}
		})
	})
	// The mode map_reduce is refused by the DSN, and WithParameter turns it
	// on for a statement. It writes the NULL group as the text "NULL".
	t.Run("group by map reduce", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, group, solr.WithParameter("aggregationMode", "map_reduce"))
			if want := [][]any{{"NULL", int64(2)}, {"héllo wörld 日本 🙂", int64(1)}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v", rows, want)
			}
		})
	})
	t.Run("map reduce group of a null integer", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			rows, err := db.QueryContext(t.Context(), "SELECT n_i, count(*) AS c FROM "+main+" WHERE id = '2' OR id = '3' GROUP BY n_i", solr.WithParameter("aggregationMode", "map_reduce"))
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			n := 0
			for rows.Next() {
				n++
			}
			err = rows.Err()
			if n != 1 || !errors.Is(err, dbimp.ErrIncomplete) || !strings.Contains(refusal(t, err).Message, "cannot be cast") {
				t.Errorf("read %d rows and %v, want one group and a cast error that wraps ErrIncomplete", n, err)
			}
		})
	})
	// The cut that D166 gives as the reason to refuse the mode: a GROUP BY
	// with an order by an aggregate and no LIMIT gives 100 groups, and says
	// nothing (recorded: "lead: a group ordered by its count, map reduce").
	t.Run("map reduce group ordered by an aggregate", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			stmt := "SELECT n_i, count(*) AS c FROM " + main + " WHERE n_i >= 0 GROUP BY n_i ORDER BY count(*) DESC"
			_, cut := query(t, db, stmt, solr.WithParameter("aggregationMode", "map_reduce"))
			_, all := query(t, db, stmt)
			if len(cut) != 100 || len(all) != 301 {
				t.Errorf("map_reduce gave %d groups and facet gave %d, want 100 and 301", len(cut), len(all))
			}
		})
	})
	t.Run("two workers", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, _, err := tryQuery(t.Context(), db, "SELECT n_s, count(*) AS c FROM "+main+" WHERE id = '1' GROUP BY n_s",
				solr.WithParameter("aggregationMode", "map_reduce"), solr.WithParameter("numWorkers", 2))
			if err == nil || !strings.Contains(refusal(t, err).Message, "IndexOutOfBounds") {
				t.Errorf("two workers gave %v, want an IndexOutOfBoundsException of a server of one node", err)
			}
		})
	})
	t.Run("having and order by an aggregate", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT n_i, count(*) AS c FROM "+main+" GROUP BY n_i HAVING count(*) > 0 ORDER BY n_i DESC LIMIT 3")
			if want := [][]any{{int64(2147483647), int64(1)}, {int64(299), int64(1)}, {int64(298), int64(1)}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v", rows, want)
			}
		})
	})
	t.Run("distinct", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if _, rows := query(t, db, "SELECT DISTINCT n_i FROM "+main); len(rows) != 302 {
				t.Errorf("read %d distinct values, want 302", len(rows))
			}
		})
	})
	t.Run("union", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if got := idsOf(t, db, "SELECT id FROM "+main+" WHERE id = '1' UNION SELECT id FROM "+main+" WHERE id = '2'"); !reflect.DeepEqual(got, []string{"1", "2"}) {
				t.Errorf("found %v, want 1 and 2", got)
			}
		})
	})
	t.Run("join", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT a.id, b.n_i FROM "+main+" a JOIN "+main+" b ON a.id = b.id WHERE a.id = '1' LIMIT 5")
			if want := [][]any{{"1", int64(2147483647)}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v", rows, want)
			}
		})
	})
	t.Run("array contains", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "SELECT id FROM "+main+" WHERE ARRAY_CONTAINS(n_ss, 'a') LIMIT 5", "ARRAY_CONTAINS")
		})
	})
	t.Run("multi-valued field in a filter", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT id, n_ss FROM "+main+" WHERE n_ss = 'a' LIMIT 5")
			if want := [][]any{{"1", []any{"a", "b"}}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("read %v, want %v", rows, want)
			}
		})
	})
	// The driver always asks for the metadata, so a result names its columns,
	// with their aliases, also when it has no rows (D166). With
	// includeMetadata false, it names the columns of the first row.
	t.Run("include metadata", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			cols, rows := query(t, db, "SELECT n_l, id AS x, n_i FROM "+main+" ORDER BY id LIMIT 3")
			if !slices.Equal(cols, []string{"n_l", "x", "n_i"}) || len(rows) != 3 {
				t.Errorf("read the columns %v and %d rows, want n_l, x and n_i, and 3 rows", cols, len(rows))
			}
			cols, rows = query(t, db, "SELECT n_l, id AS x FROM "+main+" WHERE id = 'none' LIMIT 3")
			if !slices.Equal(cols, []string{"n_l", "x"}) || len(rows) != 0 {
				t.Errorf("read the columns %v and %d rows of a result with none, want n_l and x, and no rows", cols, len(rows))
			}
			cols, _ = query(t, db, "SELECT id AS x, n_i FROM "+main+" ORDER BY id LIMIT 1", solr.WithParameter("includeMetadata", "false"))
			if !slices.Equal(cols, []string{"x", "n_i"}) {
				t.Errorf("without the metadata, the columns are %v, want the keys of the first row", cols)
			}
			cols, rows = query(t, db, "SELECT id AS x FROM "+main+" WHERE id = 'none' LIMIT 1", solr.WithParameter("includeMetadata", "false"))
			if len(cols) != 0 || len(rows) != 0 {
				t.Errorf("without the metadata, a result with no rows names the columns %v, want none", cols)
			}
		})
	})
	t.Run("two columns with one name", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, _, err := tryQuery(t.Context(), db, "SELECT id AS a, id AS b, n_i AS b FROM "+main+" ORDER BY id LIMIT 2")
			if !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("the error is %v, want dbimp.ErrNotSupported", err)
			}
			// The server gives the value of the last column to each column
			// of that name (recorded: "two columns with one name").
			_, rows := query(t, db, "SELECT id AS a, n_i AS b FROM "+main+" ORDER BY id LIMIT 1")
			if want := [][]any{{"1", int64(2147483647)}}; !reflect.DeepEqual(rows, want) {
				t.Errorf("columns with other names read %v, want %v", rows, want)
			}
		})
	})
	t.Run("export with no limit", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			_, rows := query(t, db, "SELECT id, n_i FROM "+main)
			if len(rows) < 303 {
				t.Errorf("read %d rows, want every document", len(rows))
			}
		})
	})
	t.Run("export of a field with no docValues", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "SELECT id, n_t FROM "+main, "DocValues")
		})
	})
	t.Run("limit and offset", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if got := idsOf(t, db, "SELECT id FROM "+main+" ORDER BY id LIMIT 5 OFFSET 5"); !reflect.DeepEqual(got, []string{"p002", "p003", "p004", "p005", "p006"}) {
				t.Errorf("found %v", got)
			}
		})
	})
	t.Run("offset with no limit", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "SELECT id FROM "+main+" OFFSET 5", "OFFSET without LIMIT")
		})
	})
	t.Run("fetch next", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if got := idsOf(t, db, "SELECT id FROM "+main+" ORDER BY id OFFSET 5 ROWS FETCH NEXT 5 ROWS ONLY"); !reflect.DeepEqual(got, []string{"p002", "p003", "p004", "p005", "p006"}) {
				t.Errorf("found %v", got)
			}
		})
	})
	t.Run("expression in the projection", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "SELECT n_i * 2 AS twice, CAST(n_i AS DECIMAL(10,2)) AS d FROM "+main+" WHERE id = '1' LIMIT 1", "")
		})
	})
	t.Run("explain", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			cols, rows := query(t, db, "EXPLAIN PLAN FOR SELECT id, n_i FROM "+main+" ORDER BY id LIMIT 1")
			if !slices.Equal(cols, []string{"PLAN"}) || len(rows) != 1 || !strings.Contains(fmt.Sprint(rows[0][0]), "SolrTableScan") {
				t.Errorf("read %v %v, want the plan of a scan", cols, rows)
			}
		})
	})
	// A streaming expression is another handler, which the driver does not
	// read, and both principals can run it.
	t.Run("streaming expression", func(t *testing.T) {
		for _, p := range principals {
			t.Run(p.name, func(t *testing.T) {
				pa := apiAs(t, p)
				status, body, err := pa.postForm(t.Context(), "/solr/"+main+"/stream", url.Values{"expr": {`search(` + main + `,q="id:1",fl="id,n_i",sort="id asc")`}})
				if err != nil || status != http.StatusOK {
					t.Fatalf("the streaming expression gave HTTP %d and %v: %s", status, err, body)
				}
				docs, exc := tuples(t, body)
				if exc != "" || len(docs) != 2 || docs[0]["id"] != "1" || docs[1]["EOF"] != true {
					t.Errorf("the tuples are %v with the exception %q, want the document 1 and the tuple EOF", docs, exc)
				}
			})
		}
	})
	// The server binds no argument: it refuses ? with HTTP 500 and :id with an
	// exception. The driver writes each argument as a literal (D166).
	t.Run("placeholder", func(t *testing.T) {
		for _, p := range principals {
			t.Run(p.name, func(t *testing.T) {
				status, _ := rawSQL(t, p, url.Values{"stmt": {"SELECT id FROM " + main + " WHERE id = ? LIMIT 1"}})
				if status != http.StatusInternalServerError {
					t.Errorf("the server answered ? with HTTP %d, want 500", status)
				}
				db := openAs(t, p, main)
				if got := idsOf(t, db, "SELECT id FROM "+main+" WHERE id = ? LIMIT 1", "1"); !reflect.DeepEqual(got, []string{"1"}) {
					t.Errorf("the driver found %v with the argument, want 1", got)
				}
			})
		}
	})
	t.Run("named parameter", func(t *testing.T) {
		for _, p := range principals {
			t.Run(p.name, func(t *testing.T) {
				status, body := rawSQL(t, p, url.Values{"stmt": {"SELECT id FROM " + main + " WHERE id = :id LIMIT 1"}})
				if _, exc := tuples(t, body); status != http.StatusOK || exc == "" {
					t.Errorf("the server answered :id with HTTP %d %s, want an exception", status, body)
				}
				db := openAs(t, p, main)
				if got := idsOf(t, db, "SELECT id FROM "+main+" WHERE id = @id LIMIT 1", sql.Named("id", "1")); !reflect.DeepEqual(got, []string{"1"}) {
					t.Errorf("the driver found %v with @id, want 1", got)
				}
			})
		}
	})
	t.Run("two statements", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			expectRefused(t, db, "SELECT id FROM "+main+" WHERE id = '1' LIMIT 1; SELECT id FROM "+main+" WHERE id = '2' LIMIT 1", "parse failed")
			expectRefused(t, db, "SELECT id FROM "+main+" WHERE id = '1' LIMIT 1;", "parse failed")
		})
	})
	t.Run("comments", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if got := idsOf(t, db, "-- a comment\nSELECT id /* another */ FROM "+main+" WHERE id = '1' LIMIT 1"); !reflect.DeepEqual(got, []string{"1"}) {
				t.Errorf("found %v, want 1", got)
			}
		})
	})
	t.Run("quoted names", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			cols, rows := query(t, db, "SELECT `id`, `n_i` FROM `"+main+"` WHERE `id` = '1' LIMIT 1")
			if !slices.Equal(cols, []string{"id", "n_i"}) || !reflect.DeepEqual(rows, [][]any{{"1", int64(2147483647)}}) {
				t.Errorf("read %v %v", cols, rows)
			}
			// A text in double quotes is a name.
			expectRefused(t, db, `SELECT id FROM `+main+` WHERE id = "1" LIMIT 1`, "")
			// The server matches names without regard to case, and writes
			// each key as the statement wrote it.
			if cols, _ := query(t, db, "SELECT ID, N_I FROM "+main+" WHERE ID = '1' LIMIT 1"); !slices.Equal(cols, []string{"ID", "N_I"}) {
				t.Errorf("the columns are %v, want ID and N_I", cols)
			}
		})
	})
	// The server ignores timeAllowed (recorded: "lead: a time limit"), so the
	// driver refuses WithTimeout, and a statement with the parameter still
	// reads every row.
	t.Run("time allowed", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if _, rows := query(t, db, "SELECT id, n_i FROM "+main+" ORDER BY id LIMIT 300", solr.WithParameter("timeAllowed", 1)); len(rows) != 300 {
				t.Errorf("read %d rows with timeAllowed=1, want 300", len(rows))
			}
		})
	})
	// The server has no way to cancel a statement of /sql: the task is not
	// registered under the id that the request names (recorded: "lead: cancel a
	// query by its id").
	t.Run("cancel by query id", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if _, rows := query(t, db, "SELECT id FROM "+main+" ORDER BY id LIMIT 3", solr.WithParameter("canCancel", true), solr.WithParameter("queryUUID", "dbimp-cancel")); len(rows) != 3 {
				t.Fatalf("read %d rows, want 3", len(rows))
			}
		})
		for _, p := range principals {
			t.Run(p.name+" cancels", func(t *testing.T) {
				status, body, err := apiAs(t, p).do(t.Context(), http.MethodGet, "/solr/"+main+"/tasks/cancel?queryUUID=dbimp-cancel", nil)
				if err != nil || status != http.StatusOK || !strings.Contains(string(body), "not found") {
					t.Errorf("the cancel gave HTTP %d %s and %v, want not found", status, body, err)
				}
			})
		}
	})
	t.Run("transaction", func(t *testing.T) {
		each(t, func(t *testing.T, _ principal, db *sql.DB) {
			if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
			}
			expectRefused(t, db, "BEGIN", "BEGIN")
			expectRefused(t, db, "COMMIT", "COMMIT")
		})
	})
	t.Run("rollback of the update handler", func(t *testing.T) {
		status, body, err := a.do(t.Context(), http.MethodPost, "/solr/"+main+"/update?rollback=true", map[string]any{})
		if err != nil || status != http.StatusInternalServerError || !strings.Contains(string(body), "Rollback is currently not supported") {
			t.Errorf("the rollback gave HTTP %d %s and %v, want HTTP 500 and the refusal of SolrCloud", status, body, err)
		}
	})
	// A double that holds Infinity fails the query before any row, so the
	// error is the error of the query (recorded: "lead: infinities").
	t.Run("infinity in a double", func(t *testing.T) {
		each(t, func(t *testing.T, p principal, _ *sql.DB) {
			db := openAs(t, p, collInf)
			_, _, err := tryQuery(t.Context(), db, "SELECT id, n_d, n_f, n_ds FROM "+collInf+" WHERE id = 'i1' LIMIT 1")
			if serr := refusal(t, err); !strings.Contains(serr.Message, "cannot be cast") || errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("the error is %v, want a cast error before any row", err)
			}
		})
	})
}
