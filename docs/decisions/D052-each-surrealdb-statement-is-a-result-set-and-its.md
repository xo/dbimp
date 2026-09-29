# D52. Each SurrealDB statement is a result set, and its rows follow D18

Status: Amended by D101.

D101 amends the rule for an array of mixed kinds: it is one column only
when its first value is not an object.

Ken accepted this on 2026-09-27. It decides items 5 and 8 of step 9 for SurrealDB. The response holds one
result for each statement of the request, in order. Each one is a result
set, which `Rows.NextResultSet` moves to (`driver.RowsNextResultSet`).

A result becomes rows by D18. SurrealDB sends no column metadata, so rule 1
never applies:

- An array of objects is one row for each object, and the columns are the
  keys of the first object, by rule 2. A key that a later object lacks is
  nil. A key that only a later object has is `ErrExtraColumn`, so a
  `SELECT *` over records with different fields fails at the first record
  that has a new field. Naming the fields in the statement avoids it.
- An array of other values, or of values of mixed kinds, is one column with
  the name `""`, and one row for each value, by rule 3.
- A result that is not an array, such as the result of `RETURN 1` or of
  `SELECT ... FROM ONLY`, is one row. An object gives its keys as the
  columns, and any other value gives one column with the name `""`.

The server sorts the keys of every object, so `SELECT b, a` gives the
columns `a` and `b` (measured on 2.7.0 and 3.3.0). The order of the
statement never reaches the client, so the driver keeps the order that the
server sends, as the Couchbase driver does on 7.2.

NONE and NULL are both nil for `database/sql` (item 5). CBOR tells them
apart (tag 6), but a caller of `database/sql` has one nil.
