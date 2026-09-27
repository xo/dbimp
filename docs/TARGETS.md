# Targets

This file names every database that `dbimp` aims to support, the priority
of each one, the order in which Ken listed them, and the review of the list. It
is the only copy of that list (D15). Each target
has an HTTP or HTTPS interface that takes a query that a person can type by
hand: SQL, a dialect like SQL, JSON, or another query language (D14).

The list came from three notes in `xo/websql/notes` on 2026-09-27:
`drivers.txt`, `targets.csv` and `targets-2.csv`. The row for PostgREST in
`targets-2.csv` had no links, and the links below are the ones on the
PostgREST website. Nothing in this file is measured against a server yet.
Measure a fact before a driver depends on it.

## Priorities

Ken set two priorities on 2026-09-27. A P1 target is in `targets.csv`, and a
P2 target is in `targets-2.csv`. The tables at the end keep that split. A
priority says how soon a target gets a driver. The word is "priority" and not
"tier", because `dbmeta` uses tier for the Tested, Nightly, Verified and
Archived releases in CI (D32).

Ken also named the ideal target (D16). An ideal target meets three tests:

- R: `dbrun` can start it.
- H: it takes queries and returns results over HTTP.
- S: it has a SQL dialect, or a dialect like SQL.

The review below measures each target against those three tests. It places
each one in P1, P2 or P3 by the rule of D17, which Ken accepted. The tables at
the end keep the split of the two lists that Ken wrote.

## The order

Ken wrote this order in `drivers.txt`, with these numbers. A blank line in
that file separates the groups, and each group here is one paragraph.
Couchbase is first (D23). Ken will arrange the rest after the first driver is
complete (Q1 in [PLAN.md](PLAN.md)).

1. Couchbase, with N1QL. It is the first driver, and it replaces `xo/n1ql`
   (D23 in [PLAN.md](PLAN.md)).
2. SurrealDB, with SurrealQL.
3. Neo4j, with Cypher.
4. ArangoDB.
5. Dgraph.

6. InfluxDB 3.
7. CrateDB.
8. QuestDB.
9. InfluxDB.

10. Cassandra through the Stargate API.
11. ScyllaDB and DynamoDB. The note asks whether the two are the same
    interface.

12. CouchDB.
13. MongoDB.

14. rqlite.
15. Turso, or libSQL.

16. Elasticsearch.
17. Meilisearch, or Typesense.

18. TerminusDB.

19. SPARQL: Apache Jena Fuseki, Virtuoso, Stardog, GraphDB and Blazegraph.

20. Gel, which was EdgeDB.

21. Qdrant.
22. Milvus, Weaviate, or Chroma.
23. Apache Druid.

`drivers.txt` names Meilisearch, Typesense, Milvus, Chroma, Virtuoso,
Stardog, GraphDB and Blazegraph, and the two tables do not. The tables name
these, and `drivers.txt` does not: ClickHouse, CouchDB through its view
interface, Trino and Presto, OpenSearch, Apache Pinot, ksqlDB, TDengine,
SingleStore, Snowflake, Google BigQuery, Databricks, Apache Drill, Fauna,
PostgREST, Neon, PlanetScale and Apache Solr.

## The review

On 2026-09-27 the `dbmeta` and `cql` sessions and DeepSeek reviewed the list
against D16. Gemini was asked three times and timed out each time. Unless a
line says otherwise, a fact below is not measured. Measure it before a
driver depends on it.

`dbrun` starts one container for each server, and publishes one port. A
target that needs a second container fails R until `dbmeta` teaches `dbrun`
to run a pod, which is a design change for Ken. A target that is only a
cloud service fails R for good, unless an emulator exists. Every target that
`dbrun` starts first needs its entry in `dbmeta/container/`.

### P1: meets R, H and S

