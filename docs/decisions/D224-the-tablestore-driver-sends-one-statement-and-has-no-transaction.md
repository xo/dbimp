# D224. The Tablestore driver sends one statement and has no transaction

Status: Proposed. These follow from step 6 and wait for Ken.

1. The server binds no parameter (recorded: "a question mark parameter"). The driver writes each
   argument into the text with the parser for placeholders of the root package (D34), with the
   quoting of Tablestore SQL.
2. A request holds one statement, and the server refuses two ("two statements"). The driver
   never parses a query.
3. `BeginTx` returns `dbimp.ErrNotSupported` (D20). `BEGIN`, `START TRANSACTION`, `COMMIT` and
   `ROLLBACK` are refused by the server.
4. `INSERT`, `UPDATE` and `DELETE` run through SQL and answer the affected rows, which
   `ExecContext` returns as `RowsAffected`.
5. A result comes in one answer. The driver holds the answer in memory, and reads it to its end
   (D21). A result past 2600 rows is not measured, and `next_search_token` is not used, which
   step 14a measures.
6. The API has no call to cancel a statement. The driver stops by closing the request when the
   context ends, and the server may finish the statement (D36).
7. The version is `SELECT @@version`, which answers `5.7.25-SQLFrontend-b30fece-dirty`.
8. The driver follows no redirect, sends the credentials to the host of the DSN only, and has no
   flavor. The service is hosted, so `dbrun` cannot start it.
9. The driver sends a statement once. It sends a call again only when the connection broke
   before the request reached the server (D8).

See `docs/TABLESTORE.md`.
