# Targets

This file names every database that `dbimp` aims to support, the priority
of each one, the order in which Ken listed them, and the review of the list. It
is the only copy of that list (D15). Each target
has an HTTP or HTTPS interface that takes a query that a person can type by
hand: SQL, a dialect like SQL, JSON, or another query language (D14).

The list came from three notes in `xo/websql/notes` on 2026-09-27:
`drivers.txt`, `targets.csv` and `targets-2.csv`. The row for PostgREST in
`targets-2.csv` had no links, and the links below are the ones on the
PostgREST website. Unless a line says otherwise, a fact in this file is not
measured against a server. Measure a fact before a driver depends on it.

## Priorities

Ken set two priorities on 2026-09-27. He first placed each target of
`targets.csv` in P1, and each target of `targets-2.csv` in P2, and the review
moved some of them (D17). A priority says how soon a target gets a driver.
The word is "priority" and not "tier", because `dbmeta` uses tier for the
Tested, Nightly, Verified and Staged releases in CI (D32 and dbmeta D119).

Ken also named the ideal target (D16). An ideal target meets three tests:

- R: `dbrun` can start it.
- H: it takes queries and returns results over HTTP.
- S: it has a SQL dialect, or a dialect like SQL.

D58 and D75 amend S. A graph query language that a person types meets it,
and so do AQL and Flux.

The review below measures each target against those three tests. It places
each one in P1, P2 or P3 by the rule of D17, which Ken accepted. The tables at
the end keep the split of the two lists that Ken wrote.

## The order

Ken set the order of the work on 2026-09-27 (D73 and D74), and D88
changed it. After Couchbase, SurrealDB and Neo4j, the targets are these, in
order:

1. InfluxDB 1, InfluxDB 2, and InfluxDB 3 and later, in one driver (D78).
   Done, in `v0.4.0`. See [INFLUXDB.md](INFLUXDB.md).
2. CrateDB gets no driver here, because `usql` reaches it with `pgx` on the
   PostgreSQL wire protocol (D88). See [CRATEDB.md](CRATEDB.md).
3. ArangoDB. Done, in `v0.5.0` (W13). See [ARANGODB.md](ARANGODB.md).
4. Databend. Done, in `v0.6.0` (W16). See [DATABEND.md](DATABEND.md).
5. TDengine gets no driver here, because its REST interface cuts a result
   short with no sign (D127). See [TDENGINE.md](TDENGINE.md).
6. Apache Pinot. Done, in `v0.7.0` (W19).
   See [PINOT.md](PINOT.md).
7. rqlite. Done, in `v0.8.0` (W22). See
   [RQLITE.md](RQLITE.md).
8. libSQL, and Turso. Done, in `v0.9.0` (W23).
   See [LIBSQL.md](LIBSQL.md).
9. Apache Calcite Avatica, and the products that speak it: the standalone
   Avatica server and the Apache Phoenix Query Server (D74). Done, in
   `v0.10.0` (W24). Druid gets a driver of its own (D154). See
   [AVATICA.md](AVATICA.md).
10. Apache Druid (W25 and D162). Done, in `v0.11.0`.
11. Apache Drill, over its REST interface (W26 and D165). Done, in `v0.14.0`.
12. Apache Solr, over Parallel SQL (W27 and D166). Done, in `v0.14.0`.
13. Elasticsearch (W28). OpenSearch gets a driver of its own (D162). Done, in
    `v0.14.0`.
14. OpenSearch (W29, D162 and D168). The driver is
    `github.com/xo/dbimp/opensearch`. Done, in `v0.14.0`.
15. Amazon DynamoDB (W30). ScyllaDB Alternator gets no support (D163). Done,
    in `v0.14.0`.
16. Trino and Presto, in one driver with two flavors (W31 and D173). Done, in
    `v0.12.0`.
17. ClickHouse, over its HTTP interface (W32 and D174). Done, in `v0.13.0`.
18. Google BigQuery, over its REST API (W35, D184 and D189). Done, in `v0.17.0`. The driver is `github.com/xo/dbimp/bigquery`.
    Its integration tests run on the emulator release `bigquery-0.8.1`, and on the
    service where a person supplies a project and a key file. See
    [BIGQUERY.md](BIGQUERY.md).
