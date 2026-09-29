# D102. A driver needs no ResetSession for a transaction

Status: Amends D20.

Ken decided on 2026-09-29, in the review of D97, that a decision whose
conclusion is wrong gets an amending decision. D20 said that the state of a
transaction belongs to the connection, "and `ResetSession` clears it". Only
the Couchbase driver has `ResetSession`, and the Neo4j and ArangoDB drivers
have transactions without it.

The rule that stays:

- A connection holds the state of a transaction from `BeginTx` to `Commit`
  or `Rollback`. `database/sql` ends every transaction before it hands the
  connection to another caller, so no transaction is left for a reset.
- A driver implements `driver.SessionResetter` only when a connection can
  hold state on the server that `database/sql` does not end. None does
  today.
- The Couchbase driver keeps its `ResetSession` of D41 as a guard. It sets
  the `txid` only in `BeginTx`, and clears it before it ends the
  transaction, so the guard rolls nothing back in any path that the tests
  know.

The state of a transaction still belongs to the connection, as D20 says.
