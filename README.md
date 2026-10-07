<div align="center">
  <a href="#use" title="Use">Use</a> |
  <a href="#documents" title="Documents">Documents</a> |
  <a href="#related-projects" title="Related Projects">Related Projects</a> |
  <a href="https://pkg.go.dev/github.com/xo/dbimp" title="Go Reference">Reference</a> |
  <a href="https://github.com/xo/dbimp/releases" title="Releases">Releases</a> |
  <a href="CONTRIBUTING.md" title="Contributing">Contributing</a>
</div>

<br/>

[![Unit Tests][dbimp-ci-status]][dbimp-ci]
[![Go Reference][goref-dbimp-status]][goref-dbimp]
[![Releases][release-status]][releases]
[![Discord Discussion][discord-status]][discord]

[dbimp-ci]: https://github.com/xo/dbimp/actions/workflows/test.yml "Test CI"
[dbimp-ci-status]: https://github.com/xo/dbimp/actions/workflows/test.yml/badge.svg "Test CI"
[goref-dbimp]: https://pkg.go.dev/github.com/xo/dbimp "Go Reference"
[goref-dbimp-status]: https://pkg.go.dev/badge/github.com/xo/dbimp.svg "Go Reference"
[release-status]: https://img.shields.io/github/v/release/xo/dbimp?display_name=tag "Latest Release"
[releases]: https://github.com/xo/dbimp/releases "Releases"
[discord]: https://discord.gg/WDWAgXwJqN "Discord Discussion"
[discord-status]: https://img.shields.io/discord/829150509658013727.svg?label=Discord&logo=Discord&colorB=7289da&style=flat-square "Discord Discussion"

# dbimp