19. Azure Cosmos DB, over its REST API (W36, D184 and D190). Done, in `v0.17.0`. The driver is `github.com/xo/dbimp/cosmos`.
    It reads documents and answers catalog statements (W41). See
    [COSMOS.md](COSMOS.md).
20. Amazon Athena, over its API (W37, D184 and D192). Done, in `v0.17.0`. The driver
    is `github.com/xo/dbimp/athena`. Its integration tests run only where a person
    supplies an account. See
    [ATHENA.md](ATHENA.md).
21. Databricks, over its SQL Statement Execution API (W39, D188 and D193). Done,
    in `v0.17.0`. The driver is `github.com/xo/dbimp/databricks`. Its integration
    tests run only where a person supplies a workspace. See [DATABRICKS.md](DATABRICKS.md). `usql` has a driver, which the
    new one can replace (D24).

The other targets below keep their priority, and their place in the order
is open.

### The first list

Ken wrote this order in `drivers.txt`, with these numbers. A blank line in
that file separates the groups, and each group here is one paragraph. The
numbers 10 and 20 are gone, because Stargate and Gel left the list (D84).
Couchbase is first (D23). Ken arranged the rest after the first driver was
complete (Q1 in [PLAN.md](PLAN.md), which D73 answers).

1. Couchbase, with N1QL. It is the first driver, and it replaces `xo/n1ql`
   (D23 in [decisions/](decisions/README.md)).
2. SurrealDB, with SurrealQL.
3. Neo4j, with Cypher.
4. ArangoDB.
5. Dgraph.

6. InfluxDB 3.
7. CrateDB.
8. QuestDB.
9. InfluxDB.

11. ScyllaDB and DynamoDB. The note asks whether the two are the same
    interface.

12. CouchDB.
13. MongoDB.

14. rqlite.
15. Turso, or libSQL.

16. Elasticsearch.
17. Meilisearch, or Typesense.

18. TerminusDB.

19. SPARQL: Apache Jena Fuseki, Virtuoso, Stardog and GraphDB.

21. Qdrant.
22. Milvus, Weaviate, or Chroma.
23. Apache Druid.

`drivers.txt` names Meilisearch, Typesense, Milvus, Chroma, Virtuoso,
Stardog and GraphDB, and the two tables do not. The tables name
these, and `drivers.txt` does not: ClickHouse, CouchDB through its view
interface, Trino and Presto, OpenSearch, Apache Pinot, ksqlDB, TDengine,
SingleStore, Snowflake, Google BigQuery, Databricks, Apache Drill, Fauna,
PostgREST, Neon, PlanetScale and Apache Solr.

## The review

On 2026-09-27 the `dbmeta` and `cql` sessions and DeepSeek reviewed the list
against D16. The session asked Gemini three times, and it timed out each
time. Unless a
line says otherwise, a fact below is not measured. Measure it before a
driver depends on it.

`dbrun` starts one container for each server, and publishes one port. A
target that needs a second container fails R until `dbmeta` teaches `dbrun`
to run a pod, which is a design change for Ken. A target that is only a
cloud service fails R for good, unless an emulator exists. Every target that
`dbrun` starts first needs its entry in `dbmeta/container/`.

dbmeta D112 adds the entries for the targets from InfluxDB to libSQL in the
order, and dbmeta D113 adds them for Avatica, the Phoenix Query Server
and Druid. dbmeta D114 adds InfluxDB 1 and InfluxDB 2. `dbmeta` committed
each one on 2026-09-28. The `dbmeta` session measured each entry
through `dbrun`, and each Tested release of dbmeta D112 passed `dbrun test`
on 2026-09-28. So each of these targets meets R by that measurement.

dbmeta D118, committed on 2026-09-28, adds entries for most of the other
targets. The `dbmeta` session measured each one through `dbrun` on
2026-09-28, with an ordinary user that reads and is refused a write. The
entries are Dgraph, QuestDB, CouchDB, MongoDB, Elasticsearch, OpenSearch,
Meilisearch, Typesense, Apache Solr in SolrCloud mode, TerminusDB, Apache
Jena Fuseki, Virtuoso, Qdrant, Milvus, Weaviate, Chroma, ksqlDB with Kafka,
Apache Drill, PostgREST on PostgreSQL 18, DynamoDB Local and ScyllaDB
Alternator. Stardog, GraphDB 11 and VoltDB have entries that appear only
when a licence file from Ken exists, and none of them has started yet.
Several notes below said that a target fails R, and the entries change
that. D87 applies D17 to Milvus, PostgREST and ksqlDB.

