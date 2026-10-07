# D174. The ClickHouse driver speaks HTTP

Status: Decided.

Ken named ClickHouse as a target on 2026-10-07, at step 1 of
[DRIVER.md](../DRIVER.md), after Trino and Presto (D173).

- The package is `clickhouse`, and it registers the name `clickhouse` (D26
  and D28). It takes queries over the HTTP interface of ClickHouse (D14),
  and not over the native protocol.
- `usql` uses `ClickHouse/clickhouse-go/v2` now, which speaks the native
  protocol by default. This driver can replace it (D24) only if it matches
  that client for a caller. Where it does not, the difference goes in
  CLICKHOUSE.md, and Ken decides at step 9.
- `dbrun` starts `clickhouse-25.3`, `clickhouse-25.8` and `clickhouse-26.9`,
  and the tests run on every one (DRIVER.md, step 14).
- The driver goes through steps 2 to 9 of DRIVER.md as the other drivers
  did, and Ken decides its design in step 9.
