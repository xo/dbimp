package exasol_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/exasol"
)

// These tests decode the answers that step 6 recorded from a real server, under
// testdata/exasol/, through the driver. A scripted server holds the frames that
// a test must see in order, and the test reads what the driver sent.

// member returns the request frame f as a map.
func member(t *testing.T, f dbimptest.WebSocketFrame) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(f.Content(), &m); err != nil {
		t.Fatalf("reading the frame %s: %v", f.Content(), err)
	}
	return m
}

// commands returns the command of each frame after the two of the login.
func commands(t *testing.T, srv *dbimptest.ScriptedWebSocket) []string {
	t.Helper()
	var out []string
	for _, f := range srv.Frames()[2:] {
		c, _ := member(t, f)["command"].(string)
		out = append(out, c)
	}
	return out
}

// scripted starts a scripted server with the steps after a login.
func scripted(t *testing.T, steps ...dbimptest.WebSocketFrame) *dbimptest.ScriptedWebSocket {
	t.Helper()
	script := loginSteps(t)
	for _, f := range steps {
		script = append(script, dbimptest.WebSocketStep{Reply: f})
	}
	return dbimptest.ServeWebSocketScript(t, script)
}

// scan reads every row of query into *any.
func scan(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, err
		}
		out = append(out, vals)
	}
	return cols, out, rows.Err()
}

// TestReplayTypes holds D206 for each type, as the table of every type held
// it (recorded: "select every row of types"). The third row holds the smallest
// INTERVAL YEAR TO MONTH, which does not fit dbimp.Interval, so it is an error
// that comes after two rows (D206).
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	db := replay(t, "")
	cols, got, err := scan(t, db, "SELECT * FROM DBIMP.TYPES ORDER BY ID")
	if len(cols) != 25 || cols[0] != "ID" || cols[24] != "H2" {
		t.Errorf("the columns are %q, want 25 from ID to H2", cols)
	}
	if !errors.Is(err, dbimp.ErrInvalidValue) || !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want one that wraps dbimp.ErrInvalidValue and dbimp.ErrIncomplete", err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d rows before the error, want 2", len(got))
	}
	want := map[string]string{
		"ID": "int64 1", "B": "bool true", "C": "string ab   ", "V": "string hello", "D1": "int64 9007199254740993", "D2": "*apd.Decimal 1.5",
		"D3": "*apd.Decimal 12345678901234567890123456789012345", "D7": "*apd.Decimal 1234567890123456789", "F": "float64 1.5e+300",
		"DT": "dbimp.Date 2026-10-11", "TS": "dbimp.LocalDateTime 2026-10-11T12:34:56.123", "IYM": "dbimp.Interval P5Y3M",
		"IDS": "dbimp.Interval P2DT3H4M5.678S", "G": "string POINT (1 2)", "H": "[]uint8 550e8400e29b41d4a716446655440000",
	}
	for i, c := range cols {
		w, ok := want[c]
		if !ok {
			continue
		}
		v := got[0][i]
		s := fmt.Sprintf("%T %v", v, v)
		if b, ok := v.([]byte); ok {
			s = fmt.Sprintf("[]uint8 %x", b)
		}
		if s != w {
			t.Errorf("column %s of row 1 is %s, want %s", c, s, w)
		}
		if got[1][i] != nil && c != "ID" {
			t.Errorf("column %s of row 2 is %v, want NULL", c, got[1][i])
		}
	}
	// The local time zone is the zone of the session, which the login named.
	for i, c := range cols {
		if c == "TSL" {
			ts, ok := got[0][i].(time.Time)
			if !ok || ts.Format("2006-01-02 15:04:05.999999999") != "2026-10-11 12:34:56.123" || ts.Location().String() != "CET" {
				t.Errorf("the TSL is %v, want 12:34:56.123 in the zone CET", got[0][i])
			}
		}
	}
}

