# D88. CrateDB gets no driver here

Status: Amends D73, D74 and D76.

Ken decided on 2026-09-29 that dbimp writes no driver for CrateDB. `usql`
reaches CrateDB with `pgx`, which it already ships, on the PostgreSQL wire
protocol, and `dbmeta` reads it through a dialect of its own. CockroachDB,
which also speaks the wire protocol of PostgreSQL, is served in the same way.
Ken asks the `dbmeta` session for both dialects himself. `dburl` has the
scheme `cratedb` on `pgx`, with the dialect `cratedb` (dburl D30).

This amends D73, which placed CrateDB second after Neo4j, D74, which
repeated that order, and D76, which named its driver `cratedb`. ArangoDB is now the next driver after InfluxDB.

## Why

- The HTTP interface, `POST /_sql`, builds each whole result in the memory
  of the server before it sends it, and counts it against the query circuit
  breaker. It has no paging (the source, in [CRATEDB.md](../CRATEDB.md)). So
  a large result fails on the server, whatever the driver reads.
- The `dbmeta` session measured the wire protocol on 6.4.5 on 2026-09-29,
  with `pgx` v5 and `lib/pq`. The extended protocol with `$1` parameters,
  prepared statements, a pipelined batch, DDL and DML work. So do bigint,
  double, bool, timestamp, arrays, ip and geo_point, and an object arrives as
  JSON. Transactions, COPY and LISTEN fail, as they would over HTTP.
- CrateDB answers `server_version` as 14.0, and `version()` names CrateDB.
  The postgres model of `dbmeta` runs 11 of its 55 queries there, so CrateDB
  needs a model of its own, which finds it by `version()` (dbmeta D44).
- A driver here would add no feature that `pgx` lacks, and one more driver to
  keep. Gemini and DeepSeek recommended the same on 2026-09-29.

Not measured: whether the wire protocol streams a result or builds it in
memory, and whether a cancel stops the query on the server.
