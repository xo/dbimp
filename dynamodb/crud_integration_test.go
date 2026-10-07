package dynamodb_test

import "testing"

// TestIntegrationCRUD inserts, selects, updates and deletes items in three
// tables, and compares each value that a select returns with the value that
// the test wrote (step 14a of docs/DRIVER.md). The tables have no foreign
// key, because DynamoDB has none (the entry foreign key of Schema). The
// names of the subtests are the names of the entries of features.json.
func TestIntegrationCRUD(t *testing.T) {
	crud := q(table(t, "crud", attrs(true)))
	plain := q(table(t, "plain", attrs(false)))
	indexed := schemaTable(t, "indexed")
	db := openAs(t, admin)

	t.Run("insert", func(t *testing.T) {
		must(t, db, "INSERT INTO "+crud+" VALUE {'pk': 'a', 'sk': 1, 'v': 'one'}")
		must(t, db, "INSERT INTO "+crud+" VALUE {'pk': 'a', 'sk': 2, 'v': 'two', 'other': 'x'}")
		must(t, db, "INSERT INTO "+plain+" VALUE {'pk': ?, 'n': ?, 'tags': <<'x', 'y'>>}", "p1", int64(7))
		must(t, db, "INSERT INTO "+plain+" VALUE {'pk': 'p2'}")
		must(t, db, "INSERT INTO "+indexed+" VALUE {'pk': 'i', 'sk': 1, 'g': 'gv', 'l': 'lv', 'v': 'iv'}")
		refused(t, db, "DuplicateItem", "INSERT INTO "+crud+" VALUE {'pk': 'a', 'sk': 1}")
		refused(t, db, "ValidationException", "INSERT INTO "+crud+" VALUE {'pk': 'e', 'sk': 1}, {'pk': 'e', 'sk': 2}")
	})
	t.Run("select", func(t *testing.T) {
		same(t, "the rows of a, in the order of the sort key",
			rowsOf(t, db, "SELECT pk, sk, v, other FROM "+crud+" WHERE pk = 'a'"),
			`S("a") N(1) S("one") nil`, `S("a") N(2) S("two") S("x")`)
		same(t, "ascending by the sort key",
			rowsOf(t, db, "SELECT sk FROM "+crud+" WHERE pk = 'a' ORDER BY sk"),
			"N(1)", "N(2)")
		// DESC on a sort key fails with InternalFailure on DynamoDB Local
		// 3.3.1 when an item matches (measured on 2026-10-07), so the
		// descending order is read on the key of a table with a hash key, as
		// the recording does (recorded: "crud: order by the key of a hash
		// table").
		same(t, "descending by the hash key",
			rowsOf(t, db, "SELECT pk FROM "+plain+" WHERE pk IN ['p1', 'p2'] ORDER BY pk DESC"),
			`S("p2")`, `S("p1")`)
		same(t, "the whole item", rowsOf(t, db, "SELECT * FROM "+crud+" WHERE pk = 'a' AND sk = 2"),
			`{other:S("x") pk:S("a") sk:N(2) v:S("two")}`)
		same(t, "a table with a hash key", rowsOf(t, db, "SELECT * FROM "+plain+" WHERE pk = ?", "p1"),
			`{n:N(7) pk:S("p1") tags:[S("x") S("y")]}`)
		same(t, "a read through the index", rowsOf(t, db, "SELECT pk, g, v FROM "+indexed+" WHERE pk = 'i' AND sk = 1"),
			`S("i") S("gv") S("iv")`)
	})
	t.Run("update", func(t *testing.T) {
		must(t, db, "UPDATE "+crud+" SET v = 'uno' WHERE pk = 'a' AND sk = 1")
		same(t, "after the update", rowsOf(t, db, "SELECT v FROM "+crud+" WHERE pk = 'a' AND sk = 1"), `S("uno")`)
		must(t, db, "UPDATE "+crud+" SET v = ? WHERE pk = ? AND sk = ?", "bound", "a", int64(1))
		same(t, "after the update with arguments", rowsOf(t, db, "SELECT v FROM "+crud+" WHERE pk = 'a' AND sk = 1"), `S("bound")`)
		must(t, db, "UPDATE "+crud+" SET v = 'uno' WHERE pk = 'a' AND sk = 1")
	})
	t.Run("update_returning", func(t *testing.T) {
		same(t, "the old item", rowsOf(t, db, "UPDATE "+crud+" SET v = 'one' WHERE pk = 'a' AND sk = 1 RETURNING ALL OLD *"),
			`{pk:S("a") sk:N(1) v:S("uno")}`)
		same(t, "what changed", rowsOf(t, db, "UPDATE "+crud+" SET v = 'eins' WHERE pk = 'a' AND sk = 1 RETURNING MODIFIED NEW *"),
			`{v:S("eins")}`)
		same(t, "the new item", rowsOf(t, db, "UPDATE "+crud+" SET v = 'uno' WHERE pk = 'a' AND sk = 1 RETURNING ALL NEW *"),
			`{pk:S("a") sk:N(1) v:S("uno")}`)
	})
	t.Run("delete", func(t *testing.T) {
		must(t, db, "DELETE FROM "+crud+" WHERE pk = 'a' AND sk = 2")
		same(t, "after the delete", rowsOf(t, db, "SELECT sk FROM "+crud+" WHERE pk = 'a'"), "N(1)")
		// A delete of a missing item succeeds (recorded: "crud: delete a row
		// that does not exist").
		must(t, db, "DELETE FROM "+crud+" WHERE pk = 'none' AND sk = 1")
		refused(t, db, "ValidationException", "DELETE FROM "+crud+" WHERE pk = 'a'")
	})
	t.Run("delete_returning", func(t *testing.T) {
		same(t, "the deleted item", rowsOf(t, db, "DELETE FROM "+crud+" WHERE pk = 'a' AND sk = 1 RETURNING ALL OLD *"),
			`{pk:S("a") sk:N(1) v:S("uno")}`)
		same(t, "after the delete", rowsOf(t, db, "SELECT sk FROM "+crud+" WHERE pk = 'a'"))
	})
	t.Run("insert_returning", func(t *testing.T) {
		refused(t, db, "ValidationException", "INSERT INTO "+crud+" VALUE {'pk': 'b', 'sk': 1} RETURNING ALL OLD *")
		same(t, "no item is inserted", rowsOf(t, db, "SELECT pk FROM "+crud+" WHERE pk = 'b'"))
	})
	t.Run("upsert", func(t *testing.T) {
		refused(t, db, "ValidationException", "UPSERT INTO "+crud+" VALUE {'pk': 'd', 'sk': 1}")
		refused(t, db, "ValidationException", "REPLACE INTO "+crud+" VALUE {'pk': 'd', 'sk': 1}")
	})
	t.Run("update_of_a_missing_item", func(t *testing.T) {
		refused(t, db, "ConditionalCheckFailedException", "UPDATE "+crud+" SET v = 'x' WHERE pk = 'none' AND sk = 1")
		same(t, "the item is not made", rowsOf(t, db, "SELECT pk FROM "+crud+" WHERE pk = 'none'"))
	})
	t.Run("conditional_write", func(t *testing.T) {
		must(t, db, "INSERT INTO "+crud+" VALUE {'pk': 'c', 'sk': 1, 'v': 'c', 'ss': <<'a'>>, 'l': [1]}")
		same(t, "a condition that holds", rowsOf(t, db, "UPDATE "+crud+" SET v = 'd' WHERE pk = 'c' AND sk = 1 AND attribute_exists(v) RETURNING ALL NEW *"),
			`{l:[N(1)] pk:S("c") sk:N(1) ss:[S("a")] v:S("d")}`)
		refused(t, db, "ConditionalCheckFailedException", "UPDATE "+crud+" SET v = 'x' WHERE pk = 'c' AND sk = 1 AND v = 'nomatch'")
		same(t, "a failed condition changes nothing", rowsOf(t, db, "SELECT v FROM "+crud+" WHERE pk = 'c' AND sk = 1"), `S("d")`)
	})
	t.Run("remove_an_attribute", func(t *testing.T) {
		same(t, "after REMOVE", rowsOf(t, db, "UPDATE "+crud+" REMOVE v WHERE pk = 'c' AND sk = 1 RETURNING ALL NEW *"),
			`{l:[N(1)] pk:S("c") sk:N(1) ss:[S("a")]}`)
		same(t, "a removed attribute is nil", rowsOf(t, db, "SELECT v FROM "+crud+" WHERE pk = 'c' AND sk = 1"), "nil")
		same(t, "IS MISSING finds it", rowsOf(t, db, "SELECT pk, sk FROM "+crud+" WHERE pk = 'c' AND v IS MISSING"), `S("c") N(1)`)
	})
	t.Run("add_to_a_set", func(t *testing.T) {
		same(t, "after set_add", rowsOf(t, db, "UPDATE "+crud+" SET ss = set_add(ss, <<'b'>>) WHERE pk = 'c' AND sk = 1 RETURNING ALL NEW *"),
			`{l:[N(1)] pk:S("c") sk:N(1) ss:[S("a") S("b")]}`)
	})
	t.Run("append_to_a_list", func(t *testing.T) {
		same(t, "after list_append", rowsOf(t, db, "UPDATE "+crud+" SET l = list_append(l, [2]) WHERE pk = 'c' AND sk = 1 RETURNING ALL NEW *"),
			`{l:[N(1) N(2)] pk:S("c") sk:N(1) ss:[S("a") S("b")]}`)
	})
	skipAlternator(t, "alternator_insert", "alternator_select", "alternator_update", "alternator_delete")
	t.Run("nothing is left", func(t *testing.T) {
		must(t, db, "DELETE FROM "+crud+" WHERE pk = 'c' AND sk = 1")
		must(t, db, "DELETE FROM "+plain+" WHERE pk = 'p1'")
		must(t, db, "DELETE FROM "+plain+" WHERE pk = 'p2'")
		must(t, db, "DELETE FROM "+indexed+" WHERE pk = 'i' AND sk = 1")
		for _, name := range []string{crud, plain, indexed} {
			same(t, name, rowsOf(t, db, "SELECT * FROM "+name))
		}
	})
}

// skipAlternator makes a subtest for each entry of features.json that names
// a flavor that the driver does not serve. Alternator runs no PartiQL, so D163
// drops it from the driver, and dbrun starts it only for the recordings.
func skipAlternator(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Skip("the driver serves no Alternator (D163)")
		})
	}
}
