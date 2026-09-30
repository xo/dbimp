# D144. rqlite has no transactions

Status: Decided.

Ken decided this on 2026-09-30, at step 9 of the rqlite driver. This is item
7 of step 9 of [DRIVER.md](../DRIVER.md) for rqlite. `BeginTx` returns an
error that wraps `dbimp.ErrNotSupported` (D20).

- A `BEGIN` in one request stays open on the server, and later requests run
  inside it (recorded). But every write of every client runs on the one
  write connection of the leader (RQLITE.md, Transactions). So a
  transaction of one caller would take in the writes of every other client
  until it ends, and a caller that dies leaves it open for all of them.
- The flag `transaction` makes the statements of one request atomic. A
  transaction of `database/sql` could keep its statements and send them in
  one request at `Commit`. Then a statement gives no rows and no count
  until the end, and a read inside it cannot see its writes. That is a
  transaction that is not a transaction, which DRIVER.md leaves
  unsupported.