| Target | Notes |
| --- | --- |
| ClickHouse | `dbrun` starts it today (measured in `dbmeta`). `usql` already has a driver, so it is likely P2 (D24). 64-bit integers arrive quoted in JSON. |
| Couchbase | `dbrun` starts it today (measured in `dbmeta`). The first driver (D23). Couchbase Analytics is part of this target, and not a target of its own. See "Couchbase Analytics" in [COUCHBASE.md](COUCHBASE.md). |
| Trino and Presto | `dbrun` starts both today (measured in `dbmeta`). `usql` already has drivers, so they are likely P2 (D24). Results page through `nextUri`, and an error can arrive on a later page. |
| CrateDB | Returns a column list, type codes and row arrays. Binds arguments. |
| QuestDB | Returns a column list. `/exec` binds no arguments. |
| InfluxDB 3 | SQL, with an `information_schema`. InfluxDB 1 takes InfluxQL. The JSON output can lack a schema, and the Arrow output has one. |
| rqlite | SQLite over HTTP. Returns columns and types. The `dbmeta` model for sqlite3 can read it as a flavor. |
| libSQL | The same as rqlite for `dbmeta`. The local `libsql-server` passes R. Turso is the same protocol in the cloud. |
| TDengine | Moves up from P2. Its REST interface is `taosAdapter` on port 6041, and it returns column metadata. |
| Apache Drill | Moves up from P2. Its REST interface can cap the size of a result. |
| Apache Pinot | Moves up from P2. The QuickStart image runs every part in one container. A selection with no `LIMIT` returns ten rows. |
| Apache Solr | Moves up from P2. Parallel SQL needs SolrCloud mode, which runs in one container. |
| Apache Druid | The quickstart runs in one container, but it can exceed the 4 GB limit of `dbmeta`. Measure that before R counts. |
| Elasticsearch and OpenSearch | Only through `_sql`, which pages with a cursor. The Query DSL and ES\|QL fail S. |
| SurrealDB | SurrealQL has `SELECT`, `FROM` and `WHERE`. Returns objects and no column list. |
| Amazon DynamoDB | PartiQL, on `amazon/dynamodb-local`, which supports `ExecuteStatement` (not measured). Needs SigV4. An item is a map with no column order, and a number can have 38 digits. `usql` already has a driver, so it is likely P2 (D24). |
| Databend | New. SQL on `/v1/query`. `usql` has a driver, and dbmeta D66 names it. |
| GreptimeDB | New. SQL on `/v1/sql`. |
| Apache Phoenix Query Server and Apache Calcite Avatica | New. A reviewer named it, and neither list of Ken does. DeepSeek placed the Phoenix Query Server in P1. It speaks the Avatica protocol, with JSON or protobuf over HTTP, so one driver can serve both. `usql` has a driver for Avatica, so it is likely P2 (D24). |
| Apache Kylin | New. A reviewer named it. DeepSeek placed it in P1. SQL over HTTP. |
| OrientDB | New. A reviewer named it. DeepSeek placed it in P1 or P2, and named it among the best additions to P1. It has a dialect like SQL over HTTP. DeepSeek was not sure of the licence. |

Two targets pass only if a query language that matches graph patterns counts
as S. That is Ken's decision:

- Apache Jena Fuseki, and the other SPARQL servers. A binding carries a type,
  a datatype and a language, and an unbound variable must be nil. No image
  that the Apache project publishes is known. DeepSeek placed Virtuoso in
  P2, for its GPL licence and the details of its SQL over HTTP.
- Gel. EdgeQL has `SELECT` and `FILTER` on a typed object model, and a rich
  catalog.

### P2: fails one test

Fails S:

- ArangoDB: AQL is `FOR`, `FILTER` and `RETURN`.
- Neo4j: Cypher. The Community image is GPL. Enterprise needs a licence
  acceptance that a decision must record.
- Dgraph: DQL and GraphQL.
- CouchDB: Mango is JSON.
- TerminusDB: WOQL.
- Qdrant and Weaviate: JSON filters and GraphQL.
- Meilisearch, Typesense and Chroma: JSON bodies. They are in `drivers.txt`
  only.
- ScyllaDB Alternator: Alternator does not support PartiQL
  (`ExecuteStatement`), from `docs/alternator/compatibility.md` in
  `scylladb/scylladb`, which the `cql` session read. It also needs its own
  container entry, because it uses a second port.

Fails R:

- Cassandra through Stargate: Cassandra, a coordinator and the REST service
  are three processes. `/v2/cql` is off by default, binds no arguments and
  returns JSON with no types. `xo/cql` covers Cassandra already, so the `cql`
  session advises P3.
- ksqlDB: needs Kafka.
- SingleStore: the image needs a licence key from an account.
- Snowflake, BigQuery and Databricks: cloud only. A community emulator for
  BigQuery exists, and it can move BigQuery up if it answers the same. `usql`
  has drivers for all three (D24).
