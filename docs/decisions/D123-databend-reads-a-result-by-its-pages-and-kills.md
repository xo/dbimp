# D123. Databend reads a result by its pages, and kills a query that it leaves

Status: Decided.

Ken decided this on 2026-09-29, at step 9 of the Databend driver.
This is items 8 and 9 of step 9 of [DRIVER.md](../DRIVER.md) for Databend.

- The driver reads each page by `next_uri`, one token at a time, and asks
  for the next page only when `Rows.Next` needs its first row (D21 and
  D25). A page holds at most 10,000 rows by default, and the driver sends
  no `pagination`. A first response that is `Starting` or `Running` with no
  rows is followed as a page with no rows. An error on a later page is the
  error of `Rows.Next`, and wraps `dbimp.ErrIncomplete` after a row reached
  the caller (D107).
- A disconnect does not stop a query (measured). So each statement sends a
  random `X-DATABEND-QUERY-ID`, and when the context ends, or the rows close
  before the end, the driver sends `GET /v1/query/<id>/kill` with the
  context without its end and the limit of a stop, as D67 does for Neo4j.
  The id lets it kill a query whose first response has not arrived. With
  `cancel=none`, it sends nothing.
- At the end of the rows, the driver sends `GET` on `final_uri`, which frees
  the query on the server.
