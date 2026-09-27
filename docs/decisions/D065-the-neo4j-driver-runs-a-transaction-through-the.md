# D65. The Neo4j driver runs a transaction through the tx endpoints

Status: Amended by D69.

D69 amends the last point: the transaction keeps the context of `BeginTx`.

Ken accepted this on 2026-09-27. It decides item 7 of step 9 for Neo4j.
`BeginTx` sends `POST /db/<database>/query/v2/tx` with no statement, and keeps
the id that the server returns. Each statement of the transaction goes to
`/query/v2/tx/<id>`. `Commit` sends `POST /query/v2/tx/<id>/commit`, and
`Rollback` sends `DELETE /query/v2/tx/<id>` (measured).

- `sql.TxOptions.ReadOnly` sends `"accessMode": "READ"` with the begin, and
  the server then refuses a write with `Neo.ClientError.Statement.AccessMode`
  (measured).
- An isolation level other than the default is refused, because the Query
  API takes none.
- An error in a statement ends the transaction on the server, and the next
  request to it is HTTP 404 (measured). So after an error, `Commit` returns
  that error, and `Rollback` returns nil.
- The server rolls back a transaction that nothing touches for 60 seconds
  (measured). The driver sends nothing to keep it alive, and a later request
  returns the error of HTTP 404.
- The transaction keeps no context. Each request uses the context of its
  own call (D8).
