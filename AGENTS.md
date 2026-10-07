# dbimp

`dbimp` holds `database/sql` drivers for databases that have no idiomatic Go
driver, and for groups of databases that can share one implementation.
`usql` and `dbtpl` are the consumers. `dbtpl` reaches a database through
`dbmeta`, so `dbmeta` is a consumer too. `dburl` hands each driver its DSN,
as a URL whose scheme is the name of the driver (D35).

The first drivers are clients for databases that take queries over HTTP or
HTTPS (D14). A query is SQL, a dialect like SQL, JSON, or another language
that a person can type by hand. The ideal target can be started by `dbrun`,
speaks HTTP, and has a dialect like SQL (D16). The module uses the standard
library, `github.com/cockroachdb/apd/v3` for decimals, and a binary encoding
only when Ken approves it for one database (D13).

A second goal is to reduce the dependencies of `usql`, so a driver here can
replace a driver that `usql` imports now (D24).

The repository is new. The first driver, in `couchbase/`, is a new
Couchbase driver that replaces `xo/n1ql` (D23). It was first released in
`v0.1.0`. The second, in `surrealdb/`, is in the tag `v0.2.0` (W8). The
third, in `neo4j/`, is in the tag `v0.3.0` (W9). The fourth, in `influxdb/`,
was first released in `v0.4.0` (W11), which is the first GitHub release that
holds the second and the third. The fifth, in
`arangodb/`, was first released in `v0.5.0` (W13). The sixth, in
`databend/`, was first released in `v0.6.0` (W16). The seventh, in
`pinot/`, was first released in `v0.7.0` (W19). The eighth, in
`rqlite/`, was first released in `v0.8.0` (W22). The ninth, in
`libsql/`, was first released in `v0.9.0` (W23). The tenth, in
`avatica/`, was first released in `v0.10.0` (W24). The eleventh, in
`druid/`, was first released in `v0.11.0` (W25). The twelfth, in
`trino/`, was first released in `v0.12.0` (W31). The
targets and their order are in [docs/TARGETS.md](docs/TARGETS.md).

## Standing rules

These hold in every `xo` repository, for every coding agent (D71, from
dbmeta D110).

1. Stage changes for review. Commit and push only when Ken says so.
2. Load the `simple-english` skill before you write any text that a person
   reads: a document, a code comment, an error message or a commit message.
   Follow it for that text.
3. Load the `go-pedantry` skill before you write or review Go code. Follow it
   where it does not conflict with a rule in this file. A rule here wins.

`CLAUDE.md` holds one line that imports this file, so that Claude Code and
every other agent read the same rules. Edit this file, not that one.

## Which document to read

