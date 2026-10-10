# D185. Spanner is a target

Status: Decided. Amended by D186.

Ken asked on 2026-10-10, at step 1 of [DRIVER.md](../DRIVER.md), for a driver for
Google Cloud Spanner, to be measured and written at the same time as the
drivers of D184. `usql` reaches Spanner with `googleapis/go-sql-spanner`, which
speaks gRPC.

- The package is `spanner`, and it registers the name `spanner` (D26 and D28).
  It takes its statements over the REST API of Spanner (`v1`), with JSON, and
  uses no gRPC and no binary encoding (D13 and D14).
- `dbrun` starts `spanner-2026.r4-lts`, which `dbmeta` reads already, and the
  measurement runs on it. A real project is for the checks that the emulator
  cannot give, and it costs a minimum for each node or processing unit, so Ken
  decides before any real measurement.
- The driver goes through steps 2 to 9 of DRIVER.md as the other drivers did,
  and Ken decides its design in step 9. Whether it can be a driver depends on
  step 2.
