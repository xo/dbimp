package dynamodb_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/dynamodb"
)

// These tests replay the exchanges that step 6 recorded from DynamoDB Local,
// under testdata/dynamodb/. Each one decodes a real answer through the
// driver. A fake server answers each request with the exchange whose target
// and body match, and fails the test for a request that no exchange matches,
// so the driver must send the bytes that the recording holds.

const testdata = "../testdata/dynamodb"

// releases are the releases of DynamoDB Local that step 6 recorded for this
// driver.
var releases = []string{"dynamodb-3.2.0", "dynamodb-3.3.1"}

// match matches the target and the body of a request with a recorded one.
func match(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	return r.Header.Get("X-Amz-Target") == strings.Join(ex.Request.Header["X-Amz-Target"], ",") &&
		dbimptest.DefaultMatch(r, body, ex)
}

// forEachRelease runs f for each release, with a database on a fake server
// that replays the release.
func forEachRelease(t *testing.T, f func(t *testing.T, db *sql.DB)) {
	t.Helper()
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			srv := dbimptest.ReplayRelease(t, testdata, release, match)
			db, err := sql.Open(dynamodb.Name, strings.Replace(srv.URL, "http://", "dynamodb://dbmeta:secret@", 1)+"?region=us-east-1&tls=false")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			f(t, db)
		})
	}
}

// show writes v with its Go type, so that a test compares the type of a value
// as well as its text. A decimal is written by its number, and not by its
// exponent.
func show(v any) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case *apd.Decimal:
		return "N(" + v.Text('f') + ")"
	case string:
		return fmt.Sprintf("S(%q)", v)
	case bool:
		return fmt.Sprintf("BOOL(%v)", v)
	case []byte:
		return fmt.Sprintf("B(%x)", v)
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = show(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ":" + show(v[k])
		}
		return "{" + strings.Join(parts, " ") + "}"
	}
	return fmt.Sprintf("%T(%v)", v, v)
}

// read runs a query and returns its columns and its rows, each value written
// by show.
func read(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]string, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scanning %q: %v", query, err)
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = show(v)
		}
		out = append(out, row)
	}
	return cols, out, rows.Err()
}

// TestReplayEveryType reads the item of every type through SELECT *, which is
// one column that holds a map (D163), and compares the Go type of each value
// with the type table (D135 and D169).
func TestReplayEveryType(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		cols, rows, err := read(t, db, "SELECT * FROM dbimp_types WHERE pk = 't1'")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(cols, []string{""}) || len(rows) != 1 {
			t.Fatalf("columns %q and %d rows, want one column and one row", cols, len(rows))
		}
		want := "{" + strings.Join([]string{
			"b:BOOL(true)",
			"bin:B(00ff)",
			"bs:[B(00ff) B(01)]",
			"l:[N(1) S(\"two\") {x:BOOL(false)} [S(\"a\")]]",
			"m:{a:S(\"first\") k:[N(1) S(\"two\") nil] z:S(\"last\")}",
			"n:N(12345678901234567890123456789012345678)",
			"nfrac:N(0.12345678901234567890123456789012345678)",
			"nint:N(42)",
			"nneg:N(-12345678901234567890123456789012345678)",
			"ns:[N(1) N(2.5) N(12345678901234567890123456789012345678)]",
			"nul:nil",
			"pk:S(\"t1\")",
			"s:S(\"é'\\\"\\\\ x\")",
			"ss:[S(\"a\") S(\"b\")]",
		}, " ") + "}"
		if got := rows[0][0]; got != want {
			t.Errorf("the item is\n%s\nwant\n%s", got, want)
		}
	})
}

// TestReplayRange reads the empty values and the range of numbers (recorded:
// "empty values and the range of numbers"): every digit survives, and the
// server writes no exponent.
func TestReplayRange(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		_, rows, err := read(t, db, "SELECT * FROM dbimp_types WHERE pk = 't2'")
		if err != nil {
			t.Fatal(err)
		}
		item := rows[0][0]
		for _, want := range []string{
			"nzero:N(0)",
			"s:S(\"\")",
			"bin:B()",
			"l:[]",
			"m:{}",
			"ntrail:N(0.1)",
			"n:N(0." + strings.Repeat("0", 129) + "1)",
			"nsmall:N(-0." + strings.Repeat("0", 129) + "1)",
			"nmax:N(" + strings.Repeat("9", 38) + strings.Repeat("0", 88) + ")",
			"nmin:N(-" + strings.Repeat("9", 38) + strings.Repeat("0", 88) + ")",
		} {
			if !strings.Contains(item, want) {
				t.Errorf("the item lacks %.60s", want)
			}
		}
	})
}

