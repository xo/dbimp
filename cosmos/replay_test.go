package cosmos_test

import (
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/cosmos"
)

func equal(a, b []string) bool {
	return slices.Equal(a, b)
}

// TestSelectStar holds D190: a SELECT * returns the whole document, with its
// system attributes, and the columns are the keys of the document in the
// order that they arrive (recorded: "select star of one document"). A nested
// object is a map, and a null is nil.
func TestSelectStar(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, server string, db *sql.DB) {
		t.Helper()
		cols, rows, err := read(t, db, "SELECT * FROM c WHERE c.id = 'ord1'")
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("the query gave %d rows, want 1", len(rows))
		}
		if server == hosted {
			want := []string{"id", "pk", "zeta", "alpha", "mid", "b", "abcdef", "nu", "obj", "_rid", "_self", "_etag", "_attachments", "_ts"}
			if !equal(cols, want) {
				t.Errorf("columns = %q, want %q, the stored order", cols, want)
			}
			wantRow := []string{
				`string("ord1")`, `string("a")`, "int64(1)", "int64(2)", "int64(3)", "int64(4)", "int64(5)", "nil", "{a:{b:int64(1)}}",
			}
			if !equal(rows[0][:len(wantRow)], wantRow) {
				t.Errorf("row = %q, want it to start with %q", rows[0], wantRow)
			}
		}
	})
}

// TestTheColumnsKeepTheOrderOfTheServer holds D190: the hosted account keeps the
// order of the statement in a projection, and the emulator sorts the keys by
// length and then by name (recorded: "a projection in the order of the
// statement"). The columns are the keys in the order that they arrive, and the
// driver never sorts them (hard rule 3).
func TestTheColumnsKeepTheOrderOfTheServer(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, server string, db *sql.DB) {
		t.Helper()
		cols, rows, err := read(t, db, "SELECT c.zeta, c.alpha, c.mid, c.b, c.abcdef, c.nu FROM c WHERE c.id = 'ord1'")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"zeta", "alpha", "mid", "b", "abcdef", "nu"}
		wantRow := []string{"int64(1)", "int64(2)", "int64(3)", "int64(4)", "int64(5)", "nil"}
		if server == emulator {
			want = []string{"b", "nu", "mid", "zeta", "alpha", "abcdef"}
			wantRow = []string{"int64(4)", "nil", "int64(3)", "int64(1)", "int64(2)", "int64(5)"}
		}
		if !equal(cols, want) {
			t.Errorf("columns = %q, want %q", cols, want)
		}
		if len(rows) != 1 || !equal(rows[0], wantRow) {
			t.Errorf("rows = %q, want one row %q", rows, wantRow)
		}
	})
}

// TestAMissingAttribute holds that the first row names the columns, and that a
// key that the server leaves out is not a column (recorded: "a projection
// with a missing attribute and a null"). The null is kept as nil.
func TestAMissingAttribute(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	db := openFake(t, srv.URL, "kv", "")
	cols, rows, err := read(t, db, "SELECT c.alpha, c.nu, c.nope, c.zeta FROM c WHERE c.id = 'ord1'")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"alpha", "nu", "zeta"}; !equal(cols, want) {
		t.Errorf("columns = %q, want %q", cols, want)
	}
	if len(rows) != 1 || !equal(rows[0], []string{"int64(2)", "nil", "int64(1)"}) {
		t.Errorf("rows = %q", rows)
	}
}

// TestSelectValue holds D18 rule 3 and D190: a row that is not an object has
// one column, $1, which is the name that the server gives an expression with
// no alias. An array is a []any, and an object keeps its keys as columns.
func TestSelectValue(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, server string, db *sql.DB) {
		t.Helper()
		for _, tt := range []struct {
			query string
			cols  []string
			rows  [][]string
		}{
			{"SELECT VALUE c.alpha FROM c WHERE c.id = 'ord1'", []string{"$1"}, [][]string{{"int64(2)"}}},
			{"SELECT VALUE [c.zeta, c.alpha] FROM c WHERE c.id = 'ord1'", []string{"$1"}, [][]string{{"[int64(1) int64(2)]"}}},
			{`SELECT VALUE {"z": c.zeta, "a": c.alpha} FROM c WHERE c.id = 'ord1'`, nil, nil},
			{"SELECT VALUE c.nope FROM c WHERE c.id = 'ord1'", []string{}, nil},
			{"SELECT c.id FROM c WHERE c.id = 'no such document'", []string{}, nil},
		} {
			cols, rows, err := read(t, db, tt.query)
			if err != nil {
				t.Errorf("%s: %v", tt.query, err)
				continue
			}
			if tt.cols == nil {
				// An object has the keys of the object as columns, and the
				// order of the emulator can differ.
				slices.Sort(cols)
				if !equal(cols, []string{"a", "z"}) || len(rows) != 1 {
					t.Errorf("%s: columns %q and rows %q, want the columns a and z and one row", tt.query, cols, rows)
				}
				continue
			}
			if !equal(cols, tt.cols) || !reflect.DeepEqual(rows, tt.rows) {
				t.Errorf("%s: columns %q and rows %q, want %q and %q", tt.query, cols, rows, tt.cols, tt.rows)
			}
		}
	})
}

