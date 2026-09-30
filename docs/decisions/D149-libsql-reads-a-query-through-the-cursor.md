# D149. libSQL reads a query through the cursor

Status: Decided.

Ken decided this on 2026-10-01, at step 9 of the libSQL driver. This is item
8 of step 9 of [DRIVER.md](../DRIVER.md) for libSQL.

- `QueryContext` sends the statement to `/v3/cursor`, which streams each row
  as a line of JSON, with no cap on the size (LIBSQL.md, Responses). The
  driver reads it one token at a time (D25). `/v2/pipeline` builds the whole
  answer, and fails above 10MB with `RESPONSE_TOO_LARGE`.
- A `step_error` after some rows is the error of `Rows.Next`, which wraps
  `dbimp.ErrIncomplete` (D21 and D107). An error before any row is the
  error of the query.
- A cursor leaves its stream open (recorded), so the driver closes it with
  a `close` request to `/v3/pipeline` after the rows end or close, with the
  context without its end and a limit of its own. So the rows keep the
  context of the query (hard rule 4). A cursor of a transaction
  keeps its stream, and the next statement sends the new baton (D150).
- `ExecContext` sends the statement and a `close` to `/v3/pipeline`, in one
  request, because its answer holds no rows.
