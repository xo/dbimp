# D182. Snowflake is measured for an HTTP driver

Status: Decided.

Ken asked on 2026-10-08 for a measurement of the hosted service Snowflake, to
find out whether dbimp can have a driver for it. `dbmeta` and `dbrun` connect
to a Snowflake account now (dbmeta D117, D144 and D182). It is a hosted service
with no container, so it differs from every other target here.

- The package would be `snowflake`, and it would register the name `snowflake`
  (D26 and D28). It would take statements over the SQL REST API of Snowflake,
  `/api/v2/statements`, which is HTTPS and JSON (D14). It would not use the
  protocol of `gosnowflake`, which downloads results in Arrow chunks, a binary
  encoding that hard rule 6 allows only with the approval of Ken for one
  database (D13).
- `usql` reaches Snowflake with `snowflakedb/gosnowflake` already (D24). A
  driver here can replace it only if it matches that client for a caller.
- The measurement runs on the live account that `dbrun` names. A person
  provisioned its credentials (dbmeta D117). The measurement reads them only
  through `dbrun dsn --json`, never prints them, and writes none into a file of
  this repository. Each statement is small, because the account is a trial that
  ends after 30 days or when its credit of 400 dollars is used.
- Whether Snowflake can be a driver depends on step 2 of
  [DRIVER.md](../DRIVER.md). The driver goes through steps 2 to 9, and Ken
  decides its design in step 9.