// TestReplayProjection holds D163 and D169: the columns are the names that the
// statement gives, in its order, and not the order of the members of the
// item, and an attribute that is missing is nil (recorded: "a projection").
func TestReplayProjection(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		for _, tt := range []struct {
			query string
			cols  []string
			rows  []string
		}{
			{`SELECT s, nul, "absent", nint, pk FROM dbimp_types WHERE pk = 't1'`, []string{"s", "nul", "absent", "nint", "pk"}, []string{"S(\"é'\\\"\\\\ x\") nil nil N(42) S(\"t1\")"}},
			{`SELECT s, nint FROM dbimp_types WHERE pk = 't3'`, []string{"s", "nint"}, []string{"nil nil"}},
			{`SELECT m.k[1], l[0], m.z FROM dbimp_types WHERE pk = 't1'`, []string{"k[1]", "l[0]", "z"}, []string{"S(\"two\") N(1) S(\"last\")"}},
			{`SELECT "m.z" FROM dbimp_types WHERE pk = 't1'`, []string{"m.z"}, []string{"nil"}},
			{"SELECT * FROM dbimp_types WHERE pk = 't3'", []string{""}, []string{"{pk:S(\"t3\")}"}},
			{"-- a comment\nSELECT pk FROM dbimp_types /* another */ WHERE pk = 't1'", []string{"pk"}, []string{`S("t1")`}},
			{"select pk from dbimp_types where pk = 't1'", []string{"pk"}, []string{`S("t1")`}},
			{`SELECT pk FROM "dbimp_types" WHERE "pk" = 't1'`, []string{"pk"}, []string{`S("t1")`}},
			{"SELECT pk FROM dbimp_types WHERE pk = 't1';", []string{"pk"}, []string{`S("t1")`}},
			{"SELECT pk FROM dbimp_types WHERE pk = '?'", []string{"pk"}, nil},
		} {
			cols, rows, err := read(t, db, tt.query)
			if err != nil {
				t.Errorf("%q: %v", tt.query, err)
				continue
			}
			var got []string
			for _, r := range rows {
				got = append(got, strings.Join(r, " "))
			}
			if !slices.Equal(cols, tt.cols) || !slices.Equal(got, tt.rows) {
				t.Errorf("%q: columns %q and rows %q, want %q and %q", tt.query, cols, got, tt.cols, tt.rows)
			}
		}
	})
}

// TestReplayPages holds D21 and D169: the driver follows NextToken to the end,
// which is a page of 1 MB, then a page of the rest (recorded: "a result larger
// than one page" and "the next page").
func TestReplayPages(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		cols, rows, err := read(t, db, "SELECT id FROM dbimp_page")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(cols, []string{"id"}) || len(rows) != 300 {
			t.Fatalf("columns %q and %d rows, want [id] and 300", cols, len(rows))
		}
		seen := map[string]bool{}
		for _, r := range rows {
			seen[r[0]] = true
		}
		if len(seen) != 300 {
			t.Errorf("the 300 rows hold %d ids, want 300 different ids", len(seen))
		}
	})
}

// TestReplayLimit holds that a page with no item and a token ends the result
// when the next page is missing, and that a page that holds items and a token
// comes through (recorded: "a limit with a filter").
func TestReplayLimit(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		ctx := dynamodb.WithOptions(t.Context(), dynamodb.WithParameter("Limit", 2))
		rows, err := db.QueryContext(ctx, "SELECT id FROM dbimp_page")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ids []string
		for n := 0; n < 2 && rows.Next(); n++ {
			var v any
			if err := rows.Scan(&v); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, show(v))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if want := []string{"N(43)", "N(122)"}; !slices.Equal(ids, want) {
			t.Errorf("the first page holds %v, want %v", ids, want)
		}
	})
}

// TestReplayErrors reads the errors that step 6 recorded.
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		for _, tt := range []struct {
			name  string
			query string
			opts  []dynamodb.Option
			args  []any
			typ   string
			msg   string
		}{
			{"a syntax error", "SELEC * FROM dbimp_types", nil, nil, "ValidationException", "Statement wasn't well formed, can't be processed: SELEC * FROM dbimp_types"},
			{"an unknown table", "SELECT * FROM dbimp_none", nil, nil, "ResourceNotFoundException", "Cannot do operations on a non-existent table"},
			{"LIMIT in the statement", "SELECT id FROM dbimp_page LIMIT 5", nil, nil, "ValidationException", "Unsupported clause: LIMIT at 1:33:1"},
			{"a next token that is not valid", "SELECT id FROM dbimp_page", []dynamodb.Option{dynamodb.WithParameter("NextToken", "garbage")}, nil, "ValidationException", "Invalid NextToken"},
			{"two columns with one name", "SELECT s, s FROM dbimp_types WHERE pk = 't1'", nil, nil, "ValidationException", "Duplicate identifiers in select clause: s"},
			{"an alias", "SELECT s AS x FROM dbimp_types WHERE pk = 't1'", nil, nil, "ValidationException", "Aliasing is not supported"},
			{"the version statement of usql", "SELECT version();", nil, nil, "ValidationException", ""},
			{"a number for a key of a string", "SELECT pk FROM dbimp_types WHERE pk = ?", nil, []any{int64(1)}, "ValidationException", "Key attribute's data type should match its data type in table's schema: Key pk"},
			{"an update that a condition stops", "UPDATE dbimp_crud SET v = 'x' WHERE pk = 'none' AND sk = 1 AND v = 'nomatch'", nil, nil, "ConditionalCheckFailedException", ""},
			{"an insert of a key that exists", "INSERT INTO dbimp_crud VALUE {'pk': 'a', 'sk': 1}", nil, nil, "DuplicateItem", ""},
			{"an update with no full key", "UPDATE dbimp_crud SET v = 'x' WHERE pk = 'a'", nil, nil, "ValidationException", ""},
			{"two statements in one text", "SELECT pk FROM dbimp_types WHERE pk = 't1'; SELECT pk FROM dbimp_types WHERE pk = 't2'", nil, nil, "ValidationException", ""},
			{"an empty statement", "", nil, nil, "ValidationException", ""},
			{"an upsert", "UPSERT INTO dbimp_crud VALUE {'pk': 'd', 'sk': 1}", nil, nil, "ValidationException", ""},
		} {
			args := slices.Concat(tt.args, optionArgs(tt.opts))
			_, err := db.ExecContext(t.Context(), tt.query, args...)
			var derr *dynamodb.Error
			switch {
			case !errors.As(err, &derr):
				t.Errorf("%s: %v, want a *dynamodb.Error", tt.name, err)
			case derr.Type != tt.typ || derr.HTTPStatus != http.StatusBadRequest || tt.msg != "" && derr.Message != tt.msg:
				t.Errorf("%s: %+v, want status 400, type %s and message %q", tt.name, *derr, tt.typ, tt.msg)
			}
		}
	})
}

