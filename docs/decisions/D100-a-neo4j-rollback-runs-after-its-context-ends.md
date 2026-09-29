# D100. A Neo4j rollback runs after its context ends

Status: Amends D69.

Ken decided this on 2026-09-29. D69 said that after the context of `BeginTx`
ends, `Rollback` sends nothing, and the server rolls the transaction back at
its idle timeout of 60 seconds. Until then, the server holds the locks of
the writes of the transaction, so another writer of the same node waits.

Now `Rollback` sends its request after that context ends too. It uses the
context without its end, through `context.WithoutCancel`, with the limit of
5 seconds of D67. `database/sql` calls `Rollback` when the context ends, so
the transaction ends on the server at once. If an error on the server ended
the transaction first, `Rollback` still sends nothing (D65).

The ArangoDB driver does the same for its transaction (D91), which names
every collection in `write`. The Couchbase driver keeps D45, because a write
in a Couchbase transaction does not block a write from outside it
(measured). `TestIntegrationRollbackAfterTheContext` writes a node that the
transaction holds, and fails when the write waits.