// TestReplayColumnTypes holds D206 for the methods of ColumnType.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	db := replay(t, "")
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM DBIMP.TYPES ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	type info struct {
		db, scan string
		length   int64
		hasLen   bool
		prec     int64
		scale    int64
		hasPrec  bool
	}
	want := map[string]info{
		"ID":  {db: "DECIMAL", scan: "int64", hasPrec: true, prec: 9},
		"B":   {db: "BOOLEAN", scan: "bool"},
		"C":   {db: "CHAR", scan: "string", length: 5, hasLen: true},
		"V":   {db: "VARCHAR", scan: "string", length: 100, hasLen: true},
		"D2":  {db: "DECIMAL", scan: "*apd.Decimal", hasPrec: true, prec: 10, scale: 2},
		"D3":  {db: "DECIMAL", scan: "*apd.Decimal", hasPrec: true, prec: 36},
		"F":   {db: "DOUBLE", scan: "float64"},
		"DT":  {db: "DATE", scan: "dbimp.Date"},
		"TS":  {db: "TIMESTAMP", scan: "dbimp.LocalDateTime"},
		"TSL": {db: "TIMESTAMP WITH LOCAL TIME ZONE", scan: "time.Time"},
		"IYM": {db: "INTERVAL YEAR TO MONTH", scan: "dbimp.Interval"},
		"IDS": {db: "INTERVAL DAY TO SECOND", scan: "dbimp.Interval"},
		"G":   {db: "GEOMETRY", scan: "string"},
		"H":   {db: "HASHTYPE", scan: "[]uint8", length: 16, hasLen: true},
		"H2":  {db: "HASHTYPE", scan: "[]uint8", length: 32, hasLen: true},
	}
	for _, ct := range types {
		w, ok := want[ct.Name()]
		if !ok {
			continue
		}
		var got info
		got.db, got.scan = ct.DatabaseTypeName(), ct.ScanType().String()
		got.length, got.hasLen = ct.Length()
		got.prec, got.scale, got.hasPrec = ct.DecimalSize()
		if got != w {
			t.Errorf("the column %s has %+v, want %+v", ct.Name(), got, w)
		}
		if nullable, ok := ct.Nullable(); !nullable || !ok {
			t.Errorf("the column %s has Nullable %t, %t, want true, true", ct.Name(), nullable, ok)
		}
	}
}

// TestReplayLarge reads a result of 3000 rows by its handle, with the answer of
// a fetch that the server gave for one request of all the rows (recorded:
// "select 3000 rows", "fetch over the largest size"), and closes the handle.
func TestReplayLarge(t *testing.T) {
	t.Parallel()
	srv := scripted(t, recorded(t, 83).Reply, recorded(t, 92).Reply, recorded(t, 93).Reply)
	db := open(t, srv.Addr(), "")
	_, got, err := scan(t, db, "SELECT ID, TXT FROM DBIMP.BIG ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3000 {
		t.Fatalf("read %d rows, want 3000", len(got))
	}
	if !reflect.DeepEqual(got[0], []any{int64(1), "row 1"}) || !reflect.DeepEqual(got[2999], []any{int64(3000), "row 3000"}) {
		t.Errorf("the first row is %v and the last %v", got[0], got[2999])
	}
	if want := []string{"execute", "fetch", "closeResultSet"}; !reflect.DeepEqual(commands(t, srv), want) {
		t.Errorf("the driver sent %q, want %q", commands(t, srv), want)
	}
	f := member(t, srv.Frames()[3])
	if f["resultSetHandle"] != 33.0 || f["startPosition"] != 0.0 || f["numBytes"] != float64(1<<20) {
		t.Errorf("the fetch is %v, want handle 33, position 0 and numBytes %d (D210)", f, 1<<20)
	}
}

// numbers returns the JSON list of n numbers from first.
func numbers(first, n int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprint(&b, first+i)
	}
	b.WriteByte(']')
	return b.String()
}

// page returns the answer of fetch with n rows from first.
func page(first, n int) dbimptest.WebSocketFrame {
	return text(fmt.Sprintf(`{"status":"ok","responseData":{"numRows":%d,"data":[%s]}}`, n, numbers(first, n)))
}

