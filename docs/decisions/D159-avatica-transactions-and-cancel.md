# D159. Avatica transactions and cancel

Status: Decided.

Ken decided this on 2026-10-01, at step 9 of the Avatica driver. This is
items 7, 9 and 12 of step 9 of [DRIVER.md](../DRIVER.md) for Avatica.

- `BeginTx` sends `connectionSync` with `autoCommit` false, and with
  `readOnly` and `transactionIsolation` from the options. `Commit` and
  `Rollback` send `commit` and `rollback`, and then `connectionSync` with
  `autoCommit` true. Each is a transaction of the database behind the
  server (AVATICA.md, Transactions).
- Avatica has no request that stops a statement, and the server runs a query
  to its end when the client leaves (measured). When the context ends, the
  driver stops its request and the read, and sends `closeStatement` with the
  context without its end and a limit of its own.
- The driver speaks JSON only (D153).
- Ken added this on 2026-10-01, after step 14: the `connectionSync` at the
  end of a transaction also sets `readOnly` and `transactionIsolation` back
  to their values before it, in the same request. Without it, a connection
  that `database/sql` takes back into its pool stays read-only or
  serializable on the server. If that request fails, the driver marks the
  connection bad, and `database/sql` closes it.