### P1: meets R, H and S

| Target | Notes |
| --- | --- |
| ClickHouse | Seventeenth in the order (D174). The driver is `github.com/xo/dbimp/clickhouse` (W32, D176 and D177). `dbrun` starts 25.3, 25.8 and 26.9 (measured in `dbmeta`). `usql` has a driver, `clickhouse-go/v2`, which speaks the native protocol, and this driver speaks HTTP (D24). See [CLICKHOUSE.md](CLICKHOUSE.md). 64-bit integers arrive quoted in JSON. `dburl` waits for a driver here before it settles the port and the TLS of ClickHouse over HTTP (dburl D34). |
| Couchbase | `dbrun` starts it today (measured in `dbmeta`). The first driver (D23). The driver is `github.com/xo/dbimp/couchbase`. Couchbase Analytics is part of this target, and not a target of its own. See "Flavors" in [COUCHBASE.md](COUCHBASE.md). |
| Trino and Presto | Sixteenth in the order (D173). The driver is `github.com/xo/dbimp/trino`, with Presto as a flavor (W31 and D175). `dbrun` starts Trino 476 and 483 and Presto 0.299 (measured in `dbmeta`). `usql` has a driver for each, which this driver can replace (D24). See [TRINO.md](TRINO.md). Results page through `nextUri`, and an error can arrive on a later page. `dburl` waits for a driver here before it settles their ports and their TLS (dburl D34). |
| CrateDB | No driver here (D88). `usql` reaches it with `pgx` on the PostgreSQL wire protocol, which the `dbmeta` session measured on 6.4.5, and `dbmeta` reads it through a dialect of its own. The HTTP interface builds each whole result in the memory of the server, and has no paging. R: `dbrun` starts 6.3.7 and 6.4.5. See [CRATEDB.md](CRATEDB.md). |
| ArangoDB | Next after InfluxDB, by D73 and D88. The driver is `github.com/xo/dbimp/arangodb`, from D89 to D94. R: `dbrun` starts 3.12.12. H: `POST /_api/cursor` on port 8529. S: yes, AQL, by D75. P1. See [ARANGODB.md](ARANGODB.md). |
| QuestDB | No driver here (D170). It speaks the PostgreSQL wire protocol, which `usql` reaches with `pgx`. `/exec` returns a column list and binds no arguments. |
| InfluxDB | First in the order after Neo4j (D73). The driver is `github.com/xo/dbimp/influxdb`. One driver, `influxdb`, serves InfluxDB 1, InfluxDB 2, and InfluxDB 3 and later. Its dialect `influxdb` is SQL on InfluxDB 3 and later, and its dialect `influxql` is InfluxQL through `/query` on each release (D78). Flux meets S (D75), and the driver does not speak it. R: `dbrun` starts each release that D79 names, 1.11.8 to 3.11.5. SQL has an `information_schema`. A JSON result is an array of objects with no NULL keys, so the driver reads the columns from `DESCRIBE` (D80). See [INFLUXDB.md](INFLUXDB.md). |
| rqlite | Seventh in the order after Neo4j (D73). SQLite over HTTP. The driver is `github.com/xo/dbimp/rqlite` (W22 and D141). Returns columns and types. The `dbmeta` model for sqlite3 can read it as a flavor. R: `dbrun` starts 9.4.5 and 10.3.6. H and S: `POST /db/query` answered SQL on both, as both principals (measured on 2026-09-30, W22). See [RQLITE.md](RQLITE.md). |
| libSQL | Eighth in the order after Neo4j (D73). The driver is `github.com/xo/dbimp/libsql` (W23 and D148). Its driver is `libsql`, and `turso` is an alias in `dburl` (D76). The same as rqlite for `dbmeta`. R: `dbrun` starts the local `libsql-server` 0.24.33. Turso is the same protocol in the cloud, and the cloud service fails R. See [LIBSQL.md](LIBSQL.md). |
| Apache Drill | Moves up from P2. Eleventh in the order (D162). The driver is `github.com/xo/dbimp/drill` (W26 and D165). `dbrun` starts 1.21.2 and 1.22.0 (measured in `dbmeta`). `usql` has no driver for it, so the driver replaces none (D24). See [DRILL.md](DRILL.md). It runs `CREATE TABLE AS` and refuses `INSERT`, `UPDATE` and `DELETE` (D163). Its REST interface can cap the size of a result, binds no argument, and runs a query on when the client leaves, so the driver cancels it by its `queryId`. `dburl` waits for a driver here before it settles its scheme, its port and its TLS (dburl D34). |
| Apache Pinot | Moves up from P2. Sixth in the order after Neo4j (D73). The QuickStart image runs every part in one container. A selection with no `LIMIT` returns ten rows on the single-stage engine. The driver is `github.com/xo/dbimp/pinot`, and it takes no write, because the Broker takes none (W19 and D128). R: `dbrun` starts 1.4.0 and 1.5.1. H and S: `POST /query/sql` answered SQL on both, as both principals (measured on 2026-09-30, W19). See [PINOT.md](PINOT.md). |
| Apache Solr | Moves up from P2. Twelfth in the order (D162). Parallel SQL needs SolrCloud mode, which runs in one container. The driver is `github.com/xo/dbimp/solr` (W27 and D166). `dbrun` starts 9.9.0, 9.10.1 and 10.0.0 (measured in `dbmeta`). It reads only, because the SQL of Solr refuses `INSERT`, `UPDATE` and `DELETE` (D163). `usql` has no driver for it, so the driver replaces none (D24). See [SOLR.md](SOLR.md). The answer names no type, so the driver reads the type of each column from `metadata.COLUMNS` and the luke handler. The server binds no argument, so the driver writes each one as a literal. `dburl` has no scheme for it yet. |
| Apache Druid | R: `dbrun` starts 36.0.0 and 37.0.0 in one container, with 2.7 GB at most, inside the 4 GB limit of `dbmeta` (dbmeta D113). It gets a driver of its own, and is not a flavor of Avatica (D154). The driver is `github.com/xo/dbimp/druid` (W25 and D164). It reads only, because the SQL API takes no write (D163). H and S: `POST /druid/v2/sql` answered SQL on both releases, as both principals (recorded on 2026-10-01 and 2026-10-02). See [DRUID.md](DRUID.md). |
| Elasticsearch and OpenSearch | Only through `_sql`, which pages with a cursor. The Query DSL and ES\|QL fail S. Elasticsearch is thirteenth in the order (D162), and its driver is `github.com/xo/dbimp/elasticsearch` (W28 and D167). `dbrun` starts 8.19.22, 9.4.6 and 9.5.3 (measured in `dbmeta`). It reads only, because SQL in Elasticsearch takes no write (D163). `usql` has no driver for it, so the driver replaces none (D24). See [ELASTICSEARCH.md](ELASTICSEARCH.md). OpenSearch is fourteenth in the order (D162), and its driver is `github.com/xo/dbimp/opensearch` (W29, D163 and D168). `dbrun` starts 2.19.6 and 3.9.0 (measured in `dbmeta`). It reads only, because SQL in OpenSearch takes no write (D163). `usql` has no driver for it, so the driver replaces none (D24). See [OPENSEARCH.md](OPENSEARCH.md). The server binds no argument that it keeps apart from the text, so the driver writes each one as a literal. |
| SurrealDB | The second target (W8). The driver is `github.com/xo/dbimp/surrealdb`. R: yes, `dbrun` starts 2.7.0, 3.1.6, 3.2.4 and 3.3.0 (dbmeta D103). H: yes, `POST /rpc` on port 8000. S: yes, SurrealQL. P1. Returns objects and no column list, with the keys sorted by name. See [SURREALDB.md](SURREALDB.md). |
| Neo4j | The third target (W9). The driver is `github.com/xo/dbimp/neo4j`. R: yes, `dbrun` starts 5.26.31 and 2026.09.0 (dbmeta D106). H: yes, the Query API, `POST /db/{database}/query/v2` on port 7474. S: yes, Cypher, by D58. P1. `dbrun` runs the Enterprise Edition under the evaluation agreement (D59). Returns the columns before the rows, in the order of the statement, with typed JSON (D62). See [NEO4J.md](NEO4J.md). |
| Amazon DynamoDB | Fifteenth in the order (D162). The driver is `github.com/xo/dbimp/dynamodb` (W30 and D169). R: `dbrun` starts DynamoDB Local 3.2.0 and 3.3.1, which stand in for the cloud service. H: `POST /` with JSON, signed with AWS Signature Version 4. S: PartiQL through `ExecuteStatement`. ScyllaDB Alternator speaks the same API with no PartiQL, so it fails S, and the driver does not serve it (D163). Its recordings are removed (Ken, 2026-10-07). An item is a map with no column order, and a number can have 38 digits. `usql` reaches it through `btnguyen2k/godynamo`, which this driver can replace (D24). See [DYNAMODB.md](DYNAMODB.md). |
| Databend | New. Fourth in the order after Neo4j (D73). SQL on `/v1/query`. `usql` has a driver, and dbmeta D66 names it. Its driver is `databend`, and it replaces the driver of `usql` (D76). The driver is `github.com/xo/dbimp/databend` (W16). R: `dbrun` starts 1.2.881 and 1.2.948. H and S: `POST /v1/query` answered SQL on both, as both principals (measured on 2026-09-29, W16). See [DATABEND.md](DATABEND.md). |
| GreptimeDB | No driver here (D170). It speaks the MySQL and PostgreSQL wire protocols, which `usql` reaches already. |
| Apache Phoenix Query Server and Apache Calcite Avatica | A reviewer named it, and Ken placed it ninth in the order on 2026-09-27 (D74). The driver is `github.com/xo/dbimp/avatica` (W24). It speaks the Avatica protocol in JSON only (D153), and serves the standalone Avatica server and the Phoenix Query Server. Druid gets a driver of its own (D154). R: `dbrun` starts Avatica 1.28.0 and 1.29.0, the Phoenix Query Server 2.0-5.0, and Druid 36.0.0 and 37.0.0 (dbmeta D113). H: `POST /` on port 8765. S: SQL of HSQLDB or of Phoenix. `usql` has a driver for Avatica (D24). See [AVATICA.md](AVATICA.md). |
| Apache Kylin | New. A reviewer named it. DeepSeek placed it in P1. SQL over HTTP. |
| OrientDB | New. A reviewer named it. DeepSeek placed it in P1 or P2, and named it among the best additions to P1. It has a dialect like SQL over HTTP. DeepSeek was not sure of the licence. |

