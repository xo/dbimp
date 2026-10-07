package dynamodb_test

import (
	"fmt"
	"testing"
)

// schemaTable makes the table of the schema tests: a string hash key, a
// number range key, a global secondary index gsi on g, and a local secondary
// index lsi on l (recorded: "schema: a table with a sort key and indexes"). It
// returns the quoted name.
func schemaTable(t *testing.T, name string) string {
	t.Helper()
	body := attrs(true)
	body["AttributeDefinitions"] = []any{
		map[string]any{"AttributeName": "pk", "AttributeType": "S"},
		map[string]any{"AttributeName": "sk", "AttributeType": "N"},
		map[string]any{"AttributeName": "g", "AttributeType": "S"},
		map[string]any{"AttributeName": "l", "AttributeType": "S"},
	}
	body["GlobalSecondaryIndexes"] = []any{map[string]any{
		"IndexName":  "gsi",
		"KeySchema":  []any{map[string]any{"AttributeName": "g", "KeyType": "HASH"}},
		"Projection": map[string]any{"ProjectionType": "ALL"},
	}}
	body["LocalSecondaryIndexes"] = []any{map[string]any{
		"IndexName": "lsi",
		"KeySchema": []any{
			map[string]any{"AttributeName": "pk", "KeyType": "HASH"},
			map[string]any{"AttributeName": "l", "KeyType": "RANGE"},
		},
		"Projection": map[string]any{"ProjectionType": "ALL"},
	}}
	return q(table(t, name, body))
}

// TestIntegrationSchema makes tables, keys, indexes and a time to live
// through the API, because PartiQL has no DDL (D171), and reads them through
// the driver. The names of the subtests are the names of the entries of
// features.json.
func TestIntegrationSchema(t *testing.T) {
	adminConfig(t)
	name := table(t, "schema", attrs(true))
	db := openAs(t, admin)

	t.Run("table", func(t *testing.T) {
		d := call(t, "DescribeTable", map[string]any{"TableName": name})
		desc, _ := d["Table"].(map[string]any)
		if desc["TableName"] != name || desc["TableStatus"] != "ACTIVE" {
			t.Errorf("DescribeTable = %v", desc)
		}
		must(t, db, "INSERT INTO "+q(name)+" VALUE {'pk': 'a', 'sk': 1}")
		same(t, "a read of the table", rowsOf(t, db, "SELECT pk, sk FROM "+q(name)), `S("a") N(1)`)
		must(t, db, "DELETE FROM "+q(name)+" WHERE pk = 'a' AND sk = 1")
	})
	t.Run("primary_key", func(t *testing.T) {
		d := call(t, "DescribeTable", map[string]any{"TableName": name})
		keys, _ := d["Table"].(map[string]any)["KeySchema"].([]any)
		var got []string
		for _, k := range keys {
			m, _ := k.(map[string]any)
			got = append(got, fmt.Sprint(m["AttributeName"], ":", m["KeyType"]))
		}
		same(t, "the key, hash first", got, "pk:HASH", "sk:RANGE")
		// A statement that writes names the whole key.
		refused(t, db, "ValidationException", "UPDATE "+q(name)+" SET v = 'x' WHERE pk = 'a'")
		refused(t, db, "ValidationException", "DELETE FROM "+q(name)+" WHERE pk = 'a'")
	})
	t.Run("global_secondary_index", func(t *testing.T) {
		idx := schemaTable(t, "gsi")
		must(t, db, "INSERT INTO "+idx+" VALUE {'pk': 'a', 'sk': 1, 'g': 'gv', 'l': 'lv'}")
		same(t, "a read through the global index", rowsOf(t, db, "SELECT pk, g FROM "+idx+"."+q("gsi")+" WHERE g = 'gv'"), `S("a") S("gv")`)
		// A write through an index fails (recorded: "schema: write through an
		// index").
		refused(t, db, "ValidationException", "INSERT INTO "+idx+"."+q("gsi")+" VALUE {'pk': 'b', 'sk': 1, 'g': 'x'}")
		must(t, db, "DELETE FROM "+idx+" WHERE pk = 'a' AND sk = 1")
	})
	t.Run("local_secondary_index", func(t *testing.T) {
		idx := schemaTable(t, "lsi")
		must(t, db, "INSERT INTO "+idx+" VALUE {'pk': 'a', 'sk': 1, 'g': 'gv', 'l': 'lv'}")
		same(t, "a read through the local index", rowsOf(t, db, "SELECT pk, l FROM "+idx+"."+q("lsi")+" WHERE pk = 'a' AND l = 'lv'"), `S("a") S("lv")`)
		must(t, db, "DELETE FROM "+idx+" WHERE pk = 'a' AND sk = 1")
	})
	t.Run("time_to_live", func(t *testing.T) {
		call(t, "UpdateTimeToLive", map[string]any{
			"TableName":               name,
			"TimeToLiveSpecification": map[string]any{"Enabled": true, "AttributeName": "expires"},
		})
		d := call(t, "DescribeTimeToLive", map[string]any{"TableName": name})
		desc, _ := d["TimeToLiveDescription"].(map[string]any)
		if desc["AttributeName"] != "expires" || (desc["TimeToLiveStatus"] != "ENABLED" && desc["TimeToLiveStatus"] != "ENABLING") {
			t.Errorf("DescribeTimeToLive = %v", desc)
		}
	})
	// PartiQL has no DDL, so a statement that makes a table, an index, a
	// view, a constraint or a default is refused (D171).
	for _, tt := range []struct{ name, query string }{
		{"table_in_a_statement", "CREATE TABLE " + q(prefix+"ddl") + " (pk VARCHAR PRIMARY KEY)"},
		{"index_in_a_statement", "CREATE INDEX dbimp_idx ON " + q(name) + " (v)"},
		{"foreign_key", "CREATE TABLE " + q(prefix+"ddl") + " (pk VARCHAR PRIMARY KEY, ref VARCHAR REFERENCES " + q(name) + " (pk))"},
		{"unique_constraint", "ALTER TABLE " + q(name) + " ADD CONSTRAINT u UNIQUE (v)"},
		{"view", "CREATE VIEW " + q(prefix+"view") + " AS SELECT * FROM " + q(name)},
		{"default_value", "CREATE TABLE " + q(prefix+"ddl") + " (pk VARCHAR PRIMARY KEY, v VARCHAR DEFAULT 'x')"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			refused(t, db, "ValidationException", tt.query)
		})
	}
}
