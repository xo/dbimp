# D220. The MaxCompute driver sends one statement and reads the CSV

Status: Proposed. Ken decided items 3, 5 and 6 on 2026-10-29, and the others follow from step 6.

1. The driver reads a result as CSV only. It uses neither the Tunnel nor Arrow, so it needs no
   binary encoding and no package (D13, decided by Ken on 2026-10-29). It streams the CSV with the
   parser of `encoding/csv`, so that `Rows.Close` stops the read (the result has at most 10,000
   rows, which is the cap of the server).
2. The server binds no parameter (recorded: "a question mark parameter", "a named parameter").
   The driver writes each argument into the text with the parser for placeholders of the root
   package (D34), with the quoting of MaxCompute SQL, and it counts a placeholder in a literal or
   a comment as text.
3. The caller sets script mode. The driver never parses a query to count its statements. A DSN
   key that starts with `odps.` is a setting of every statement, such as
   `?odps.sql.submit.mode=script`, and `WithParameter` sets one for one statement (D109). The
   driver sends the settings that it receives as the property `settings`.
4. The server has no transaction. `BeginTx` returns `dbimp.ErrNotSupported` (D20). `UPDATE`,
   `DELETE` and `MERGE` work only on a transactional table, which the caller makes.
5. The cap of 10,000 rows is silent, and the driver cannot see it. The driver returns the rows
   that it has, and `docs/MAXCOMPUTE.md` says so. The caller pages with `ORDER BY` and
   `LIMIT <offset>, <count>` in its own SQL. Ken confirmed this on 2026-10-29.
6. A statement runs in three calls (create the instance, poll `?taskstatus`, read `?result`). A
   statement of DDL or `SHOW` can answer at once in the body of the create call. The driver
   polls with a growing wait and stops at the end of the context.
7. When the context ends, the driver sends `PUT <instance>` with the status `Terminated`, with a
   new context of a few seconds, and then returns `ctx.Err()`. The server keeps running a
   statement when the client leaves, so the driver cancels it on the server (D36).
8. The driver sends a statement once. It sends a call again only when the connection broke
   before the request reached the server, because a reset can follow a request that ran (D8).

See `docs/MAXCOMPUTE.md`, sections "Requests", "Parameters", "Transactions", "Cancellation and
timeouts" and "Statements".
