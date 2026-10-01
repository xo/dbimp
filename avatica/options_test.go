package avatica_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/avatica"
)

// fakeServer is a fake server of Avatica. It keeps each request, and answers
// a statement that starts with SELECT with a column A of INTEGER, whose first
// frame holds the row 1 and whose next frame holds the row 2, and any other
// statement with the count 2.
type fakeServer struct {
	mu   sync.Mutex
	reqs []fakeRequest
	// missing answers each fetch as a statement that the server does not
	// know.
	missing bool
	nextID  int
}

// fakeRequest is one request that the fake server received.
type fakeRequest struct {
	auth string
	raw  string
	body map[string]any
}

// signature is the signature of a result with the column A of INTEGER.
const signature = `{"columns":[{"ordinal":0,"nullable":1,"label":"A","columnName":"A","precision":32,"scale":0,"type":{"type":"scalar","id":4,"name":"INTEGER","rep":"PRIMITIVE_INT"}}],"sql":null,"parameters":[],"cursorFactory":{"style":"LIST","clazz":null,"fieldNames":null},"statementType":null}`

func (s *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	s.mu.Lock()
	s.reqs = append(s.reqs, fakeRequest{auth: r.Header.Get("Authorization"), raw: string(b), body: body})
	missing := s.missing
	s.nextID++
	id := s.nextID
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	result := func(sql string) string {
		if strings.HasPrefix(sql, "SELECT") {
			return `{"response":"executeResults","missingStatement":false,"results":[{"response":"resultSet","connectionId":"c","statementId":1,"ownStatement":true,"signature":` + signature + `,"firstFrame":{"offset":0,"done":false,"rows":[[1]]},"updateCount":-1}]}`
		}
		return `{"response":"executeResults","missingStatement":false,"results":[{"response":"resultSet","connectionId":"c","statementId":1,"ownStatement":true,"signature":null,"firstFrame":null,"updateCount":2}]}`
	}
	switch body["request"] {
	case "connectionSync":
		props, _ := body["connProps"].(map[string]any)
		out := map[string]any{"connProps": "connPropsImpl", "autoCommit": true, "readOnly": false, "transactionIsolation": 2}
		for k, v := range props {
			if k != "dirty" {
				out[k] = v
			}
		}
		b, _ := json.Marshal(map[string]any{"response": "connectionSync", "connProps": out})
		_, _ = w.Write(b)
	case "createStatement":
		_, _ = fmt.Fprintf(w, `{"response":"createStatement","connectionId":"c","statementId":%d}`, id)
	case "prepareAndExecute":
		sql, _ := body["sql"].(string)
		_, _ = io.WriteString(w, result(sql))
	case "prepare":
		sql, _ := body["sql"].(string)
		params := make([]string, strings.Count(sql, "?"))
		for i := range params {
			params[i] = `{"signed":false,"precision":0,"scale":0,"parameterType":4,"typeName":"INTEGER","className":"java.lang.Integer","name":"?` + strconv.Itoa(i+1) + `"}`
		}
		sqlJSON, _ := json.Marshal(sql)
		_, _ = fmt.Fprintf(w, `{"response":"prepare","statement":{"connectionId":"c","id":%d,"signature":{"columns":[],"sql":%s,"parameters":[%s],"cursorFactory":{"style":"LIST","clazz":null,"fieldNames":null},"statementType":null}}}`, id, sqlJSON, strings.Join(params, ","))
	case "execute":
		h, _ := body["statementHandle"].(map[string]any)
		sig, _ := h["signature"].(map[string]any)
		sql, _ := sig["sql"].(string)
		_, _ = io.WriteString(w, result(sql))
	case "fetch":
		if missing {
			_, _ = io.WriteString(w, `{"response":"fetch","frame":null,"missingStatement":true,"missingResults":true}`)
			return
		}
		_, _ = io.WriteString(w, `{"response":"fetch","frame":{"offset":1,"done":true,"rows":[[2]]}}`)
	default:
		_, _ = fmt.Fprintf(w, `{"response":%q}`, body["request"])
	}
}

// requests returns the requests that the server received whose name is
// name, or every request for "".
func (s *fakeServer) requests(name string) []fakeRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []fakeRequest
	for _, r := range s.reqs {
		if name == "" || r.body["request"] == name {
			out = append(out, r)
		}
	}
	return out
}

