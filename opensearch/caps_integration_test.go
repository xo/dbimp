package opensearch_test

import (
	"strconv"
	"sync"
	"testing"

	"github.com/xo/dbimp/opensearch"
)

// TestIntegrationCaps holds the caps that D163 names, with the size limit of the
// server lowered to 100 rows so that 300 rows pass it. A plain SELECT carries the
// page size, and the driver reads every row. A statement with no page size is cut
// at the size limit with no sign, and so is a LIMIT above it on the 3 series. Since
// dbmeta v0.4.0 the ordinary user of the 2 series reads a cursor too (recorded:
// "300 rows with a size limit of 100" and "a limit of 250 with a size limit of
// 100").
func TestIntegrationCaps(t *testing.T) {
	rows := rowsIndex(t)
	s := newAdminAPI(t)
	s.settingsOf(t, map[string]any{"plugins.query.size_limit": 100})
	forEach(t, func(t *testing.T, e *target) {
		count := func(query string, args ...any) int {
			t.Helper()
			_, got, err := e.read(t, query, args...)
			if err != nil {
				t.Fatalf("%s: %v", query, err)
			}
			return len(got)
		}
		if got := count("SELECT n FROM " + rows + " ORDER BY n"); got != 300 {
			t.Errorf("a plain SELECT read %d rows, want 300", got)
		}
		if got := count("SELECT n FROM "+rows+" ORDER BY n", opensearch.WithFetchSize(100)); got != 300 {
			t.Errorf("a plain SELECT with the page size 100 read %d rows, want 300", got)
		}
		if got := count("SELECT n FROM "+rows+" ORDER BY n", opensearch.WithParameter("fetch_size", 0)); got != 100 {
			t.Errorf("a plain SELECT with no page size read %d rows, want 100: the size limit", got)
		}
		wantLimit := 100
		if e.old {
			wantLimit = 250
		}
		if got := count("SELECT n FROM " + rows + " ORDER BY n LIMIT 250"); got != wantLimit {
			t.Errorf("a LIMIT of 250 read %d rows, want %d", got, wantLimit)
		}
	})
}

// bigName is the index of 10050 rows with a distinct text each, which bigIndex
// makes once for the run. It has 50 rows more than the size limit of the server
// and its limit on the groups of 3.9.0, which are 10000 by default.
var (
	bigOnce sync.Once
	bigName = prefix + "big"
)

// bigIndex makes the index of 10050 rows once for the run, and returns its name.
func bigIndex(t *testing.T) string {
	t.Helper()
	return fixture(t, &bigOnce, bigName, numberedRows(10050, func(i int) string { return "g" + strconv.Itoa(i) }))
}

// TestIntegrationDefaultCaps holds the caps of the server at their defaults, on an
// index of 10050 rows (D163). A plain SELECT carries the page size, and the driver
// reads every row. A statement with no page size, and a LIMIT above the cap on the
// 3 series, stop at 10000 rows with no sign. A GROUP BY and a DISTINCT stop at 1000
// groups on the 2 series and at 10000 groups on the 3 series, with no sign
// (recorded: "a group by of 1050 groups" and "300 rows with a size limit of 100").
// The ordinary user of the 2 series cannot read a cursor, so its plain SELECT stops
// at 10000 rows.
func TestIntegrationDefaultCaps(t *testing.T) {
	big := bigIndex(t)
	forEach(t, func(t *testing.T, e *target) {
		count := func(query string, args ...any) int {
			t.Helper()
			_, got, err := e.read(t, query, args...)
			if err != nil {
				t.Fatalf("%s: %v", query, err)
			}
			return len(got)
		}
		if got := count("SELECT n FROM " + big + " ORDER BY n"); got != 10050 {
			t.Errorf("a plain SELECT read %d rows, want 10050", got)
		}
		if got := count("SELECT n FROM "+big+" ORDER BY n", opensearch.WithParameter("fetch_size", 0)); got != 10000 {
			t.Errorf("a plain SELECT with no page size read %d rows, want 10000: the size limit", got)
		}
		wantLimit := 10050
		if !e.old {
			wantLimit = 10000
		}
		if got := count("SELECT n FROM " + big + " ORDER BY n LIMIT 10050"); got != wantLimit {
			t.Errorf("a LIMIT of 10050 read %d rows, want %d", got, wantLimit)
		}
		wantGroups := 10000
		if e.old {
			wantGroups = 1000
		}
		if got := count("SELECT s, COUNT(*) FROM " + big + " GROUP BY s"); got != wantGroups {
			t.Errorf("a GROUP BY of 10050 groups read %d groups, want %d", got, wantGroups)
		}
		if got := count("SELECT DISTINCT s FROM " + big); got != wantGroups {
			t.Errorf("a DISTINCT of 10050 values read %d values, want %d", got, wantGroups)
		}
	})
}
