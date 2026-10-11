# D215. Rule 4 names the Exasol driver

Status: Accepted.

Rule 4 of AGENTS.md forbids a `context.Context` in a struct, and it names the drivers that hold one, as D194 does for three others. The Exasol driver needs one in three places, because `database/sql` calls a method with no context:

- The rows of a query hold the context of the query. `Next` needs it for `fetch`, and `Close` needs it, without its end, for `closeResultSet` (D210).
- A statement holds the context of `PrepareContext`, without its end, for `closePreparedStatement` on `Close` (D208).
- A transaction holds the context of `BeginTx`, without its end, for `COMMIT` and `ROLLBACK` (D209). `database/sql` rolls a transaction back after its context ended.

A request that frees something on the server takes the context without its end, with a limit of 5 seconds, as D150 does for libSQL. Ken accepted it. It amends the text of rule 4 in AGENTS.md, which names the three places.
