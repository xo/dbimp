# D209. A transaction of the Exasol driver turns autocommit off

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 7, under D20.

`BeginTx` sets the attribute `autocommit` to false, and `Commit` and `Rollback` send `COMMIT` or `ROLLBACK` and set it to true again, or to the value that it had before. If the DSN has `autocommit=false`, the attribute does not change.

`TxOptions` with `ReadOnly`, or with an isolation level other than the default, give an error that wraps `dbimp.ErrNotSupported`. The server has neither (recorded: "set a transaction isolation level"). The server undoes DDL in a transaction with `ROLLBACK`.
