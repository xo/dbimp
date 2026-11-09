package hive_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/hive"
)

// open opens a database on a replay of the statement.
func open(t *testing.T, release, statement string) *sql.DB {
	t.Helper()
	db, err := sql.Open(hive.Name, replay(t, release, statement))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestReplaySelectOne(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := open(t, release, "select 1")
			var n int64
			if err := db.QueryRowContext(t.Context(), "select 1").Scan(&n); err != nil || n != 1 {
				t.Fatalf("select 1 = %d, %v", n, err)
			}
		})
	}
}

func TestReplayNamesAndOrder(t *testing.T) {
	t.Parallel()
	const stmt = "select 2 as b, 1 as a, 3 as `weird name`"
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := open(t, release, stmt)
			rows, err := db.QueryContext(t.Context(), stmt)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			cols, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"b", "a", "weird name"}
			if len(cols) != 3 || cols[0] != want[0] || cols[1] != want[1] || cols[2] != want[2] {
				t.Errorf("columns = %q, want %q (D18)", cols, want)
			}
			if err := rows.Err(); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestReplayTypes reads the row of every type that the recording of Hive holds,
// and checks the Go type of each value (D234).
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	var stmt string
	for _, r := range load(t, "hive-4.2.1") {
		if r.call == "ExecuteStatement" && len(r.stmt) > 12 && r.stmt[:12] == "select cast(" && containsAll(r.stmt, "c_tinyint", "c_struct") {
			stmt = r.stmt
			break
		}
	}
	if stmt == "" {
		t.Fatal("no recorded statement of every type")
	}
	db := open(t, "hive-4.2.1", stmt)
	rows, err := db.QueryContext(t.Context(), stmt)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatal(err)
	}
	types, _ := rows.ColumnTypes()
	dec, _, err := apd.NewFromString("1.50")
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range cols {
		got := vals[i]
		switch c {
		case "c_tinyint", "c_smallint", "c_int", "c_bigint":
			if _, ok := got.(int64); !ok {
				t.Errorf("%s = %T, want int64", c, got)
			}
		case "c_float", "c_double":
			if _, ok := got.(float64); !ok {
				t.Errorf("%s = %T, want float64", c, got)
			}
		case "c_decimal":
			d, ok := got.(*apd.Decimal)
			if !ok || d.Cmp(dec) != 0 || d.String() != "1.50" {
				t.Errorf("c_decimal = %v (%T), want 1.50 with its scale (D228)", got, got)
			}
		case "c_boolean":
			if got != true {
				t.Errorf("c_boolean = %v, want true", got)
			}
		case "c_binary":
			if b, ok := got.([]byte); !ok || string(b) != "ab" {
				t.Errorf("c_binary = %v, want ab", got)
			}
		case "c_date":
			if d, ok := got.(dbimp.Date); !ok || d.String() != "2026-01-02" {
				t.Errorf("c_date = %v (%T), want 2026-01-02", got, got)
			}
		case "c_timestamp":
			if d, ok := got.(dbimp.LocalDateTime); !ok || d.Time.Nanosecond != 123456789 {
				t.Errorf("c_timestamp = %v (%T), want the nanoseconds 123456789", got, got)
			}
		case "c_array":
			if a, ok := got.([]any); !ok || len(a) != 2 {
				t.Errorf("c_array = %v (%T), want a slice of 2", got, got)
			}
		case "c_map", "c_struct":
			if _, ok := got.(map[string]any); !ok {
				t.Errorf("%s = %T, want map[string]any", c, got)
			}
		case "c_char":
			if got != "ab  " {
				t.Errorf("c_char = %q, want a padded value", got)
			}
		}
		if types[i].DatabaseTypeName() == "" {
			t.Errorf("%s has no database type name", c)
		}
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

// TestReplayPages reads 1000 rows in pages of 100 (D235).
func TestReplayPages(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			var stmt string
			for _, r := range load(t, release) {
				if r.call == "ExecuteStatement" && strings.Contains(r.stmt, "a.x*100+b.x*10+c.x") {
					stmt = r.stmt
					break
				}
			}
			db := open(t, release, stmt)
			rows, err := db.QueryContext(hive.WithOptions(t.Context(), hive.WithFetchSize(100)), stmt)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			n := 0
			for rows.Next() {
				var v int64
				if err := rows.Scan(&v); err != nil {
					t.Fatal(err)
				}
				n++
			}
			if err := rows.Err(); err != nil || n != 1000 {
				t.Errorf("read %d rows, %v, want 1000", n, err)
			}
		})
	}
}