// TestPaging holds D210: the driver fetches from the position of the next row,
// with the size of fetchsize, reads the pages one after another, and closes the
// handle after the last row.
func TestPaging(t *testing.T) {
	t.Parallel()
	srv := scripted(t, result(9, 2500, []string{"N"}, ""), page(1, 1000), page(1001, 1000), page(2001, 500), ok)
	db := open(t, srv.Addr(), "&fetchsize=64")
	_, got, err := scan(t, db, "SELECT N FROM T")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2500 {
		t.Fatalf("read %d rows, want 2500", len(got))
	}
	for i, r := range got {
		if r[0] != int64(i+1) {
			t.Fatalf("row %d holds %v, want %d", i, r[0], i+1)
		}
	}
	frames := srv.Frames()
	if len(frames) != 7 {
		t.Fatalf("the driver sent %d frames, want 7", len(frames))
	}
	for i, pos := range []float64{0, 1000, 2000} {
		f := member(t, frames[3+i])
		if f["command"] != "fetch" || f["startPosition"] != pos || f["resultSetHandle"] != 9.0 || f["numBytes"] != 64.0*1024 {
			t.Errorf("fetch %d is %v, want position %v, handle 9 and 65536 bytes", i, f, pos)
		}
	}
	if c := member(t, frames[6]); c["command"] != "closeResultSet" || !reflect.DeepEqual(c["resultSetHandles"], []any{9.0}) {
		t.Errorf("the last frame is %v, want closeResultSet of handle 9", c)
	}
}

// TestPagingFirstMessage holds D210: the first message can hold rows, and the
// fetch then starts after them.
func TestPagingFirstMessage(t *testing.T) {
	t.Parallel()
	srv := scripted(t, result(4, 5, []string{"N"}, "[[1,2]]"), page(3, 3), ok)
	db := open(t, srv.Addr(), "")
	_, got, err := scan(t, db, "SELECT N FROM T")
	if err != nil || len(got) != 5 {
		t.Fatalf("read %d rows, %v, want 5", len(got), err)
	}
	if f := member(t, srv.Frames()[3]); f["startPosition"] != 2.0 {
		t.Errorf("the fetch starts at %v, want 2", f["startPosition"])
	}
}

// TestPagingFaults holds D210 for a page that cannot be read: an answer with no
// row is an error and not an endless loop, and a page with too few values is an
// error.
func TestPagingFaults(t *testing.T) {
	t.Parallel()
	for name, steps := range map[string][]dbimptest.WebSocketFrame{
		"a fetch of no rows":    {result(9, 5, []string{"N"}, ""), text(`{"status":"ok","responseData":{"numRows":0,"data":[[]]}}`), ok},
		"a short column":        {result(9, 5, []string{"N"}, ""), text(`{"status":"ok","responseData":{"numRows":3,"data":[[1,2]]}}`), ok},
		"a wrong column count":  {result(9, 5, []string{"N"}, ""), text(`{"status":"ok","responseData":{"numRows":1,"data":[[1],[2]]}}`), ok},
		"data that is no lists": {result(9, 5, []string{"N"}, ""), text(`{"status":"ok","responseData":{"numRows":1,"data":[1]}}`), ok},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := scripted(t, steps...)
			db := open(t, srv.Addr(), "")
			_, _, err := scan(t, db, "SELECT N FROM T")
			if err == nil {
				t.Fatal("the read succeeded, want an error")
			}
		})
	}
}

// TestReplayCompression holds D214: the driver asks for compression at the login,
// sends each later frame as a binary frame of zlib data, and reads a text frame
// and a binary frame (recorded: "send the credentials and ask for compression",
// "select with compression").
func TestReplayCompression(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query    string
		compress bool
	}{{"", true}, {"&compression=false", false}} {
		t.Run(strconv.FormatBool(tt.compress), func(t *testing.T) {
			t.Parallel()
			srv := scripted(t, recorded(t, 311).Reply)
			db := open(t, srv.Addr(), tt.query)
			cols, got, err := scan(t, db, "SELECT 1 AS ONE, 'zlib' AS KIND")
			if err != nil || !reflect.DeepEqual(cols, []string{"ONE", "KIND"}) || !reflect.DeepEqual(got, [][]any{{int64(1), "zlib"}}) {
				t.Fatalf("the read gave %q, %v, %v", cols, got, err)
			}
			frames := srv.Frames()
			login := member(t, frames[1])
			if login["useCompression"] != tt.compress {
				t.Errorf("the login has useCompression %v, want %t", login["useCompression"], tt.compress)
			}
			attrs, _ := login["attributes"].(map[string]any)
			if _, has := attrs["compressionEnabled"]; has != tt.compress {
				t.Errorf("the login attributes are %v, want compressionEnabled %t", attrs, tt.compress)
			}
			want := dbimptest.FrameText
			if tt.compress {
				want = dbimptest.FrameBinary
			}
			if frames[2].Type != want || frames[2].Zlib != tt.compress {
				t.Errorf("the statement went in a %s frame with zlib %t, want %s and %t", frames[2].Type, frames[2].Zlib, want, tt.compress)
			}
			if frames[0].Type != dbimptest.FrameText || frames[1].Type != dbimptest.FrameText {
				t.Error("the login was compressed, and the server reads it as text")
			}
		})
	}
}