One group of targets passes only if a query language that matches graph
patterns counts as S. D58 says that such a language meets S when a person types it, and
step 2 of [DRIVER.md](DRIVER.md) judges each target on its own:

- Apache Jena Fuseki, and the other SPARQL servers. A binding carries a type,
  a datatype and a language, and an unbound variable must be nil. The Apache
  project publishes no image, and dbmeta D118 builds one for Fuseki. DeepSeek placed Virtuoso in
  P2, for its GPL licence and the details of its SQL over HTTP.

### P2: fails one test

Fails S:

- Dgraph: DQL and GraphQL.
- CouchDB: Mango is JSON.
- TerminusDB: WOQL.
- Qdrant and Weaviate: JSON filters and GraphQL.
- Meilisearch, Typesense and Chroma: JSON bodies. They are in `drivers.txt`
  only.
- Milvus: JSON bodies. It moved up from P3, because `dbrun` starts the
  standalone server in one container, with etcd inside it and its data on
  the local disk, and no MinIO (dbmeta D118), so it meets R (D87).
- PostgREST: its filter language in the URL is not a statement. It moved up
  from P3, because `dbrun` starts it on PostgreSQL 18 (dbmeta D118), so it
  meets R (D87).
- ScyllaDB Alternator: Alternator does not support PartiQL
  (`ExecuteStatement`), from `docs/alternator/compatibility.md` in
  `scylladb/scylladb`, which the `cql` session read. It also needs its own
  container entry, because it uses a second port.

