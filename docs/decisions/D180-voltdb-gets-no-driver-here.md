# D180. VoltDB gets no driver here

Status: Amends D179.

Ken decided on 2026-10-08, at step 2 of [DRIVER.md](../DRIVER.md), that dbimp
writes no driver for VoltDB. It fails H: the releases that `dbrun` starts have
no HTTP interface to measure (D16, D17 and D179).

This amends D179, which named VoltDB as a target for an HTTP driver. VoltDB
goes back to the second priority in [TARGETS.md](../TARGETS.md), and the
order of the drivers is unchanged.

## Why

- Two models said on 2026-10-08 that VoltDB has an HTTP and JSON interface at
  `/api/1.0/` on port 8080, and neither measured it (VOLTDB.md, Second
  opinions).
- The `dbmeta` session found that nothing listens on port 8080 on
  `voltdb-14.1.0` or on `voltdb-15.2.0`, with `/proc/net/tcp`, and that
  `@SystemInformation` lists no HTTP port. The ports are 21212 for the client,
  21211 for the admin, 3021, 7181, 11781, 5555 and 9092. A `jsonapi` setting
  in the deployment file was dropped when `init` converted the file to YAML,
  as seen on 15.2.0 (reported by `dbmeta` on 2026-10-08, not measured again in
  this repository).
- A driver for VoltDB would use the native protocol on port 21212. That is a
  binary encoding, which hard rule 6 of AGENTS.md allows only with the approval
  of Ken for one database (D13), and Ken did not give it.
- `usql` reaches VoltDB with `VoltDB/voltdb-client-go/voltdbclient` already, so
  nothing is lost for a user.

## When this can change

A release of VoltDB that serves the HTTP interface, or a way to turn it on in
the deployment file of these releases, reopens the question. The step that
found the refusal is step 2, and the measurement starts again there.
