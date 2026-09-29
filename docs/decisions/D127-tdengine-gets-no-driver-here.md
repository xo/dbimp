# D127. TDengine gets no driver here

Status: Amends D73 and D74.

Ken decided on 2026-09-29, at step 6 of the TDengine driver, that dbimp
writes no driver for TDengine, and that it moves to P3 in
[TARGETS.md](../TARGETS.md). It meets a condition of "When it cannot be a
driver" in [DRIVER.md](../DRIVER.md): the server cuts a result short and
gives no way to learn it (D21).

This amends D73, which placed TDengine fifth after Neo4j, and D74, which
repeated that order. Apache Pinot is now the next target after Databend.

## Why

- Over REST, taosAdapter writes `code` 0 before the rows. When the query
  fails while its result streams, it ends the body as valid JSON, with a
  `rows` that counts only what it sent. A `SELECT` of 1,000,000 rows that
  `KILL QUERY` stopped ended with `code` 0 and 311,296 rows on 3.4.2.8, and
  184,320 on 3.3.8.8. `DROP DATABASE` during the read ended the same way,
  with 339,968 and 188,416 rows (measured on 2026-09-29,
  [TDENGINE.md](../TDENGINE.md)). A driver cannot tell such a result from a
  whole one.
- The WebSocket interface on `/ws` reports that failure: the next `fetch`
  answered `code` 24 after `DROP DATABASE`, on both releases (measured). But
  it needs three things that the rules of this repository do not allow
  without a change: a WebSocket client written here, because the standard
  library has none (D13); a connection that is no request of HTTP for each
  statement (D14); and the raw block of taosd, a binary encoding, for the
  rows (D13).
- `usql` has no driver for TDengine, so no consumer loses one (D24).

The measurements stay in TDENGINE.md, as the record of why. `dburl` keeps
the provisional scheme `tdengine` (dburl D36) until its session decides
what to do with it.
