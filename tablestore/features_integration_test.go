package tablestore_test

import (
	"math"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/tablestore"
)

// TestIntegrationFeatures tests the functions and the statements that
// docs/TABLESTORE.md names, and the refusals of the entries that the survey marks
// no. Each subtest is one entry of testdata/tablestore/features.json.
func TestIntegrationFeatures(t *testing.T) {
	db := connect(t)
	tbl := makeTable(t, db, "feat", tablestore.KeyInteger, "i BIGINT, s MEDIUMTEXT")
	exec(t, db, "INSERT INTO "+tbl+" (id, i, s) VALUES (1, 100, 'a')")
	exec(t, db, "INSERT INTO "+tbl+" (id, i, s) VALUES (2, 200, 'b')")
	exec(t, db, "INSERT INTO "+tbl+" (id, i, s) VALUES (3, 0, 'c')")

	t.Run("select @@version", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT @@version;")
		v, _ := got[0][0].(string)
		if !regexp.MustCompile(`^5\.7\.\d+-SQLFrontend`).MatchString(v) {
			t.Errorf("SELECT @@version gave %#v, want a version of the form 5.7.n-SQLFrontend", got)
		}
		// VERSION() is refused, so the driver does not answer it itself (D181 does
		// not apply).
		refused(t, db, "SELECT VERSION() AS v;", "OTSParameterInvalid")
	})
	t.Run("database function", func(t *testing.T) {
		refused(t, db, "SELECT DATABASE();", "OTSParameterInvalid")
	})
	t.Run("current user function", func(t *testing.T) {
		refused(t, db, "SELECT CURRENT_USER();", "OTSParameterInvalid")
	})
	t.Run("show databases", func(t *testing.T) {
		refused(t, db, "SHOW DATABASES;", "OTSUnsupportOperation")
	})
	t.Run("convert function", func(t *testing.T) {
		refused(t, db, "SELECT CONVERT(i, SIGNED) AS a FROM "+tbl+" WHERE id = 3;", "OTSParameterInvalid")
	})
	t.Run("concat function", func(t *testing.T) {
		if got := rowsOf(t, db, "SELECT CONCAT('n', i) AS a FROM "+tbl+" WHERE id = 1;"); !reflect.DeepEqual(got, [][]any{{"n100"}}) {
			t.Errorf("CONCAT gave %#v", got)
		}
	})
	t.Run("now function", func(t *testing.T) {
		// The server reads and writes the wall clock of Asia/Shanghai, and the driver
		// reads a DATETIME as the wall clock of that zone, so NOW() is the time now
		// there.
		// This is the test of the zone of the server (docs/TABLESTORE.md, "Types").
		got := rowsOf(t, db, "SELECT NOW() AS a;")
		dt, ok := got[0][0].(dbimp.LocalDateTime)
		if !ok {
			t.Fatalf("NOW() gave %#v, want a dbimp.LocalDateTime", got[0][0])
		}
		if diff := time.Since(dt.In(zone)).Abs(); diff > 5*time.Minute {
			t.Errorf("NOW() is %v, which is %v from the time now, so the zone of the server is not Asia/Shanghai", dt, diff)
		}
	})
	t.Run("current timestamp", func(t *testing.T) {
		if got := rowsOf(t, db, "SELECT CURRENT_TIMESTAMP AS a;"); len(got) != 1 {
			t.Errorf("CURRENT_TIMESTAMP gave %#v", got)
		}
	})
	t.Run("from unixtime function", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT FROM_UNIXTIME(i) AS a FROM "+tbl+" WHERE id = 3;")
		want := dbimp.LocalDateTime{Date: dbimp.Date{Year: 1970, Month: 1, Day: 1}, Time: dbimp.LocalTime{Hour: 8}}
		if !reflect.DeepEqual(got, [][]any{{want}}) {
			t.Errorf("FROM_UNIXTIME(0) gave %#v, want %v, which is the epoch at Asia/Shanghai", got, want)
		}
	})
	t.Run("date format function", func(t *testing.T) {
		if got := rowsOf(t, db, "SELECT DATE_FORMAT(FROM_UNIXTIME(i), '%Y') AS a FROM "+tbl+" WHERE id = 3;"); !reflect.DeepEqual(got, [][]any{{"1970"}}) {
			t.Errorf("DATE_FORMAT gave %#v", got)
		}
	})
	t.Run("statement timeout hint", func(t *testing.T) {
		// The server accepts the hint. A test did not show that it stops a statement,
		// so the driver does not offer WithTimeout (D224).
		if got := rowsOf(t, db, "/*+ MAX_EXECUTION_TIME(1000) */ SELECT 1 AS a;"); !reflect.DeepEqual(got, [][]any{{int64(1)}}) {
			t.Errorf("the hint gave %#v", got)
		}
	})
	t.Run("start transaction", func(t *testing.T) {
		refused(t, db, "START TRANSACTION;", "OTSUnsupportOperation")
		refused(t, db, "COMMIT;", "OTSUnsupportOperation")
		refused(t, db, "ROLLBACK;", "OTSUnsupportOperation")
	})
	t.Run("set autocommit", func(t *testing.T) {
		refused(t, db, "SET AUTOCOMMIT=0;", "OTSUnsupportOperation")
	})
	t.Run("time series name of a table", func(t *testing.T) {
		refused(t, db, "SELECT * FROM "+tbl+"::multi_model LIMIT 1;", "OTSParameterInvalid")
	})
	t.Run("limit with an offset", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT id FROM "+tbl+" ORDER BY id LIMIT 1, 2;")
		if !reflect.DeepEqual(got, [][]any{{int64(2)}, {int64(3)}}) {
			t.Errorf("LIMIT 1, 2 gave %#v, want the ids 2 and 3", got)
		}
	})
	t.Run("two statements", func(t *testing.T) {
		refused(t, db, "SELECT 1 AS a; SELECT 2 AS a;", "OTSParameterInvalid")
	})
	t.Run("a number the table cannot hold", func(t *testing.T) {
		for _, v := range []float64{math.NaN(), math.Inf(1)} {
			_, err := db.ExecContext(t.Context(), "INSERT INTO "+tbl+" (id, i) VALUES (9, ?)", v)
			if err == nil {
				t.Errorf("INSERT of %v returned no error", v)
			}
		}
	})
}