Fails R:

- SingleStore: Ken chose that `dbrun` gets no entry for it (dbmeta D118).
- Snowflake: cloud only, and the one server is a hosted trial account. Ken asked
  for a driver on 2026-10-09 (D182 and D183). The driver is
  `github.com/xo/dbimp/snowflake` (W34). It speaks the SQL REST API, and its
  integration tests run only where a person supplies an account. Done, in
  `v0.16.0`. See [SNOWFLAKE.md](SNOWFLAKE.md). `usql` has a driver, which the
  new one can replace (D24).
- BigQuery and Databricks: cloud only. A community emulator for BigQuery
  exists. Ken moved BigQuery into the order on 2026-10-10 (D184), and
  Databricks on the same day (D188). The BigQuery driver is
  `github.com/xo/dbimp/bigquery` (W35 and D189). See [BIGQUERY.md](BIGQUERY.md).
  `usql` has drivers for both (D24).
- Neon: cloud only. A local copy needs a proxy and PostgreSQL.
- PlanetScale: cloud only, and `psdb.v1alpha1` is the internal protocol of
  its serverless driver, not a documented public API.

Held in P2 by Ken on 2026-09-29 (D87):

- ksqlDB. `dbrun` starts it with Kafka (dbmeta D118), so it meets R, and it
  meets H and S by the review. A push query streams new rows for as long as
  the client listens, and never ends, so it stays in P2 until step 2 of
  [DRIVER.md](DRIVER.md) measures its push queries and its pull queries.

