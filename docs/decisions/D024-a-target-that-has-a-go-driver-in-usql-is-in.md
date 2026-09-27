# D24. A target that has a Go driver in usql is in scope

Status: Decided.

Ken decided this on 2026-09-27. A second goal of this repository is to reduce
the dependencies of `usql`. A driver here can replace a driver that `usql`
imports now, such as the ones for ClickHouse, Trino, Presto, DynamoDB,
Snowflake, BigQuery and Databricks. Such a target is likely P2.

When a driver here replaces one in `usql`, `dbmeta` measures its model again
on the new driver, because `dbmeta` hard rule 10 requires the package that
`usql` uses.
