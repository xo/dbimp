# D179. The VoltDB driver speaks HTTP

Status: Decided. Amended by D180.

Ken named VoltDB as a target on 2026-10-08, at step 1 of
[DRIVER.md](../DRIVER.md), after ClickHouse (D174). It was a place in the
second priority before that (TARGETS.md).

- The package is `voltdb`, and it registers the name `voltdb` (D26 and D28).
  It takes queries over the HTTP and JSON interface of VoltDB (D14), and not
  over the native binary protocol.
- `usql` uses `VoltDB/voltdb-client-go/voltdbclient` now, which speaks the
  native protocol. This driver can replace it (D24) only if it matches that
  client for a caller. Where it does not, the difference goes in VOLTDB.md,
  and Ken decides at step 9.
- `dbrun` starts `voltdb-14.1.0` and `voltdb-15.2.0`, and the tests run on
  every one (DRIVER.md, step 14).
- Whether VoltDB can be a driver at all depends on step 2. Two models said on
  2026-10-08 that it can, and neither measured it. The driver goes through
  steps 2 to 9 as the other drivers did, and Ken decides its design in step 9.
