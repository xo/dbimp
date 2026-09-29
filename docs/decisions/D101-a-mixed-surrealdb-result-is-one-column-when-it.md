# D101. A mixed SurrealDB result is one column when it starts with a value

Status: Amends D52.

Ken decided on 2026-09-29, in the review of D97, that a decision whose
conclusion is wrong gets an amending decision. D52 said that the result of
a SurrealDB statement that is an array "of values of mixed kinds" is one
column. The driver has never done that for every such array, and the code
is the rule that stays:

- The shape comes from the first value of the result, by D18.
- If the first value is not an object, the result is one column, named
  `""`, and a later object is a value of that column.
- If the first value is an object, its keys are the columns. A later value
  that is not an object is an error that wraps `dbimp.ErrColumnCount`
  (`surrealdb/rows.go`).

D89 states the same rule for ArangoDB.
