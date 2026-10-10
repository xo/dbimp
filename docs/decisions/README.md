# Decisions

Every decision of this repository is a file in this folder, one for each
decision, named by its number and its title. This table is the index. Find
the number here, then open the file.

Each file opens with its status. `Decided` means that Ken settled it, or
that it follows from a rule every `xo` repository keeps. `Proposed` means a
recommendation that waits for Ken. A decision that changes an earlier one
says so in its status, as "Amends D65", and the earlier one says it back, as
"Amended by D69". Read the status before the decision.

A decision is never edited to change its conclusion. A later decision that
replaces or amends one names it, and the older one gains a status that names
the later one. A new decision gets the next number and a file of its own.
Add its row here. `TestTheDecisionIndexIsComplete` fails when a decision has
no row or a row is wrong, and it prints the row to add.
`TestAnAmendmentPointsBothWays` fails when an amendment names only one side.
D72 moved the decisions here from `docs/PLAN.md`. Work items are in
[BACKLOG.md](../BACKLOG.md), and the two series never mix.

| # | Decision | Status |
| --- | --- | --- |
| [D1](D001-the-module-path-is-github-com-xo-dbimp.md) | The module path is github.com/xo/dbimp | Decided |
| [D2](D002-the-go-directive-is-1-27-1.md) | The go directive is 1.27.1 | Decided |
| [D3](D003-the-repository-uses-the-xo-layout.md) | The repository uses the xo layout | Amended by D71 and D72 |
| [D4](D004-each-driver-is-its-own-package-and-shared-code.md) | Each driver is its own package, and shared code is in the root package | Decided |
| [D5](D005-dburl-owns-the-schemes-and-the-aliases.md) | dburl owns the schemes and the aliases | Decided |
| [D6](D006-go-conventions-are-the-ones-dburl-and-dbmeta.md) | Go conventions are the ones dburl and dbmeta keep | Decided |
| [D7](D007-a-package-holds-no-configuration.md) | A package holds no configuration | Decided |
| [D8](D008-every-driver-keeps-the-same-contract-with.md) | Every driver keeps the same contract with database/sql | Decided |
| [D9](D009-a-test-that-needs-a-server-reads-a-dsn-and-skips.md) | A test that needs a server reads a DSN and skips without one | Decided |
| [D10](D010-golangci-lint-runs-in-ci-at-a-pinned-version.md) | golangci-lint runs in CI at a pinned version | Decided |
| [D11](D011-agent-skills-are-committed-as-copies.md) | Agent skills are committed as copies | Amended by D71 |
| [D12](D012-every-text-a-person-reads-follows-simple-english.md) | Every text a person reads follows simple-english | Decided |
| [D13](D013-the-module-depends-on-the-standard-library-apd.md) | The module depends on the standard library, apd, and approved binary encodings | Decided |
| [D14](D014-the-first-drivers-speak-http.md) | The first drivers speak HTTP | Decided |
| [D15](D015-the-targets-are-in-docs-targets-md.md) | The targets are in docs/TARGETS.md | Decided |
| [D16](D016-an-ideal-target-meets-three-tests.md) | An ideal target meets three tests | Amended by D58 and D75 |
| [D17](D017-the-priorities-follow-the-three-tests.md) | The priorities follow the three tests | Amended by D84 and D87 |
| [D18](D018-a-result-that-is-not-a-table-becomes-rows-by.md) | A result that is not a table becomes rows by three rules | Decided |
| [D19](D019-a-json-number-is-never-decoded-through-float64.md) | A JSON number is never decoded through float64 | Decided |
| [D20](D020-a-driver-never-fakes-a-transaction.md) | A driver never fakes a transaction | Amended by D102 |
| [D21](D021-a-driver-reads-a-result-to-its-end.md) | A driver reads a result to its end | Decided |
| [D22](D022-the-licence-is-mit.md) | The licence is MIT | Decided |
| [D23](D023-the-first-driver-is-a-new-couchbase-driver.md) | The first driver is a new Couchbase driver | Decided |
| [D24](D024-a-target-that-has-a-go-driver-in-usql-is-in.md) | A target that has a Go driver in usql is in scope | Decided |
| [D25](D025-the-code-uses-encoding-json-v2-and-the.md) | The code uses encoding/json/v2 and the conventions of Go 1.27 | Decided |
| [D26](D026-a-driver-is-named-for-its-database.md) | A driver is named for its database | Decided |
| [D27](D027-a-dsn-is-a-standard-url.md) | A DSN is a standard URL | Decided |
| [D28](D028-a-driver-registers-one-name.md) | A driver registers one name | Decided |
| [D29](D029-the-module-imports-no-other-xo-package.md) | The module imports no other xo package | Decided |
| [D30](D030-the-couchbase-driver-registers-as-couchbase.md) | The Couchbase driver registers as couchbase | Decided |
| [D31](D031-couchbase-is-tested-on-the-enterprise-image-with.md) | Couchbase is tested on the Enterprise image, with an ordinary user | Decided |
| [D32](D032-a-target-has-a-priority-not-a-tier.md) | A target has a priority, not a tier | Decided |
| [D33](D033-a-decimal-is-an-apd-decimal.md) | A decimal is an apd.Decimal | Decided |
| [D34](D034-the-root-package-holds-a-parser-for-placeholders.md) | The root package holds a parser for placeholders | Decided |
| [D35](D035-a-dsn-is-a-url-whose-scheme-is-the-name-of-the.md) | A DSN is a URL whose scheme is the name of the driver | Decided |
| [D36](D036-rows-read-the-body-as-a-stream-and-close-closes.md) | Rows read the body as a stream, and Close closes it | Amended by D90 |
| [D37](D037-the-shared-test-helpers-are-the-package.md) | The shared test helpers are the package dbimptest | Decided |
| [D38](D038-the-couchbase-dsn.md) | The Couchbase DSN | Amended by D43 and D46 |
| [D39](D039-the-values-of-couchbase.md) | The values of Couchbase | Amended by D44 and D136 |
| [D40](D040-parameters-and-options-of-couchbase.md) | Parameters and options of Couchbase | Amended by D43 and D109 |
| [D41](D041-the-first-couchbase-driver-has-transactions.md) | The first Couchbase driver has transactions | Decided |
| [D42](D042-how-the-couchbase-driver-reads-a-result.md) | How the Couchbase driver reads a result | Amended by D107 |
| [D43](D043-the-couchbase-driver-has-a-key-for-durability.md) | The Couchbase driver has a key for durability | Amends D38 and D40 |
| [D44](D044-a-couchbase-string-scans-into-a-byte-slice-as.md) | A Couchbase string scans into a byte slice as base64 | Amends D39 |
| [D45](D045-a-couchbase-transaction-keeps-the-context-of.md) | A Couchbase transaction keeps the context of BeginTx | Amended by D69, D90 and D91 |
| [D46](D046-the-couchbase-dsn-has-a-key-for-the-timeout-of-a.md) | The Couchbase DSN has a key for the timeout of a transaction | Amends D38 |
| [D47](D047-the-surrealdb-driver-is-named-surrealdb.md) | The SurrealDB driver is named surrealdb | Decided |
| [D48](D048-the-surrealdb-url-names-the-namespace-and-the.md) | The SurrealDB URL names the namespace and the database in its path | Decided |
| [D49](D049-the-surrealdb-driver-speaks-cbor-written-here.md) | The SurrealDB driver speaks CBOR, written here, and JSON as an option | Decided |
| [D50](D050-the-surrealdb-driver-sends-each-statement-to.md) | The SurrealDB driver sends each statement to POST /rpc | Decided |
| [D51](D051-the-surrealdb-dsn-has-the-key-auth-for-the-level.md) | The SurrealDB DSN has the key auth for the level of the user | Decided |
| [D52](D052-each-surrealdb-statement-is-a-result-set-and-its.md) | Each SurrealDB statement is a result set, and its rows follow D18 | Amended by D101 |
| [D53](D053-the-go-types-of-surrealdb.md) | The Go types of SurrealDB | Amended by D70 and D113 |
| [D54](D054-the-surrealdb-driver-has-no-transactions.md) | The SurrealDB driver has no transactions | Decided |
| [D55](D055-the-errors-and-the-results-of-a-surrealdb-write.md) | The errors and the results of a SurrealDB write | Decided |
| [D56](D056-a-surrealdb-query-stops-when-its-context-ends.md) | A SurrealDB query stops when its context ends | Decided |
| [D57](D057-the-surrealdb-driver-exports-version.md) | The SurrealDB driver exports Version | Decided |
| [D58](D058-a-graph-query-language-that-a-person-types-meets.md) | A graph query language that a person types meets S | Amends D16 |
| [D59](D059-neo4j-runs-as-enterprise-edition-under-the.md) | Neo4j runs as Enterprise Edition under the evaluation agreement | Decided |
| [D60](D060-the-neo4j-driver-is-named-neo4j.md) | The Neo4j driver is named neo4j | Decided |
| [D61](D061-the-neo4j-url-names-the-database-in-its-path.md) | The Neo4j URL names the database in its path | Decided |
| [D62](D062-the-neo4j-driver-speaks-typed-json-and-asks-for.md) | The Neo4j driver speaks typed JSON, and asks for the best version | Decided |
| [D63](D063-the-go-types-of-neo4j.md) | The Go types of Neo4j | Amended by D138 and D139 |
| [D64](D064-the-neo4j-driver-binds-ordinal-n-as-the.md) | The Neo4j driver binds ordinal n as the parameter $n | Decided |
| [D65](D065-the-neo4j-driver-runs-a-transaction-through-the.md) | The Neo4j driver runs a transaction through the tx endpoints | Amended by D69 |
| [D66](D066-the-neo4j-driver-reads-each-result-as-it-arrives.md) | The Neo4j driver reads each result as it arrives, and its errors at the end | Amended by D105 |
| [D67](D067-the-neo4j-dsn-has-the-key-cancel-for-how-a-query.md) | The Neo4j DSN has the key cancel, for how a query stops | Amended by D95 |
| [D68](D068-the-neo4j-driver-serves-neo4j-5-26-and-later-and.md) | The Neo4j driver serves Neo4j 5.26 and later, and has no flavors | Decided |
| [D69](D069-a-neo4j-transaction-keeps-the-context-of-begintx.md) | A Neo4j transaction keeps the context of BeginTx | Amends D45 and D65, and amended by D100 |
| [D70](D070-a-surrealdb-recordid-writes-its-surrealql-form.md) | A SurrealDB RecordID writes its SurrealQL form as text | Amends D53 |
| [D71](D071-every-xo-repository-is-set-up-for-coding-agents.md) | Every xo repository is set up for coding agents the same way | Amends D3 and D11 |
| [D72](D072-the-decisions-are-one-file-each.md) | The decisions are one file each | Amends D3 |
| [D73](D073-the-targets-after-neo4j-in-order.md) | The targets after Neo4j, in order | Amended by D74, D84, D88 and D127 |
| [D74](D074-avatica-and-its-flavors-come-after-libsql.md) | Avatica and its flavors come after libSQL | Amends D73, and amended by D88, D127 and D154 |
| [D75](D075-aql-and-flux-meet-s.md) | AQL and Flux meet S | Amends D16 |
| [D76](D076-the-names-of-the-databend-libsql-and-cratedb.md) | The names of the Databend, libSQL and CrateDB drivers | Amended by D88 |
| [D77](D077-an-influxdb-result-is-an-array-of-objects-read.md) | An InfluxDB result is an array of objects, read by rule 2 of D18 | Amended by D80 |
| [D78](D078-one-influxdb-driver-with-the-dialects-influxdb.md) | One InfluxDB driver, with the dialects influxdb and influxql | Amended by D85 |
| [D79](D079-the-influxdb-releases-that-the-tests-run.md) | The InfluxDB releases that the tests run | Decided |
| [D80](D080-the-influxdb-sql-dialect-reads-its-columns-from.md) | The InfluxDB SQL dialect reads its columns from DESCRIBE | Amends D77 |
| [D81](D081-an-influxql-series-is-a-result-set-with-its.md) | An InfluxQL series is a result set, with its name and its tags | Amended by D96 |
| [D82](D082-the-influxdb-dsn-names-the-database-in-its-path.md) | The InfluxDB DSN names the database in its path | Decided |
| [D83](D083-the-influxql-dialect-asks-for-chunks-only-on.md) | The InfluxQL dialect asks for chunks only on InfluxDB 1 | Decided |
| [D84](D084-gel-blazegraph-and-stargate-leave-the-targets.md) | Gel, Blazegraph and Stargate leave the targets | Amends D17 and D73 |
| [D85](D085-the-influxdb-driver-takes-insert-of-the-influx-shell.md) | The InfluxDB driver takes INSERT of the influx shell | Amends D78 |
| [D86](D086-roundtrip-serves-a-database-that-cannot-do-every.md) | RoundTrip serves a database that cannot do every step | Decided |
| [D87](D087-milvus-and-postgrest-move-to-p2-and-ksqldb.md) | Milvus and PostgREST move to P2, and ksqlDB stays in P2 | Amends D17 |
| [D88](D088-cratedb-gets-no-driver-here.md) | CrateDB gets no driver here | Amends D73, D74 and D76 |
| [D89](D089-an-aql-result-becomes-columns-by-its-shape.md) | An AQL result becomes columns by its shape | Decided |
| [D90](D090-arangodb-streams-each-cursor-and-stops-it.md) | ArangoDB streams each cursor, and stops it | Amends D36 and D45, and amended by D99 |
| [D91](D091-an-arangodb-transaction-names-every-collection.md) | An ArangoDB transaction names every collection | Amends D45 |
| [D92](D092-the-arangodb-driver-takes-ddl-of-its-own.md) | The ArangoDB driver takes DDL of its own | Decided |
| [D93](D093-the-arangodb-dsn-names-the-database-in-its-path.md) | The ArangoDB DSN names the database in its path | Decided |
| [D94](D094-every-http-driver-takes-its-secret-as-the.md) | Every HTTP driver takes its secret as the password | Amended by D110 |
| [D95](D095-the-neo4j-tag-ends-the-statement.md) | The Neo4j tag ends the statement | Amends D67 |
| [D96](D096-the-first-influxql-column-is-measurement.md) | The first InfluxQL column is measurement | Amends D81 |
| [D97](D097-every-driver-is-compared-with-the-first-before.md) | Every driver is compared with the first before its commit | Decided |
| [D98](D098-a-second-scheme-reaches-a-driver-by-its-name.md) | A second scheme reaches a driver by its name | Decided |
| [D99](D099-a-long-arangodb-query-has-its-tag-at-the-start.md) | A long ArangoDB query has its tag at the start | Amends D90 |
| [D100](D100-a-neo4j-rollback-runs-after-its-context-ends.md) | A Neo4j rollback runs after its context ends | Amends D69 |
| [D101](D101-a-mixed-surrealdb-result-is-one-column-when-it.md) | A mixed SurrealDB result is one column when it starts with a value | Amends D52 |
| [D102](D102-a-driver-needs-no-resetsession-for-a-transaction.md) | A driver needs no ResetSession for a transaction | Amends D20 |
| [D103](D103-the-go-types-redirects-flavors-and-encoding-of.md) | The Go types, redirects, flavors and encoding of ArangoDB | Decided |
| [D104](D104-how-the-arangodb-driver-binds-an-argument.md) | How the ArangoDB driver binds an argument | Decided |
| [D105](D105-an-early-neo4j-close-stops-the-statement.md) | An early Neo4j Close stops the statement | Amends D66 |
| [D106](D106-an-arangodb-geo-index-on-one-field-reads-geojson.md) | An ArangoDB geo index on one field reads GeoJSON | Decided |
| [D107](D107-errincomplete-means-a-failure-after-a-row.md) | ErrIncomplete means a failure after a row | Amends D42 |
| [D108](D108-every-wire-format-meets-the-driver-at-the-row.md) | Every wire format meets the driver at the row | Decided |
| [D109](D109-every-driver-takes-the-same-options.md) | Every driver takes the same options | Amends D40 |
| [D110](D110-d94-names-each-driver.md) | D94 names each driver | Amends D94, and amended by D116 |
| [D111](D111-three-facts-of-the-influxdb-driver.md) | Three facts of the InfluxDB driver | Decided |
| [D112](D112-two-facts-of-the-neo4j-driver.md) | Two facts of the Neo4j driver | Decided |
| [D113](D113-the-go-types-of-surrealdb-that-d53-left-out.md) | The Go types of SurrealDB that D53 left out | Amends D53 |
| [D114](D114-only-couchbase-gives-json-text.md) | Only Couchbase gives JSON text | Decided |
| [D115](D115-an-influxdb-query-stops-when-the-client-leaves.md) | An InfluxDB query stops when the client leaves | Decided |
| [D116](D116-auth-bearer-sends-the-scheme-that-each-server-takes.md) | auth=bearer sends the scheme that each server takes | Amends D110 |
| [D117](D117-the-databend-dsn-names-the-database-in-its-path.md) | The Databend DSN names the database in its path | Decided |
| [D118](D118-the-go-types-of-databend.md) | The Go types of Databend | Amended by D138 |
| [D119](D119-databend-reads-nested-values-by-the-schema-and.md) | Databend reads nested values by the schema, and speaks JSON only | Amended by D136 |
| [D120](D120-databend-binds-parameters-on-the-server.md) | Databend binds parameters on the server | Amended by D124 |
| [D121](D121-a-databend-transaction-is-carried-in-the-session.md) | A Databend transaction is carried in the session | Decided |
| [D122](D122-a-databend-connection-keeps-its-session.md) | A Databend connection keeps its session | Amended by D126 |
| [D123](D123-databend-reads-a-result-by-its-pages-and-kills.md) | Databend reads a result by its pages, and kills a query that it leaves | Decided |
| [D124](D124-a-databend-decimal-argument-is-a-json-string.md) | A Databend decimal argument is a JSON string | Amends D120 |
| [D125](D125-databend-counts-the-rows-that-a-result-names.md) | Databend counts the rows that a result names | Decided |
| [D126](D126-a-databend-connection-keeps-its-session-when-it.md) | A Databend connection keeps its session when it is reused | Amends D122 |
| [D127](D127-tdengine-gets-no-driver-here.md) | TDengine gets no driver here | Amends D73 and D74 |
| [D128](D128-the-pinot-driver-is-read-only.md) | The Pinot driver is read-only | Decided |
| [D129](D129-the-pinot-dsn-names-a-broker.md) | The Pinot DSN names a Broker | Decided |
| [D130](D130-the-go-types-of-pinot.md) | The Go types of Pinot, and NULL | Decided |
| [D131](D131-pinot-runs-a-query-on-the-multi-stage-engine.md) | Pinot runs a query on the multi-stage engine | Decided |
| [D132](D132-pinot-arguments-are-written-into-the-text.md) | Pinot arguments are written into the text | Decided |
| [D133](D133-pinot-reads-one-answer-and-cancels-by-its-id.md) | Pinot reads one answer, and cancels a query by its id | Amended by D134 |
| [D134](D134-pinot-cancels-only-a-query-whose-answer-has-not.md) | Pinot cancels only a query whose answer has not arrived | Amends D133 |
| [D135](D135-a-value-has-the-go-type-that-fits-the-type-that.md) | A value has the Go type that fits the type that its database names | Amended by D138 |
| [D136](D136-the-drivers-follow-d135.md) | The drivers follow D135 | Amends D39 and D119, and amended by D138 |
| [D137](D137-the-types-are-mapped-before-a-driver-is-written.md) | The types are mapped before a driver is written | Decided |
| [D138](D138-the-root-package-defines-the-types-that-go-lacks.md) | The root package defines the types that Go lacks | Amends D63, D118, D135 and D136, and amended by D139 |
| [D139](D139-offsettime-and-vector-join-the-root-package.md) | OffsetTime and Vector join the root package | Amends D63 and D138 |
| [D140](D140-a-value-of-sqlite-keeps-its-storage-class.md) | A value of SQLite keeps its storage class | Decided |
| [D141](D141-the-rqlite-dsn-names-a-node.md) | The rqlite DSN names a node | Decided |
| [D142](D142-rqlite-runs-one-statement-a-request.md) | rqlite runs one statement a request | Decided |
| [D143](D143-rqlite-binds-each-argument-on-the-server.md) | rqlite binds each argument on the server | Decided |
| [D144](D144-rqlite-has-no-transactions.md) | rqlite has no transactions | Decided |
| [D145](D145-rqlite-bounds-each-statement-by-its-context.md) | rqlite bounds each statement by its context | Amended by D146 |
| [D146](D146-rqlite-ends-a-request-at-its-timeout.md) | rqlite ends a request at its timeout | Amends D145 |
| [D147](D147-the-go-types-of-libsql.md) | The Go types of libSQL | Decided |
| [D148](D148-the-libsql-dsn-names-a-server.md) | The libSQL DSN names a server | Decided |
| [D149](D149-libsql-reads-a-query-through-the-cursor.md) | libSQL reads a query through the cursor | Decided |
| [D150](D150-libsql-transactions-live-on-a-stream.md) | libSQL transactions live on a stream | Decided |
| [D151](D151-libsql-follows-a-base-url-on-its-own-host.md) | libSQL follows a base_url on its own host | Decided |
| [D152](D152-libsql-binds-typed-arguments-on-the-server.md) | libSQL binds typed arguments on the server | Decided |
| [D153](D153-avatica-speaks-json-only.md) | Avatica speaks JSON only | Decided |
| [D154](D154-druid-gets-a-driver-of-its-own.md) | Druid gets a driver of its own | Amends D74 |
| [D155](D155-the-go-types-of-avatica.md) | The Go types of Avatica | Decided |
| [D156](D156-the-avatica-dsn-names-a-server.md) | The Avatica DSN names a server | Decided |
| [D157](D157-avatica-reads-a-result-in-frames.md) | Avatica reads a result in frames | Decided |
| [D158](D158-avatica-binds-typed-parameters.md) | Avatica binds typed parameters | Decided |
| [D159](D159-avatica-transactions-and-cancel.md) | Avatica transactions and cancel | Decided |
| [D160](D160-avatica-keeps-the-calendar-and-binds-no-array.md) | Avatica keeps the calendar of the server and binds no array | Decided |
| [D161](D161-a-driver-tests-each-product-it-recorded.md) | A driver tests each product that it recorded | Decided |
| [D162](D162-six-http-targets-after-avatica.md) | Six HTTP targets after Avatica | Decided |
| [D163](D163-read-only-targets-and-dynamodb-columns.md) | Read-only targets, the caps of OpenSearch, and DynamoDB columns | Decided |
| [D164](D164-the-druid-driver.md) | The Druid driver | Decided |
| [D165](D165-the-drill-driver.md) | The Drill driver | Decided |
| [D166](D166-the-solr-driver.md) | The Solr driver | Decided |
| [D167](D167-the-elasticsearch-driver.md) | The Elasticsearch driver | Decided |
| [D168](D168-the-opensearch-driver.md) | The OpenSearch driver | Decided |
| [D169](D169-the-dynamodb-driver.md) | The DynamoDB driver | Decided |
| [D170](D170-questdb-and-greptimedb-get-no-driver.md) | QuestDB and GreptimeDB get no driver | Decided |
| [D171](D171-no-ddl-in-the-drivers-and-a-read-only-sql-layer-later.md) | No DDL in the drivers, and a read only SQL layer later | Decided |
| [D172](D172-druid-reads-a-few-types-of-the-round-trip-on-a-push.md) | Druid reads a few types of the round trip on a push | Decided |
| [D173](D173-trino-and-presto-in-one-driver.md) | Trino and Presto in one driver | Decided |
| [D174](D174-the-clickhouse-driver-speaks-http.md) | The ClickHouse driver speaks HTTP | Decided |
| [D175](D175-the-trino-and-presto-driver.md) | The Trino and Presto driver | Decided |
| [D176](D176-the-clickhouse-driver-reads-json-and-kills-queries.md) | The ClickHouse driver reads JSON and kills queries | Decided |
| [D177](D177-the-rest-of-the-clickhouse-driver.md) | The rest of the ClickHouse driver | Decided |
| [D178](D178-answers-for-drill-solr-elasticsearch-opensearch-and-dynamodb.md) | Answers for Drill, Solr, Elasticsearch, OpenSearch and DynamoDB | Decided |
| [D179](D179-the-voltdb-driver-speaks-http.md) | The VoltDB driver speaks HTTP | Decided. Amended by D180 |
| [D180](D180-voltdb-gets-no-driver-here.md) | VoltDB gets no driver here | Amends D179 |
| [D181](D181-select-version-for-databases-with-no-query-for-it.md) | SELECT version() for databases with no query for it | Decided |
| [D182](D182-snowflake-is-measured-for-an-http-driver.md) | Snowflake is measured for an HTTP driver | Decided |
| [D183](D183-the-snowflake-driver.md) | The Snowflake driver | Decided |
| [D184](D184-bigquery-cosmos-db-and-athena-are-targets.md) | BigQuery, Cosmos DB and Athena are targets | Decided |
| [D185](D185-spanner-is-a-target.md) | Spanner is a target | Decided. Amended by D186 |
| [D186](D186-spanner-gets-no-driver-here.md) | Spanner gets no driver here | Amends D185. Amended by D187 |
| [D187](D187-spanner-is-measured-on-the-hosted-service.md) | Spanner is measured on the hosted service | Amends D186 |
| [D188](D188-databricks-is-a-target.md) | Databricks is a target | Decided |
| [D189](D189-the-bigquery-driver.md) | The BigQuery driver | Decided. Amended by D195 |
| [D190](D190-the-cosmos-db-driver.md) | The Cosmos DB driver | Decided |
| [D191](D191-the-spanner-driver.md) | The Spanner driver | Decided. Amended by D195 |
| [D192](D192-the-athena-driver.md) | The Athena driver | Decided |
| [D193](D193-the-databricks-driver.md) | The Databricks driver | Decided |
| [D194](D194-rule-4-names-three-more-drivers.md) | Rule 4 names three more drivers | Decided |
| [D195](D195-answers-for-bigquery-and-spanner-after-the-live-run.md) | Answers for BigQuery and Spanner after the live run | Decided. Amends D189 and D191 |
| [D196](D196-the-spanner-driver-reads-the-emulator.md) | The Spanner driver reads the emulator | Decided. Adds to D187, D191 and D195 |
| [D197](D197-the-error-of-a-driver-matches-errauthentication.md) | The error of a driver matches ErrAuthentication | Decided |
| [D198](D198-spanner-database-role-and-ddl-batches.md) | Spanner database role and DDL batches | Decided |
