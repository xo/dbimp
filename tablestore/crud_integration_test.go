package tablestore_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/tablestore"
)

// TestIntegrationCRUD tests the four statements of CRUD on a mapped table, with
// arguments, and the refusals of the other forms. Each subtest is one entry of
// testdata/tablestore/features.json.
func TestIntegrationCRUD(t *testing.T) {
	db := connect(t)
	tbl := makeTable(t, db, "crud", tablestore.KeyInteger, "s MEDIUMTEXT, i BIGINT")

	t.Run("insert", func(t *testing.T) {
		res := exec(t, db, "INSERT INTO "+tbl+" (id, s, i) VALUES (?, ?, ?)", int64(1), "one", int64(10))
		if n := affected(t, res); n != 1 {
			t.Errorf("INSERT changed %d rows, want 1", n)
		}
		// A NULL argument is the literal NULL, and the row keeps no value for it.
		exec(t, db, "INSERT INTO "+tbl+" (id, s, i) VALUES (?, ?, ?)", int64(2), nil, int64(20))
		exec(t, db, "INSERT INTO "+tbl+" (id, s) VALUES (3, 'three')")
		// A second row with the same key is an error of the server, not a change.
		_, err := db.ExecContext(t.Context(), "INSERT INTO "+tbl+" (id, s) VALUES (?, ?)", int64(1), "again")
		var e *tablestore.Error
		if !errors.As(err, &e) || e.Code != "OTSParameterInvalid" {
			t.Errorf("a duplicate key gave %v, want OTSParameterInvalid", err)
		}
	})
	t.Run("select", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT id, s, i FROM "+tbl+" ORDER BY id")
		want := [][]any{{int64(1), "one", int64(10)}, {int64(2), nil, int64(20)}, {int64(3), "three", nil}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SELECT gave %#v, want %#v", got, want)
		}
		if got := rowsOf(t, db, "SELECT s FROM "+tbl+" WHERE id = ?", int64(1)); !reflect.DeepEqual(got, [][]any{{"one"}}) {
			t.Errorf("SELECT with an argument gave %#v", got)
		}
		if got := rowsOf(t, db, "SELECT id FROM "+tbl+" WHERE id = ?", int64(99)); len(got) != 0 {
			t.Errorf("SELECT of a missing key gave %#v, want no row", got)
		}
	})
	t.Run("update", func(t *testing.T) {
		res := exec(t, db, "UPDATE "+tbl+" SET s = ? WHERE id = ?", "uno", int64(1))
		if n := affected(t, res); n != 1 {
			t.Errorf("UPDATE changed %d rows, want 1", n)
		}
		if got := rowsOf(t, db, "SELECT s FROM "+tbl+" WHERE id = 1"); !reflect.DeepEqual(got, [][]any{{"uno"}}) {
			t.Errorf("the row after the update is %#v", got)
		}
		if n := affected(t, exec(t, db, "UPDATE "+tbl+" SET s = 'x' WHERE id = 99")); n != 0 {
			t.Errorf("UPDATE of a missing key changed %d rows, want 0", n)
		}
	})
	t.Run("delete", func(t *testing.T) {
		if n := affected(t, exec(t, db, "DELETE FROM "+tbl+" WHERE id = ?", int64(3))); n != 1 {
			t.Errorf("DELETE changed %d rows, want 1", n)
		}
		if got := rowsOf(t, db, "SELECT id FROM "+tbl+" ORDER BY id"); !reflect.DeepEqual(got, [][]any{{int64(1)}, {int64(2)}}) {
			t.Errorf("the rows after the delete are %#v", got)
		}
	})
	t.Run("upsert", func(t *testing.T) {
		refused(t, db, "UPSERT INTO "+tbl+" (id, s) VALUES (9, 'u')", "OTSParameterInvalid")
	})
	t.Run("replace", func(t *testing.T) {
		refused(t, db, "REPLACE INTO "+tbl+" (id, s) VALUES (1, 'r')", "OTSParameterInvalid")
	})
	t.Run("insert on duplicate key update", func(t *testing.T) {
		refused(t, db, "INSERT INTO "+tbl+" (id, s) VALUES (1, 'd') ON DUPLICATE KEY UPDATE s = 'd'", "OTSParameterInvalid")
	})
	t.Run("transaction", func(t *testing.T) {
		tx, err := db.BeginTx(t.Context(), nil)
		if err == nil {
			_ = tx.Rollback()
			t.Fatal("BeginTx returned a transaction")
		}
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
		}
	})
}

// refused makes sure that the server refuses the statement with an error of the
// code code, and that the driver reports it as a *tablestore.Error. The refusal
// of the credential is another error, so none of these is one.
func refused(t *testing.T, db interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, query, code string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), query)
	var e *tablestore.Error
	if !errors.As(err, &e) {
		t.Fatalf("%s: the error is %v, want a *tablestore.Error", query, err)
	}
	if e.HTTPStatus != 400 {
		t.Errorf("%s: the status is %d, want 400", query, e.HTTPStatus)
	}
	if e.Code != code && e.Code != "OTSUnsupportOperation" {
		t.Errorf("%s: the code is %s, want %s or OTSUnsupportOperation", query, e.Code, code)
	}
	if errors.Is(err, dbimp.ErrAuthentication) {
		t.Errorf("%s: the refusal matches dbimp.ErrAuthentication", query)
	}
}
