# D133. Pinot reads one answer, and cancels a query by its id

Status: Amended by D134.

Ken decided this on 2026-09-30, at step 9 of the Apache Pinot driver.
This is items 7 to 9 of step 9 of [DRIVER.md](../DRIVER.md) for Apache
Pinot.

- The Broker sends the whole result in one body, with the rows before the
  exceptions ([PINOT.md](../PINOT.md)). The driver reads it one token at a
  time (D25), and needs no cursor, because the answer holds every row. An
  exception after the rows, or `partialResult` true with none, is the error
  of `Rows.Next`, which wraps `dbimp.ErrIncomplete` after a row reached the
  caller (D21 and D107). An exception before any row is the error of the
  query.
- A client that leaves does not stop a query: the query ran on until the
  heap of the server was used up (measured). So each query names itself
  with `clientQueryId`, which the driver chooses. When the context ends, or
  the rows close before their end, the driver sends
  `DELETE /query/<clientQueryId>?client=true`, with the context without its
  end and the limit of a stop, as D123 does for Databend. With
  `cancel=none`, it sends nothing.
- `BeginTx` returns `dbimp.ErrNotSupported` (D20 and D128).