// TestReplayPrepared holds D208: the driver prepares the statement, sends each
// argument in the form that the server reads for the declared type of its
// parameter, decimals as strings and a double with a fraction, sends back the
// columns as the server sent them, and closes the statement (recorded: "prepare an
// insert with every kind of column", "execute the prepared insert with strings
// and numbers").
func TestReplayPrepared(t *testing.T) {
	t.Parallel()
	srv := scripted(t, recorded(t, 48).Reply, recorded(t, 49).Reply, recorded(t, 55).Reply)
	db := open(t, srv.Addr(), "")
	local := dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 10, Day: 11}, Time: dbimp.LocalTime{Hour: 1, Minute: 2, Second: 3, Nanosecond: 456789000}}
	res, err := db.ExecContext(t.Context(), recordedSQL(t, 48),
		100, true, "abc", "param", int64(123456789012345678), "12345.67", "123456789012345678901234567890123456", 2.5,
		dbimp.Date{Year: 2026, Month: 10, Day: 11}, local, local, dbimp.Interval{Months: 12*12 + 3},
		dbimp.Interval{Days: 2, Nanoseconds: 3*3600e9 + 4*60e9 + 5e9 + 678e6}, "POINT (3 4)",
		[]byte{0x55, 0x0e, 0x84, 0x00, 0xe2, 0x9b, 0x41, 0xd4, 0xa7, 0x16, 0x44, 0x66, 0x55, 0x44, 0x00, 0x01})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Errorf("RowsAffected = %d, %v, want 1", n, err)
	}
	if want := []string{"createPreparedStatement", "executePreparedStatement", "closePreparedStatement"}; !reflect.DeepEqual(commands(t, srv), want) {
		t.Fatalf("the driver sent %q, want %q", commands(t, srv), want)
	}
	f := member(t, srv.Frames()[3])
	wantData := `[["100"],[true],["abc"],["param"],["123456789012345678"],["12345.67"],["123456789012345678901234567890123456"],[2.5],["2026-10-11"],["2026-10-11 01:02:03.456789"],["2026-10-11 01:02:03.456789"],["+000000012-03"],["+000000002 03:04:05.678000000"],["POINT (3 4)"],["550e8400e29b41d4a716446655440001"]]`
	var want any
	if err := json.Unmarshal([]byte(wantData), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f["data"], want) {
		t.Errorf("the data is %v, want %v", f["data"], want)
	}
	if f["statementHandle"] != 17.0 || f["numColumns"] != 15.0 || f["numRows"] != 1.0 {
		t.Errorf("the request is %v, want handle 17, 15 columns and 1 row", f)
	}
	var cols map[string]any
	if err := json.Unmarshal(recorded(t, 48).Reply.Content(), &cols); err != nil {
		t.Fatal(err)
	}
	rd, _ := cols["responseData"].(map[string]any)
	pd, _ := rd["parameterData"].(map[string]any)
	if !reflect.DeepEqual(f["columns"], pd["columns"]) {
		t.Errorf("the columns are %v, want those of the server %v", f["columns"], pd["columns"])
	}
	if c := member(t, srv.Frames()[4]); c["statementHandle"] != 17.0 {
		t.Errorf("the close is %v, want handle 17", c)
	}
}

// recordedSQL returns the text of the statement of the recorded exchange n.
func recordedSQL(t *testing.T, n int) string {
	t.Helper()
	s, _ := member(t, recorded(t, n).Request)["sqlText"].(string)
	return s
}

