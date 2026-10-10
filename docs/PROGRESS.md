# Progress

This file says where the work in progress stands, so that a session that
crashed can start again from it. Update it at each checkpoint. A finished
item leaves this file, and its record stays in [BACKLOG.md](BACKLOG.md).

## How to resume

While the working tree of `../dbmeta` holds changes that do not compile,
`go run ./cmd/dbrun` fails. Build `dbrun` from `git archive HEAD` of dbmeta
into a scratch folder, and run that copy.

1. Read this file, then run `git status` and `git log --oneline -3`.
2. Work in low-resource mode. Run at most one agent, and keep at most one
   server of `dbrun` running at a time.
3. Before you start a server, run `dbrun status` from `../dbmeta/test`, and
   remove each server of ours that no step needs.
4. At each checkpoint, update this file, and commit the finished part to
   `main`, as a commit that says what it holds, with no commit that is
   only a checkpoint. Do not push. Ken decides when `main` is pushed.

## The drivers after Avatica

Ken named six targets after Avatica on 2026-10-01 (D162). Each one goes
through step 8a of [DRIVER.md](DRIVER.md), and then all six go through step
9 together. Ken added Trino and Presto (D173) and ClickHouse (D174) on
2026-10-07, so the table below holds eight rows. The scratch notes of each target are outside the repository, in
the scratch folder of the session. They can be lost, so the files below are
the record.

| Target | Work item | Step reached | On disk | Next |
| --- | --- | --- | --- | --- |
| Apache Druid | W25 | Released in `v0.11.0` | The driver, its tests and DRUID.md. Its integration jobs in CI had not finished when it was tagged | Watch the nightly run, which runs every type of the round trip |
| Apache Drill | W26 | Released in `v0.14.0` | `drill/`, its tests, the type table, the interface table, the sections Integration tests and Compared with Couchbase of DRILL.md. The integration tests passed on 1.21.2 and 1.22.0 as both principals | The `dburl`, `dbmeta` and `usql` sessions check their side against `v0.14.0` |
| Apache Solr | W27 | Released in `v0.14.0` | `solr/`, its tests, the type table, the interface table, the sections Integration tests and Compared with Couchbase of SOLR.md. The integration tests passed on 9.9.0, 9.10.1 and 10.0.0 as both principals | The `dburl`, `dbmeta` and `usql` sessions check their side against `v0.14.0` |
| Elasticsearch | W28 | Released in `v0.14.0` | `elasticsearch/`, its tests, the type table, the interface table, the sections Faults, Integration tests and Compared with Couchbase of ELASTICSEARCH.md. The integration tests passed on 8.19.22, 9.4.6 and 9.5.3 as both principals | The `dburl`, `dbmeta` and `usql` sessions check their side against `v0.14.0` |
| OpenSearch | W29 | Released in `v0.14.0` | `opensearch/`, its tests, the type table, the interface table, the sections Faults, Integration tests and Compared with Couchbase of OPENSEARCH.md. The integration tests passed on 2.19.6 and 3.9.0 as both principals. `dbrun` no longer starts 3.8.0, so step 6 was recorded again on 3.9.0 | The `dburl`, `dbmeta` and `usql` sessions check their side against `v0.14.0` |
| DynamoDB | W30 | Released in `v0.14.0` | `dynamodb/`, its tests, the type table, the interface table, the sections Integration tests and Compared with Couchbase of DYNAMODB.md, and the removal of Alternator from the recordings and from the workflow, which Ken decided on 2026-10-07. The integration tests passed on 3.2.0 and 3.3.1 as the administrator, which is the one principal | The `dburl`, `dbmeta` and `usql` sessions check their side against `v0.14.0` |
| Trino and Presto | W31 | Released in `v0.12.0` | `trino/`, its tests, TRINO.md and D175. CI passed 30 of 30 jobs on the tagged commit | The `dburl`, `dbmeta` and `usql` sessions check their side against `v0.12.0` |
| Snowflake | W34 | Released in `v0.16.0` | `snowflake/`, its tests, SNOWFLAKE.md, D182 and D183, and the change of the workflow that leaves a hosted driver out. CI passed 42 of 42 jobs on the tagged commit. The integration tests ran by hand on the live trial account on 2026-10-10 and passed | The `dburl`, `dbmeta` and `usql` sessions check their side against `v0.16.0`: `dburl` writes the key as the password of the URL |
| Databricks | W39 | Released in `v0.17.0` | `databricks/`, its tests, DATABRICKS.md and D188 to D193. The integration tests passed live on a free workspace | Step 20: the requests to the consumers |
| BigQuery | W35 | Released in `v0.17.0` | `bigquery/`, its tests, BIGQUERY.md and D184, D189, D195. The integration tests passed on the emulator `bigquery-0.8.1` and on the hosted service | Step 20 |
| Athena | W37 | Released in `v0.17.0` | `athena/`, its tests, ATHENA.md and D192. The integration tests passed on a hosted account | Step 20 |
| Cosmos DB | W36 and W41 | Released in `v0.17.0` | `cosmos/`, its tests, COSMOS.md, D190 and the catalog statements for `dbmeta`. The integration tests passed on the emulator and on a hosted account | Step 20, and tell `dbmeta` the statements |
| Spanner | W38 | Released in `v0.17.0` | `spanner/`, its tests, SPANNER.md, D187, D191 and D195. The integration tests passed on a hosted instance | Step 20, and the emulator entry of `dbmeta` |
| ClickHouse | W32 | Released in `v0.13.0` | `clickhouse/`, its tests, CLICKHOUSE.md, D176 and D177. CI passed 32 of 32 jobs on the tagged commit, with the integration jobs of 25.8 and 26.9 | Step 20: the requests to `dburl` and `usql` (the request to `dbmeta` is done by its `v0.3.0`) |

## Next

The order of D162 is done as far as step 17a. Druid, Trino and Presto, and
ClickHouse are released. Drill, Solr, Elasticsearch, OpenSearch and DynamoDB
are committed and wait for their release. Ken reviews the open questions that
D178 left, and then the release of those five goes through steps 18 to 20 of
[DRIVER.md](DRIVER.md), when Ken says so.

## Waiting for Ken

- The open questions of DRILL.md, SOLR.md, ELASTICSEARCH.md, OPENSEARCH.md and
  DYNAMODB.md that D178 left open. The rows of the table above name them.
- The release of the five committed drivers.