Placed in P2 by Ken on 2026-09-27. These places are his opinion, and
nothing about these targets is measured:

- Azure Cosmos DB. `usql` has a driver, and a Linux emulator exists. Ken moved it
  into the order on 2026-10-10 (D184).
- Apache Ignite. dbmeta D66 names it.
- VoltDB: no driver here (D180). Ken moved it into the order on 2026-10-08
  (D179), and the `dbmeta` session then found that neither release serves an
  HTTP interface. See [VOLTDB.md](VOLTDB.md).
- Amazon Athena, which is cloud only. Ken moved it into the order on 2026-10-10
  (D184).
- Google Cloud Spanner, over its REST API (W38, D185, D187 and D191). Done, in
  `v0.17.0`. The driver is `github.com/xo/dbimp/spanner`. Spanner Omni serves gRPC
  and no REST API, so D186 had closed it, and D187 reopened it on the hosted
  service. The driver speaks the REST API with JSON. Its integration tests run on
  the Cloud Spanner emulator of `dbmeta` in CI (D196), and on a hosted instance
  where a person supplies a key file. See [SPANNER.md](SPANNER.md). `usql` has a
  driver on gRPC, which the new one can replace (D24).
- Prometheus, with PromQL.
- VictoriaMetrics, with PromQL.
- Loki, with LogQL.
- Datasette. DeepSeek said that it meets R, H and S, but that it can be read
  only.

### P3: fails two tests, or is gone

- MongoDB Atlas Data API: MongoDB ended it on 2025-09-30, with the rest of
  Atlas App Services. The target does not exist.
- Fauna: the service shut down on 2025-05-30. Fauna said that it will release an
  open source version.
- TDengine: no driver here (D127). Its REST interface ends a result that
  failed as valid JSON with `code` 0, so a driver cannot tell a cut result
  from a whole one (measured on 3.3.8.8 and 3.4.2.8). Its WebSocket
  interface reports the failure, and needs a binary encoding and a transport
  that is no request of HTTP for each statement. See
  [TDENGINE.md](TDENGINE.md).

Placed in P3 by Ken on 2026-09-27. These places are his opinion, and
nothing about these targets is measured:

- Kusto (Azure Data Explorer), which is KQL with a local emulator.
- Apache Doris. DeepSeek said that it meets R, H and S if its HTTP interface
  for SQL is sufficient.
- StarRocks. DeepSeek said the same as for Doris.

### What to start with

Two tests break a tie inside P1. The first test asks whether the server
sends column metadata, so that a row keeps the order of the statement. The
second test asks whether `usql` has no driver yet (D24). Both reviewers named
the same first targets: CrateDB, QuestDB, rqlite, libSQL, InfluxDB 3 and
TDengine. Each has a catalog that a `dbmeta` model can read. Ken then set
the order in D73 and D74, and "The order" above holds it.

## The first list: targets.csv

Every row of `targets.csv`, except Gel and Stargate, which left it (D84).
The link on each name goes to the documentation of its HTTP interface.

