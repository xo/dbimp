# D184. BigQuery, Cosmos DB and Athena are targets

Status: Decided.

Ken asked on 2026-10-10, at step 1 of [DRIVER.md](../DRIVER.md), for a driver
for each of three services that `usql` reaches with a client that is heavier
than a driver here needs (D24). It came after Snowflake (D182 and D183).

- The three packages are `bigquery`, `cosmos` and `athena`, and each registers
  the name of its package (D26 and D28). Each takes its statements over the HTTP
  API of its service, with JSON, and uses no binary encoding (D13 and D14). An
  answer that only a binary encoding can carry is a finding of step 2, and Ken
  decides what to do with it.
- BigQuery has a community emulator that `dbrun` starts (`bigquery-0.7.2` and
  `bigquery-0.8.1`), and Cosmos DB has an emulator that `dbrun` starts
  (`cosmos-EN20260907`). The measurement starts on the emulators, which need no
  credential. A real account is for the checks that an emulator cannot give, and
  `dbsetup` provisions it when Ken asks (dbmeta D117).
- Athena is a service with no emulator. Its measurement needs an account of
  Amazon, and Ken decides at step 2 whether a measurement is worth its cost.
- Each driver goes through steps 2 to 9 of DRIVER.md as the other drivers did,
  and Ken decides its design in step 9. Whether each can be a driver depends on
  step 2.