// TestPreparedStatement holds D208: a statement of database/sql is prepared
// once, runs many times with the one handle, and closes with the statement.
func TestPreparedStatement(t *testing.T) {
	t.Parallel()
	exec := recorded(t, 208).Reply
	srv := scripted(t, recorded(t, 207).Reply, exec, exec, recorded(t, 209).Reply)
	db := open(t, srv.Addr(), "")
	st, err := db.PrepareContext(t.Context(), "INSERT INTO DBIMP.TYPES (ID, DT) VALUES (?, ?)")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := st.ExecContext(t.Context(), i, "2026-10-11"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil { //nolint:sqlclosecheck // The test reads the error of Close, which sends the close of the statement.
		t.Fatal(err)
	}
	want := []string{"createPreparedStatement", "executePreparedStatement", "executePreparedStatement", "closePreparedStatement"}
	if !reflect.DeepEqual(commands(t, srv), want) {
		t.Errorf("the driver sent %q, want %q", commands(t, srv), want)
	}
}

// TestPreparedRefuses holds D208: a wrong number of arguments is an error that
// the driver finds, and a syntax error shows when the statement is prepared
// (recorded: "prepare a statement with a syntax error").
func TestPreparedRefuses(t *testing.T) {
	t.Parallel()
	one := text(`{"status":"ok","responseData":{"statementHandle":2,"parameterData":{"numColumns":1,"columns":[{"name":"","dataType":{"type":"VARCHAR","size":10,"characterSet":"UTF8"}}]},"results":[{"resultType":"rowCount","rowCount":0}],"numResults":1}}`)
	srv := scripted(t, recorded(t, 207).Reply, recorded(t, 209).Reply, one, recorded(t, 209).Reply)
	db := open(t, srv.Addr(), "")
	_, err := db.ExecContext(t.Context(), "INSERT INTO DBIMP.TYPES (ID, DT) VALUES (?, ?)", 1)
	if !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("one argument for two placeholders gave %v, want %v", err, dbimp.ErrArguments)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO T VALUES (?)", sql.Named("p", 1)); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a named argument gave %v, want %v", err, dbimp.ErrNotSupported)
	}
}

// TestPrepareSyntaxError holds D208.
func TestPrepareSyntaxError(t *testing.T) {
	t.Parallel()
	srv := scripted(t, failure("42000", "syntax error"))
	db := open(t, srv.Addr(), "")
	st, err := db.PrepareContext(t.Context(), "INSERT INTO")
	if err == nil {
		defer st.Close()
	}
	if e, ok := errors.AsType[*exasol.Error](err); !ok || e.Code != "42000" {
		t.Errorf("the error is %v, want an *exasol.Error with the code 42000", err)
	}
}

// TestTransaction holds D209: BeginTx turns autocommit off, Commit and
// Rollback send their statement and turn it on, and an end that fails still
// turns it on.
func TestTransaction(t *testing.T) {
	t.Parallel()
	rowCount := text(`{"status":"ok","responseData":{"results":[{"resultType":"rowCount","rowCount":0}],"numResults":1}}`)
	for _, tt := range []struct {
		name  string
		steps []dbimptest.WebSocketFrame
		end   func(*sql.Tx) error
		query string
		want  []string
		fails bool
	}{
		{"commit", []dbimptest.WebSocketFrame{ok, rowCount, ok}, (*sql.Tx).Commit, "", []string{"setAttributes", "execute", "setAttributes"}, false},
		{"rollback", []dbimptest.WebSocketFrame{ok, rowCount, ok}, (*sql.Tx).Rollback, "", []string{"setAttributes", "execute", "setAttributes"}, false},
		{"a commit that fails", []dbimptest.WebSocketFrame{ok, failure("42000", "no"), ok}, (*sql.Tx).Commit, "", []string{"setAttributes", "execute", "setAttributes"}, true},
		{"autocommit off in the DSN", []dbimptest.WebSocketFrame{rowCount}, (*sql.Tx).Commit, "&autocommit=false", []string{"execute"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := scripted(t, tt.steps...)
			db := open(t, srv.Addr(), tt.query)
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.end(tx); (err != nil) != tt.fails {
				t.Errorf("the end gave %v, want an error: %t", err, tt.fails)
			}
			if got := commands(t, srv); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("the driver sent %q, want %q", got, tt.want)
			}
			frames := srv.Frames()[2:]
			if tt.query == "" {
				if a := member(t, frames[0])["attributes"]; !reflect.DeepEqual(a, map[string]any{"autocommit": false}) {
					t.Errorf("the first frame sets %v, want autocommit false", a)
				}
				if a := member(t, frames[2])["attributes"]; !reflect.DeepEqual(a, map[string]any{"autocommit": true}) {
					t.Errorf("the last frame sets %v, want autocommit true", a)
				}
			}
			stmt := "COMMIT"
			if tt.name == "rollback" {
				stmt = "ROLLBACK"
			}
			if got := member(t, frames[len(frames)-min(len(frames), 2)])["sqlText"]; tt.name != "a commit that fails" && tt.query == "" && got != stmt {
				t.Errorf("the statement is %v, want %s", got, stmt)
			}
		})
	}
}

