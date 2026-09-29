# D41. The first Couchbase driver has transactions

Status: Decided.

Ken decided this on 2026-09-27. The query service has transactions through
a `txid`, and the first release of the driver supports them (D20):

- `BeginTx` sends `BEGIN WORK`, and the connection keeps the `txid` that it
  returns. Each statement on the connection carries the `txid` until the
  transaction ends.
- `Tx.Commit` sends `COMMIT WORK`, and `Tx.Rollback` sends `ROLLBACK WORK`,
  each with the `txid`. The connection then forgets it.
- `ResetSession` rolls back a transaction that is still open, and returns
  `driver.ErrBadConn` if that fails, so that `database/sql` drops the
  connection rather than hand its state to another caller.
- `TxOptions.ReadOnly` sends `readonly`. An isolation level other than the
  default is an error that wraps `dbimp.ErrNotSupported`, because the query
  service has one level.
- The timeout of a transaction is an option, as D46 describes.

A transaction lives on the one query node that began it. The driver talks to
one host (D38), so every statement of a transaction reaches that node.

`COMMIT WORK`, a `txid` that expired, and a `txid` sent after the end are
not measured yet. W5 measures them in step 6, and step 14a tests the
transaction as a feature.

Note of 2026-09-29: W5 measured them. [COUCHBASE.md](../COUCHBASE.md) holds
the codes 17004 and 17010 under "Transactions".
