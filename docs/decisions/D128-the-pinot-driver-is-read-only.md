# D128. The Pinot driver is read-only

Status: Decided.

Ken decided on 2026-09-30, at step 6 of the Apache Pinot driver, that the
driver runs queries and takes no write. The Broker of Pinot refuses every
insert, update and delete of rows through SQL: `INSERT … VALUES` on both
engines, `INSERT … SELECT`, `UPDATE`, `DELETE` and `CREATE TABLE` fail with
150 `SQLParsingError` on 1.4.0 and 1.5.1 (measured,
[PINOT.md](../PINOT.md)). Rows arrive in Pinot by ingestion, and a table
through the REST API of the Controller. DRIVER.md stops at a server that
refuses one of insert, select, update and delete, and Ken chose to go on
with a driver for the reads.

- The driver sends each statement to the Broker, and a write reaches the
  caller as the error of the server. The driver fakes no write, and adds no
  DDL of its own, as the ArangoDB driver does (D92), because a write of rows
  is ingestion, which the Broker does not run.
- `BeginTx` returns `dbimp.ErrNotSupported`, because Pinot has no
  transactions (D20).
- The entries of insert, update and delete in the survey of step 5a say
  `no`, with the recorded refusal.
- The integration tests make their tables and load their rows through the
  Controller, outside the driver, and read them through the driver.
