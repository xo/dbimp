package cosmos

import "testing"

func TestParseCatalog(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		statement string
		catalog   bool
		name      string
		where     map[string]string
	}{
		{`SELECT * FROM "$databases"`, true, "$databases", map[string]string{}},
		{`select * from $databases;`, true, "$databases", map[string]string{}},
		{"SELECT *\n FROM \"$containers\" -- the list\n WHERE database = 'a''b' AND container = 'c\\'d'", true, "$containers", map[string]string{"database": "a'b", "container": "c'd"}},
		{`SELECT * FROM "$offers" WHERE Database = 'x'`, true, "$offers", map[string]string{"database": "x"}},
		{`SELECT * FROM c`, false, "", nil},
		{`SELECT * FROM "$unknown"`, false, "", nil},
		{`SELECT c.id FROM c WHERE c.name = '$databases'`, false, "", nil},
		{`SELECT VALUE COUNT(1) FROM c`, false, "", nil},
		{`SELECT * FROM c WHERE c.a < 1`, false, "", nil},
		{`SELECT`, false, "", nil},
		{``, false, "", nil},
		{`INSERT INTO "$databases"`, false, "", nil},
	} {
		cs, ok, err := parseCatalog(tt.statement, nil)
		if ok != tt.catalog || (ok && err != nil) {
			t.Errorf("parseCatalog(%q) = %v, %v, want %v and no error", tt.statement, ok, err, tt.catalog)
			continue
		}
		if ok && (cs.catalog.Name != tt.name || len(cs.where) != len(tt.where)) {
			t.Errorf("parseCatalog(%q) = %s %v, want %s %v", tt.statement, cs.catalog.Name, cs.where, tt.name, tt.where)
			continue
		}
		for k, v := range tt.where {
			if cs.where[k] != v {
				t.Errorf("parseCatalog(%q): %s is %q, want %q", tt.statement, k, cs.where[k], v)
			}
		}
	}
}

// TestTheCatalogsAreComplete holds that each reserved name of D190 item 17 has a
// catalog with a fetch function and columns.
func TestTheCatalogsAreComplete(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, c := range catalogs() {
		if seen[c.Name] || c.fetch == nil || len(c.Columns) == 0 {
			t.Errorf("the catalog %s is repeated, or has no fetch or no column", c.Name)
		}
		seen[c.Name] = true
	}
	for _, name := range []string{"$databases", "$containers", "$stored_procedures", "$triggers", "$functions", "$offers", "$users", "$permissions", "$account"} {
		if !seen[name] {
			t.Errorf("the catalog %s is missing (D190 item 17)", name)
		}
	}
}
