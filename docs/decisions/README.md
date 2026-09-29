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
| [D39](D039-the-values-of-couchbase.md) | The values of Couchbase | Amended by D44 |
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
| [D63](D063-the-go-types-of-neo4j.md) | The Go types of Neo4j | Decided |
| [D64](D064-the-neo4j-driver-binds-ordinal-n-as-the.md) | The Neo4j driver binds ordinal n as the parameter $n | Decided |
| [D65](D065-the-neo4j-driver-runs-a-transaction-through-the.md) | The Neo4j driver runs a transaction through the tx endpoints | Amended by D69 |
| [D66](D066-the-neo4j-driver-reads-each-result-as-it-arrives.md) | The Neo4j driver reads each result as it arrives, and its errors at the end | Amended by D105 |
| [D67](D067-the-neo4j-dsn-has-the-key-cancel-for-how-a-query.md) | The Neo4j DSN has the key cancel, for how a query stops | Amended by D95 |
| [D68](D068-the-neo4j-driver-serves-neo4j-5-26-and-later-and.md) | The Neo4j driver serves Neo4j 5.26 and later, and has no flavors | Decided |
| [D69](D069-a-neo4j-transaction-keeps-the-context-of-begintx.md) | A Neo4j transaction keeps the context of BeginTx | Amends D45 and D65, and amended by D100 |
| [D70](D070-a-surrealdb-recordid-writes-its-surrealql-form.md) | A SurrealDB RecordID writes its SurrealQL form as text | Amends D53 |
| [D71](D071-every-xo-repository-is-set-up-for-coding-agents.md) | Every xo repository is set up for coding agents the same way | Amends D3 and D11 |
| [D72](D072-the-decisions-are-one-file-each.md) | The decisions are one file each | Amends D3 |
| [D73](D073-the-targets-after-neo4j-in-order.md) | The targets after Neo4j, in order | Amended by D74, D84 and D88 |
| [D74](D074-avatica-and-its-flavors-come-after-libsql.md) | Avatica and its flavors come after libSQL | Amends D73, and amended by D88 |
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
| [D110](D110-d94-names-each-driver.md) | D94 names each driver | Amends D94 |
| [D111](D111-three-facts-of-the-influxdb-driver.md) | Three facts of the InfluxDB driver | Decided |
| [D112](D112-two-facts-of-the-neo4j-driver.md) | Two facts of the Neo4j driver | Decided |
| [D113](D113-the-go-types-of-surrealdb-that-d53-left-out.md) | The Go types of SurrealDB that D53 left out | Amends D53 |
| [D114](D114-only-couchbase-gives-json-text.md) | Only Couchbase gives JSON text | Decided |
| [D115](D115-an-influxdb-query-stops-when-the-client-leaves.md) | An InfluxDB query stops when the client leaves | Decided |