| If you are | Read |
| --- | --- |
| looking for an open question | the end of [docs/PLAN.md](docs/PLAN.md), and of each product document |
| asking why something is the way it is | the index in [docs/decisions/README.md](docs/decisions/README.md) |
| looking for the next piece of work | [docs/BACKLOG.md](docs/BACKLOG.md) |
| resuming work after a crash | [docs/PROGRESS.md](docs/PROGRESS.md) |
| writing the Couchbase driver | [docs/COUCHBASE.md](docs/COUCHBASE.md), then D23 and W5 |
| writing the SurrealDB driver | [docs/SURREALDB.md](docs/SURREALDB.md), then W8 |
| writing the Neo4j driver | [docs/NEO4J.md](docs/NEO4J.md), then W9 |
| writing the Avatica driver | [docs/AVATICA.md](docs/AVATICA.md), then W24, D74 and D153 to D159 |
| writing the InfluxDB driver | [docs/INFLUXDB.md](docs/INFLUXDB.md), then W11 and D78 to D83 |
| asking why CrateDB has no driver here | [docs/CRATEDB.md](docs/CRATEDB.md), then D88 |
| writing the ArangoDB driver | [docs/ARANGODB.md](docs/ARANGODB.md), then W13 and D89 to D94 |
| writing the Databend driver | [docs/DATABEND.md](docs/DATABEND.md), then D73 and D76 |
| asking why TDengine has no driver here | [docs/TDENGINE.md](docs/TDENGINE.md), then D127 |
| writing the Apache Pinot driver | [docs/PINOT.md](docs/PINOT.md), then W19 and D128 to D134 |
| writing the rqlite driver | [docs/RQLITE.md](docs/RQLITE.md), then W22, D73 and D140 |
| writing the libSQL and Turso driver | [docs/LIBSQL.md](docs/LIBSQL.md), then W23, D76 and D147 to D152 |
| writing the Apache Druid driver | [docs/DRUID.md](docs/DRUID.md), then W25 and D162 |
| writing the Apache Drill driver | [docs/DRILL.md](docs/DRILL.md), then W26 and D162 |
| writing the Apache Solr driver | [docs/SOLR.md](docs/SOLR.md), then W27 and D162 |
| writing the Elasticsearch driver | [docs/ELASTICSEARCH.md](docs/ELASTICSEARCH.md), then W28 and D162 |
| writing the OpenSearch driver | [docs/OPENSEARCH.md](docs/OPENSEARCH.md), then W29 and D162 |
| writing the Amazon DynamoDB driver | [docs/DYNAMODB.md](docs/DYNAMODB.md), then W30 and D162 |
| writing the Trino and Presto driver | [docs/TRINO.md](docs/TRINO.md), then W31, D162 and D173 |
| writing the ClickHouse driver | [docs/CLICKHOUSE.md](docs/CLICKHOUSE.md), then W32 and D174 |
| choosing the next database | [docs/TARGETS.md](docs/TARGETS.md), then D16, D17, D73 and D74 in [docs/decisions/](docs/decisions/README.md) |
| adding a driver | [docs/DRIVER.md](docs/DRIVER.md), which is every step in order |
| mapping the types of a database onto Go types | [docs/TYPES.md](docs/TYPES.md), then step 8a of [docs/DRIVER.md](docs/DRIVER.md), D135, D137 and D138 |
| reviewing a driver before its commit | step 17a of [docs/DRIVER.md](docs/DRIVER.md), then D97 |
| sharing code between two drivers | [docs/DESIGN.md](docs/DESIGN.md), then D4 in [docs/decisions/](docs/decisions/README.md). Shared code goes in the root package |
| using or changing the root package or dbimptest | [docs/DESIGN.md](docs/DESIGN.md) |
| testing against a server | "Before you commit" below, and D9 |
| answering a lint finding | "Linting" below, and D10 |
| adding a dependency | hard rule 6, and D13 |
| adding or updating an agent skill | [CONTRIBUTING.md](CONTRIBUTING.md), under Agent skills, and D11 |
| writing any text that a user can read: a document, a code comment, an error message or a commit message | the `simple-english` skill. Load it first. See D12 |
| writing or reviewing Go code | the `go-pedantry` skill. Load it first. A rule in this file wins where the two differ. See D71 |

`CONTRIBUTING.md` holds the same material for a person, and it is shorter.
Each decision is a file in `docs/decisions/` (D72).
[README.md](README.md) is for a person who uses a driver.

A document that is not in this table does not exist. If you cannot find where
something is written down, it is not written down. Do not decide an open
question yourself. The open questions are at the end of `docs/PLAN.md` and
of each `docs/<PRODUCT>.md`. Ask Ken.

## Hard rules

1. Each driver registers one name with `database/sql`, which is the name of
   its package and of its database, and no alias (D28 and D30). Its DSN is a
   standard URL whose scheme is that name, parsed with `net/url`, with no
   form kept from an earlier driver (D27 and D35). Never write a list of
   schemes or of aliases here. That taxonomy belongs to `dburl`. See D5.
2. A package holds no configuration and logs nothing. A setting is a value
   that the caller owns and passes in, through the DSN or through a
   `driver.Connector`. One `usql` driver borrowed the configuration of
   another driver and shipped a fault. See D7.
3. A NULL reaches `database/sql` as nil. Never hand it a zero value. A row
   keeps the column order of the statement, and each value is a decoded Go
   value. Never sort the columns, return JSON text in place of a value, or
   wrap one column in an object. Each of these faults was found in a driver
   that `dbmeta` then had to replace. A value has the Go type that fits the
   type that the database names for its column, and a string column gives a
   string. See D8 and D135.