// open returns a database on a new fake server, with the user information
// and the query of a DSN.
func open(t *testing.T, userinfo, query string) (*sql.DB, *fakeServer) {
	t.Helper()
	s := &fakeServer{}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	dsn := strings.Replace(srv.URL, "http://", "avatica://"+userinfo, 1)
	if query != "" {
		dsn += "?" + query
	}
	db, err := sql.Open(avatica.Name, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, s
}

// TestConnect holds D156 and D157: a connection sends the credentials in the
// info of openConnection, turns autoCommit on, and closeConnection closes it.
// auth=basic also sends them by HTTP basic authentication.
func TestConnect(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query string
		basic bool
	}{{"", false}, {"auth=basic", true}} {
		db, s := open(t, "u:p%C3%A9@", tt.query)
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
		open := s.requests("openConnection")
		if len(open) != 1 {
			t.Fatalf("the server received %d openConnection, want 1", len(open))
		}
		info, _ := open[0].body["info"].(map[string]any)
		if info["user"] != "u" || info["password"] != "pé" || !strings.Contains(open[0].raw, "\\u00e9") {
			t.Errorf("openConnection sent %s, want the user and the password, with é escaped (D158)", open[0].raw)
		}
		if got := open[0].auth != ""; got != tt.basic {
			t.Errorf("with %q, openConnection sent the header Authorization %q", tt.query, open[0].auth)
		}
		syncs := s.requests("connectionSync")
		if props, _ := syncs[0].body["connProps"].(map[string]any); props["autoCommit"] != true {
			t.Errorf("the first connectionSync sent %v, want autoCommit true (D157)", props)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if n := len(s.requests("closeConnection")); n != 1 {
			t.Errorf("the server received %d closeConnection, want 1", n)
		}
	}
}

// TestQueryFrames holds D157: the driver asks for frames of the frame size,
// fetches the next frame from the offset after the last row, and closes the
// statement after the last row.
func TestQueryFrames(t *testing.T) {
	t.Parallel()
	db, s := open(t, "", "")
	ctx := avatica.WithOptions(t.Context(), avatica.WithFrameSize(7))
	rows, err := db.QueryContext(ctx, "SELECT A FROM T")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil || len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("read %v and %v, want 1 and 2", got, err)
	}
	pe := s.requests("prepareAndExecute")
	fetch := s.requests("fetch")
	if len(pe) != 1 || pe[0].body["maxRowsInFirstFrame"] != 7.0 || pe[0].body["maxRowCount"] != -1.0 {
		t.Errorf("prepareAndExecute sent %v, want maxRowsInFirstFrame 7 and maxRowCount -1", pe)
	}
	if len(fetch) != 1 || fetch[0].body["offset"] != 1.0 || fetch[0].body["fetchMaxRowCount"] != 7.0 {
		t.Errorf("fetch sent %v, want the offset 1 and fetchMaxRowCount 7", fetch)
	}
	if n := len(s.requests("closeStatement")); n != 1 {
		t.Errorf("the server received %d closeStatement, want 1", n)
	}
}

// TestMissingStatement holds that an answer with missingStatement true,
// which the server sends with no error, is an error, and wraps
// dbimp.ErrIncomplete after a row.
func TestMissingStatement(t *testing.T) {
	t.Parallel()
	db, s := open(t, "", "")
	s.missing = true
	rows, err := db.QueryContext(t.Context(), "SELECT A FROM T")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); n != 1 || !errors.Is(err, dbimp.ErrIncomplete) || !strings.Contains(fmt.Sprint(err), "does not know the statement") {
		t.Errorf("read %d rows and %v, want 1 row and the missing statement", n, err)
	}
}

