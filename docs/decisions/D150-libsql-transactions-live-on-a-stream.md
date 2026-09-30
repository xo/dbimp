# D150. libSQL transactions live on a stream

Status: Decided.

Ken decided this on 2026-10-01, at step 9 of the libSQL driver. This is item
7 of step 9 of [DRIVER.md](../DRIVER.md) for libSQL.

- `BeginTx` sends `BEGIN` on a new stream. Each statement of the
  transaction sends the baton of the last answer, so it runs on the same
  SQLite connection. `Commit` sends `COMMIT` and `close`, and `Rollback`
  sends `ROLLBACK` and `close` (LIBSQL.md, Transactions). The transaction
  keeps its baton, and the context of `BeginTx`, because `Commit` and
  `Rollback` of `driver.Tx` take none, as a transaction of Couchbase, Neo4j,
  ArangoDB and Databend keeps it (hard rule 4).
- A stream expires after 10 seconds with no request, and that rolls back
  its transaction (recorded). The error `STREAM_EXPIRED` then says that the
  transaction was rolled back, and it is never `driver.ErrBadConn`, because
  `database/sql` would run the statement again outside the transaction.
- `ReadOnly` fails with `dbimp.ErrNotSupported`, because the server takes an
  `INSERT` inside `BEGIN TRANSACTION READONLY` (recorded). So does any
  isolation level but the default.
- One transaction runs one statement at a time, because the client waits
  for each answer before it sends the next request on a stream (the Hrana
  spec).
