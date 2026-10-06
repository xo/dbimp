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

## The six drivers of D162

Ken named six targets after Avatica on 2026-10-01 (D162). Each one goes
through step 8a of [DRIVER.md](DRIVER.md), and then all six go through step
9 together. No Go code of a driver exists yet. The scratch notes of each
target are outside the repository, in the scratch folder of the session.
They can be lost, so the files below are the record.

| Target | Work item | Step reached | On disk | Next |
| --- | --- | --- | --- | --- |
| Apache Druid | W25 | Step 17a: done | `druid/`, its tests, and DRUID.md with the comparison of step 17a, staged for Ken | Ken reviews the staged driver and its open questions |
| Apache Drill | W26 | Step 9: decided | Recordings, `features.json`, the document with its type table, and D165 | Step 10, after the driver before it in D162 |
| Apache Solr | W27 | Step 9: decided | Recordings, `features.json`, the document with its type table, and D166 | Step 10, after the driver before it in D162 |
| Elasticsearch | W28 | Step 9: decided | Recordings, `features.json`, the document with its type table, and D167 | Step 10, after the driver before it in D162 |
| OpenSearch | W29 | Step 9: decided | Recordings, `features.json`, the document with its type table, and D168 | Step 10, after the driver before it in D162 |
| DynamoDB | W30 | Step 9: decided | Recordings, `features.json`, `docs/DYNAMODB.md` with its type table, and D169. D163 dropped Alternator | Step 10, after OpenSearch |

## Next

Ken accepted D163 to D169 on 2026-10-02. Each driver goes on at step 10,
one driver at a time, in the order of D162: Druid, Drill, Solr,
Elasticsearch, OpenSearch, then DynamoDB.

## Waiting for Ken

- The review and the commit of the staged Druid driver (W25).