4. `context.Context` comes first, is named `ctx`, and is never stored in a
   struct, except by a Couchbase, Neo4j, ArangoDB, Databend, libSQL, Avatica
   or Trino transaction, by an Avatica connection, and by the rows of an
   ArangoDB cursor, of a Databend query, of a libSQL cursor, of an Avatica
   query or of a Trino query (D45, D69, D90, D91, D122, D123, D149, D150, D157,
   D159 and D175). The
   library never calls `context.Background` or `context.TODO`.
   Every driver implements the context forms of the `database/sql/driver`
   interfaces and stops work when the context ends. See D8.
5. Every error reaches the caller, wrapped with `%w`. Return
   `driver.ErrBadConn` only when the connection is broken and the statement
   did not reach the server, because `database/sql` then runs it again. See
   D8.
6. The module depends on the standard library, on
   `github.com/cockroachdb/apd/v3` for decimals (D33), and on a package for a
   binary encoding only when Ken approves it for one database. HTTP, JSON,
   TLS, authentication, request signing and the parser for placeholders are
   written here. Ask Ken before you add any package. `depguard` in
   `.golangci.yml` holds the list. See D13.
7. Never import `dburl`, `dbmeta`, `usql` or `dbtpl`. Read their source when
   you need a fact, and run `dbrun` from a checkout of `dbmeta` as a tool.
   The dependency runs the other way. See D29.
8. Never write a `//go:build` constraint on an operating system or an
   architecture, and never branch on either. A driver is tested on
   `linux/amd64` only.
9. A unit test needs no server. A test that needs one reads a DSN from an
   environment variable and skips when it is empty. See D9.
10. Never start a container by hand. `dbrun` in `dbmeta` starts every server.
    A database that `dbrun` does not know gets its entry in `dbmeta` first.
    See D9.
11. Register a driver from `init`, never from a test body. `go test
    -count=2` fails on a second registration, and that is why CI runs it.
12. Ken reviews the staged changes. Stage your work and stop. Commit, push,
    tag or publish a release only when Ken says so, and ask again for each
    one. Steps 18 to 20 of [docs/DRIVER.md](docs/DRIVER.md) hold the order.

## Layout

| Path | Holds |
| --- | --- |
| `/` | The root package `dbimp`. It holds the code that the drivers share: the HTTP client, the adapters that encode and decode values, and other utilities. `doc.go` holds its package documentation. See D4. |
| `<driver>/` | One driver, as the package `github.com/xo/dbimp/<driver>`, named for its database and not for its query language, such as `couchbase` (D26). It imports the root package. That path is the `GoPackage` of its scheme in `dburl`, and a consumer imports it directly. See D4. |
| `dbimptest/` | The helpers for the tests of a driver: recorded exchanges, the contract, the test for goroutines, the two tables, the survey of step 5a and the round trip of a type. Only a test, and the command `dbimptest/cmd/record`, import it (D37). `dbimptest/cmd/record` records the exchanges of step 6. |
| `testdata/` | Golden files, and the recorded exchanges of each driver under `testdata/<driver>/`. |
| `docs_test.go` | The rules for the documents, from D3, D71 and D72. |
| `skills_test.go` | The rule for the agent skills, from D11. |
| `gates_test.go` | The gates that hold the steps of `docs/DRIVER.md`, from W6. `gates_self_test.go` tests the gates themselves. |

There is no registry of drivers and no package that imports every driver.
This differs from the other `xo` repositories. Examples are `Example` tests
in the package of the driver.

## Sibling repositories

`dbimp` works with several repositories in the same family. A change often
crosses between them. Each one is checked out next to this one.

