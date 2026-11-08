# D236. Rule 4 names the Hive driver

Status: Decided on 2026-11-09.

Rule 4 of AGENTS.md forbids a `context.Context` in a struct, and it names the drivers that hold one, as D215 does
for Exasol. The package `hive` (W56, D235) needs one in two places, because `database/sql` calls a method with no
context:

- A connection holds the context of `Connect`, without its end. `Close` needs it for `CloseSession`, and the
  driver needs it to cancel and close the operation of a statement whose context ended (D235).
- The rows of a query hold the context of the query. `Next` needs it for `FetchResults`, because the rows read
  page by page after `QueryContext` returns.

A request that frees something on the server takes the context without its end, with a limit of 10 seconds, as D215
does. Ken accepted the design of the driver on 2026-11-09 by D235, which sends `CancelOperation`, `CloseOperation` and
`CloseSession`. This decision amends the text of rule 4 in AGENTS.md, which names the connection and the rows of a Hive
query.
