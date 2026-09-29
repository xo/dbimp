# D20. A driver never fakes a transaction

Status: Amended by D102.

D102 amends the last point: a driver needs no `ResetSession` for a
transaction, because `database/sql` ends it before it reuses the
connection.

Ken accepted this on 2026-09-27. Most HTTP interfaces keep no state between
requests and have no transactions. If the product has none, `BeginTx` returns a
sentinel error that says so. If the product has one, such as Neo4j, rqlite,
libSQL, Trino and Couchbase, the state belongs to the connection, and
`ResetSession` clears it. Each such driver records its design in a decision of
its own.