| Repository | What it holds |
| --- | --- |
| `xo/dburl` | Connection string parsing, schemes, aliases and the `GoPackage` of each scheme |
| `xo/usql` | The command line client. A driver is `drivers/<name>/<name>.go` there, and [usql/docs/DRIVER.md](https://github.com/xo/usql/blob/main/docs/DRIVER.md) is the reference |
| `xo/dbmeta` | Database metadata queries. It also holds `dbrun`, which starts every test server |
| `xo/dbtpl` | Code generation from database schemas, which reads through `dbmeta` |
| `xo/cql` | The `database/sql` driver for Cassandra, and the nearest example of a driver repository |
| `xo/n1ql` | The old `database/sql` driver for Couchbase. The `couchbase` driver here replaces it, and it gets no new code (D23) |

`dburl`, `dbmeta`, `cql`, `n1ql` and this repository record decisions as
numbered entries, `D1`, `D2` and so on. Each repository has its own series. Name
the repository when you cite a decision of another one, as in "dbmeta D61". Work
items are a separate series, `W1`, `W2` and so on. Do not mix the two series in
one commit message.

## Go conventions

Match the surrounding code. D6 holds the conventions, and these are the ones
that are easy to miss:

- A sentinel error is a constant of a defined string type, never a variable
  made with `errors.New`. Compare errors with `errors.Is` and `errors.As`.
- An error message is lower case, starts with a gerund, and names what
  failed, such as `reading columns for %s: %w`. It does not say "failed to"
  or "error".
- Accept interfaces and return concrete types. Keep an interface to three
  methods or fewer, and define it where it is consumed.
- A package name is one short lower case word. An exported name does not
  repeat it.
- A receiver is named with one or two lower case letters, and every method
  of a type uses the same one.
- Read and write JSON with `encoding/json/v2` and `encoding/json/jsontext`,
  never with `encoding/json`. Decode a result one token at a time as it
  arrives, and never hold the whole result in memory (D25).
- A nullable value is `sql.Null[T]`, and a UUID is the standard `uuid.UUID`
  (D25).
- Use `any`, `new(expr)`, `t.Context()` in tests, and `b.Loop()` in
  benchmarks.

## Linting

`.golangci.yml` holds the rules. The posture is `default: all` with a disable
list, and the reason for each entry is written beside it. The version is
pinned in `.github/workflows/test.yml`. See D10.

A linter that makes idiomatic Go worse is disabled, with the reason. Only a
real defect gets a code change. A change made only to quiet a linter is
itself a defect. Two such changes were written in `dbmeta` and reverted, so
do not write them here:

- Never write `defer func() { _ = rows.Close() }()`. Write
  `defer rows.Close()`. `errcheck` is configured to allow it.
- Never add an empty `case` to satisfy `exhaustive`. That linter is disabled.

Read the output of `golangci-lint run` as a list of questions, not a list of
tasks.

## Before you commit

Run these in the repository root. `gofmt -l .` must print nothing.

```bash
gofmt -l . && go vet ./... && go build ./... && go test -race -count=2 ./...
golangci-lint run ./...
```

To run the integration tests of a driver, start its server with `dbrun` from
`dbmeta`, and set the environment variable that the driver reads to the DSN
that `dbrun` prints. The release name has the form `<product>-<release>`:

```bash
(cd ../dbmeta/test && go run ./cmd/dbrun start <release>)
export <DRIVER>_DSN=$(cd ../dbmeta/test && go run ./cmd/dbrun dsn --json <release> | jq -r '.[0].url')
go test -race -count=1 -run Integration ./<driver>/...
```

## Writing documentation

A new document goes in `docs/`. Only `README.md`, `AGENTS.md`, `CLAUDE.md`
and `CONTRIBUTING.md` belong in the repository root, and
`TestTheRootHoldsFourDocuments` enforces that (D71). Add a new document to the
table at the top of this file and to the one in `README.md`. See D3.

A decision goes in `docs/decisions/`, as a file of its own with the next
number, and a row in `docs/decisions/README.md` (D72). A work item goes in
`docs/BACKLOG.md`. The rules for each are at the top of its index.

Load the `simple-english` skill before you write any text that a user can
read. That includes `README.md` and every other document, code comments,
error messages, commit messages and the text of test failures.
Follow it for that text (D12). In short, write short sentences in the active
voice. Use `can`, `will` and `must`, and never `should` or `may`. Write no
contractions, no semicolons and no em dashes. Put the condition before the
command, and use one word for one meaning.
