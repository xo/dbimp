# D188. Databricks is a target

Status: Decided.

Ken asked on 2026-10-10, at step 1 of [DRIVER.md](../DRIVER.md), for a driver for
Databricks, with BigQuery, Spanner, Cosmos DB and Athena (D184, D185 and
D187). `usql` reaches Databricks with a client that is heavier than a driver
here needs (D24).

- The package is `databricks`, and it registers the name of its package (D26 and
  D28). It takes its statements over the SQL Statement Execution API, which
  is HTTP and JSON, and it uses no binary encoding (D13 and D14). The API can
  answer in the Arrow stream format. An answer that only a binary encoding can
  carry is a finding of step 2, and Ken decides what to do with it.
- Databricks is a hosted service with no emulator and no entry in `dbrun`. The
  measurement runs on a workspace and a SQL warehouse that `dbsetup` provisions
  (dbmeta D117), as for Snowflake (D182) and Spanner (D187).
- The driver goes through steps 2 to 9 of DRIVER.md as the other drivers did,
  and Ken decides its design in step 9. Whether it can be a driver depends on
  step 2.
