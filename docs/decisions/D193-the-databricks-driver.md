# D193. The Databricks driver

Status: Decided.

These are the answers to the step 9 questions of
[DATABRICKS.md](../DATABRICKS.md) (W39). Ken decided them on 2026-10-10.

1. The package is `databricks`, and it registers the name `databricks` with no
   alias (D26, D28 and D30). It uses no binary encoding (D13). It reads the format
   `JSON_ARRAY` of the SQL Statement Execution API.
2. The driver reads a result with the disposition `INLINE` only. It returns an
   error when the answer says `truncated`, or when the statement fails for the
   size of the result. A later release can add `EXTERNAL_LINKS` when a recording
   shows a second chunk and the body of a link.
3. The driver accepts the 3 fraction digits that the server gives for TIMESTAMP and
   TIMESTAMP_NTZ values. DATABRICKS.md and its type table say so, and a caller who
   needs microseconds casts to a string in SQL.
4. The DSN is `databricks://token:<pat>@host/<warehouse-id>?catalog=&schema=`. The
   personal access token is the password (D94). The path is the id of the SQL
   warehouse, and `catalog` and `schema` are keys of the query. OAuth with a
   client id and secret can come later.
5. `BeginTx` returns the error of D20, because the API has no session and no
   transaction. The driver sends the catalog and the schema of the DSN with every
   statement. `SET`, `USE`, temporary views and variables do not persist between
   statements, and DATABRICKS.md says so.
6. An INTERVAL DAY TO SECOND value and an INTERVAL YEAR TO MONTH value are a
   `dbimp.Interval`.
7. The driver parses `type_text` to decode ARRAY, STRUCT, MAP and VARIANT. An ARRAY
   is a `[]any`, a STRUCT is a `map[string]any`, a MAP is a `map[string]any` that
   loses its key type, and a VARIANT is a decoded value. A JSON null and a SQL NULL
   of a VARIANT are both nil (D18).
8. The driver sends `wait_timeout` of 50 seconds, and it polls when the answer is
   still pending. `Exec` reads the count row (`num_affected_rows`) when the answer
   has one. `RowsAffected` returns the wrapped `dbimp.ErrNotSupported` when it has
   none, as for a `CREATE TABLE AS SELECT` (D178).
9. The other proposals of step 9 stand as DATABRICKS.md writes them:
   - Parameters are named (`:p`) or positional (`?`), each with an explicit type.
     The driver refuses BINARY, ARRAY, MAP and STRUCT parameters.
   - The driver passes several statements in one request to the server, which
     answers a parse error.
   - The error type exposes the HTTP status, `error_code`, `sql_state` and the
     message.
   - The default timeout allows the 15 seconds that the first statement after the
     warehouse stops can wait.
   - The driver gives no answer to a version request (D181).
   - The Bearer token goes only to the configured host, and the driver never
     sends it to an external link.
   - The driver serves no flavor.
10. The integration tests run on a workspace only where a person supplies it, in
    the variable `DATABRICKS_DSN`, and skip when it is empty (rule 9). The unit
    tests run on the recorded exchanges. The free workspace has a daily compute
    quota that `dbmeta` shares.
11. The host in the DSN is written as the workspace names it, and the driver uses
    HTTPS on port 443. A key of the query, such as `tls=false`, lets a test use a
    plain HTTP server.
12. Ken accepted these choices of the driver on 2026-10-11:
    - A nil argument is sent with the type `VOID`. A live run accepted it.
    - When the deadline of the context is under 50 seconds, the driver sends
      `wait_timeout` of `0s` and polls, so that it can cancel the statement.
    - The key `timeout` and `WithTimeout` are a limit of the client with no
      default, and they cancel the statement on the server.
    - `WithDatabase` sets the schema. `WithCatalog` and `WithSchema` also exist.
      `WithReadonly(true)` returns `dbimp.ErrNotSupported`.
    - The user of the DSN is empty or `token`, the path is one warehouse id, and
      `tls=false` selects HTTP.
    - A TIMESTAMP parameter goes in UTC, cut to microseconds. An interval with
      both months and days is refused.
    - `Exec` reads the whole answer only when the first column is
      `num_affected_rows`.
    - A truncated result, a result of more than one chunk and external links are
      errors before any row.
    - An unknown scalar type reads as a string, and a complex type that the driver
      cannot parse is an error.
