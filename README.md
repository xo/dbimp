# dbimp

`dbimp` holds Go `database/sql` drivers for databases that have no idiomatic
Go driver, and for groups of databases that can share one implementation.
[usql](https://github.com/xo/usql) and [dbtpl](https://github.com/xo/dbtpl)
use them. The first drivers are for databases that take queries over HTTP.
The drivers use the Go standard library, with
[apd](https://github.com/cockroachdb/apd) for decimals, and need no cgo.

The repository is new. It holds the drivers `couchbase`, which was first
released in `v0.1.0`, `surrealdb`, which was first released in `v0.2.0`,
`neo4j`, which the tag `v0.3.0` holds, and `influxdb`, which no release
holds yet. [docs/TARGETS.md](docs/TARGETS.md)
names the databases it aims to support, and the order of the work.

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
| [docs/DESIGN.md](docs/DESIGN.md) | The design of the code that every driver shares |
| [docs/COUCHBASE.md](docs/COUCHBASE.md) | What is measured about the Couchbase query service |
| [docs/SURREALDB.md](docs/SURREALDB.md) | What is known about the HTTP interface of SurrealDB |
| [docs/NEO4J.md](docs/NEO4J.md) | What is known about the HTTP interface of Neo4j |
| [docs/AVATICA.md](docs/AVATICA.md) | What is known about Apache Calcite Avatica and the products that speak it |
| [docs/INFLUXDB.md](docs/INFLUXDB.md) | What InfluxDB 1, 2 and 3 answer, as measured on seven releases |
| [docs/CRATEDB.md](docs/CRATEDB.md) | What is known about CrateDB, and why it has no driver here |
| [docs/ARANGODB.md](docs/ARANGODB.md) | What is known about ArangoDB, before a server runs |
| [docs/DATABEND.md](docs/DATABEND.md) | What is known about Databend, before a server runs |
| [docs/TDENGINE.md](docs/TDENGINE.md) | What is known about TDengine, before a server runs |
| [docs/PINOT.md](docs/PINOT.md) | What is known about Apache Pinot, before a server runs |
| [docs/RQLITE.md](docs/RQLITE.md) | What is known about rqlite, before a server runs |
| [docs/LIBSQL.md](docs/LIBSQL.md) | What is known about libSQL and Turso, before a server runs |
| [docs/BACKLOG.md](docs/BACKLOG.md) | The planned work, in order |