// TestExec holds that Exec gives updateCount, and that Exec of a query
// closes its rows.
func TestExec(t *testing.T) {
	t.Parallel()
	db, s := open(t, "", "")
	for query, want := range map[string]int64{"UPDATE T SET A = 1": 2, "SELECT A FROM T": 0} {
		res, err := db.ExecContext(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := res.RowsAffected(); n != want || err != nil {
			t.Errorf("%s gave %d and %v, want %d", query, n, err, want)
		}
	}
	if n := len(s.requests("closeStatement")); n != 2 {
		t.Errorf("the server received %d closeStatement, want 2", n)
	}
}

// TestArguments holds D158: a statement with arguments goes as prepare and
// execute with the whole handle, and a named argument is an error.
func TestArguments(t *testing.T) {
	t.Parallel()
	db, s := open(t, "", "")
	if _, err := db.ExecContext(t.Context(), "UPDATE T SET A = ? WHERE B = ?", 1, "é"); err != nil {
		t.Fatal(err)
	}
	ex := s.requests("execute")
	if len(ex) != 1 {
		t.Fatalf("the server received %d execute, want 1", len(ex))
	}
	h, _ := ex[0].body["statementHandle"].(map[string]any)
	if _, ok := h["signature"].(map[string]any); !ok || !strings.Contains(ex[0].raw, `{"type":"LONG","value":1}`) || !strings.Contains(ex[0].raw, `{"type":"STRING","value":"`+"\\u00e9"+`"}`) {
		t.Errorf("execute sent %s, want the handle with its signature, and a TypedValue for each argument", ex[0].raw)
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE T SET A = ?", sql.Named("a", 1)); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a named argument gave %v, want dbimp.ErrArguments", err)
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE T SET A = ?", 1, 2); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("two arguments for one parameter gave %v, want dbimp.ErrArguments", err)
	}
}

// TestOptions holds the options of one statement (D109).
func TestOptions(t *testing.T) {
	t.Parallel()
	db, s := open(t, "", "")
	for _, tt := range []struct {
		opt  avatica.Option
		want error
	}{
		{avatica.WithTimeout(time.Second), dbimp.ErrNotSupported},
		{avatica.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{avatica.WithReadonly(true), dbimp.ErrNotSupported},
		{avatica.WithDatabase("S"), dbimp.ErrNotSupported},
		{avatica.WithFrameSize(0), dbimp.ErrInvalidValue},
		{avatica.WithTimeout(0), nil},
		{avatica.WithReadonly(false), nil},
	} {
		if _, err := db.ExecContext(t.Context(), "UPDATE T SET A = 1", tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("the option gave %v, want %v", err, tt.want)
		}
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE T SET A = 1", avatica.WithParameter("maxRowCount", 10)); err != nil {
		t.Fatal(err)
	}
	pe := s.requests("prepareAndExecute")
	if last := pe[len(pe)-1]; last.body["maxRowCount"] != 10.0 {
		t.Errorf("WithParameter sent %s, want maxRowCount 10", last.raw)
	}
}

// TestTransaction holds D159: BeginTx sends connectionSync with autoCommit
// false and the options, and the end sends commit or rollback, then sets the
// properties back.
func TestTransaction(t *testing.T) {
	t.Parallel()
	db, s := open(t, "", "")
	tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "UPDATE T SET A = 1"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var props []string
	for _, r := range s.requests("connectionSync") {
		p, _ := r.body["connProps"].(map[string]any)
		delete(p, "connProps")
		delete(p, "dirty")
		b, _ := json.Marshal(p, json.Deterministic(true))
		props = append(props, string(b))
	}
	want := []string{`{"autoCommit":true}`, `{"autoCommit":false,"readOnly":true,"transactionIsolation":8}`, `{"autoCommit":true,"readOnly":false,"transactionIsolation":2}`}
	if strings.Join(props, " ") != strings.Join(want, " ") {
		t.Errorf("connectionSync sent %q, want %q", props, want)
	}
	if n := len(s.requests("commit")); n != 1 {
		t.Errorf("the server received %d commit, want 1", n)
	}
	if _, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelSnapshot}); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("the isolation of a snapshot gave %v, want dbimp.ErrNotSupported", err)
	}
}

// TestCancelClosesTheStatement holds D159: when the context of a query ends,
// the driver still sends closeStatement, because the server has no way to
// stop the statement.
func TestCancelClosesTheStatement(t *testing.T) {
	t.Parallel()
	db, s := open(t, "", "")
	ctx, cancel := context.WithCancel(t.Context())
	rows, err := db.QueryContext(ctx, "SELECT A FROM T")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cancel()
	for rows.Next() {
	}
	if err := rows.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("the read after the end of the context gave %v, want context.Canceled", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if n := len(s.requests("closeStatement")); n != 1 {
		t.Errorf("the server received %d closeStatement after the context ended, want 1", n)
	}
}