| Database | Model | Query language | HTTP endpoint | Request type | Response type |
| --- | --- | --- | --- | --- | --- |
| [SurrealDB](https://surrealdb.com/docs/reference/rest-api/http-protocol) | Multi-model (Doc/Graph) | SurrealQL | `POST /sql` | text/plain, application/json | application/json, application/cbor |
| [ArangoDB](https://docs.arangodb.com/stable/develop/http-api/queries/aql-queries/) | Multi-model (Doc/Graph) | AQL | `POST /_api/cursor` | application/json, application/x-velocypack | application/json, application/x-velocypack |
| [ClickHouse](https://clickhouse.com/docs/interfaces/http) | Columnar / OLAP | SQL | `POST /, GET /` | text/plain, application/x-www-form-urlencoded | application/json, text/tab-separated-values, text/csv, application/octet-stream |
| [Couchbase](https://docs.couchbase.com/server/current/n1ql-rest-query/index.html) | Multi-model (Doc/KV) | SQL++ / N1QL | `POST /query/service` | application/json, application/x-www-form-urlencoded | application/json |
| [Apache CouchDB](https://docs.couchdb.org/en/stable/api/database/find.html) | Document (JSON) | Mango (JSON AST) / MapReduce | `POST /{db}/_find, GET /{db}/_design/{ddoc}/_view/{view}` | application/json | application/json |
| [Amazon DynamoDB](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ql-reference.html) | Document / Key-Value | PartiQL (SQL-like) / DynamoDB JSON | `POST /` | application/x-amz-json-1.0 | application/x-amz-json-1.0 |
| [ScyllaDB Alternator](https://docs.scylladb.com/manual/stable/alternator/alternator.html) | Key-Value / Document | PartiQL (SQL-like) / DynamoDB JSON | `POST /` | application/x-amz-json-1.0 | application/x-amz-json-1.0 |
| [CrateDB](https://cratedb.com/docs/crate/reference/en/latest/interfaces/http.html) | Distributed SQL / Document | SQL | `POST /_sql` | application/json | application/json |
| [QuestDB](https://questdb.com/docs/reference/api/rest/) | Time-series | SQL | `GET /exec, POST /exec` | text/plain, application/x-www-form-urlencoded, multipart/form-data | application/json, text/csv |
| [Neo4j](https://neo4j.com/docs/query-api/current/) | Graph | Cypher | `POST /db/{database}/query/v2` | application/json, application/vnd.neo4j.query | application/json, application/vnd.neo4j.query |
| [Dgraph](https://dgraph.io/docs/query-language/dql-fundamentals/) | Graph | DQL / GraphQL | `POST /query, POST /graphql` | application/dql, application/graphql, application/json | application/json |
| [InfluxDB](https://docs.influxdata.com/influxdb3/core/query-data/execute-queries/http-api/) | Time-series | SQL (v3) / InfluxQL (v1) | `POST /api/v3/query, POST /query` | application/json, application/sql | application/json, text/csv, application/vnd.apache.arrow.stream |
| [rqlite](https://rqlite.io/docs/api/http-api/) | Distributed Relational (SQLite) | SQL | `POST /db/query, POST /db/execute` | application/json | application/json |
| [Turso / libSQL](https://docs.turso.tech/sdk/http/reference) | Distributed Relational (SQLite) | SQL | `POST /v2/pipeline, POST /v1/queries` | application/json | application/json |
| [Trino / Presto](https://trino.io/docs/current/develop/client-protocol.html) | Federated Columnar OLAP | SQL | `POST /v1/statement` | text/plain | application/json |
| [Apache Druid](https://druid.apache.org/docs/latest/querying/sql-api) | Columnar OLAP | Druid SQL / Native JSON | `POST /druid/v2/sql, POST /druid/v2` | application/json | application/json, text/csv |
| [Elasticsearch / OpenSearch](https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-sql-query) | Search / Document | Query DSL / SQL / ES\|QL | `POST /_search, POST /_sql` | application/json, application/cbor, application/smile, application/x-ndjson | application/json, application/cbor, text/csv |
| [Apache Jena Fuseki (SPARQL)](https://jena.apache.org/documentation/fuseki2/fuseki-server-protocol.html) | RDF Graph | SPARQL | `POST /{dataset}/query, GET /{dataset}/query` | application/sparql-query, application/x-www-form-urlencoded | application/sparql-results+json, text/turtle, text/csv |
| [TerminusDB](https://terminusdb.com/docs/terminuscms/reference-guides/rest-api) | Versioned Graph / Document | WOQL (Datalog) / GraphQL | `POST /api/woql/{org}/{db}` | application/json | application/json |
| [Qdrant](https://api.qdrant.tech/api-reference/search/points) | Vector Search | JSON Filters & Vector Queries | `POST /collections/{col}/points/query` | application/json | application/json |
| [Weaviate](https://weaviate.io/developers/weaviate/api/graphql) | Vector / Graph | GraphQL / REST | `POST /v1/graphql, POST /v1/objects` | application/json, application/graphql | application/json |
| [MongoDB Atlas Data API](https://www.mongodb.com/docs/atlas/app-services/data-api/generated-endpoints/) | Document | MQL (Extended JSON) | `POST /endpoint/data/v1/action/{find, aggregate}` | application/json, application/ejson | application/json, application/ejson |

## The second list: targets-2.csv

Every row of `targets-2.csv`.

| Database | Model | Query language | HTTP endpoint | Request type | Response type |
| --- | --- | --- | --- | --- | --- |
| [Apache Pinot](https://docs.pinot.apache.org/reference/api-reference/query-api) | Real-time Distributed OLAP | ANSI SQL | `POST /query/sql` | application/json | application/json |
| [ksqlDB](https://docs.ksqldb.io/en/latest/developer-guide/api/) | Streaming SQL Engine (Kafka) | ksqlDB SQL | `POST /query-stream, POST /ksql` | application/json, application/vnd.ksql.v1+json | application/json, application/vnd.ksql.v1+json |
| [TDengine](https://docs.tdengine.com/reference/rest-api/) | Time-series Database | SQL | `POST /rest/sql` | text/plain, application/json | application/json |
| [SingleStore (Data API)](https://docs.singlestore.com/cloud/developer-resources/data-api/) | Distributed Relational / HTAP | SQL | `POST /api/v2/query/rows, POST /api/v2/exec` | application/json | application/json |
| [Snowflake (SQL API)](https://docs.snowflake.com/en/developer-guide/sql-api/index) | Cloud Data Warehouse | ANSI SQL | `POST /api/v2/statements` | application/json | application/json |
| [Google BigQuery](https://cloud.google.com/bigquery/docs/reference/rest/v2/jobs/query) | Serverless Cloud Data Warehouse | GoogleSQL / Legacy SQL | `POST /bigquery/v2/projects/{projectId}/queries` | application/json | application/json |
| [Databricks (SQL Exec API)](https://docs.databricks.com/api/workspace/statementexecution) | Lakehouse / Distributed OLAP | ANSI SQL | `POST /api/2.0/sql/statements` | application/json | application/json, application/vnd.apache.arrow.stream |
| [Apache Drill](https://drill.apache.org/docs/rest-api-introduction/) | Schema-free SQL Query Engine | ANSI SQL | `POST /query.json` | application/json | application/json |
| [Fauna](https://docs.fauna.com/fauna/current/reference/http/) | Document-Relational / Distributed | FQL (Fauna Query Language) | `POST /query` | application/json | application/json |
| [PostgREST (PostgreSQL)](https://docs.postgrest.org) | Relational (Direct PG REST Layer) | Declarative SQL Filter DSL | `GET /{table}?select=...&{filter}, POST /{table}` | application/json, application/x-www-form-urlencoded | application/json, text/csv, application/geo+json |
| [Neon Serverless Driver](https://neon.tech/docs/serverless/serverless-driver) | Serverless Postgres | PostgreSQL SQL | `POST /sql` | application/json | application/json |
| [PlanetScale API](https://planetscale.com/docs/tutorials/planetscale-serverless-driver) | Serverless MySQL (Vitess) | MySQL SQL | `POST /psdb.v1alpha1.Database/Execute` | application/json | application/json |
| [Apache Solr](https://solr.apache.org/guide/solr/latest/query-guide/parallel-sql-interface.html) | Search / Document Engine | SQL (via Parallel SQL Engine) | `POST /solr/{collection}/sql` | application/x-www-form-urlencoded | application/json, text/csv |