// TestTransactionRefuses holds D209: the options of a transaction that the
// server has no form of give dbimp.ErrNotSupported, and no frame goes out.
func TestTransactionRefuses(t *testing.T) {
	t.Parallel()
	srv := scripted(t)
	db := open(t, srv.Addr(), "")
	for name, opts := range map[string]*sql.TxOptions{
		"read only":        {ReadOnly: true},
		"serializable":     {Isolation: sql.LevelSerializable},
		"read committed":   {Isolation: sql.LevelReadCommitted},
		"repeatable read":  {Isolation: sql.LevelRepeatableRead},
		"read uncommitted": {Isolation: sql.LevelReadUncommitted},
	} {
		if tx, err := db.BeginTx(t.Context(), opts); !errors.Is(err, dbimp.ErrNotSupported) {
			if err == nil {
				_ = tx.Rollback()
			}
			t.Errorf("BeginTx with %s gave %v, want %v", name, err, dbimp.ErrNotSupported)
		}
	}
	if n := len(srv.Frames()); n > 2 {
		t.Errorf("the driver sent %d frames after the login, want none", n-2)
	}
}

// TestTransactionContext holds D209: the rollback that database/sql runs after
// the context of the transaction ended still reaches the server.
func TestTransactionContext(t *testing.T) {
	t.Parallel()
	rowCount := text(`{"status":"ok","responseData":{"results":[{"resultType":"rowCount","rowCount":0}],"numResults":1}}`)
	srv := scripted(t, ok, rowCount, ok)
	db := open(t, srv.Addr(), "")
	ctx, cancel := context.WithCancel(t.Context())
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	// database/sql rolls the transaction back itself when the context ends,
	// and Rollback after it returns sql.ErrTxDone, so wait for the frames.
	_ = tx.Rollback()
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.Frames()) < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if want := []string{"setAttributes", "execute", "setAttributes"}; !reflect.DeepEqual(commands(t, srv), want) {
		t.Errorf("the driver sent %q, want %q", commands(t, srv), want)
	}
}