- Neon: cloud only. A local copy needs a proxy and PostgreSQL.
- PlanetScale: cloud only, and `psdb.v1alpha1` is the internal protocol of
  its serverless driver, not a documented public API.

Placed in P2 by Ken on 2026-09-27. These places are his opinion, and
nothing about these targets is measured:

- Azure Cosmos DB. `usql` has a driver, and a Linux emulator exists.
- Apache Ignite. dbmeta D66 names it.
- VoltDB. dbmeta D66 names it.
- Amazon Athena, which is cloud only.
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
- PostgREST: needs PostgreSQL, and its filter language in the URL is not a
  statement.
- Milvus: JSON bodies, and the standalone server needs etcd and MinIO.

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
TDengine. Each has a catalog that a `dbmeta` model can read.

## The first list: targets.csv

The 24 rows of `targets.csv`. The link on each name goes to the
documentation of its HTTP interface.

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
| [Neo4j](https://neo4j.com/docs/http-api/current/) | Graph | Cypher | `POST /db/{database}/tx/commit` | application/json | application/json |
| [Dgraph](https://dgraph.io/docs/query-language/dql-fundamentals/) | Graph | DQL / GraphQL | `POST /query, POST /graphql` | application/dql, application/graphql, application/json | application/json |
| [InfluxDB](https://docs.influxdata.com/influxdb3/core/query-data/execute-queries/http-api/) | Time-series | SQL (v3) / InfluxQL (v1) | `POST /api/v3/query, POST /query` | application/json, application/sql | application/json, text/csv, application/vnd.apache.arrow.stream |
| [rqlite](https://rqlite.io/docs/api/http-api/) | Distributed Relational (SQLite) | SQL | `POST /db/query, POST /db/execute` | application/json | application/json |
| [Turso / libSQL](https://docs.turso.tech/sdk/http/reference) | Distributed Relational (SQLite) | SQL | `POST /v2/pipeline, POST /v1/queries` | application/json | application/json |
| [Trino / Presto](https://trino.io/docs/current/develop/client-protocol.html) | Federated Columnar OLAP | SQL | `POST /v1/statement` | text/plain | application/json |
| [Apache Druid](https://druid.apache.org/docs/latest/querying/sql-api) | Columnar OLAP | Druid SQL / Native JSON | `POST /druid/v2/sql, POST /druid/v2` | application/json | application/json, text/csv |
| [Elasticsearch / OpenSearch](https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-sql-query) | Search / Document | Query DSL / SQL / ES\|QL | `POST /_search, POST /_sql` | application/json, application/cbor, application/smile, application/x-ndjson | application/json, application/cbor, text/csv |
| [Apache Jena Fuseki (SPARQL)](https://jena.apache.org/documentation/fuseki2/fuseki-server-protocol.html) | RDF Graph | SPARQL | `POST /{dataset}/query, GET /{dataset}/query` | application/sparql-query, application/x-www-form-urlencoded | application/sparql-results+json, text/turtle, text/csv |
| [Gel (formerly EdgeDB)](https://docs.gel.com/reference/edgeql_over_http) | Graph-Relational | EdgeQL / GraphQL | `POST /db/{db}/edgeql, POST /db/{db}/graphql` | application/json, application/graphql | application/json |
| [TerminusDB](https://terminusdb.com/docs/terminuscms/reference-guides/rest-api) | Versioned Graph / Document | WOQL (Datalog) / GraphQL | `POST /api/woql/{org}/{db}` | application/json | application/json |
| [Qdrant](https://api.qdrant.tech/api-reference/search/points) | Vector Search | JSON Filters & Vector Queries | `POST /collections/{col}/points/query` | application/json | application/json |
| [Weaviate](https://weaviate.io/developers/weaviate/api/graphql) | Vector / Graph | GraphQL / REST | `POST /v1/graphql, POST /v1/objects` | application/json, application/graphql | application/json |
| [MongoDB Atlas Data API](https://www.mongodb.com/docs/atlas/app-services/data-api/generated-endpoints/) | Document | MQL (Extended JSON) | `POST /endpoint/data/v1/action/{find, aggregate}` | application/json, application/ejson | application/json, application/ejson |
| [Cassandra via Stargate](https://stargate.io/docs/latest/cql/cql-using.html) | Wide-column / Document | CQL | `POST /v2/cql` | text/plain, application/json | application/json |

## The second list: targets-2.csv

The 13 rows of `targets-2.csv`.

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