// optionArgs returns opts as arguments of a statement.
func optionArgs(opts []dynamodb.Option) []any {
	args := make([]any, len(opts))
	for i, o := range opts {
		args[i] = o
	}
	return args
}

// TestReplayParameters sends each argument as a typed value, and the server
// accepts the bytes of the recording (recorded: "positional parameters", "a
// number parameter with 38 digits" and "a parameter of each type").
func TestReplayParameters(t *testing.T) {
	t.Parallel()
	d := func(s string) *apd.Decimal {
		v, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		_, rows, err := read(t, db, "SELECT pk, nint FROM dbimp_types WHERE pk = ? AND nint = ?", "t1", int64(42))
		if err != nil || len(rows) != 1 || strings.Join(rows[0], " ") != `S("t1") N(42)` {
			t.Errorf("positional parameters: %v, %v", rows, err)
		}
		_, rows, err = read(t, db, "SELECT pk FROM dbimp_types WHERE pk = 't1' AND n = ?", d("12345678901234567890123456789012345678"))
		if err != nil || len(rows) != 1 {
			t.Errorf("a number of 38 digits: %v, %v", rows, err)
		}
		if _, err := db.ExecContext(t.Context(),
			"INSERT INTO dbimp_types VALUE {'pk': 'par', 's': ?, 'n': ?, 'bin': ?, 'b': ?, 'nul': ?, 'm': ?, 'l': ?, 'ss': ?, 'ns': ?, 'bs': ?}",
			"s", d("1.50"), []byte{0, 0xff}, true, nil, map[string]any{"k": int64(1)}, []any{"a"},
			dynamodb.Set{"a"}, dynamodb.Set{d("1")}, dynamodb.Set{[]byte{1}},
		); err != nil {
			t.Errorf("a parameter of each type: %v", err)
		}
	})
}

// TestReplayWrites runs the statements that write. The server answers an empty
// list of items, so a write has no column and no row, and no count (recorded:
// "crud: update"). RETURNING gives the item as the one column (D163).
func TestReplayWrites(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		for _, q := range []string{
			"INSERT INTO dbimp_crud VALUE {'pk': 'a', 'sk': 1, 'v': 'one'}",
			"UPDATE dbimp_crud SET v = 'uno' WHERE pk = 'a' AND sk = 1",
			"DELETE FROM dbimp_crud WHERE pk = 'a' AND sk = 2",
			"DELETE FROM dbimp_crud WHERE pk = 'none' AND sk = 1",
		} {
			res, err := db.ExecContext(t.Context(), q)
			if err != nil {
				t.Errorf("%s: %v", q, err)
				continue
			}
			if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("%s: RowsAffected = %v, want dbimp.ErrNotSupported", q, err)
			}
			cols, rows, err := read(t, db, q)
			if err != nil || len(cols) != 0 || len(rows) != 0 {
				t.Errorf("%s: columns %q, rows %v and %v, want none", q, cols, rows, err)
			}
		}
		for _, q := range []string{
			"UPDATE dbimp_crud SET v = 'one' WHERE pk = 'a' AND sk = 1 RETURNING ALL OLD *",
			"DELETE FROM dbimp_crud WHERE pk = 'a' AND sk = 1 RETURNING ALL OLD *",
		} {
			cols, rows, err := read(t, db, q)
			if err != nil || !slices.Equal(cols, []string{""}) || len(rows) != 1 || !strings.HasPrefix(rows[0][0], "{") {
				t.Errorf("%s: columns %q, rows %v and %v, want one column with a map", q, cols, rows, err)
			}
		}
	})
}
