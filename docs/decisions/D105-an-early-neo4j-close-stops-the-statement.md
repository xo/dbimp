# D105. An early Neo4j Close stops the statement

Status: Amends D66.

Ken decided this on 2026-09-29, after Gemini and DeepSeek were asked. D66
said that `Rows.Close` reads the body to its end, so that an error after the
rows is never lost. The code never did that. It closed the body and read
nothing more, as D36 and step 12 of [DRIVER.md](../DRIVER.md) say. Both
models said that the code is right: a caller who closes early does not want
the rest, and to drain a large answer only to see its last error costs the
whole result. They differed on the statement that runs on. Gemini said to
leave it to the context. DeepSeek said to stop it, as the end of a context
does, and Ken chose that.

Outside a transaction:

- `Rows.Close` before the end closes the body, reads nothing more, and
  returns no error that the rest of the answer would hold.
- If the answer came in chunks, with no `Content-Length`, `Close` stops the
  statement on the server, as the end of its context does (D67 and D95). The
  server sends a small answer with a `Content-Length`, after the statement
  ended (recorded), so `QueryRow`, which closes after one row, sends nothing
  more.
- The server stops a statement by itself when it cannot write the next row.
  A statement runs on only when its answer so far fits in the buffers of the
  connection, and it computes before its next row. A statement that sent
  5000 rows and then counted to 3000000000 still ran 2 seconds after the
  client left, and the stop ended it (measured on 2026.09.0 on 2026-09-29).
  `TestIntegrationEarlyClose` holds both.

In a transaction:

- `Rows.Close` before the end reads the rest of the answer, as D66 said,
  because the server fails the next statement of the transaction with
  `TransactionAccessedConcurrently`, or rolls the transaction back, while the
  answer of the last one is unread (tested on 5.26.31 and 2026.09.0).
  `TERMINATE TRANSACTION` would end the whole transaction, so `Close` stops
  nothing.
- An error of the server in the rest ends the transaction, and `Commit`
  returns it (D65). An error of the connection reaches the caller from
  `Close`.
