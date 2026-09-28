# D87. Milvus and PostgREST move to P2, and ksqlDB stays in P2

Status: Amends D17.

Ken decided on 2026-09-29 how the rule of D17 places three targets, after
dbmeta D118 gave each of them a `dbrun` entry. Each one failed R before.

- Milvus moves from P3 to P2. It failed R, because the standalone server
  needed etcd and MinIO, and it fails S, because its queries are JSON bodies.
  `dbrun` starts it in one container, with etcd inside it and its data on the
  local disk, so it fails S only.
- PostgREST moves from P3 to P2. It failed R, because it needs PostgreSQL,
  and it fails S, because its filter language in the URL is not a statement.
  `dbrun` starts it on PostgreSQL 18, so it fails S only. D17 had moved it to
  P3.
- ksqlDB stays in P2. It failed R only, because it needs Kafka, and `dbrun`
  starts it with Kafka. By D17 it would move to P1. A push query streams new
  rows for as long as the client listens, and never ends, so it stays in P2
  until step 2 of [DRIVER.md](../DRIVER.md) measures its push queries and its
  pull queries.

Only R is measured for the three. H and S are the opinions of the review of
2026-09-27. A priority does not set the order, which D73 and D74 hold.