// TestReplayErrors reads the errors that the servers recorded.
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		release, stmt, state, text string
	}{
		{"hive-4.2.1", "select nope from nothing", "42S02", "Table not found"},
		{"hive-4.2.1", "selct 1", "42000", "ParseException"},
		{"impala-4.5.3", "select nope from nothing", "HY000", "AnalysisException"},
	} {
		t.Run(tt.release+" "+tt.stmt, func(t *testing.T) {
			t.Parallel()
			db := open(t, tt.release, tt.stmt)
			_, err := db.ExecContext(t.Context(), tt.stmt)
			e, ok := errors.AsType[*hive.Error](err)
			if !ok {
				t.Fatalf("the error is %v (%T), want a *hive.Error", err, err)
			}
			if e.SQLState != tt.state || !strings.Contains(e.Message, tt.text) {
				t.Errorf("the error is %q %q, want %q and a message with %q", e.SQLState, e.Message, tt.state, tt.text)
			}
			if errors.Is(err, dbimp.ErrAuthentication) {
				t.Error("a statement error matches dbimp.ErrAuthentication")
			}
		})
	}
}

// TestReplayRunErrorOfHive reads the error of a statement that compiles and fails
// while it runs, which GetOperationStatus reports.
func TestReplayRunErrorOfHive(t *testing.T) {
	t.Parallel()
	const stmt = "select assert_true(false)"
	db := open(t, "hive-4.2.1", stmt)
	_, err := db.ExecContext(t.Context(), stmt)
	e, ok := errors.AsType[*hive.Error](err)
	if !ok || !strings.Contains(e.Message, "ASSERT_TRUE") {
		t.Fatalf("the error is %v, want a *hive.Error about ASSERT_TRUE", err)
	}
}

// TestReplayRowsAffected reads the count of rows that Hive says, and the error
// that Impala makes for it (docs/HIVE.md, Responses).
func TestReplayRowsAffected(t *testing.T) {
	t.Parallel()
	const stmt = "insert into dbimp_hive_t values (1,'a'),(2,'b'),(3,'c')"
	db := open(t, "hive-4.2.1", stmt)
	res, err := db.ExecContext(t.Context(), stmt)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 3 {
		t.Errorf("RowsAffected() = %d, %v, want 3", n, err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId() = %v, want dbimp.ErrNotSupported", err)
	}
	idb := open(t, "impala-4.5.3", stmt)
	res, err = idb.ExecContext(t.Context(), stmt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected() on Impala = %v, want dbimp.ErrNotSupported", err)
	}
}

// TestReplayNoResultSet runs a statement that has no result set, and the driver
// fetches nothing for it (docs/HIVE.md, Responses).
func TestReplayNoResultSet(t *testing.T) {
	t.Parallel()
	const stmt = "create table dbimp_hive_t (id int)"
	db := open(t, "hive-4.2.1", stmt)
	rows, err := db.QueryContext(t.Context(), stmt)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Error("a statement with no result set returned a row")
	}
	if err := rows.Err(); err != nil {
		t.Errorf("Err() = %v", err)
	}
}

// TestReplayRefusesAnOption holds the options that the servers cannot honor.
func TestReplayRefusesAnOption(t *testing.T) {
	t.Parallel()
	db := open(t, "hive-4.2.1", "select 1")
	for name, opt := range map[string]hive.Option{
		"WithReadonly": hive.WithReadonly(true),
		"WithDatabase": hive.WithDatabase("x"),
	} {
		_, err := db.ExecContext(hive.WithOptions(t.Context(), opt), "select 1")
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: %v, want dbimp.ErrNotSupported", name, err)
		}
	}
}
