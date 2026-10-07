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
9 together. The scratch notes of each target are outside the repository, in
the scratch folder of the session. They can be lost, so the files below are
the record.

| Target | Work item | Step reached | On disk | Next |
| --- | --- | --- | --- | --- |
| Apache Druid | W25 | Released in `v0.11.0` | The driver, its tests and DRUID.md. Its integration jobs in CI had not finished when it was tagged | Watch the nightly run, which runs every type of the round trip |
| Apache Drill | W26 | Step 17a: staged for Ken | `drill/`, its tests, the type table, the interface table, the sections Integration tests and Compared with Couchbase of DRILL.md. The integration tests passed on 1.21.2 and 1.22.0 as both principals | Ken reviews the staged work and the open questions 9 to 15 of DRILL.md |
| Apache Solr | W27 | Step 17a: staged for Ken | `solr/`, its tests, the type table, the interface table, the sections Integration tests and Compared with Couchbase of SOLR.md. The integration tests passed on 9.9.0, 9.10.1 and 10.0.0 as both principals | Ken reviews the staged work and the open questions 8 to 15 of SOLR.md |
| Elasticsearch | W28 | Step 17a: staged for Ken | `elasticsearch/`, its tests, the type table, the interface table, the sections Faults, Integration tests and Compared with Couchbase of ELASTICSEARCH.md. The integration tests passed on 8.19.22, 9.4.6 and 9.5.3 as both principals | Ken reviews the staged work and the open questions of ELASTICSEARCH.md |
| OpenSearch | W29 | Step 17a: staged for Ken | `opensearch/`, its tests, the type table, the interface table, the sections Faults, Integration tests and Compared with Couchbase of OPENSEARCH.md. The integration tests passed on 2.19.6 and 3.9.0 as both principals. `dbrun` no longer starts 3.8.0, which step 6 recorded | Ken reviews the staged work and the open questions 11 to 18 of OPENSEARCH.md |
| DynamoDB | W30 | Step 17a: staged for Ken | `dynamodb/`, its tests, the type table, the interface table, the sections Integration tests and Compared with Couchbase of DYNAMODB.md, and the line of the workflow that drops Alternator for this driver. The integration tests passed on 3.2.0 and 3.3.1 as the administrator, which is the one principal | Ken reviews the staged work and the open questions of DYNAMODB.md |
| Trino and Presto | W31 | Released in `v0.12.0` | `trino/`, its tests, TRINO.md and D175. CI passed 30 of 30 jobs on the tagged commit | The `dburl`, `dbmeta` and `usql` sessions check their side against `v0.12.0` |
| ClickHouse | W32 | Released in `v0.13.0` | `clickhouse/`, its tests, CLICKHOUSE.md, D176 and D177. CI passed 32 of 32 jobs on the tagged commit, with the integration jobs of 25.8 and 26.9 | Step 20: the requests to `dburl` and `usql` (the request to `dbmeta` is done by its `v0.3.0`) |

## Next

Ken accepted D163 to D169 on 2026-10-02. Each driver goes on at step 10,
one driver at a time, in the order of D162: Druid, Drill, Solr,
Elasticsearch, OpenSearch, then DynamoDB.

## Waiting for Ken

- The review and the commit of the staged Druid driver (W25).
- The review and the commit of the staged DynamoDB driver (W30), and the open
  questions of DYNAMODB.md.