`dbimp` holds Go `database/sql` drivers for databases that have no idiomatic
Go driver, and for groups of databases that can share one implementation.
[usql](https://github.com/xo/usql) and [dbtpl](https://github.com/xo/dbtpl)
use them. The first drivers are for databases that take queries over HTTP.
The drivers use the Go standard library, with
[apd](https://github.com/cockroachdb/apd) for decimals, and need no cgo.

The repository is new. It holds the drivers `couchbase`, which was first
released in `v0.1.0`, `surrealdb` and `neo4j`, which the tags `v0.2.0` and
`v0.3.0` hold, `influxdb`, which was first released in `v0.4.0` with the
other three, `arangodb`, which was first released in `v0.5.0`, `databend`,
which was first released in `v0.6.0`, `pinot`, which was first released in
`v0.7.0`, `rqlite`, which was first released in `v0.8.0`, and `libsql`,
which was first released in `v0.9.0`, `avatica`, which was first
released in `v0.10.0`, and `druid`, which was first released in `v0.11.0`.
[docs/TARGETS.md](docs/TARGETS.md) names the databases it aims to support,
and the order of the work.

## Use

Each driver is its own package, and there is no package that imports every
driver. Import the driver for your database. Open it with the name of the
database, and with a URL whose scheme is that name:

```go
import (
	"database/sql"

	_ "github.com/xo/dbimp/couchbase"
)

db, err := sql.Open("couchbase", "couchbase://user:pass@localhost:8093/")
```

A driver registers one name and knows no alias.
[dburl](https://github.com/xo/dburl) turns an alias, such as `n1ql`, into the
URL that the driver reads, so every alias works in `usql`.

## Documents

| Document | Holds |
| --- | --- |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to change this repository |
| [AGENTS.md](AGENTS.md) | The rules, written for a coding agent. They apply to a person too |
| [CLAUDE.md](CLAUDE.md) | One line that imports `AGENTS.md` for Claude Code |
| [docs/PLAN.md](docs/PLAN.md) | The purpose of the project, and the open questions |
| [docs/decisions/README.md](docs/decisions/README.md) | Every decision, one file each, and their index |
| [docs/TARGETS.md](docs/TARGETS.md) | Every target database, its priority, and the review of the list |
| [docs/DRIVER.md](docs/DRIVER.md) | Every step to add a driver, in order |
| [docs/TYPES.md](docs/TYPES.md) | The kinds of type, the Go type of each, and the types of every driver |
| [docs/DESIGN.md](docs/DESIGN.md) | The design of the code that every driver shares |
| [docs/COUCHBASE.md](docs/COUCHBASE.md) | What is measured about the Couchbase query service |
| [docs/SURREALDB.md](docs/SURREALDB.md) | What is known about the HTTP interface of SurrealDB |
| [docs/NEO4J.md](docs/NEO4J.md) | What is known about the HTTP interface of Neo4j |
| [docs/AVATICA.md](docs/AVATICA.md) | What is known about Apache Calcite Avatica and the Phoenix Query Server, measured for its driver |
| [docs/INFLUXDB.md](docs/INFLUXDB.md) | What InfluxDB 1, 2 and 3 answer, as measured on seven releases |
| [docs/CRATEDB.md](docs/CRATEDB.md) | What is known about CrateDB, and why it has no driver here |
| [docs/ARANGODB.md](docs/ARANGODB.md) | What ArangoDB 3.12 answers, as measured |
| [docs/DATABEND.md](docs/DATABEND.md) | What Databend 1.2.881 and 1.2.948 answer, as measured |
| [docs/TDENGINE.md](docs/TDENGINE.md) | What TDengine answers, as measured, and why it has no driver here |
| [docs/PINOT.md](docs/PINOT.md) | What Apache Pinot 1.4.0 and 1.5.1 answer, as measured |
| [docs/RQLITE.md](docs/RQLITE.md) | What is known about rqlite, measured for its driver |
| [docs/LIBSQL.md](docs/LIBSQL.md) | What is known about libSQL and Turso, measured for its driver |
| [docs/DRUID.md](docs/DRUID.md) | What is known about Apache Druid, for its driver |
| [docs/DRILL.md](docs/DRILL.md) | What is known about Apache Drill, for its driver |
| [docs/SOLR.md](docs/SOLR.md) | What is known about Apache Solr, for its driver |
| [docs/ELASTICSEARCH.md](docs/ELASTICSEARCH.md) | What is known about Elasticsearch, for its driver |
| [docs/OPENSEARCH.md](docs/OPENSEARCH.md) | What is known about OpenSearch, for its driver |
| [docs/DYNAMODB.md](docs/DYNAMODB.md) | What is known about Amazon DynamoDB and ScyllaDB Alternator, for its driver |
| [docs/PROGRESS.md](docs/PROGRESS.md) | Where the work in progress stands, and how to resume it after a crash |
| [docs/TRINO.md](docs/TRINO.md) | What is known about Trino and Presto, for their driver |
| [docs/CLICKHOUSE.md](docs/CLICKHOUSE.md) | What is known about ClickHouse over HTTP, for its driver |
| [docs/BACKLOG.md](docs/BACKLOG.md) | The planned work, in order |

## Related projects

`dbimp` is one of the `xo` projects for databases. Each one is a separate
repository:

| Project | What it is |
| --- | --- |
| [usql](https://github.com/xo/usql) | A command line client for SQL and NoSQL databases, modeled on `psql`. It uses the drivers of `dbimp` |
| [dburl](https://github.com/xo/dburl) | Parses the URL of a database, and names the driver that opens it. It gives each driver of `dbimp` its DSN |
| [dbmeta](https://github.com/xo/dbmeta) | Reads the metadata of a database: its schemas, tables, columns and the rest. Its command `dbrun` starts the servers that the tests of `dbimp` use |
| [dbtpl](https://github.com/xo/dbtpl) | Generates Go code from the schema of a database. It reads the schema through `dbmeta` |
| [dbimp](https://github.com/xo/dbimp) | This repository: `database/sql` drivers for databases that have no idiomatic Go driver |
| [cql](https://github.com/xo/cql) | The `database/sql` driver for Cassandra |
| [tblfmt](https://github.com/xo/tblfmt) | Writes a result set as a text table, one row at a time. `usql` uses it |
| [rline](https://github.com/xo/rline) | A readline package for Go, which reads a line of text that a person edits. `usql` uses it |
| [transit](https://github.com/xo/transit) | A Go port of tree-sitter, a parser of source code. `rline` uses it to highlight syntax, and `usql` to complete statements |
