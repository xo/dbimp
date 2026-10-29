package tablestore //nolint:testpackage // The tests read the options, which the package keeps unexported.

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestOptionsRefuseWhatTheServerCannotDo holds D109: an option that the server
// cannot honor fails the statement with dbimp.ErrNotSupported, and a value that
// the DSN refuses fails it with dbimp.ErrInvalidValue. The server answers
// nothing here, because the statement never leaves the driver.
func TestOptionsRefuseWhatTheServerCannotDo(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		name string
		opt  Option
		want error
	}{
		{"a timeout", WithTimeout(time.Second), dbimp.ErrNotSupported},
		{"a negative timeout", WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"read only", WithReadonly(true), dbimp.ErrNotSupported},
		{"another instance", WithDatabase("other"), dbimp.ErrNotSupported},
		{"the member version", WithParameter("version", 1), dbimp.ErrInvalidValue},
		{"an unknown member", WithParameter("nope", 1), dbimp.ErrInvalidValue},
		{"a string for sql_version", WithParameter("sql_version", "1"), dbimp.ErrInvalidValue},
		{"an integer for the token", WithParameter("search_token", 1), dbimp.ErrInvalidValue},
	} {
		_, err := db.ExecContext(t.Context(), "SELECT 1 AS a;", tt.opt)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want %v", tt.name, err, tt.want)
		}
	}
}

// TestOptionsTakenFromTheContext holds that WithOptions sets the options of every
// statement that the context starts, and that an option that asks for nothing
// changes nothing (recorded: "select 1").
func TestOptionsTakenFromTheContext(t *testing.T) {
	t.Parallel()
	db := replay(t)
	ctx := WithOptions(t.Context(), WithTimeout(time.Second))
	if _, err := db.ExecContext(ctx, "SELECT 1 AS a;"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a timeout from the context gave %v, want dbimp.ErrNotSupported", err)
	}
	ctx = WithOptions(t.Context(), WithTimeout(0), WithReadonly(false), WithDatabase("dbmeta"))
	if n, err := countRows(ctx, t, db, "SELECT 1 AS a, 'x' AS b;"); err != nil || n != 1 {
		t.Fatalf("options that ask for nothing: %d rows and %v", n, err)
	}
}

// TestWithParameter sends the members of SQLQueryRequest that the option sets.
// The server answers the recorded requests only, so a request that differs in one
// byte fails the replay (recorded: "lead: sql_version 3", "lead: a search token of
// no meaning").
func TestWithParameter(t *testing.T) {
	t.Parallel()
	db := replay(t)
	const query = "SELECT id FROM dbimp_it_t WHERE id = 3;"
	for _, v := range []int{0, 2, 3, 4} {
		_, rows := values(t, db, query, WithParameter("sql_version", v))
		if !reflect.DeepEqual(rows, [][]any{{int64(3)}}) {
			t.Errorf("sql_version %d gave %#v", v, rows)
		}
	}
	_, rows := values(t, db, query, WithParameter("sql_version", int64(3)))
	if len(rows) != 1 {
		t.Errorf("an int64 version gave %#v", rows)
	}
	if _, rows := values(t, db, "SELECT id FROM dbimp_it_t ORDER BY id;", WithParameter("search_token", "abc")); len(rows) != 5 {
		t.Errorf("a search token gave %d rows, want 5", len(rows))
	}
}

// TestOptionsAreNotArguments holds that an Option does not count as an argument
// of a statement, so a statement with a placeholder takes its value and its
// option together (D109).
func TestOptionsAreNotArguments(t *testing.T) {
	t.Parallel()
	db := replay(t)
	res, err := db.ExecContext(t.Context(), "DELETE FROM dbimp_it_t WHERE id = ?;", WithReadonly(false), int64(1))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Errorf("%d rows affected and %v, want 1", n, err)
	}
}
