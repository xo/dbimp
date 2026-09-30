# D145. rqlite bounds each statement by its context

Status: Amended by D146.

Ken decided this on 2026-09-30, at step 9 of the rqlite driver. This is item
9 of step 9 of [DRIVER.md](../DRIVER.md) for rqlite.

- The server stops a read when the client disconnects (measured), so a read
  ends when its context ends, with no more request.
- A write that has started does not stop when the client leaves (the server
  source), and rqlite has no request that cancels a statement. So when the
  context has a deadline, the driver sends `db_timeout` with the time that
  is left, and the server stops a read with `query timeout` and a write
  with `execute timeout`, and keeps nothing of the write (recorded).
- `WithTimeout` sets `db_timeout` too (D109). When both apply, the shorter
  one goes.
