# D126. A Databend connection keeps its session when it is reused

Status: Amends D122.

Ken decided this on 2026-09-29, after the `usql` session measured the
Databend driver of `v0.6.0`. D122 kept the session of a connection, and set
it back to the session of the DSN in `ResetSession`. `database/sql` calls
`ResetSession` each time a connection goes back to the pool, so a caller that
runs each statement through `*sql.DB`, as `usql` does, lost a `USE` or a
`SET` before its next statement. With databend-go, `use system` held. A
pooled connection of MySQL or PostgreSQL keeps its `USE` and its `SET` too.

So the driver has no `ResetSession`, and a connection keeps its session for
as long as it lives, as D102 allows for a driver that holds nothing that a
reset must end. A caller who wants the session of the DSN again opens a new
connection. The rest of D122 holds: the options of one statement do not stay
in the session, and a transaction keeps the context of `BeginTx`.
