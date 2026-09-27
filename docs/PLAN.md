# Decisions

Every decision that shapes this repository, in the order it was made. Entries
are append only. A decision is never edited to change its conclusion. When a
later decision replaces one, the older entry keeps its text and gains a line
at the top saying what replaced it.

Each heading carries a status. `Decided` means Ken settled it or it follows
from a rule every `xo` repository keeps. `Proposed` means it is a
recommendation that waits for Ken. An amendment must be visible from both
sides: if D11 amends D4, then D4 says so too.

Work items live in [BACKLOG.md](BACKLOG.md) and are numbered `W1`, `W2` and
so on. Decisions are numbered `D1`, `D2` and so on. The two series never mix.

| Decision | Title | Status |
| --- | --- | --- |
| [D1](#d1-the-module-path-is-githubcomxodbimp-decided) | The module path is github.com/xo/dbimp | Decided |
| [D2](#d2-the-go-directive-is-1271-decided) | The go directive is 1.27.1 | Decided |
| [D3](#d3-the-repository-uses-the-xo-layout-decided) | The repository uses the xo layout | Decided |
| [D4](#d4-each-driver-is-its-own-package-and-shared-code-is-in-the-root-package-decided) | Each driver is its own package, and shared code is in the root package | Decided |
| [D5](#d5-dburl-owns-the-schemes-and-the-aliases-decided) | dburl owns the schemes and the aliases | Decided |
| [D6](#d6-go-conventions-are-the-ones-dburl-and-dbmeta-keep-decided) | Go conventions are the ones dburl and dbmeta keep | Decided |
| [D7](#d7-a-package-holds-no-configuration-decided) | A package holds no configuration | Decided |
| [D8](#d8-every-driver-keeps-the-same-contract-with-databasesql-decided) | Every driver keeps the same contract with database/sql | Decided |
| [D9](#d9-a-test-that-needs-a-server-reads-a-dsn-and-skips-without-one-decided) | A test that needs a server reads a DSN and skips without one | Decided |
| [D10](#d10-golangci-lint-runs-in-ci-at-a-pinned-version-decided) | golangci-lint runs in CI at a pinned version | Decided |
| [D11](#d11-agent-skills-are-committed-as-copies-decided) | Agent skills are committed as copies | Decided |
| [D12](#d12-every-text-a-person-reads-follows-simple-english-decided) | Every text a person reads follows simple-english | Decided |
| [D13](#d13-the-module-depends-on-the-standard-library-apd-and-approved-binary-encodings-decided) | The module depends on the standard library, apd, and approved binary encodings | Decided |
| [D14](#d14-the-first-drivers-speak-http-decided) | The first drivers speak HTTP | Decided |
| [D15](#d15-the-targets-are-in-docstargetsmd-decided) | The targets are in docs/TARGETS.md | Decided |
| [D16](#d16-an-ideal-target-meets-three-tests-decided) | An ideal target meets three tests | Decided |
| [D17](#d17-the-priorities-follow-the-three-tests-decided) | The priorities follow the three tests | Decided |
| [D18](#d18-a-result-that-is-not-a-table-becomes-rows-by-three-rules-decided) | A result that is not a table becomes rows by three rules | Decided |
| [D19](#d19-a-json-number-is-never-decoded-through-float64-decided) | A JSON number is never decoded through float64 | Decided |
| [D20](#d20-a-driver-never-fakes-a-transaction-decided) | A driver never fakes a transaction | Decided |
| [D21](#d21-a-driver-reads-a-result-to-its-end-decided) | A driver reads a result to its end | Decided |
| [D22](#d22-the-licence-is-mit-decided) | The licence is MIT | Decided |
| [D23](#d23-the-first-driver-is-a-new-couchbase-driver-decided) | The first driver is a new Couchbase driver | Decided |
| [D24](#d24-a-target-that-has-a-go-driver-in-usql-is-in-scope-decided) | A target that has a Go driver in usql is in scope | Decided |
| [D25](#d25-the-code-uses-encodingjsonv2-and-the-conventions-of-go-127-decided) | The code uses encoding/json/v2 and the conventions of Go 1.27 | Decided |
| [D26](#d26-a-driver-is-named-for-its-database-decided) | A driver is named for its database | Decided |
| [D27](#d27-a-dsn-is-a-standard-url-decided) | A DSN is a standard URL | Decided |
| [D28](#d28-a-driver-registers-one-name-decided) | A driver registers one name | Decided |
| [D29](#d29-the-module-imports-no-other-xo-package-decided) | The module imports no other xo package | Decided |
| [D30](#d30-the-couchbase-driver-registers-as-couchbase-decided) | The Couchbase driver registers as couchbase | Decided |
| [D31](#d31-couchbase-is-tested-on-the-enterprise-image-with-an-ordinary-user-decided) | Couchbase is tested on the Enterprise image, with an ordinary user | Decided |
| [D32](#d32-a-target-has-a-priority-not-a-tier-decided) | A target has a priority, not a tier | Decided |
| [D33](#d33-a-decimal-is-an-apddecimal-decided) | A decimal is an apd.Decimal | Decided |
| [D34](#d34-the-root-package-holds-a-parser-for-placeholders-decided) | The root package holds a parser for placeholders | Decided |
| [D35](#d35-a-dsn-is-a-url-whose-scheme-is-the-name-of-the-driver-decided) | A DSN is a URL whose scheme is the name of the driver | Decided |
| [D36](#d36-rows-read-the-body-as-a-stream-and-close-closes-it-decided) | Rows read the body as a stream, and Close closes it | Decided |
| [D37](#d37-the-shared-test-helpers-are-the-package-dbimptest-decided) | The shared test helpers are the package dbimptest | Decided |
| [D38](#d38-the-couchbase-dsn-decided) | The Couchbase DSN | Decided |
| [D39](#d39-the-values-of-couchbase-decided) | The values of Couchbase | Decided |
| [D40](#d40-parameters-and-options-of-couchbase-decided) | Parameters and options of Couchbase | Decided |
| [D41](#d41-the-first-couchbase-driver-has-transactions-decided) | The first Couchbase driver has transactions | Decided |
| [D42](#d42-how-the-couchbase-driver-reads-a-result-decided) | How the Couchbase driver reads a result | Decided |
| [D43](#d43-the-couchbase-driver-has-a-key-for-durability-decided) | The Couchbase driver has a key for durability | Decided |
| [D44](#d44-a-couchbase-string-scans-into-a-byte-slice-as-base64-decided) | A Couchbase string scans into a byte slice as base64 | Decided |
| [D45](#d45-a-couchbase-transaction-keeps-the-context-of-begintx-decided) | A Couchbase transaction keeps the context of BeginTx | Decided |
| [D46](#d46-the-couchbase-dsn-has-a-key-for-the-timeout-of-a-transaction-decided) | The Couchbase DSN has a key for the timeout of a transaction | Decided |
| [D47](#d47-the-surrealdb-driver-is-named-surrealdb-decided) | The SurrealDB driver is named surrealdb | Decided |
| [D48](#d48-the-surrealdb-url-names-the-namespace-and-the-database-in-its-path-decided) | The SurrealDB URL names the namespace and the database in its path | Decided |
| [D49](#d49-the-surrealdb-driver-speaks-cbor-written-here-and-json-as-an-option-decided) | The SurrealDB driver speaks CBOR, written here, and JSON as an option | Decided |
| [D50](#d50-the-surrealdb-driver-sends-each-statement-to-post-rpc-decided) | The SurrealDB driver sends each statement to POST /rpc | Decided |
| [D51](#d51-the-surrealdb-dsn-has-the-key-auth-for-the-level-of-the-user-decided) | The SurrealDB DSN has the key auth for the level of the user | Decided |
| [D52](#d52-each-surrealdb-statement-is-a-result-set-and-its-rows-follow-d18-decided) | Each SurrealDB statement is a result set, and its rows follow D18 | Decided |
| [D53](#d53-the-go-types-of-surrealdb-decided) | The Go types of SurrealDB | Decided |
| [D54](#d54-the-surrealdb-driver-has-no-transactions-decided) | The SurrealDB driver has no transactions | Decided |
| [D55](#d55-the-errors-and-the-results-of-a-surrealdb-write-decided) | The errors and the results of a SurrealDB write | Decided |
| [D56](#d56-a-surrealdb-query-stops-when-its-context-ends-decided) | A SurrealDB query stops when its context ends | Decided |
| [D57](#d57-the-surrealdb-driver-exports-version-decided) | The SurrealDB driver exports Version | Decided |

### D1. The module path is github.com/xo/dbimp. Decided.

Every `xo` module is named `github.com/xo/<repository>`. A driver is a
package inside this module, so its import path is
`github.com/xo/dbimp/<driver>` (D4). The root package `dbimp` holds
the code that the drivers share.

### D2. The go directive is 1.27.1. Decided.

`go.mod` says `go 1.27.1` and has no `toolchain` line. That is Ken's target,
and `dbmeta`, `cql` and `n1ql` use the same line.

### D3. The repository uses the xo layout. Decided.

This matches `dbmeta` and `cql`:

- The root holds `README.md`, `CLAUDE.md` and `CONTRIBUTING.md`, and no other
  Markdown. Every other document is in `docs/`, and each one is named in the
  table in `CLAUDE.md` and in the table in `README.md`.
- `CLAUDE.md` follows the order that `dbmeta/CLAUDE.md` uses: purpose, a
  "Which document to read" table, numbered hard rules with their reasons,
  layout, Go conventions, linting, the commands to run before committing, and
  how to write documentation. It also carries the table of sibling
  repositories from `usql/CLAUDE.md`.
- `CONTRIBUTING.md` holds the same material for a person, and it is shorter.
  No repository has an `AGENTS.md`, and this one has none.
- `docs/PLAN.md` is this file. It holds decisions and open questions only.
- `docs/BACKLOG.md` holds work items, in the format that
  `usql/docs/BACKLOG.md` uses.
- Examples are `Example` tests. Golden files go in `testdata/`.
- CI is one GitHub Actions workflow, `.github/workflows/test.yml`, on
  `ubuntu-latest`, with `go-version-file: go.mod`. There is no Makefile and no
  matrix of operating systems.
- There is one `.gitignore`, at the root. `.gitattributes` sets
  `* text=auto eol=lf`, as in `dbmeta` and `cql`.

`docs_test.go` holds these rules: `TestTheRootHoldsThreeDocuments`,
`TestEveryDocumentIsInTheTable`, `TestEveryLinkResolves`,
`TestEveryDecisionReferenceExists`, `TestTheDecisionIndexIsComplete` and
`TestEveryTestNameInTheDocsExists`.

### D4. Each driver is its own package, and shared code is in the root package. Decided.

Ken decided this on 2026-09-27. A driver is the package
`github.com/xo/dbimp/<driver>`, in a folder at the root with the name of the
driver. That import path is the `GoPackage` of its scheme in `dburl`. A
consumer imports each driver it wants directly, so a driver that nobody
imports costs a consumer nothing.

The root package `dbimp` holds the code that the drivers share: the HTTP
client, the adapters that encode and decode values, and the other utilities
that two or more drivers use. Every driver imports it. Most targets in
[TARGETS.md](TARGETS.md) share a large part, such as authentication, a
decoder that turns a JSON result into rows, and the `database/sql` types
that sit on top.

This differs from the other `xo` repositories in one way. There is no
central registry of drivers, and no umbrella package that imports every
driver. `usql` already selects drivers with build tags, and a second list
here makes two copies of one fact.

The repository is one module. D13 keeps the dependencies to the standard
library, `apd` and a few approved binary encodings, so no driver carries a
dependency tree that another driver must not have.

The exported API of the root package is a public API, because a driver
outside this repository can import it too. Keep it small, and export a name
only when a driver in this repository needs it.

### D5. dburl owns the schemes and the aliases. Decided.

`dburl` owns the taxonomy of schemes, aliases and flavors. This repository
never writes a list of schemes or of aliases. A consumer that holds a URL in
any form reads `dburl` for it, and `dburl` hands the driver one complete URL
(D35). This follows `dbmeta` hard rule 1.

Each driver calls `sql.Register` from `init` with one name, which is the name
of its package and of its database (D28 and D30). The scheme of the URL that
it reads is that name. The driver parses that URL with `net/url` (D27), and
nothing else.

When a driver lands here, the `dburl` session changes its scheme: the
`GoPackage` becomes `github.com/xo/dbimp/<driver>`, `RequiresCGO` says
whether it needs a C compiler, and the `Driver` name becomes the name of the
driver if it differs (D30). Step 16 of [DRIVER.md](DRIVER.md) is that move.

### D6. Go conventions are the ones dburl and dbmeta keep. Decided.

The conventions in "Go conventions" in `dbmeta/CLAUDE.md` apply here:

- Errors are wrapped with `%w`. A message is lower case, starts with a
  gerund, and names what failed.
- A sentinel error is a constant of a defined string type, as in
  `type Error string` and `const ErrX Error = "x"`. A variable made with
  `errors.New` can be reassigned by any importer, and a constant cannot.
- `ctx` is the first parameter and is never stored in a struct.
- Accept interfaces, return concrete types, and keep an interface to three
  methods or fewer, defined where it is consumed.
- A package name is one short word, and an exported name does not repeat it.
- A receiver is one or two letters, the same on every method.
- Generics replace `interface{}` for a container of one type.

The `go-pedantry` skill holds the longer form (D11).

### D7. A package holds no configuration. Decided.

A driver keeps no setting in a package level variable. A setting arrives in
the DSN, or in a `driver.Connector` that the caller builds and passes to
`sql.OpenDB`. A driver logs nothing.

One `usql` driver borrowed the configuration of another driver and shipped a
fault to users. `go_n1ql` keeps its credentials in package variables, and
that is the pattern not to copy. This follows `dbmeta` hard rule 6 and
`cql` D8.

### D8. Every driver keeps the same contract with database/sql. Decided.

Each driver does all of these:

- It implements `driver.DriverContext` and `driver.Connector`, and the
  context forms of every interface it implements, such as
  `driver.QueryerContext` and `driver.ExecerContext`. It stops work when the
  context ends.
- It returns a NULL as nil and never as a zero value. `go-cql-driver`
  returned an empty string for a CQL NULL, which hid faults for months, until
  the move to `xo/cql` exposed them (dbmeta D62 and dbmeta D93).
- It keeps the column order of the statement, returns decoded Go values, and
  never wraps one column in an object. `go_n1ql` sorted the columns by name,
  returned JSON text, and wrapped a row of one column as `{"v": ...}`, and
  each of those ruled out a `dbmeta` model (dbmeta D94).
- It sends a statement to the server as the caller wrote it, and does not
  split it. `vertica-sql-go` splits at every `;` and so cannot create a SQL
  function (dbmeta D88). If a product needs a split, the rule for it is
  written in a decision first.
- It returns every error to the caller, wrapped with `%w`. It returns
  `driver.ErrBadConn` only when the statement did not reach the server,
  because `database/sql` then runs the statement again, and a second run of a
  write is a second write.

`dbmeta` needs nothing beyond a correct driver. It takes any value with a
`QueryContext` method and runs every statement itself. `dbmeta` hard rule 10
requires its test module to use the same package that `usql` uses for each
database, so a driver lands in `usql` and in `dbmeta` together.

### D9. A test that needs a server reads a DSN and skips without one. Decided.

A unit test needs no server. A test that needs one reads the DSN from an
environment variable named for the driver in upper case, followed by `_DSN`,
such as `CQL_DSN` in `cql`. It calls `t.Skip` when the variable is empty. The
name of such a test holds the word `Integration`, so that
`go test -run Integration` selects it. There are no build tags for this and
no testcontainers.

`dbrun` in `dbmeta` starts every server, and nothing else does. `dbrun dsn`
prints the DSN that `dbmeta` gives a driver. A database that `dbrun` does not
know gets its entry in `dbmeta/container/` first, which is work for the
`dbmeta` session or for Ken. The CI workflow checks out a pinned commit of
`dbmeta` and runs `dbrun`, as in `cql` D20 and `n1ql` D28. A new pin is a
commit in this repository.

A driver is tested as the administrator and as an ordinary user. The parity
tests in `dbmeta` found queries that the server refused to an ordinary user,
and nobody had recorded them (dbmeta D61).

### D10. golangci-lint runs in CI at a pinned version. Decided.

`.golangci.yml` sets `version: "2"` and `default: all`, with a disable list.
Each disabled linter has its reason beside it. The version is pinned in the
workflow, so that a new release cannot turn on a linter that nobody chose.
This is the posture of `dburl`, `dbmeta` and `cql`.

A linter that makes idiomatic Go worse is disabled. Only a real defect gets a
code change. A change made only to quiet a linter is itself a defect.

### D11. Agent skills are committed as copies. Decided.

The repository carries two agent skills, `simple-english` and `go-pedantry`,
copied from `dbmeta`. Each is an ordinary folder under `.agents/skills` and
under `.claude/skills`, and `skills-lock.json` names its source.

A symbolic link is refused, because a Windows checkout writes a link as a text
file, and Claude Code then loads no skill and reports nothing.
`TestSkillsAreCopies` fails on a link, and it fails when the two folders
differ. This follows dbmeta D89 and `cql` D15.

The first copies in this repository were symbolic links, made before this
file existed. They were replaced with copies on 2026-09-27.

### D12. Every text a person reads follows simple-english. Decided.

Ken asked that every agent load the `simple-english` skill before it writes
any text that a user can read. That includes `README.md` and every other
document, code comments, error messages, commit messages and the text of test
failures.
He repeated it on 2026-09-27. The short
form is in "Writing documentation" in `CLAUDE.md`. This follows dbmeta D89.

### D13. The module depends on the standard library, apd, and approved binary encodings. Decided.

Ken decided this on 2026-09-27. The module is close to free of dependencies.
It has three kinds of dependency, and no other:

1. The Go standard library. HTTP, JSON, TLS, authentication and request
   signing are written with it. That includes the AWS Signature Version 4
   that DynamoDB needs, and the parser for placeholders of D34.
2. `github.com/cockroachdb/apd/v3`, which holds a decimal (D33).
3. A package for a binary encoding that the standard library does not have,
   such as CBOR, when Ken approves it for one database.

A binary encoding is considered for each database on its own, and Ken
decides each case. Two questions decide it: whether the package for the
encoding can come into this module with few dependencies, and whether the
gain is worth the cost of keeping it. Examples are CBOR for SurrealDB,
VelocyPack for ArangoDB, Smile for Elasticsearch, and the Arrow stream for
InfluxDB 3 and Databricks. Most targets in [TARGETS.md](TARGETS.md) also
answer in JSON, so a binary encoding needs a reason that JSON cannot meet.

Ask Ken before you add any package. When he approves one, add it to the
`depguard` list in `.golangci.yml`, so that the list is the record of every
package that the module can import.

Ken approved CBOR for SurrealDB on 2026-09-27, written in the root package
with the standard library, so it adds no package (D49).

### D14. The first drivers speak HTTP. Decided.

Ken decided this on 2026-09-27. The first drivers here are adapters and
clients for databases that have an HTTP or HTTPS interface: SQL, NoSQL and
others. Each one takes a query that a person can type by hand, such as SQL, a
dialect like SQL, JSON, or another query language, because that is what
`usql` sends.

Each driver is pure Go and never needs cgo, because `net/http` is all that it
needs. A consumer builds it with `CGO_ENABLED=0`, and `RequiresCGO` in
`dburl` is false for every one of them.

### D15. The targets are in docs/TARGETS.md. Decided.

[TARGETS.md](TARGETS.md) names every target, and the order that Ken wrote.
It is the only copy of that list. The notes it came from, in
`xo/websql/notes`, are its source and are not read again. A change to the
list is a change to that file.

### D16. An ideal target meets three tests. Decided.

Ken decided this on 2026-09-27. An ideal target can be started by `dbrun`,
takes queries and returns results over HTTP, and has a SQL dialect or a
dialect like SQL. [TARGETS.md](TARGETS.md) names the three tests R, H and S,
and measures each target against them.

### D17. The priorities follow the three tests. Decided.

A target that meets R, H and S is P1. A target that fails one is P2.
A target that fails two or more, or that no longer exists, is P3. Inside
a priority, a target whose server sends column metadata, and a target that
`usql` has no driver for, come first.

Ken accepted this rule on 2026-09-27. The review in [TARGETS.md](TARGETS.md)
applies it. It moves Pinot,
TDengine, Drill and Solr up to P1. It moves ArangoDB, Neo4j, Dgraph,
CouchDB, TerminusDB, Qdrant, Weaviate, ScyllaDB Alternator and Stargate down
to P2. It moves the MongoDB Atlas Data API, Fauna and PostgREST to P3.
The `dbmeta` session proposed the rule.

### D18. A result that is not a table becomes rows by three rules. Decided.

This answers Q3. Ken accepted it on 2026-09-27.

`database/sql` reads the columns before the first row, so the column set is
fixed at that point. A `dbmeta` model reads each row by position, so every
row must have the same columns. The three rules are these:

1. If the server sends column metadata, the columns are that metadata, in its
   order.
2. If the server sends only objects, the columns are the keys of the first
   object, in the order that they appear in the response. Read the keys with
   `jsontext.Decoder.ReadToken` (D25), and never decode a row into a map. A key
   that a later row lacks is nil. A key that only a later row has is an error,
   and never a new column.
3. If the query returns whole documents, nodes, paths or points with no
   projection, the result has one column that holds each value. The column
   has the name that the server gives it, and never the name of a wrapper.

The union of the fields of every row is never the column set, because it
needs the whole result before the first row. Each driver decides whether a
missing key and a JSON null are different, in a decision of its own, as
n1ql D24 did for Couchbase MISSING. The `dbmeta` session proposed these rules.

### D19. A JSON number is never decoded through float64. Decided.

A float64 holds an integer exactly only up to 2^53. ClickHouse, Druid and
Elasticsearch send 64-bit integers, and DynamoDB sends numbers with up to 38
digits. Each driver reads a number as the raw text of its token from
`jsontext` (D25), and converts the text by the type of its column. A decimal
becomes an `*apd.Decimal` (D33). Ken accepted this on 2026-09-27.

### D20. A driver never fakes a transaction. Decided.

Ken accepted this on 2026-09-27. Most HTTP interfaces keep no state between
requests and have no transactions. If the product has none, `BeginTx` returns a
sentinel error that says so. If the product has one, such as Neo4j, rqlite,
libSQL, Trino and Couchbase, the state belongs to the connection, and
`ResetSession` clears it. Each such driver records its design in a decision of
its own.

### D21. A driver reads a result to its end. Decided.

Ken accepted this on 2026-09-27. A driver follows every page, cursor and next
link until the server says that the result is complete. It never returns a
result that the server cut short as if it were complete. Pinot adds `LIMIT 10`
to a selection that has no limit, and a driver that says nothing returns ten
rows. If a server cuts a result and gives no way to read the rest, the driver
returns an error.

### D22. The licence is MIT. Decided.

Ken added `LICENSE` on 2026-09-27. It is the MIT licence, with the copyright
of Kenneth Shaw, as in `dbmeta`, `dburl` and `usql`. No file carries a
licence header.

### D23. The first driver is a new Couchbase driver. Decided.

Ken decided this on 2026-09-27. Couchbase, through its query service and
SQL++ (N1QL), is the first target in P1. This repository gets a clean
implementation, and it replaces `xo/n1ql`. It is not a port of `go_n1ql`,
and it keeps none of the three faults that D8 names.

dbmeta D94 ties the Couchbase model to a rewrite of `xo/n1ql`. When this
driver works, `dburl`, `usql` and `dbmeta` move to it, and `dbmeta` measures
its model again, as dbmeta D93 did when Cassandra moved to `xo/cql`.

### D24. A target that has a Go driver in usql is in scope. Decided.

Ken decided this on 2026-09-27. A second goal of this repository is to reduce
the dependencies of `usql`. A driver here can replace a driver that `usql`
imports now, such as the ones for ClickHouse, Trino, Presto, DynamoDB,
Snowflake, BigQuery and Databricks. Such a target is likely P2.

When a driver here replaces one in `usql`, `dbmeta` measures its model again
on the new driver, because `dbmeta` hard rule 10 requires the package that
`usql` uses.

### D25. The code uses encoding/json/v2 and the conventions of Go 1.27. Decided.

Ken decided this on 2026-09-27. The module targets Go 1.27.1 or newer (D2),
and it uses the newest conventions of Go where they fit:

- JSON is read and written with `encoding/json/v2` and
  `encoding/json/jsontext`, never with `encoding/json`.
- A nullable value is `sql.Null[T]`, never a type such as `sql.NullString`.
- A UUID is `uuid.UUID` from the standard library.

The reason for `json/v2` is speed. `jsontext.Decoder` reads one token at a
time from the body of the response. A driver can then decode a result as it
arrives from the server, and hand each row to `database/sql` before the rest
of the result has arrived. The whole result is never held in memory, and the
keys of an object keep their order (D18). Go 1.27.1 builds both packages with
no `GOEXPERIMENT` setting. A build on 2026-09-27 showed this.

### D26. A driver is named for its database. Decided.

Ken decided this on 2026-09-27. The package of a driver takes the name of the
database it targets, and never the name of the query language. The
Couchbase driver is `github.com/xo/dbimp/couchbase`, not `n1ql`. A driver
that serves several products takes the name of the product that the others
follow, and its documentation names the others. The name is one short lower
case word, as D6 requires.

The name that the driver registers with `database/sql` is the same name, as
D30 decides.

### D27. A DSN is a standard URL. Decided.

Ken decided this on 2026-09-27. A driver takes only a standard URL as its
DSN, and parses it with `net/url`, by the rules of the Go standard library.
`dburl` then writes that URL for each scheme. A driver keeps no form of DSN
from an earlier driver, such as the list of hosts that `xo/cql` accepts or
the address of a cluster manager that `go_n1ql` accepts. A DSN that
`net/url` refuses is an error.

### D28. A driver registers one name. Decided.

Ken decided this on 2026-09-27. Each driver calls `sql.Register` once, with
one name. It registers no alias. A package outside this repository, such as
`dburl`, keeps any alias that it chooses for a database, and turns the alias
into the one name. D30 says which name that is.

### D29. The module imports no other xo package. Decided.

Ken decided this on 2026-09-27. This module never imports `dburl`, `dbmeta`,
`usql` or `dbtpl`. It reads information from them when it needs a fact, such
as a scheme in `dburl/scheme.go` or a release in `dbmeta/container/`. It runs
`dbrun` from a checkout of `dbmeta` as a tool, to start the servers for its
tests. None of them is in `go.mod`, and `depguard` refuses each one.

### D30. The Couchbase driver registers as couchbase. Decided.

Ken decided this on 2026-09-27, and it answers Q10. The Couchbase driver
registers the one name `couchbase` with `database/sql`, the same as its
package (D26 and D28). It never registers `n1ql`.

`dburl` names the scheme `n1ql` today, with `couchbase` as an alias, because
`go_n1ql` registers `n1ql`. At the move of step 16 of
[DRIVER.md](DRIVER.md), the `dburl` session makes `couchbase` the `Driver`
name of the scheme and keeps `n1ql` as an alias. The change waits for the
move, because `usql` opens `go_n1ql` by the name `n1ql` until then.

The aliases of the scheme belong to `dburl`, and this repository does not
choose them (D35). The `dburl` session reported that the `Dialect` of the
scheme changes from `n1ql` to `couchbase` at the rename, because the
`Dialect` of a scheme names its own `Driver` (dburl D19). A consumer that
matches on the `Dialect` `n1ql`, in `usql`, `dbtpl` or `dbmeta`, changes in
the same release.

The rule for every driver follows from D26 and D28: the registered name is
the name of the package, which is the name of the database. Where `dburl`
names a scheme differently, the `dburl` session renames it at the move.

### D31. Couchbase is tested on the Enterprise image, with an ordinary user. Decided.

Ken accepted this on 2026-09-27. `dbmeta` starts Couchbase from the image
`docker.io/library/couchbase`, whose bare tag is the Enterprise edition,
which is free for development. The `Init` step of that entry creates only
`Administrator` today. Step 4 of [DRIVER.md](DRIVER.md) needs an ordinary user
as well, so the `dbmeta` session adds one, with a password that `dbrun`
knows.

### D32. A target has a priority, not a tier. Decided.

Ken decided this on 2026-09-27, and it answers Q9. [TARGETS.md](TARGETS.md)
places each target in P1, P2 or P3. The word tier stays with `dbmeta`, which
uses it for the Tested, Nightly, Verified and Archived releases in CI.

### D33. A decimal is an apd.Decimal. Decided.

Ken decided this on 2026-09-27, and it answers Q6. A decimal is held as an
`apd.Decimal` from `github.com/cockroachdb/apd/v3`. It is the only dependency
that is not the standard library or a binary encoding (D13).

The candidates were compared on 2026-09-27. `apd/v3` had its latest release,
v3.2.3, on 2026-03-23, by the Go module proxy. It uses only the standard
library, and it holds arbitrary precision with a context that sets the
precision and the rounding. It has NaN and infinity, which the `Decimal128`
of MongoDB needs. CockroachDB uses it. `shopspring/decimal` had no release
after 2024-04-12, and `govalues/decimal` holds at most 19 digits, which is
too few for DynamoDB and ClickHouse.

A driver hands a decimal to `database/sql` as an `*apd.Decimal`, and a new
one for each value. An `apd.Decimal` holds a pointer to its digits once they
are large, so a copy of the value shares them with the original.
[DESIGN.md](DESIGN.md) says how `dbimp.Assign` stores one.

`go.mod` requires `apd/v3` v3.2.3, from the first code that uses it, in the
root package.

### D34. The root package holds a parser for placeholders. Decided.

Ken decided this on 2026-09-27, and it answers Q7. Some servers bind no
arguments. For them, the root package holds a small parser that finds each
`?` and each `@name` in a statement. It skips string literals, quoted
identifiers and comments, so that a `?` inside a literal stays as it is. That
fault is in `go_n1ql`. The driver then puts each argument into the statement
as a literal, written by an escaper that the driver supplies for its product.
The escaper is never shared, because each product quotes in its own way. Hive
reads a doubled quote as two literals joined (dbmeta D78).

A driver whose server binds arguments sends them to the server, and does not
use the parser for that.

An existing package can replace the parser if one fits D13. None was found on
2026-09-27. The sanitizer of `pgx` is `pgx/v5/internal/sanitize`, which
cannot be imported, and `interpolateParams` in `go-sql-driver/mysql` is not
exported.

### D35. A DSN is a URL whose scheme is the name of the driver. Decided.

Ken decided this on 2026-09-27, and it answers Q11. `dburl` hands each driver
a complete URL, and the scheme of that URL is the one name that the driver
registers (D28), such as `couchbase://`. A driver knows no alias and accepts
no other scheme. `dburl` turns every alias into that URL.

The driver reads the rest of the URL by D27: the user information, the host,
the path and the query keys. Whether it speaks HTTPS or HTTP is a key of the
query, or another part of the URL that its decisions of step 9 of
[DRIVER.md](DRIVER.md) name. It is never a second scheme, because the driver
has one name.

### D36. Rows read the body as a stream, and Close closes it. Decided.

This answers Q8, from Ken's first design and a consultation on 2026-09-27.
Ken accepted it on 2026-09-27.
Gemini answered. DeepSeek timed out twice. The source of Go 1.27 settled the
two claims that the design depends on.

The design is this:

1. `Rows` holds the body of the response and a `jsontext.Decoder` that reads
   from it, and no other buffer. `jsontext.Decoder` keeps its own buffer and
   reads from the body into it (measured in `encoding/json/jsontext` in Go
   1.27.1). A `bufio.Reader` in front of it adds only a second copy.
2. `Rows.Next` reads the tokens of one row, and no more.
3. When `Next` reaches the end of the rows, it reads the rest of the
   response, such as the `errors`, `status` and `metrics` of Couchbase. An
   error there comes back from `Next`, so a result cut short by the server
   never looks complete (D21). The body is then at EOF.
4. `Rows.Close` before the end closes the body and reads nothing more. On
   HTTP/1.1 the transport then closes the connection, and does not drain it
   (measured: `bodyEOFSignal.earlyCloseFn` in `net/http/transport.go` of Go
   1.27.1). On HTTP/2 the stream is reset and the connection stays. To drain
   a large result to save one connection costs more than the connection.
5. `Rows.Close` after the end closes a body that is already at EOF, so the
   connection goes back to the pool.
6. A cancelled context aborts the read of the body, and `Next` returns the
   error of the context, never `driver.ErrBadConn`.

A server can keep running a query after the client closes the connection.
Couchbase has `DELETE /admin/active_requests/<id>`, Trino has a `DELETE` on the
next URI, and ArangoDB has a `DELETE` on the cursor. `Close` has no context,
and the library never makes one. So a driver relies on the server noticing the
closed connection, and item 7 of step 6 of [DRIVER.md](DRIVER.md) measures that
for each product. Gemini said that Couchbase stops such a query, and nothing
measured it. If a product does not stop, its driver records a decision of its
own, and Ken makes it. The choice is between keeping `context.WithoutCancel` of
the context of the query in `Rows`, which breaks the rule that a struct never
holds a context, and a timeout that the connector holds.

### D37. The shared test helpers are the package dbimptest. Decided.

Ken accepted this on 2026-09-27. D4 puts the code that the drivers share in
the root package. The helpers for the tests of a driver are the exception.
They are the package `github.com/xo/dbimp/dbimptest`, which only a test
imports.

The helpers import `testing` and `net/http/httptest`. Every driver imports
the root package. If the root package held the helpers, every consumer of a
driver links both packages and runs their `init`, for code that only a test
uses. The root package keeps what a driver needs at run time,
and `dbimptest` keeps what it needs in a test.

The first reason written here was that `httptest` adds a command line flag
to every program. The source of Go 1.27.1 showed that it adds the flag only
when the command line already names it, so that reason was wrong.

`dbimptest` is not a driver, so the gates of W6 skip its folder.
[DESIGN.md](DESIGN.md) holds what it contains.

### D38. The Couchbase DSN. Decided.

Ken accepted this on 2026-09-27. D43 adds the key `durability_level`, and
D46 adds the key `txtimeout`.

The DSN is `couchbase://user:pass@host:port/?key=value` (D27 and D35). The
user information holds the credentials, which the driver sends with basic
authentication, and never in the `creds` parameter, which puts the password
in the body. The host and the port are the query service, with the port
8093 by default, or 18093 when `tls` is true. The path is empty or `/`, and
any other path is an error. The keys of the query are these:

- `tls`: `true` to speak HTTPS, `false` by default.
- `query_context`: the bucket and the scope that a collection named alone
  belongs to, such as `default:dbmeta._default`.
- `scan_consistency`: `not_bounded` by default, or `request_plus`.
- `timeout`: a duration, such as `30s`, that the server enforces with code
  1080.

The driver talks to the host of the URL only. It does not find the other
nodes of a cluster, because `dbrun` cannot test that, and a cluster in a
container answers with addresses that a client outside it cannot reach
([COUCHBASE.md](COUCHBASE.md)). It follows no redirect, and none was seen.
It asks Ken before it adds a key.

### D39. The values of Couchbase. Decided.

Ken accepted this on 2026-09-27. D44 amends how a string scans into a byte
slice.

The signature names the kind of each column, and the driver decodes each
value by what it holds, with `ColumnTypeDatabaseTypeName` the kind in upper
case: `NUMBER`, `STRING`, `BOOLEAN`, `NULL`, `MISSING`, `ARRAY`, `OBJECT` or
`JSON`.

- A number is an `int64` when it is an integer that fits, and a `float64`
  otherwise, as `dbimp.Number` decodes it. The server sends a larger integer
  as a float64 itself, so no `*apd.Decimal` arrives from Couchbase.
  Correction of 2026-09-27: the server rounds a larger integer to a float64,
  and sends the digits of the rounded value, such as
  `123456789012345680000000000000`, with no point and no exponent. Those
  digits are too large for an `int64`, so `dbimp.Number` returns them as an
  `*apd.Decimal`, which holds exactly what the server sent. A test of the
  driver found this.
- A string is a `string`, a boolean is a `bool`, and a time, a binary value
  and a UUID are the strings that the server sends, because SQL++ has no
  type for them. A caller scans a time into a `time.Time` through
  `database/sql`.
- An array is a `[]any` and an object is a `map[string]any`, as `dbimp.Any`
  decodes them. A scan into a `*[]byte` or a `*jsontext.Value` gets the JSON
  text of the value instead, so a caller that wants the text keeps it.
- NULL and MISSING are both nil, as n1ql D24 decided, because
  `database/sql` has one NULL. A caller that must tell them apart reads the
  kind of the column.
- On 7.2.9 the columns arrive in the order of their names, and the driver
  returns that order, as n1ql D33 decided. It does not send a second request
  to learn the order of the projection.

### D40. Parameters and options of Couchbase. Decided.

Ken accepted this on 2026-09-27.

The driver sends each argument to the server, which binds it (D34). A
positional argument goes in `args`, and a `sql.Named` argument goes in
`$name`. `?` and `$1` in a statement are both positional, and the driver
changes neither. A value is encoded with `encoding/json/v2`, so a map, a
slice and a struct are arguments as well as a scalar. The driver implements
`driver.NamedValueChecker` for that, and returns `driver.ErrSkip` for a
`driver.Valuer`.

An option of one query comes from the DSN, then from the context through
`WithOptions`, then from an argument of the type `Option`, in that order, as
cql D23 does. The options are the keys of D38 without `tls`, and `readonly`.

### D41. The first Couchbase driver has transactions. Decided.

Ken decided this on 2026-09-27. The query service has transactions through
a `txid`, and the first release of the driver supports them (D20):

- `BeginTx` sends `BEGIN WORK`, and the connection keeps the `txid` that it
  returns. Each statement on the connection carries the `txid` until the
  transaction ends.
- `Tx.Commit` sends `COMMIT WORK`, and `Tx.Rollback` sends `ROLLBACK WORK`,
  each with the `txid`. The connection then forgets it.
- `ResetSession` rolls back a transaction that is still open, and returns
  `driver.ErrBadConn` if that fails, so that `database/sql` drops the
  connection rather than hand its state to another caller.
- `TxOptions.ReadOnly` sends `readonly`. An isolation level other than the
  default is an error that wraps `dbimp.ErrNotSupported`, because the query
  service has one level.
- The timeout of a transaction is an option, as D40 describes.

A transaction lives on the one query node that began it. The driver talks to
one host (D38), so every statement of a transaction reaches that node.

`COMMIT WORK`, a `txid` that expired, and a `txid` sent after the end are
not measured yet. W5 measures them in step 6, and step 14a tests the
transaction as a feature.

### D42. How the Couchbase driver reads a result. Decided.

Ken accepted this on 2026-09-27.

A result is one response, and it does not page. The driver reads the
`signature` for the columns, then each row of `results` as `Rows.Next` asks
(D36). After the last row it reads the rest of the response. If `errors` is
not empty, or `status` is not `success`, `Rows.Next` returns an error that
wraps `dbimp.ErrIncomplete` and holds each code and message (D21). A
`status` of `stopped` is an error for the same reason, because the result
is cut short.

The driver relies on the server to stop a query when the client
disconnects, which it does on every release (D36). `Rows.Close` sends no
cancel. The driver uses no binary encoding, because the server offers none.
Couchbase Analytics waits for a later work item, because `dbrun` does not
publish its port.

### D43. The Couchbase driver has a key for durability. Decided.

Ken decided this on 2026-09-27, and it amends D38 and D40. The DSN takes the
key `durability_level`, with the values that the server takes: `none`,
`majority`, `majorityAndPersistActive` and `persistToMajority`. If the DSN
has no such key, the driver sends none, and the server uses its default,
which is `majority`. `WithOptions` sets it for one transaction, by the order
of D40.

The survey found that a transaction with the default durability cannot
commit on the one node that `dbrun` starts, and fails with code 17007
([COUCHBASE.md](COUCHBASE.md)). The integration tests set
`durability_level=none`, and a test of the default expects that refusal.

### D44. A Couchbase string scans into a byte slice as base64. Decided.

Ken decided this on 2026-09-27, and it amends D39. A JSON document has no
type for bytes, so bytes are stored as a base64 string, which is how json/v2
encodes a `[]byte` argument (D40). Nothing in a response marks such a string
([COUCHBASE.md](COUCHBASE.md)).

When the value of a column is a JSON string and the destination is a
`*[]byte`, a `*sql.RawBytes` or a `*sql.Null[[]byte]`, the driver decodes the
string with the standard base64 encoding, with padding, which is the one
that json/v2 writes. If the string is not valid base64, the driver copies
the bytes of the string as they are, and returns no error. So a `[]byte`
makes a round trip, and text that is not base64 still scans.

Text that happens to be valid base64, such as `abcd`, is decoded too. That
is the cost of the rule, and a caller that wants the text scans into a
`*string`. A value that is not a string, such as an object or an array,
still scans into a `*[]byte` as its JSON text, as D39 says.

### D45. A Couchbase transaction keeps the context of BeginTx. Decided.

Ken decided this on 2026-09-27. It is the one exception to hard rule 4,
which says that the library never stores a context. `driver.Tx.Commit` and
`driver.Tx.Rollback` take no context, and `COMMIT WORK` and `ROLLBACK WORK`
are requests that need one. `database/sql` defines the context of `BeginTx`
as the lifetime of the transaction: it rolls the transaction back when that
context ends.

So the transaction keeps that context, and nothing else keeps one:

- `Commit` sends `COMMIT WORK` with it.
- `Rollback` sends `ROLLBACK WORK` with it while it is live. When it has
  ended, which is when `database/sql` rolls back by itself, `Rollback`
  forgets the `txid` and sends nothing.

That is safe because of what the server does with a transaction that nobody
ends. Measured on 8.0.3 on 2026-09-27: a transaction that was left open
applied none of its writes, did not block a write to the same key from
outside it, and ended at its `txtimeout`, after which its `txid` was refused
with code 17010. The timeout is 15 seconds for a request that sets none, by
the documentation of Couchbase, and a setting of the node can lower it
([COUCHBASE.md](COUCHBASE.md)).

`pgx`, `gosnowflake` and `go-mssqldb` keep the context of `BeginTx` in the
same way, which was read in their source on 2026-09-27.

### D46. The Couchbase DSN has a key for the timeout of a transaction. Decided.

Ken decided this on 2026-09-27, and it amends D38. The DSN takes the key
`txtimeout`, a duration such as `30m`, and `WithTransactionTimeout` sets it
for one transaction, by the order of D40. The driver sends it with
`BEGIN WORK`. If the DSN has no such key, the driver sends none, and the
server ends a transaction 15 seconds after it began.

Fifteen seconds is too short for a person who types statements into an open
transaction in `usql`. The server takes any length: 2 minutes, 20 minutes
and 1 hour were accepted on 8.0.3 on 2026-09-27, and the documentation names
no maximum. The interactive shell of Couchbase, `cbq`, uses 2 minutes. So
`usql` sets a long default, such as 30 minutes, when the URL has none, and
that change is part of the move of step 16 of [DRIVER.md](DRIVER.md). The
driver keeps the default of the server for every other caller, because an
abandoned transaction holds its state on the server until its timeout.

### D47. The SurrealDB driver is named surrealdb. Decided.

Ken accepted it on 2026-09-27, with D48. It follows D26 and D30, and decides
items 1 and 2 of step 9 of [DRIVER.md](DRIVER.md) for SurrealDB. The package
is `github.com/xo/dbimp/surrealdb`, and it registers the one name
`surrealdb` with `database/sql`, which is the name of the database. The
scheme of its URL is `surrealdb` (D35), and the `Driver` of the scheme in
`dburl` and the dialect in `dbmeta` take the same name. `dburl` keeps any
alias, such as `surreal`.

### D48. The SurrealDB URL names the namespace and the database in its path. Decided.

Ken decided this on 2026-09-27. It decides the form of item 3 of step 9 for
SurrealDB. The URL is
`surrealdb://user:pass@host:port/<namespace>/<database>`, such as
`surrealdb://root:pw@127.0.0.1:8000/dbmeta/dbmeta`. Every request of
SurrealDB names a namespace and a database, so the path must hold exactly
two segments, and a URL with fewer or more is refused. A segment is decoded
by the rules of `net/url`, so a name with a `/` is written `%2F`.

The default port is 8000. The key `tls=true` makes the driver speak HTTPS on
the same port. Whether SurrealDB serves TLS on the port that it binds is not
measured, and step 6 measures it if `dbrun` can start it with TLS.
The other query keys wait for the measurements of step 6, and each one is a
decision of its own.

### D49. The SurrealDB driver speaks CBOR, written here, and JSON as an option. Decided.

Ken decided this on 2026-09-27, and approved CBOR for SurrealDB by D13. It
decides item 12 of step 9 for SurrealDB.

The driver sends each request and reads each response in CBOR, with
`Content-Type` and `Accept` set to `application/cbor`. JSON writes a
decimal, a datetime, a duration, a UUID and a record id as strings, NONE as
null, and bytes as an array of numbers, and 2.7 writes a range as the dump
of a Rust enum (measured on 2.7.0 and 3.3.0 on 2026-09-27). CBOR keeps each
of them with a tag. Both SDKs of SurrealDB speak CBOR.

The encoder and the decoder of CBOR are written in the root package, with
the standard library only, so no dependency is added (D13). The decoder
reads one item at a time, as `jsontext` does, so a result is never held in
memory (D25). A later driver that speaks CBOR uses the same code.

The DSN key `encoding=json` makes the driver speak JSON, so that a person
can read the requests and the responses while they debug. In JSON, a value
arrives as the JSON type that the server wrote, and the driver does not
guess the type of a string. The default is `encoding=cbor`.

### D50. The SurrealDB driver sends each statement to POST /rpc. Decided.

Ken accepted this on 2026-09-27. It decides items 6 and 8 of step 9 for SurrealDB. The driver sends each
statement as the RPC method `query`, with the text and the arguments as
`params`, to `POST /rpc`. `POST /sql` binds a parameter of its query string
as a string, so `$x` of `?x=5` is `"5"` (measured on 3.3.0). `/rpc` keeps the
type of each argument (measured on 2.7.0 and 3.3.0).

Each request sends `Surreal-NS` and `Surreal-DB`, from the path of the URL
(D48).

SurrealQL has named parameters only, and `$1` is a syntax error (measured on
both releases). So `sql.Named("x", v)` binds `$x`, and a positional argument
is an error that says to use `sql.Named`. The driver never rewrites the text
of a statement. Ken decided the treatment of a positional argument on
2026-09-27.

The server sends the whole response at once. 3.3.0 sent a `Content-Length`
and 2.7.0 sent chunks, and neither pages. 100000 records arrived in one body
of 3.4 MB (measured). The driver still reads the body one item at a time as
`Rows.Next` asks (D21 and D25).

### D51. The SurrealDB DSN has the key auth for the level of the user. Decided.

Ken decided this on 2026-09-27, and it adds a key to D48. The key `auth` is
`root`, `namespace` or `database`, and the default is `root`. It says where
the user of the URL is defined. For `database`, the driver sends the headers
`Surreal-Auth-NS` and `Surreal-Auth-DB` with the names of the path, and for
`namespace` it sends `Surreal-Auth-NS`.

A database user is refused with HTTP 401 without those headers, and root is
refused with HTTP 401 with them (measured on 2.7.0 and 3.3.0). So the driver
cannot use one form for both, and it never tries a second form after a
failure. `dbrun` writes `?auth=database` into the URL of its ordinary user.

### D52. Each SurrealDB statement is a result set, and its rows follow D18. Decided.

Ken accepted this on 2026-09-27. It decides items 5 and 8 of step 9 for SurrealDB. The response holds one
result for each statement of the request, in order. Each one is a result
set, which `Rows.NextResultSet` moves to (`driver.RowsNextResultSet`).

A result becomes rows by D18. SurrealDB sends no column metadata, so rule 1
never applies:

- An array of objects is one row for each object, and the columns are the
  keys of the first object, by rule 2. A key that a later object lacks is
  nil. A key that only a later object has is `ErrExtraColumn`, so a
  `SELECT *` over records with different fields fails at the first record
  that has a new field. Naming the fields in the statement avoids it.
- An array of other values, or of values of mixed kinds, is one column with
  the name `""`, and one row for each value, by rule 3.
- A result that is not an array, such as the result of `RETURN 1` or of
  `SELECT ... FROM ONLY`, is one row. An object gives its keys as the
  columns, and any other value gives one column with the name `""`.

The server sorts the keys of every object, so `SELECT b, a` gives the
columns `a` and `b` (measured on 2.7.0 and 3.3.0). The order of the
statement never reaches the client, so the driver keeps the order that the
server sends, as the Couchbase driver does on 7.2.

NONE and NULL are both nil for `database/sql` (item 5). CBOR tells them
apart (tag 6), but a caller of `database/sql` has one nil.

### D53. The Go types of SurrealDB. Decided.

Ken accepted this on 2026-09-27. It decides item 4 of step 9 for SurrealDB. In CBOR, the driver decodes
each value by its tag:

- An integer is an `int64`, a float is a `float64`, and a decimal (tag 10) is
  an `*apd.Decimal` (D33).
- A string is a `string`, a boolean is a `bool`, and bytes are `[]byte`.
- A datetime (tags 0 and 12) is a `time.Time` in UTC, with its nanoseconds.
- A duration (tags 13 and 14) is a `time.Duration`. A duration longer than
  a `time.Duration` holds, about 292 years, is an error.
- A UUID (tags 9 and 37) is a `uuid.UUID` (D25).
- A record id (tag 8) is a `surrealdb.RecordID`, with the table and the key.
  Its `String` method writes it as SurrealQL does, such as `person:tobie`.
  Ken decided this on 2026-09-27.
- An array and a set (tag 56) are `[]any`, and an object is
  `map[string]any`.
- A geometry (tags 88 to 94) is the `map[string]any` of its GeoJSON, as the
  server writes it in JSON.
- A table (tag 7), a range (tags 49 to 51), a file (tag 55) and a future (tag
  15) are strings, in the form that SurrealQL writes them.
- NONE (tag 6) and NULL are nil (D52).

An argument is encoded the other way, so a `time.Time`, a `uuid.UUID`, a
`RecordID` and an `*apd.Decimal` keep their types on the server.

### D54. The SurrealDB driver has no transactions. Decided.

Ken accepted this on 2026-09-27. It decides item 7 of step 9 for SurrealDB. `BeginTx` returns
`ErrNotSupported`. The RPC methods `begin` and `commit` do not exist over
HTTP, on either release, and every request is a transaction of its own
(measured). The Go SDK refuses an interactive transaction over HTTP too.
`BEGIN`, `COMMIT` and `CANCEL` still work inside the text of one statement,
which is one request (measured), and D20 allows that, because the server
runs it.

### D55. The errors and the results of a SurrealDB write. Decided.

Ken accepted this on 2026-09-27. It decides how the SurrealDB driver reports an error, with item 8 of step
9.

- A response with a status other than 200 is an error from `QueryContext`
  or `ExecContext`. A parse error anywhere in the text is HTTP 400, and no
  statement runs. A wrong password is HTTP 401 (measured).
- An error of the RPC call, such as a parse error in `/rpc`, is HTTP 200 with
  an `error` object, and it is an error from `QueryContext`.
- A statement that fails is HTTP 200 with `"status": "ERR"` for that
  statement, and the other statements still run (measured). The error
  reaches the caller when the rows reach that result set. `ExecContext`
  reads every result, and returns the first error.
- `RowsAffected` and `LastInsertId` return `ErrNotSupported`. The server
  sends no count, and a `DELETE` returns an empty array.

Ken decided on 2026-09-27 that the errors take the form of the errors of the
Couchbase driver, so that a caller such as `usql` reads both drivers in the
same way. A failed statement, a failed RPC call, and a request that the
server refuses with a body of JSON, such as HTTP 400, are each a
`*ResponseError`. It holds the HTTP status, the status of the statement,
such as `ERR`, and each `Error`, which `errors.As` finds. An `Error` is a
value with `Code`, `Kind` and `Msg`. `Kind` is the kind that 3.x names,
which Couchbase has no form for. A response that is neither JSON nor CBOR,
such as the plain text of HTTP 401, is a `*dbimp.StatusError`, as in the
Couchbase driver.

### D56. A SurrealDB query stops when its context ends. Decided.

Ken accepted this on 2026-09-27. It decides items 9 and 10 of step 9 for SurrealDB. The server stops a
query when the client disconnects. `SLEEP 3s; CREATE cancel:x` was left after
1 second, and `cancel:x` did not exist 4 seconds later, on 2.7.0 and 3.3.0
(measured). So the driver closes the body when the context ends, as D36
says, and sends nothing more.

The driver follows no redirect, and sends the credentials only to the host of
the URL. The server sent no redirect in any measurement.

No other product speaks this interface, so the driver has no flavors (item
11).

### D57. The SurrealDB driver exports Version. Decided.

Ken decided this on 2026-09-27, for step 16 of [DRIVER.md](DRIVER.md). No
statement of SurrealQL returns the version, and only the RPC method
`version` and `GET /version` do, for both principals (measured on 2.7.0 and
3.3.0). The `Version` function of a driver of `usql` gets only the methods
of a query. So the driver exports `Version(ctx, dc)`, which calls the RPC
method `version` on a connection of the driver. A caller hands it the
connection inside `sql.Conn.Raw`, and `usql` does that from its `Version`
function. The driver never rewrites the text of a statement to serve it.

## Open questions

Do not decide these yourself. Ask Ken.

### Q1. Which targets come after Couchbase?

Couchbase is first (D23). Ken will arrange the other targets after the first
driver is complete. The review in [TARGETS.md](TARGETS.md) names a start:
CrateDB, QuestDB, rqlite, libSQL, InfluxDB 3 and TDengine, by D17.

On 2026-09-27, Ken named SurrealDB as the second target. W8 is that work.
The targets after SurrealDB are still open.

### Q2. Which licence? Answered by D22.

### Q3. How does a result that is not a table become rows? Answered by D18.

### Q4. Does the Couchbase driver here replace xo/n1ql? Answered by D23.

### Q5. Is a target that already has a Go driver in usql in scope? Answered by D24.

### Q6. What Go type holds a decimal? Answered by D33.

### Q7. What does a driver do when the server binds no arguments? Answered by D34.

### Q8. How does rows.Close release a cursor on the server? Answered by D36.

### Q9. Is "tier" the right word? Answered by D32.

### Q10. Does the registered name follow the package name? Answered by D30.

### Q11. Which short aliases does the Couchbase scheme keep? Answered by D35.
