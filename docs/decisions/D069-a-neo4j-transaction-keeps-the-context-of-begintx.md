# D69. A Neo4j transaction keeps the context of BeginTx

Status: Amends D45 and D65, and amended by D100.

D100 amends the rollback after the context ends: it sends its request with
the context without its end.

Ken decided this on 2026-09-27. It amends the last point of D65, which said
that the transaction keeps no context. `Commit` and `Rollback` of
`driver.Tx` take no context, and each sends a request that needs one. Hard
rule 4 forbids `context.Background`. So the transaction keeps the context of
`BeginTx`, which `database/sql` defines as the lifetime of the transaction,
as a Couchbase transaction does (D45). `Commit` and `Rollback` send their
request with it. After that context ends, `Rollback` sends nothing, and the
server rolls the transaction back at its idle timeout of 60 seconds
(measured). Each statement of the transaction still uses the context of its
own call.
