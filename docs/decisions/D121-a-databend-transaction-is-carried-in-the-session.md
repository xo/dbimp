# D121. A Databend transaction is carried in the session

Status: Decided.

Ken decided on 2026-09-29 that the Databend driver has transactions, as
item 7 of step 9 of [DRIVER.md](../DRIVER.md). The client carries a
transaction: each response returns `session`, with `txn_state` and
`internal`, and the next statement sends them back
([DATABEND.md](../DATABEND.md)).

- `BeginTx` sends `BEGIN`, and each later statement of the connection sends
  the `session` of the last response, until `Commit` sends `COMMIT` or
  `Rollback` sends `ROLLBACK`.
- A DDL statement commits the transaction. When a response inside the
  transaction returns a `txn_state` other than `Active`, the driver marks
  the transaction ended. `Commit` then returns an error that says so, and
  sends nothing, as D65 does for Neo4j. `Rollback` returns nil, because
  nothing is left to roll back.
- `ReadOnly` and an isolation level other than the default fail with
  `dbimp.ErrNotSupported`, because the server has neither.

D122 proposes the rest: the state `Fail`, and the context of `BeginTx`.
