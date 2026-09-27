# dbimp

`dbimp` holds Go `database/sql` drivers for databases that have no idiomatic
Go driver, and for groups of databases that can share one implementation.
[usql](https://github.com/xo/usql) and [dbtpl](https://github.com/xo/dbtpl)
use them. The first drivers are for databases that take queries over HTTP.
The drivers use the Go standard library, with
[apd](https://github.com/cockroachdb/apd) for decimals, and need no cgo.

The repository is new. It holds one driver, for Couchbase, which is not yet
released. [docs/TARGETS.md](docs/TARGETS.md) names the databases it aims to
support.

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
| [CLAUDE.md](CLAUDE.md) | The rules, written for a coding agent. They apply to a person too |
| [docs/PLAN.md](docs/PLAN.md) | Every decision, and the open questions |
| [docs/TARGETS.md](docs/TARGETS.md) | Every target database, its priority, and the review of the list |
| [docs/DRIVER.md](docs/DRIVER.md) | Every step to add a driver, in order |
| [docs/DESIGN.md](docs/DESIGN.md) | The design of the code that every driver shares |
| [docs/COUCHBASE.md](docs/COUCHBASE.md) | What is measured about the Couchbase query service |
| [docs/BACKLOG.md](docs/BACKLOG.md) | The planned work, in order |
