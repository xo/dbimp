# D187. Spanner is measured on the hosted service

Status: Amends D186.

Ken decided on 2026-10-10 that dbimp builds the drivers for BigQuery, Spanner,
Cosmos DB and Athena against the hosted services as needed, with credentials
that the peer `dbsetup` provides. This reopens Spanner, which D186 closed.

## Why

- D186 closed Spanner because the only server that `dbrun` starts, Spanner Omni,
  serves no REST API (D16 and D17). The hosted service serves the REST API
  of Spanner at `spanner.googleapis.com`, so H can be met there.
- Snowflake, the first hosted target, set the pattern: one login for one
  database, and the measurement runs on the service (D182).
- `dbsetup` made the instance `dbimp-spanner` and the database `dbimp_test`, with
  a service account that holds `roles/spanner.databaseAdmin` on that one
  database. The key file is outside the repository, and a run reads it into an
  environment variable only.
- `dbmeta` is switching back to the Cloud Spanner emulator, which serves REST on
  port 9020. When that entry exists, the measurement is repeated on it, so that a
  test can run without an account.

## What does not change

- The driver is REST and JSON only. gRPC and a binary encoding stay forbidden
  without the approval of Ken (D13 and hard rule 6).
- The questions of step 9 go to Ken as for every other driver.