// TestAnExpressionWithNoAlias holds that the server names it $1, $2 and so
// on, and that the driver keeps those names (recorded: "an expression with no
// alias").
func TestAnExpressionWithNoAlias(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, _ string, db *sql.DB) {
		t.Helper()
		cols, rows, err := read(t, db, "SELECT c.zeta + 1, c.alpha * 2, 'x' FROM c WHERE c.id = 'ord1'")
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"$1", "$2", "$3"}; !equal(cols, want) {
			t.Errorf("columns = %q, want %q", cols, want)
		}
		if want := [][]string{{"int64(2)", "int64(4)", `string("x")`}}; !reflect.DeepEqual(rows, want) {
			t.Errorf("rows = %q, want %q", rows, want)
		}
	})
}

// TestTheTypesOfAScalar holds the type table of D190 for a value that a
// statement writes (recorded: "a literal of each type").
func TestTheTypesOfAScalar(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, _ string, db *sql.DB) {
		t.Helper()
		cols, rows, err := read(t, db, "SELECT 1 AS x, 'a' AS y, null AS z, true AS t")
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for i, c := range cols {
			got[c] = rows[0][i]
		}
		want := map[string]string{"x": "int64(1)", "y": `string("a")`, "z": "nil", "t": "bool(true)"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("values = %v, want %v", got, want)
		}
	})
}

// TestNumbers holds D190: an integer that fits is an int64, and a number with
// a fraction or an exponent is a float64, whatever the service computed, and
// the exponent with three digits that the hosted account writes is read
// (recorded: "select the numbers", "arithmetic beyond 2^53").
func TestNumbers(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	db := openFake(t, srv.URL, "kv", "")
	cols, rows, err := read(t, db, "SELECT c.i64, c.u64, c.i64min, c.ovr, c.huge, c.dec, c.one, c.negzero, c.tiny, c.under, c.frac FROM c WHERE c.id = 'nums'")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"int64(9007199254740993)", "float64(1.8446744073709552e+19)", "int64(-9223372036854775808)", "float64(1.8446744073709552e+19)",
		"float64(1.2345678901234568e+29)", "float64(0.1)", "int64(1)", "int64(0)", "float64(5e-324)", "int64(0)", "float64(1.2345678901234567e+19)",
	}
	if len(rows) != 1 || !equal(rows[0], want) {
		t.Errorf("columns %q rows %q, want %q", cols, rows, want)
	}
	_, rows, err = read(t, db, "SELECT 9007199254740993 + 1 AS a, 9007199254740993 AS b, 0.1 + 0.2 AS c, 1/3 AS d")
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"int64(9007199254740994)", "int64(9007199254740993)", "float64(0.30000000000000004)", "float64(0.3333333333333333)"}
	if len(rows) != 1 || !equal(rows[0], want) {
		t.Errorf("rows %q, want %q", rows, want)
	}
}

// TestAStringThatLooksLikeAnotherType holds D135 and D190: a date, a UUID, a
// binary value and a decimal are plain strings, because the server cannot tell
// them from other text (recorded: "dates as text").
func TestAStringThatLooksLikeAnotherType(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	db := openFake(t, srv.URL, "kv", "")
	_, rows, err := read(t, db, "SELECT c.ts, c.dt, c.tm, c.uuid, c.b64, c.dec, c.ts < '2027' AS cmp FROM c WHERE c.id = 'ty1'")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %q", rows)
	}
	for i, v := range rows[0][:6] {
		if !strings.HasPrefix(v, "string(") {
			t.Errorf("column %d is %s, want a string", i, v)
		}
	}
}

// TestParameters holds D190: the server binds by name, and the name has an @.
// sql.Named("p", v) names @p, and a name with the @ is kept (recorded: "a
// parameter that is a string").
func TestParameters(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, server string, db *sql.DB) {
		t.Helper()
		for _, tt := range []struct {
			arg  any
			want string
		}{
			{"s", `string("s")`},
			{int64(5), "int64(5)"},
			{1.5, "float64(1.5)"},
			{true, "bool(true)"},
			{nil, "nil"},
			{[]any{1, 2}, "[int64(1) int64(2)]"},
			{map[string]any{"a": 1}, "{a:int64(1)}"},
			{int64(9007199254740993), "int64(9007199254740993)"},
			{"2026-10-10T00:00:00Z", `string("2026-10-10T00:00:00Z")`},
		} {
			cols, rows, err := read(t, db, "SELECT @p AS v, IS_DEFINED(@p) AS d, IS_NULL(@p) AS n", sql.Named("p", tt.arg))
			if err != nil {
				t.Errorf("binding %#v: %v", tt.arg, err)
				continue
			}
			if len(rows) != 1 || len(cols) == 0 {
				t.Errorf("binding %#v: columns %q rows %q", tt.arg, cols, rows)
				continue
			}
			got := map[string]string{}
			for i, c := range cols {
				got[c] = rows[0][i]
			}
			if got["v"] != tt.want {
				t.Errorf("binding %#v: v is %s, want %s", tt.arg, got["v"], tt.want)
			}
		}
	})
}

