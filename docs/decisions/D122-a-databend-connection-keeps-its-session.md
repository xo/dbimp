# D122. A Databend connection keeps its session

Status: Decided.

Ken decided this on 2026-09-29, at step 9 of the Databend driver.
The server keeps no session for a client of HTTP. Each response returns
`session`, with the database, the role and the settings that the statement
left, such as after `USE` or `SET` ([DATABEND.md](../DATABEND.md)).

- A connection keeps the `session` of its last response, and sends it with
  its next statement, so `USE` and `SET` hold for the rest of the
  connection, as they do on a server with sessions.
- `ResetSession` sets it back to the session of the DSN, which names the
  database and the settings of D117 and D118. `database/sql` calls it before
  it reuses the connection. This is a reason for `ResetSession` that D102
  does not cover, because D102 is about a transaction.
- In a transaction (D121), `txn_state` `Fail`, after an error, makes
  `Commit` return the error that ended the transaction, and `Rollback` sends
  `ROLLBACK`, which the server needs to end it (measured).
- The transaction keeps the context of `BeginTx` for `Commit` and
  `Rollback`, as D69 does for Neo4j, and `Rollback` sends its request after
  that context ends, as D100 does. Hard rule 4 of AGENTS.md then names this
  exception.
- The driver sends no cookie, so a temporary table stays unsupported: the
  server refuses one without a cookie (measured). A caller who needs one
  asks Ken for a key that turns the cookie on.
