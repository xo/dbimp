# D142. rqlite runs one statement a request

Status: Decided.

Ken decided this on 2026-09-30, at step 9 of the rqlite driver. This is
items 5 and 8 of step 9 of [DRIVER.md](../DRIVER.md) for rqlite, and the
endpoints.

- Each call of `database/sql` sends one statement, as the one element of
  the array. `QueryContext` sends it to `/db/request`, so that a write with
  `RETURNING` returns its rows, which `/db/query` refuses (recorded).
  `ExecContext` sends it to `/db/execute`. With `WithReadonly(true)`, a
  query goes to `/db/query`, whose read-only connection refuses a write.
  A user with only the permission `query` can then run only such queries.
- Every request asks for `blob_array`, because only that form tells a BLOB
  from text. No request asks for `associative`, which sorts the columns.
- The server sends the whole answer at once (RQLITE.md, Responses). The
  driver reads it one token at a time (D25). An `error` in the element of
  the statement is the error of the query, before any row, because the
  server reads every row before it answers. HTTP 500, which an infinity
  gives, is an error with the text of the body.
- A NULL is nil. SQLite has no value that is missing, so NULL and missing
  are one value (item 5, D18).
- `RowsAffected` and `LastInsertId` are the values of the server for a
  statement whose first word, after any comment, is `INSERT`, `UPDATE`,
  `DELETE`, `REPLACE` or `WITH`. For any other statement, such as
  `CREATE TABLE`, both are 0, because the server gives the counts of the
  last write before it (recorded).
