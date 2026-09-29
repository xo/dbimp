# D91. An ArangoDB transaction names every collection

Status: Amends D45.

Ken decided on 2026-09-29 how `BeginTx` works on ArangoDB. This is item 7
of step 9 of [DRIVER.md](../DRIVER.md). A stream transaction must name its
write collections when it begins, and a write to another collection fails
with error 1652 (measured on 3.12.12). `BeginTx` does not know which
collections the statements of the transaction will write.

- `BeginTx` lists the collections of the database that are not system
  collections, and begins a stream transaction with `POST /_api/transaction/begin`, which names each of them
  in `write`. A transaction with `ReadOnly` names none, and reads through
  `allowImplicit`.
- Each statement of the transaction sends the header `x-arango-trx-id`.
- `Commit` is `PUT /_api/transaction/<id>`, and `Rollback` is `DELETE` on the
  same path (measured).
- A collection that a statement makes after `BeginTx` is not in the
  transaction.
- The transaction keeps the context of `BeginTx` for `Commit` and `Rollback`,
  which take none, as D69 does for Neo4j. After that context ends,
  `Rollback` sends its `DELETE` with the context without its end, and with
  the limit of 5 seconds that D67 gives a stop. A transaction that nobody aborts names
  every collection in `write`, so it would hold each of them until its idle
  timeout. The review of D97 found this on 2026-09-29, and D100 gives Neo4j
  the same rule.
- The server ends a transaction that stays idle for 60 seconds by default.
