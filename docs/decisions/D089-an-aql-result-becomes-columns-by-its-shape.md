# D89. An AQL result becomes columns by its shape

Status: Decided.

Ken decided on 2026-09-29 how the ArangoDB driver turns a result into
columns. This is item 5 of step 9 of [DRIVER.md](../DRIVER.md). The cursor
API sends no list of columns, and each value of `result` is what `RETURN`
gave (measured on 3.12.12). So rule 1 of D18 never applies:

- If the first value is an object that starts with the keys `_key`, `_id`
  and `_rev`, it is a stored document. If it starts with `_key`, `_id`,
  `_from`, `_to` and `_rev`, it is a stored edge. Both are measured, the
  edge on 2026-09-29, after a review found that no recording held one. The
  result is one column, named `""`, that holds each value, by rule 3 of D18.
  Documents of one collection can have different attributes (measured), and
  rule 3 keeps them whole.
- If the first value is another object, such as the result of
  `RETURN {a: u.a, b: u.b}`, its keys are the columns, in the order of the
  query (measured), by rule 2 of D18. A key that a later row lacks is nil,
  and a key that only a later row has is `dbimp.ErrExtraColumn`.
- A scalar, an array, or a result whose values have different shapes and
  whose first value is not an object, is one column named `""`, as D101
  says for SurrealDB.
- The driver cannot tell an object that the query builds from an attribute
  that holds one. So `RETURN d.address`, where each address is an object,
  gives the keys of the first address as the columns, and an empty object
  gives no columns. A query that wants the object whole names it:
  `RETURN {address: d.address}`.
- The shape comes from the first value. A later value that is not an object,
  in a result whose first value is one, is an error.
- A result with no rows has no columns. A write with no `RETURN` gives an
  empty `result` (measured).

A JSON `null` is nil, and so is an attribute that a projection names and a
document lacks, because AQL reads it as `null` (measured).
