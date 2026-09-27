# D73. The targets after Neo4j, in order

Status: Amended by D74.

Ken decided on 2026-09-27 the order in which the drivers after Neo4j are
written. It answers Q1 in [PLAN.md](../PLAN.md). Couchbase (D23), SurrealDB
and Neo4j came first. The order is this:

1. InfluxDB.
2. CrateDB.
3. ArangoDB.
4. Databend.
5. TDengine.
6. Apache Pinot.
7. rqlite.
8. libSQL, and Turso, which speaks the same protocol in the cloud.

Each driver follows [DRIVER.md](../DRIVER.md), one at a time, in this
order. Step 1 of DRIVER.md needs Ken to name the target, and this decision
names them.

InfluxDB 3 speaks SQL and InfluxQL. InfluxDB 1 speaks InfluxQL, and
InfluxDB 2 speaks Flux (not measured). Which releases and which languages
the InfluxDB driver serves is a decision of its step 9, and not of this
one.

[TARGETS.md](../TARGETS.md) lists other targets that this order does not
name: Dgraph, QuestDB, Cassandra through the Stargate API, ScyllaDB and
DynamoDB, CouchDB and MongoDB. They keep their rows and their priority, and
their place in the order is open.