// TestExec holds D207 and D208 for Exec: a count of rows, no last insert id, and
// a statement that returns a result set has no count (recorded: "update two
// rows", "select the rows").
func TestExec(t *testing.T) {
	t.Parallel()
	srv := scripted(t, recorded(t, 37).Reply, result(0, 1, []string{"N"}, "[[1]]"))
	db := open(t, srv.Addr(), "")
	res, err := db.ExecContext(t.Context(), recordedSQL(t, 37))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 2 {
		t.Errorf("RowsAffected = %d, %v, want 2", n, err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want %v", err, dbimp.ErrNotSupported)
	}
	res, err = db.ExecContext(t.Context(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected of a select gave %v, want %v", err, dbimp.ErrNotSupported)
	}
}

// TestQueryOfACount holds the rule of the driver that a statement with a count
// and no rows, run with Query, has no column and no row.
func TestQueryOfACount(t *testing.T) {
	t.Parallel()
	srv := scripted(t, recorded(t, 37).Reply)
	db := open(t, srv.Addr(), "")
	cols, got, err := scan(t, db, recordedSQL(t, 37))
	if err != nil || len(cols) != 0 || len(got) != 0 {
		t.Errorf("the read gave %q, %v, %v, want no column and no row", cols, got, err)
	}
}

// TestReplayErrors holds D8: an error of the server is an *exasol.Error with its
// code and its text (recorded: "syntax error", "missing table", "primary key
// violation").
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	for _, n := range []int{146, 272, 275} {
		ex := recorded(t, n)
		srv := scripted(t, ex.Reply)
		db := open(t, srv.Addr(), "")
		_, err := db.ExecContext(t.Context(), "SELECT 1")
		e, ok := errors.AsType[*exasol.Error](err)
		if !ok || e.Code == "" || !strings.Contains(e.Message, "(Session: ") || !strings.HasPrefix(e.Error(), "exasol: "+e.Code) {
			t.Errorf("exchange %d: the error is %v, want an *exasol.Error with a code and a text", n, err)
		}
		if errors.Is(err, driver.ErrBadConn) {
			t.Errorf("exchange %d: an error of the server is never driver.ErrBadConn", n)
		}
	}
}

// TestReplayTimeZone holds D206: ALTER SESSION SET TIME_ZONE changes the zone
// that a TIMESTAMP WITH LOCAL TIME ZONE is read in, and the driver reads the new
// zone from the attributes of the answer (recorded: "set the time zone to
// Tokyo", "local time zone in Tokyo").
func TestReplayTimeZone(t *testing.T) {
	t.Parallel()
	srv := scripted(t, recorded(t, 31).Reply, recorded(t, 32).Reply)
	db := open(t, srv.Addr(), "")
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), recordedSQL(t, 31)); err != nil {
		t.Fatal(err)
	}
	var tsl time.Time
	var ts dbimp.LocalDateTime
	if err := conn.QueryRowContext(t.Context(), recordedSQL(t, 32)).Scan(&tsl, &ts, new(any), new(any), new(any)); err != nil {
		t.Fatal(err)
	}
	if tsl.Format("2006-01-02 15:04:05") != "2026-10-11 19:34:56" || tsl.Location().String() != "Asia/Tokyo" {
		t.Errorf("the TSL is %v, want 19:34:56 in Asia/Tokyo", tsl)
	}
	if ts.String() != "2026-10-11T12:34:56.123" {
		t.Errorf("the TS is %v, want 12:34:56.123, with no zone", ts)
	}
}

// TestPing holds D8: Ping sends getAttributes.
func TestPing(t *testing.T) {
	t.Parallel()
	srv := scripted(t, recorded(t, 6).Reply)
	db := open(t, srv.Addr(), "")
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := commands(t, srv); !reflect.DeepEqual(got, []string{"getAttributes"}) {
		t.Errorf("the driver sent %q, want getAttributes", got)
	}
}

// TestLogin holds D205 and D212: the login sends the attributes of the DSN, the
// password is encrypted and never sent as text, and the user and the client
// name are as the DSN says.
func TestLogin(t *testing.T) {
	t.Parallel()
	srv := scripted(t, recorded(t, 6).Reply)
	db, err := sql.Open(exasol.Name, "exasol://alice:s3cr3t@"+srv.Addr()+"/MY_SCHEMA?validateservercertificate=false&clientname=app&querytimeout=30&autocommit=false&protocol=4")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	frames := srv.Frames()
	if first := member(t, frames[0]); first["command"] != "login" || first["protocolVersion"] != 4.0 {
		t.Errorf("the first frame is %v, want login with protocolVersion 4", first)
	}
	second := member(t, frames[1])
	if _, has := second["command"]; has {
		t.Error("the second frame of the login has a command")
	}
	if second["username"] != "alice" || second["clientName"] != "app" || second["driverName"] != "dbimp" || second["useCompression"] != true {
		t.Errorf("the credentials are %v", second)
	}
	pw, _ := second["password"].(string)
	if pw == "" || strings.Contains(pw, "s3cr3t") || len(pw) < 100 {
		t.Errorf("the password is %q, want it encrypted with RSA and written in Base64", pw)
	}
	want := map[string]any{"autocommit": false, "compressionEnabled": true, "currentSchema": "MY_SCHEMA", "queryTimeout": 30.0}
	if got := second["attributes"]; !reflect.DeepEqual(got, want) {
		t.Errorf("the attributes are %v, want %v", got, want)
	}
}
