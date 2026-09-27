# D45. A Couchbase transaction keeps the context of BeginTx

Status: Decided.

Ken decided this on 2026-09-27. It is the one exception to hard rule 4,
which says that the library never stores a context. `driver.Tx.Commit` and
`driver.Tx.Rollback` take no context, and `COMMIT WORK` and `ROLLBACK WORK`
are requests that need one. `database/sql` defines the context of `BeginTx`
as the lifetime of the transaction: it rolls the transaction back when that
context ends.

So the transaction keeps that context, and nothing else keeps one:

- `Commit` sends `COMMIT WORK` with it.
- `Rollback` sends `ROLLBACK WORK` with it while it is live. When it has
  ended, which is when `database/sql` rolls back by itself, `Rollback`
  forgets the `txid` and sends nothing.

That is safe because of what the server does with a transaction that nobody
ends. Measured on 8.0.3 on 2026-09-27: a transaction that was left open
applied none of its writes, did not block a write to the same key from
outside it, and ended at its `txtimeout`, after which its `txid` was refused
with code 17010. The timeout is 15 seconds for a request that sets none, by
the documentation of Couchbase, and a setting of the node can lower it
([COUCHBASE.md](../COUCHBASE.md)).

`pgx`, `gosnowflake` and `go-mssqldb` keep the context of `BeginTx` in the
same way, which was read in their source on 2026-09-27.
