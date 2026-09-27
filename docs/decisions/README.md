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
| [D17](D017-the-priorities-follow-the-three-tests.md) | The priorities follow the three tests | Decided |
| [D18](D018-a-result-that-is-not-a-table-becomes-rows-by.md) | A result that is not a table becomes rows by three rules | Decided |
| [D19](D019-a-json-number-is-never-decoded-through-float64.md) | A JSON number is never decoded through float64 | Decided |
| [D20](D020-a-driver-never-fakes-a-transaction.md) | A driver never fakes a transaction | Decided |
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
| [D36](D036-rows-read-the-body-as-a-stream-and-close-closes.md) | Rows read the body as a stream, and Close closes it | Decided |
| [D37](D037-the-shared-test-helpers-are-the-package.md) | The shared test helpers are the package dbimptest | Decided |
| [D38](D038-the-couchbase-dsn.md) | The Couchbase DSN | Amended by D43 and D46 |
| [D39](D039-the-values-of-couchbase.md) | The values of Couchbase | Amended by D44 |
| [D40](D040-parameters-and-options-of-couchbase.md) | Parameters and options of Couchbase | Amended by D43 |
| [D41](D041-the-first-couchbase-driver-has-transactions.md) | The first Couchbase driver has transactions | Decided |
| [D42](D042-how-the-couchbase-driver-reads-a-result.md) | How the Couchbase driver reads a result | Decided |
| [D43](D043-the-couchbase-driver-has-a-key-for-durability.md) | The Couchbase driver has a key for durability | Amends D38 and D40 |
| [D44](D044-a-couchbase-string-scans-into-a-byte-slice-as.md) | A Couchbase string scans into a byte slice as base64 | Amends D39 |
| [D45](D045-a-couchbase-transaction-keeps-the-context-of.md) | A Couchbase transaction keeps the context of BeginTx | Decided |
| [D46](D046-the-couchbase-dsn-has-a-key-for-the-timeout-of-a.md) | The Couchbase DSN has a key for the timeout of a transaction | Amends D38 |
| [D47](D047-the-surrealdb-driver-is-named-surrealdb.md) | The SurrealDB driver is named surrealdb | Decided |
| [D48](D048-the-surrealdb-url-names-the-namespace-and-the.md) | The SurrealDB URL names the namespace and the database in its path | Decided |
| [D49](D049-the-surrealdb-driver-speaks-cbor-written-here.md) | The SurrealDB driver speaks CBOR, written here, and JSON as an option | Decided |
| [D50](D050-the-surrealdb-driver-sends-each-statement-to.md) | The SurrealDB driver sends each statement to POST /rpc | Decided |
| [D51](D051-the-surrealdb-dsn-has-the-key-auth-for-the-level.md) | The SurrealDB DSN has the key auth for the level of the user | Decided |
| [D52](D052-each-surrealdb-statement-is-a-result-set-and-its.md) | Each SurrealDB statement is a result set, and its rows follow D18 | Decided |
| [D53](D053-the-go-types-of-surrealdb.md) | The Go types of SurrealDB | Decided, and D70 proposes an amendment |
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
| [D66](D066-the-neo4j-driver-reads-each-result-as-it-arrives.md) | The Neo4j driver reads each result as it arrives, and its errors at the end | Decided |
| [D67](D067-the-neo4j-dsn-has-the-key-cancel-for-how-a-query.md) | The Neo4j DSN has the key cancel, for how a query stops | Decided |
| [D68](D068-the-neo4j-driver-serves-neo4j-5-26-and-later-and.md) | The Neo4j driver serves Neo4j 5.26 and later, and has no flavors | Decided |
| [D69](D069-a-neo4j-transaction-keeps-the-context-of-begintx.md) | A Neo4j transaction keeps the context of BeginTx | Amends D65 |
| [D70](D070-a-surrealdb-recordid-writes-its-surrealql-form.md) | A SurrealDB RecordID writes its SurrealQL form as text | Proposed. Amends D53 |
| [D71](D071-every-xo-repository-is-set-up-for-coding-agents.md) | Every xo repository is set up for coding agents the same way | Amends D3 and D11 |
| [D72](D072-the-decisions-are-one-file-each.md) | The decisions are one file each | Amends D3 |
| [D73](D073-the-targets-after-neo4j-in-order.md) | The targets after Neo4j, in order | Amended by D74 |
| [D74](D074-avatica-and-its-flavors-come-after-libsql.md) | Avatica and its flavors come after libSQL | Amends D73 |
| [D75](D075-aql-and-flux-meet-s.md) | AQL and Flux meet S | Amends D16 |
| [D76](D076-the-names-of-the-databend-libsql-and-cratedb.md) | The names of the Databend, libSQL and CrateDB drivers | Decided |
| [D77](D077-an-influxdb-result-is-an-array-of-objects-read.md) | An InfluxDB result is an array of objects, read by rule 2 of D18 | Decided |
| [D78](D078-one-influxdb-driver-with-the-dialects-influxdb.md) | One InfluxDB driver, with the dialects influxdb and influxql | Decided |
| [D79](D079-the-influxdb-releases-that-the-tests-run.md) | The InfluxDB releases that the tests run | Decided |
