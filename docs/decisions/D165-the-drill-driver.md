# D165. The Drill driver

Status: Decided.

These are the items of step 9 of [DRIVER.md](../DRIVER.md) for Apache Drill
(W26). Each fact is in [DRILL.md](../DRILL.md). D163 decides that the driver
is read only: `CREATE TABLE AS` runs, and the server refuses `INSERT`,
`UPDATE` and `DELETE`.

1. The package and the name that it registers are `drill` (D26 and D28).
2. The DSN is `drill://user:password@host:8047`, with no path. The keys are
   `tls` (false by default), `schema` (none by default, sent as
   `defaultSchema`) and `autolimit` (none by default, sent as `autoLimit`).
   Any other key is refused. The alternative is the default schema in the
   path.
3. The Go types are the type table of DRILL.md. A list of a scalar type
   arrives under the name of its element, such as `BIGINT`, so the driver
   gives a `[]any` when the JSON token is an array, and
   `ColumnTypeScanType` names `any` for such a column once the first row
   shows it.
4. NULL and a missing value are one value, nil. When a column changes its
   type between two files, the server gives NULL for each value of the
   second type and no sign of it. The driver cannot see this. Its
   documentation names it, with `typeof` in the statement as the way to
   find it. Ken decided on 2026-10-02 that this is enough, and that Drill
   does not meet "When it cannot be a driver".
5. Drill binds no argument, so the driver writes each one as a literal with
   the escaper of the root package (D34).
6. `BeginTx` returns `dbimp.ErrNotSupported`, because Drill has no
   transactions.
7. Each request sends the option `drill.exec.http.rest.errors.verbose`, so
   that an error before any row has its message. The driver reads the rows
   one token at a time, and reads `queryState` at the end. For `FAILED`
   after some rows, it reads the message from `GET /profiles/{id}.json`, and
   the error wraps `dbimp.ErrIncomplete` (D21 and D107). A cut by
   `exec.query.max_rows` gives no sign, and the documentation names it.
8. When the context ends, the driver sends `GET /profiles/cancel/{id}` with
   the `queryId` of the first batch, with the context without its end and a
   limit of its own, because the server runs a query on when the client
   leaves. Before the first batch the driver has no id, and only closes
   the request.
9. The driver follows no redirect. A 307 to `/mainLogin` is a wrong
   password. The credentials go to the host of the DSN only.
10. Each connection keeps the session cookie of the server, so that
    `ALTER SESSION` reaches the next statement, and the server keeps one
    session for each connection. The alternative is no cookie, which makes
    a session on the server for each request, idle for 3600 seconds.
11. The driver serves no flavor. Dremio speaks another REST API.
12. The driver uses JSON only.