// TestTheParametersOfSeveralNames holds that the list of the body holds one
// entry for each argument, in the order of the arguments (recorded:
// "parameters in another order").
func TestTheParametersOfSeveralNames(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, _ string, db *sql.DB) {
		t.Helper()
		_, rows, err := read(t, db, "SELECT @p AS v, @q AS w", sql.Named("q", 2), sql.Named("p", 1))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || len(rows[0]) != 2 {
			t.Fatalf("rows = %q", rows)
		}
	})
}

// TestAPositionalArgument holds D190: the server has no positional
// placeholder, so an argument with no name is refused before any request.
func TestAPositionalArgument(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	db := openFake(t, srv.URL, "kv", "")
	_, _, err := read(t, db, "SELECT @p AS v", 1)
	if !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("the error is %v, want %v", err, dbimp.ErrArguments)
	}
}

// TestTheTrailingSemicolon holds D190: the driver strips one trailing
// semicolon, which both servers answer with HTTP 400 and SC1010 (recorded: "a
// trailing semicolon"). The statement then matches the recording of the
// statement with no semicolon.
func TestTheTrailingSemicolon(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, _ string, db *sql.DB) {
		t.Helper()
		for _, q := range []string{"select 1 as a;", "select 1 as a"} {
			_, rows, err := read(t, db, q)
			if err != nil {
				t.Errorf("%q: %v", q, err)
				continue
			}
			if len(rows) != 1 || !equal(rows[0], []string{"int64(1)"}) {
				t.Errorf("%q: rows = %q", q, rows)
			}
		}
	})
}

// TestAStatementWithAComment holds that a comment that starts with two hyphens
// goes to the server as it is (recorded: "a line comment").
func TestAStatementWithAComment(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, _ string, db *sql.DB) {
		t.Helper()
		_, rows, err := read(t, db, "SELECT 1 AS a -- comment")
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || !equal(rows[0], []string{"int64(1)"}) {
			t.Errorf("rows = %q", rows)
		}
	})
}

// TestExecReadsOnly holds D190: Exec fails with dbimp.ErrNotSupported for
// every statement, and sends nothing.
func TestExecReadsOnly(t *testing.T) {
	t.Parallel()
	// The server is not started, so a request would fail with another error.
	db, err := sql.Open(cosmos.Name, "cosmos://x:"+escapedKey+"@127.0.0.1:1/db/c?tls=false")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"INSERT INTO c VALUES (1)", "UPDATE c SET c.n = 1", "DELETE FROM c", "SELECT 1"} {
		if _, err := db.ExecContext(t.Context(), q); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("Exec(%q) = %v, want %v", q, err, dbimp.ErrNotSupported)
		}
		func() {
			stmt, err := db.PrepareContext(t.Context(), q)
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			if _, err := stmt.ExecContext(t.Context()); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("Exec of the prepared %q = %v, want %v", q, err, dbimp.ErrNotSupported)
			}
		}()
	}
}

// TestBeginTx holds D20 and D190: a transaction is an error, and nothing is
// faked.
func TestBeginTx(t *testing.T) {
	t.Parallel()
	db, err := sql.Open(cosmos.Name, "cosmos://x:"+escapedKey+"@127.0.0.1:1/db/c?tls=false")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx = %v, want %v", err, dbimp.ErrNotSupported)
	}
}

// TestPing holds that the ping reads the account, GET / (recorded: "the
// account").
func TestPing(t *testing.T) {
	t.Parallel()
	forEachServer(t, func(t *testing.T, _ string, db *sql.DB) {
		t.Helper()
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

// TestAPartitionKey holds that a query for one partition key sends the header
// of the key, and no header of cross partition (recorded: "a query for one
// partition key"), whether the key comes from an option, from the context or
// from the DSN.
func TestAPartitionKey(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	const query = "SELECT c.id FROM c WHERE c.pk = 'b'"
	for _, tt := range []struct {
		name string
		db   *sql.DB
		opts []cosmos.Option
	}{
		{name: "an argument", db: openFake(t, srv.URL, "kv", ""), opts: []cosmos.Option{cosmos.WithPartitionKey("b")}},
		{name: "the DSN", db: openFake(t, srv.URL, "kv", "partitionkey=b")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := make([]any, len(tt.opts))
			for i, o := range tt.opts {
				args[i] = o
			}
			_, rows, err := read(t, tt.db, query, args...)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 {
				t.Errorf("rows = %q, want 2 rows", rows)
			}
		})
	}
	t.Run("the context", func(t *testing.T) {
		t.Parallel()
		db := openFake(t, srv.URL, "kv", "")
		rows, err := db.QueryContext(cosmos.WithOptions(t.Context(), cosmos.WithPartitionKey("b")), query)
		if err != nil {
			t.Fatal(err)
		}
		_, got, err := readRows(t, rows)
		if err != nil || len(got) != 2 {
			t.Errorf("rows = %q, error %v, want 2 rows", got, err)
		}
	})
}
