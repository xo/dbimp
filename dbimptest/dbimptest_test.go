package dbimptest_test

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

func TestContractOnTheFakeDriver(t *testing.T) {
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := openFake(url)
			if err != nil {
				t.Fatal(err)
			}
			return db
		},
		Query: "SELECT 1",
		Columns: dbimptest.ColumnsCase{
			Body: `{"columns":["b","a","c"],"rows":[[1,2,3]]}`,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   `{"columns":["a","b"],"rows":[[1,null]]}`,
			Column: 1,
		},
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: `{"columns":["a"],"rows":[[1],[2]],"error":"boom"}`,
			Rows: 2,
		},
		Stream: dbimptest.StreamCase{
			Head: `{"columns":["a"],"rows":[`,
			Row:  `[1]`,
			Sep:  `,`,
			Tail: `]}`,
		},
	})
}

func TestRecordThenReplay(t *testing.T) {
	dir := t.TempDir()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Set-Cookie", "session=secret")
		_, _ = io.WriteString(w, `{"echo":`+string(body)+`}`)
	}))
	defer backend.Close()

	rec := dbimptest.NewRecorder(dir, "fake-1.0", http.DefaultTransport)
	rec.Label(1, dbimptest.Administrator)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, backend.URL+"/query", strings.NewReader(`{"statement":"SELECT 1"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("admin", "password")
	res, err := rec.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if err := rec.WriteManifest("fake"); err != nil {
		t.Fatal(err)
	}

	m, err := dbimptest.ReadManifest(filepath.Join(dir, dbimptest.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 1 || m.Entries[0].Item != 1 || m.Entries[0].Principal != dbimptest.Administrator {
		t.Fatalf("the manifest is %+v, want one entry for item 1 as the administrator", m)
	}
	b, err := os.ReadFile(filepath.Join(dir, m.Entries[0].File))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"password", "YWRtaW46cGFzc3dvcmQ=", "session=secret"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("the recording holds the credential %q", secret)
		}
	}

	srv := dbimptest.Replay(t, dir, nil)
	req, err = http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/query", strings.NewReader(`{ "statement" : "SELECT 1" }`))
	if err != nil {
		t.Fatal(err)
	}
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"echo":{"statement":"SELECT 1"}}`; string(body) != want {
		t.Errorf("the replayed body is %s, want %s", body, want)
	}
}

func TestRecordThenReplayATruncatedBody(t *testing.T) {
	dir := t.TempDir()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"a":1},`)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flushing: %v", err)
		}
		panic(http.ErrAbortHandler)
	}))
	defer backend.Close()

	rec := dbimptest.NewRecorder(dir, "fake-1.0", http.DefaultTransport)
	rec.Label(6, dbimptest.Administrator)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, backend.URL+"/rows", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rec.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(res.Body)
	res.Body.Close()
	if string(got) != `[{"a":1},` || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("the recorded response reads %q and %v, want the part that arrived and io.ErrUnexpectedEOF", got, err)
	}
	if err := rec.WriteManifest("fake"); err != nil {
		t.Fatal(err)
	}
	m, err := dbimptest.ReadManifest(filepath.Join(dir, dbimptest.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	ex, err := dbimptest.ReadExchange(filepath.Join(dir, m.Entries[0].File))
	if err != nil {
		t.Fatal(err)
	}
	if !ex.Response.Truncated {
		t.Error("the exchange does not say that the body was truncated")
	}

	srv := dbimptest.Replay(t, dir, nil)
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/rows", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, err = io.ReadAll(res.Body)
	if string(got) != `[{"a":1},` || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("the replayed response reads %q and %v, want the part that arrived and io.ErrUnexpectedEOF", got, err)
	}
}

func TestTablesWriteThenCompare(t *testing.T) {
	doc := filepath.Join(t.TempDir(), "FAKE.md")
	text := "# Fake\n\n" +
		dbimptest.TypesBegin + "\n" + dbimptest.TypesEnd + "\n\n" +
		dbimptest.InterfacesBegin + "\n" + dbimptest.InterfacesEnd + "\n"
	if err := os.WriteFile(doc, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := []dbimptest.TypeRow{
		{Wire: "number", Kind: "integer", Go: "int64", ScanType: "int64", DatabaseType: "NUMBER", Nullable: true},
	}
	c, err := fake{}.OpenConnector("fake://localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, name := range []string{
		"driver.DriverContext", "driver.Connector", "io.Closer on the connector",
		"driver.Pinger", "driver.SessionResetter", "driver.Validator",
		"driver.NamedValueChecker", "driver.QueryerContext", "driver.ExecerContext",
		"driver.ConnPrepareContext", "driver.ConnBeginTx", "driver.RowsColumnScanner",
		"driver.RowsNextResultSet", "driver.RowsColumnTypeScanType",
		"driver.RowsColumnTypeDatabaseTypeName", "driver.RowsColumnTypeLength",
		"driver.RowsColumnTypeNullable", "driver.RowsColumnTypePrecisionScale",
	} {
		reasons[name] = "a reason"
	}
	t.Setenv(dbimptest.EnvUpdate, "1")
	dbimptest.TypeTable(t, doc, rows)
	dbimptest.InterfaceTable(t, doc, reasons, c, fake{}, &fakeConn{}, &fakeRows{})
	t.Setenv(dbimptest.EnvUpdate, "")
	dbimptest.TypeTable(t, doc, rows)
	dbimptest.InterfaceTable(t, doc, reasons, c, fake{}, &fakeConn{}, &fakeRows{})

	b, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| number | integer | `int64` | `int64` | `NUMBER` | yes |",
		"| `driver.RowsColumnScanner` | yes | a reason |",
		"| `driver.Pinger` | no | a reason |",
		"| `io.Closer on the connector` | yes | a reason |",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the document does not hold %q:\n%s", want, b)
		}
	}
}

// TestKinds holds that Kinds sets the kind of each row by its wire type.
func TestKinds(t *testing.T) {
	t.Parallel()
	rows := dbimptest.Kinds(t, []dbimptest.TypeRow{{Wire: "number"}, {Wire: "text"}}, map[string]string{"number": "integer", "text": "string"})
	if rows[0].Kind != "integer" || rows[1].Kind != "string" {
		t.Errorf("the kinds are %q and %q, want integer and string", rows[0].Kind, rows[1].Kind)
	}
}
