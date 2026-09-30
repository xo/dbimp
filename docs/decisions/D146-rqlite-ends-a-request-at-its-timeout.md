# D146. rqlite ends a request at its timeout

Status: Amends D145.

Ken decided this on 2026-09-30, at step 14 of the rqlite driver.
`WithTimeout` sets `db_timeout` (D145), and the driver also ends the request
at that time, with a context of its own that the rows end when they close.

- The integration tests found that 9.4.5 ignores `db_timeout` for a read on
  `/db/request`, where a query goes (D142). A read with `db_timeout=500ms`
  ran to its end, and on `/db/query` and `/db/execute` the same value
  stopped it (measured with `curl` on 2026-09-30). 10.3.6 honors it on every
  endpoint.
- A read on `/db/request` stops when its client leaves, on both releases
  (measured from the processor time of `rqlited`). So the end of the request
  stops the read at the timeout on 9.4.5 too. There the error is
  `context.DeadlineExceeded`, and on 10.3.6 it is the error of the server,
  `query timeout`, whichever comes first.
- A write that has started does not stop when its client leaves. On
  `/db/execute`, `db_timeout` stops it on both releases (measured).
