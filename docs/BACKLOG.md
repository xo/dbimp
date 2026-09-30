# dbimp backlog

This file records planned work for `dbimp`. Each item names the files,
decisions and repositories it touches, so that the work can start without
rediscovering the context.

## How to refer to an item

Every item has an identifier of the form `W` followed by a number. Three rules
govern it:

1. Identifiers are append only. A new item takes the next unused number.
2. An identifier is never reused. When W1 is finished, no later item becomes
   W1.
3. An identifier is never renumbered. Removing W2 does not turn W3 into W2.

An item that is finished or abandoned keeps its heading and gains a status in
that heading. It does not disappear. A heading with no status is open. The
statuses in use are `Done`, `Dropped` and `Superseded by Wn`.

Decisions are a separate series, `D1`, `D2` and so on, in
[decisions/](decisions/README.md). Do not mix the two series in one commit message.

## Where the items came from

W1 to W3 came from setting up the repository on 2026-09-27, from the
conventions that `dbmeta` described for the `xo` repositories. W4 came from
the review of the targets on the same day, from what the `cql` session
learned when it rewrote `xo/cql`. W5 came from D23, and from what the `n1ql`
session measured on the Couchbase query service. W6 came from the advice of
the `dbmeta` session on `docs/DRIVER.md`. W7 came from Ken on 2026-09-27,
who asked that every driver test CRUD, the features of its database and
every native type, after it asks two models what they are. W8 came from Ken
on 2026-09-27, who named SurrealDB as the second target, and W9 from Ken the
same day, who named Neo4j as the third. W10 came from the `dbmeta` session on
2026-09-27, which found two faults of the SurrealDB driver through `usql`. W11
came from Ken on 2026-09-27, who set the order of the drivers after Neo4j
in D73. W12 came from that order, and D88 dropped it. W13 came from Ken on
2026-09-29, who asked for the ArangoDB driver, W14 from D94 and D110, and W15
from D109. W16 came from Ken on 2026-09-29, who named Databend as the next
target at step 1, W17 from D119, and W18 from Ken on 2026-09-29, who
named TDengine as the next target at step 1. W19 came from Ken on
2026-09-30, who named Apache Pinot as the next target at step 1. W20 came
from Ken on 2026-09-30, who asked for one table of every type of every
driver after D135, and W21 from D138. W22 came from Ken on 2026-09-30,
who named rqlite as the next target at step 1.

## W1. Set up the repository in the xo layout. Done.

Write the three root documents, `docs/PLAN.md` and this file, the lint
configuration, the CI workflow, the tests for the documents and the skills,
and the copies of the two agent skills. See D3, D10, D11 and D12.

Ken reviewed it and it went into the first commit on 2026-09-27.

## W2. Add the first driver. Done.

The first driver is Couchbase, and it replaces `xo/n1ql` (D23). W5 is that
work. Do W4 and W6 first. [DRIVER.md](DRIVER.md) holds every step to add a
driver, in order, and this item does not repeat them.

W5 wrote the Couchbase driver, and `v0.1.0` released it on 2026-09-27.

## W3. Run the integration tests in CI. Done.

The jobs `releases` and `integration` went into `.github/workflows/test.yml`
on 2026-09-27, staged for Ken to review, with the pin `ec91128` and the
owner `dbimp-ci`. The products come from the folders of `testdata/`, because
a driver has the name of its product (D30). Ken committed them on
2026-09-27, and the first run passed on couchbase-7.2.9 and couchbase-8.0.3
as both principals:
https://github.com/xo/dbimp/actions/runs/36309539254.

`dbmeta` committed dbmeta D101 and dbmeta D102 on 2026-09-27, as `eb35c7c`.
The pin moved to that commit, staged for Ken to review. From dbmeta D102,
`dbrun list --json` and `dbrun dsn --json` have a field `principals`, with
the administrator first and then the ordinary user, each with its own `url`. The workflow reads the
`url` of the principal with the role `user`, and no longer builds that DSN
itself. A product that has no such principal gets an empty DSN, so its tests
for the ordinary user skip. From dbmeta D101, the `dsn` field of Couchbase is
the same `couchbase://` URL as the `url` field.

When the first driver exists, add the jobs that `cql/.github/workflows/test.yml`
has: a job that reads the releases from `dbrun list --json --names`, and a
matrix job that starts each release with `dbrun` and runs `go test -run
Integration` with the DSN set. Pin the `dbmeta` commit in the workflow, as in
`cql` D20. The pin is `ec91128` or later. `2b93710` holds the ordinary
Couchbase user (dbmeta D96), and `ec91128` gives each server an owner
(dbmeta D98). The workflow sets `DBMETA_OWNER` to name itself, such as
`dbimp-ci`. Do not write the list of releases in the workflow.

## W4. Design the root package before the first driver. Done.

Write `docs/DESIGN.md`, and add it to the tables in `AGENTS.md` and
`README.md`. It holds the design of the code that every driver shares in the
root package (D4). It follows D33 for decimals, D34 for placeholders and
D36 for reading and closing a result. Cover each of these points, which the
`cql` session reported on 2026-09-27:

1. Implement `driver.RowsColumnScanner` from Go 1.27, so that
   `database/sql` hands the destination of the caller to the driver.
2. Keep one canonical Go type for each wire type. Use it for a scan into
   `*any`, for `Next`, and for `ColumnTypeScanType`, and pin each row of that
   table with a test.
3. Keep the raw JSON of each column and decode it when it is scanned. Read
   the keys of an object in order with `jsontext.Decoder.ReadToken` (D18 and
   D25), and convert a number from the raw text of its token (D19).
4. Return `ColumnTypeDatabaseTypeName` in upper case.
5. Keep a NULL apart from an empty value. A NULL scanned into a `*string` is
   the error that `database/sql` gives for it. A NULL scanned into a slice or
   a map is nil.
6. The `Connector` owns the `http.Client` and its transport, and implements
   `io.Closer`. Never change `http.DefaultTransport` or another global.
   `go_n1ql` turned off TLS verification for the whole process.
7. Once a driver implements `driver.NamedValueChecker`, `database/sql` stops
   calling `driver.Valuer`. Return `driver.ErrSkip` for a `Valuer`, or
   `sql.Null[T]` breaks. Use the standard `uuid.UUID` from Go 1.27 (cql D25).
8. Take options from the DSN, then the context, then an argument, as cql D23
   does. Many targets bind by name, so support `sql.Named`.
9. Return the error of the server from `QueryContext`, before any rows.
   Read the status and the body of every response, because ClickHouse and
   Trino can report an error after a first part that looked good. Never retry
   a POST.
10. The DSN is a standard URL, parsed with `net/url`, and no form of DSN
    from an earlier driver is kept (D27). Refuse an unknown key and a
    repeated key. Write a round trip test and a fuzz test
    for the DSN on the first day. They found real faults in `cql`.
11. Fake the server with `httptest.Server` and responses recorded in
    `testdata/`. Each integration test makes its own namespace.
12. Give the shared test helpers a recording mode that writes the files and
    the manifest of step 6 of `docs/DRIVER.md` from a real server.
13. Build the shared HTTP layer to the rules in step 12 of
    `docs/DRIVER.md`: no `http.Client.Timeout`, a proxy from the
    environment, gzip left to the transport, no retry of a request that can
    have reached the server, a body read only as `Rows.Next` asks, and no
    credential in an error. Gemini and DeepSeek proposed these rules in
    their review of `docs/DRIVER.md`.

`cql/docs/DESIGN.md` is the nearest example.

[DESIGN.md](DESIGN.md) holds the design, and the last section maps each
point above to the code. The code is in the root package and in `dbimptest`
(D37). Ken committed it on 2026-09-27 as `f1dfe6d`.

## W5. Write the Couchbase driver. Done.

Write the first driver, for the Couchbase query service and SQL++, to D23.
It replaces `xo/n1ql`. Do W4 and W6 first, then follow
[DRIVER.md](DRIVER.md). [COUCHBASE.md](COUCHBASE.md) holds every measured
fact about the query service and every fault of `go_n1ql` that the driver
must not repeat.

Ken decided the questions of step 9 in D38 to D42. The driver has
transactions from its first release (D41), so step 6 also records
`COMMIT WORK`, a `txid` that expired, and a `txid` sent after the end.

The survey of step 5a is done, on 2026-09-27, in
`testdata/couchbase/features.json`, and each of its entries is settled
against 8.0.3 and recorded on the three releases. `COMMIT WORK` works only
with `durability_level: "none"` on a node alone, so the driver takes the key
`durability_level` (D43).

The package is `github.com/xo/dbimp/couchbase` (D26). It registers the one
name `couchbase` (D30), and it takes a `couchbase://` URL (D35). Its
integration tests read `COUCHBASE_DSN` (D9).

The `dbmeta` session added the ordinary user on 2026-09-27 (D31, and
dbmeta D96). Two questions about the output of `dbrun dsn --json` remain,
and each is a change to `dbrun`, so it goes to Ken through the `dbmeta`
session once step 9 settles the URL. The first asks how `dbrun` prints the
`couchbase://` URL: as a second field, or in a form for each consumer. The
second asks whether it also prints a DSN for the ordinary user. `dbrun` now
prints the `couchbase://` URL in the field `url`, so the first question is
answered. Ken answered the second on 2026-09-27: `dbrun` prints every
principal (dbmeta D102).

Steps 8 to 15 went in on 2026-09-27, staged for Ken to review. The package
`couchbase` holds the driver, with its DSN, contract, replay and table tests.
`couchbase/integration_test.go` names a test for each entry of the survey,
and every one passed on 7.2.9, 7.6.12 and 8.0.3 as both principals on
2026-09-27. The integration tests found four faults of the first version,
which each have a test now:

1. The rows of a null signature, such as those of `CREATE INDEX`, failed.
2. `BEGIN WORK` found no `txid`, because its signature is `"json"`.
3. `dbimptest.RoundTrip` sent its teardown with the context of the test,
   which ends before the cleanup runs, so the teardown never ran.
4. A large float that is a whole number arrives as its digits, and is a
   `*apd.Decimal`, which D39 said never arrives.

Ken committed steps 8 to 17 on 2026-09-27 as `373a7d6`. The workflow passed
on it: https://github.com/xo/dbimp/actions/runs/36309539254. Ken approved
the tag, and the release `v0.1.0` is public:
https://github.com/xo/dbimp/releases/tag/v0.1.0.

The requests of step 16 went to the sessions of `dburl`, `usql` and
`dbmeta` on 2026-09-27, each naming `v0.1.0`:

1. `dburl`: rename the scheme to `couchbase` with `n1ql` as an alias, set
   `GoPackage` to `github.com/xo/dbimp/couchbase` and `RequiresCGO` to
   false, and make the generator write the URL of D35. Whether the
   `Dialect` becomes `couchbase` now is a question for Ken.
2. `usql`: import the driver, drop the `strconv.Unquote` in `Version`,
   change the prefix of its errors, and add a long `txtimeout` default
   (D46).
3. `dbmeta`: measure its model on the driver, and ask Ken whether
   `dbrun dsn --json` prints a DSN for the ordinary user.

The `dburl` session reported on 2026-09-27 that its change is staged for
Ken. The `Driver` of the scheme is `couchbase`, with the aliases `n1ql` and
`n1`, and the `Dialect` is `couchbase`. Its generator writes no default
port, because the driver picks 8093 or 18093 from `tls`. It checked the
output of the generator against `couchbase.ParseDSN` at `v0.1.0`. Ken
then ruled that the generator writes the default port, as rule 7 of `dburl`
says: 8093, or 18093 when `tls` is true by `strconv.ParseBool`, which is how
the driver reads it. `dburl` released it as `v0.33.0` on 2026-09-27, as
dburl D25. dburl D34 later removed that port, because the driver picks it.

The `usql` session reported on 2026-09-27 that its change is staged for
Ken. `drivers/couchbase` imports the driver at `v0.1.0`, reads the version
with no `strconv.Unquote`, reports the code and the message of an `Error`,
and adds `txtimeout=30m` to a URL that has none. It does not set
`durability_level`. It can commit only after `dburl` tags the rename of the
scheme, because `usql` pins `dburl` `v0.32.0`, where the scheme is still
`n1ql`. `dburl` `v0.33.0` has the rename, and the `usql` session was told.

The `usql` session reported on 2026-09-27 that its change is staged for
Ken, on `dburl` `v0.33.0`, and that its tests pass. `couchbase://` and
`n1ql://` both reach the driver through `usql`.

The `dbmeta` session reported on 2026-09-27 that its change is staged for
Ken. It uses the driver at `v0.1.0`, and renames its dialect from `n1ql` to
`couchbase` (dbmeta D101). It measured its model on 8.0.3 through the
driver, as both principals.

Each of the three sessions reported that its change is staged, so this item
is done. `usql` committed its change on 2026-09-27 as `8407785`, on `dbimp`
`v0.1.0` and `dburl` `v0.33.0`. Ken decided that `usql` does not set
`durability_level`. Its README explains `durability_level=none` for a server
of one node instead.

At the move of step 16, three repositories change. The `n1ql` session listed
them in its W13:

1. `dburl`: the scheme gets the `Driver` name `couchbase`, with `n1ql` as an
   alias, and a new `GoPackage` and `DriverURL`. The `Dialect` becomes
   `couchbase`, so `usql`, `dbtpl` and `dbmeta` change any match on `n1ql` in
   the same release (D30). The `dburl` session writes its generator from the
   parser of this driver, at the version that `usql` pins.
2. `usql`: `drivers/couchbase` imports this driver, changes the prefix of
   its errors, and drops the `strconv.Unquote` in its `Version` function. It
   adds `txtimeout=30m`, or another long default, to a URL that has no
   `txtimeout`, so that a person who types into a transaction has the time
   (D46).
3. `dbmeta`: it measures its Couchbase model on this driver (dbmeta D94).

## W6. Write the tests that hold DRIVER.md. Done.

Write the gates that hold the steps of [DRIVER.md](DRIVER.md), before the
first driver is done. Each one fails with a message that names the step that
was skipped. Where a table can be made from the code, generate it and test
only that the file is current. The `dbmeta` session proposed the list, from
what its own gate tests caught.

The gates are in `gates_test.go`, and "The tests that tell you what you
forgot" in [DRIVER.md](DRIVER.md) lists them. When the gates went in, the
repository had no driver, so each gate passed with nothing to read. `gates_self_test.go` makes
sure that each gate passes a complete driver and fails an incomplete one.
The code and the design of W4 went in with the gates, on 2026-09-27, staged
for Ken to review. Ken committed them the same day as `f1dfe6d`.

## W7. Add the survey of features and the round trip of types. Done.

Steps 5a and 14a of [DRIVER.md](DRIVER.md) needed three things, which went
in on 2026-09-27, staged for Ken to review. Gemini and DeepSeek proposed the
parts, and Ken asked for them.

1. `dbimptest.Features` is the form of `testdata/<driver>/features.json`.
2. `dbimptest.RoundTrip` runs the round trip of one type. It runs every step
   itself and compares each value and its Go type, so a test cannot skip a
   step or compare nothing. Its tests hold a store in memory with three
   faults, and each one fails.
3. `TestEveryDriverHasItsFeatures` in `gates_test.go` holds the survey. The
   complete driver and the incomplete driver of `gates_self_test.go` hold
   the gate itself.

[DESIGN.md](DESIGN.md) describes the first two, and the table in
[DRIVER.md](DRIVER.md) lists the gate. Ken committed them on 2026-09-27 as
`c9795a5`.

Two ideas of the models were not taken. A gate on the history of git, which
makes sure that the survey came before the tests, breaks when a branch is
rebased or squashed, and Ken reviews the order of the work anyway. A linter
that looks for a comparison after each `Scan` is a guess, and the helper
does the comparing itself.

## W8. Write the SurrealDB driver. Done.

Ken named SurrealDB as the second target on 2026-09-27 (Q1 in
[PLAN.md](PLAN.md)). Follow [DRIVER.md](DRIVER.md) from step 2.
[SURREALDB.md](SURREALDB.md) holds every fact about its HTTP interface.

`dbrun` starts 2.7.0 and 3.3.0 in the Tested tier, and 3.1.6 and 3.2.4 in
the Nightly tier, from dbmeta D103 at commit `78f1e44`, with the ordinary
user `dbmeta_user`. When the work began, `dburl` had no scheme for
SurrealDB, and `usql` had no driver for it.

Steps 1 to 15 and 17 went in on 2026-09-27, staged for Ken to review. Ken
decided D47 to D57. The package `surrealdb` holds the driver, with its DSN,
contract, replay, codec and table tests. `surrealdb/integration_test.go`
names a test for each entry of the survey, and every one passed on 2.7.0,
3.1.6, 3.2.4 and 3.3.0 as both principals on 2026-09-27. The root package
gained the CBOR codec of D49, and `dbimptest` gained a binary body for an
exchange, a content type for the contract, named arguments for the round
trip, and a body of text or CBOR and headers for each principal in the
recording command.

The fuzz test of the DSN found a fault that `v0.1.0` has for Couchbase too:
`net/url` reads the host `::` as `:`. `dbimp.ParseURL` now refuses it.

Step 16 wrote these requests. Step 20 sends them after Ken approves the
release that holds the driver, and each names that tag:

1. `dburl`: add a scheme whose `Driver` and `Dialect` are `surrealdb` (D47),
   with the `GoPackage` `github.com/xo/dbimp/surrealdb` and `RequiresCGO`
   false. Its generator writes
   `surrealdb://user:pass@host:port/<namespace>/<database>`, and passes the
   keys `tls`, `auth` and `encoding` through (D48, D49 and D51). It adds no
   port, because the driver uses 8000 (dburl D34). Aliases such as `surreal` are for
   `dburl` and Ken.
2. `usql`: add `drivers/surrealdb`, which imports the driver. Its `Version`
   asserts `*sql.DB`, takes a `Conn`, and calls `surrealdb.Version` inside
   `Conn.Raw` (D57). Each statement of a query is a result set (D52), so it
   shows each one.
3. `dbmeta`: nothing to change now. Its entry already holds what the tests
   need. A model for SurrealDB is a question for Ken.

Ken committed steps 1 to 17 on 2026-09-27 as `bd0f165`, and the workflow
passed on it, with 2.7.0 and 3.3.0 as both principals:
https://github.com/xo/dbimp/actions/runs/36316196918. That is the gate of
step 18. At Ken's request, the session tagged `v0.2.0` on `82c1902` the same day. It holds the
driver, and `dburl` released the scheme `surrealdb` in `v0.34.0`, checked
against `v0.2.0` (dburl D26). The `usql` session added `drivers/surrealdb` on
`v0.2.0` before step 20, in its commit `54c12a4` (W10). The release notes
and the GitHub release of step 19, and the requests of step 20, waited for
Ken. On 2026-09-29, Ken closed this item as overtaken: `v0.4.0` and
`v0.5.0` are the GitHub releases that hold the driver, and the step 20 of
`v0.5.0` in W13 reached `dburl`, `dbmeta` and `usql`. The tag `v0.2.0` stays,
with no GitHub release.

## W9. Write the Neo4j driver. Done.

Ken named Neo4j as the third target on 2026-09-27 (Q1 in
[PLAN.md](PLAN.md)). Cypher meets S by D58, so it is P1. `dbrun` runs the
Enterprise Edition under the evaluation agreement (D59). Follow
[DRIVER.md](DRIVER.md).

When the work began, `dburl` had no scheme for Neo4j, and `usql` had no
driver for it. Ken chose the aliases `nj`, `neo` and `n4j`. The `dbmeta`
session was asked for the entry of step 4 on 2026-09-27, with the facts
below, which this session read and did not measure:

- The image is `docker.io/library/neo4j`, and each release has a tag with
  `-enterprise`. On 2026-09-27 three lines were rebuilt that month: 4.4
  (4.4.48), 5.26, the release of long term support (5.26.31), and the
  monthly releases, the newest 2026.09.0. The support page of Neo4j gives
  5.26 hotfixes until 2028-06-06, gives each monthly release hotfixes until
  the next one, and ended the support of 4.4 on 2025-11-30.
- 4.4 cannot run under the evaluation agreement (D59), so the floor is 5.26
  and the ceiling is the newest monthly release.
- The HTTP interface is on port 7474. Bolt, on port 7687, is not HTTP.
- In the Community Edition every user is an administrator. The Enterprise
  Edition has the roles `PUBLIC`, which every user holds, `reader`, `editor`,
  `publisher`, `architect` and `admin` (the Operations Manual of Neo4j, read
  on 2026-09-27). `publisher` reads, writes, and makes new labels, property
  keys and types of relationship, and cannot make an index or a constraint,
  so it is the proposal for the ordinary user.

Steps 3 and 5a went in on 2026-09-27, staged for Ken to review:
[NEO4J.md](NEO4J.md) is the draft of step 3, and
`testdata/neo4j/features.json` is the survey, with every entry not measured.
The Query API of 5.19 and later, `POST /db/<database>/query/v2`, is the
interface to measure. It names the columns before the rows, and it has
explicit transactions from 5.26.

Steps 4 to 9 went in on 2026-09-27, staged for Ken to review:

- Step 4: dbmeta commit `1335428` on `main` holds the entry, from
  dbmeta D106, with `neo4j-5.26.31` and `neo4j-2026.09.0` in the Tested tier.
- Step 6: `testdata/neo4j/` holds the recordings of every request of
  `requests.json` on both releases, as both principals, and
  `manifest.json`. Every entry of `features.json` is settled against
  2026.09.0: APOC and decimal are `no`, and every other entry is `yes`.
- Step 7: the leads of Gemini and DeepSeek, and what the servers answered,
  are in the Second opinions of [NEO4J.md](NEO4J.md).
- Step 8: [NEO4J.md](NEO4J.md) was complete, apart from the type table and
  the interface table of step 10.
- Step 9: D60 to D69 in [decisions/](decisions/README.md). Ken chose the path, the
  positional arguments, the three ways to cancel, the temporal types and the
  context of a transaction, and accepted the ten decisions, on 2026-09-27.

Steps 10 to 17 went in on 2026-09-27, staged for Ken to review:

- Step 10: `neo4j/tables_test.go` writes the type table and the interface
  table into [NEO4J.md](NEO4J.md).
- Steps 11 to 13: the package `neo4j/`, with its replay tests of the
  recordings, the contract, the tests of the cancel of D67 against a fake
  server with recorded answers, and the tests of the DSN. The fuzz tests of
  the DSN, of a duration and of a date ran for 60 seconds each and found
  nothing.
- Steps 14 and 14a: `neo4j/integration_test.go` passed on 5.26.31 and
  2026.09.0 as both principals. Every entry of the survey names its test.
- Step 15: the workflow needs no new job, because its matrix comes from the
  folders under `testdata/`. `DBMETA_COMMIT` moved to `19a8a9a`, which names
  the URL of Neo4j (dbmeta D109).
- Step 17: the row of [TARGETS.md](TARGETS.md) names the package.

The work found a fault in the root package: `dbimp.ArrayRows` took a row
with fewer values than columns, and kept the values of the row before in
the gap. It now returns `ErrColumnCount`, as [DESIGN.md](DESIGN.md) says.

Step 16 wrote these requests. Step 20 sends them after Ken approves the
release that holds the driver, and each names that tag:

1. `dburl`: add a scheme whose `Driver` and `Dialect` are `neo4j` (D60),
   with the aliases `nj`, `neo` and `n4j` that Ken chose, the `GoPackage`
   `github.com/xo/dbimp/neo4j` and `RequiresCGO` false. Its generator writes
   `neo4j://user:pass@host:port/<database>`, and passes the keys `tls` and
   `cancel` through (D61 and D67). It adds no port, because the driver uses
   7474, or 7473 with `tls=true` (dburl D34). A path that is empty means the database `neo4j`. The tools
   of Neo4j write `neo4j://host:7687` for Bolt, which this driver does not
   speak.
2. `usql`: add `drivers/neo4j`, which imports the driver. Its `Version` runs
   `CALL dbms.components() YIELD name, versions WHERE name = 'Neo4j Kernel'
   RETURN versions[0]`, which the ordinary user can run (recorded). A
   statement with no `RETURN`, such as `CREATE (:A)`, gives a result set with
   no columns, which `tblfmt` writes as `psql` does from `v0.19.1` (tblfmt
   D31).
3. `dbmeta`: nothing to change now. `dbrun` names the URL of Neo4j from
   dbmeta D109. A model for Neo4j is a question for Ken.

Ken committed steps 1 to 17 on 2026-09-27 as `b475894`, and the workflow
passed on it, on 5.26.31 and 2026.09.0:
https://github.com/xo/dbimp/actions/runs/36330535630. That is the gate of
step 18. The tag `v0.3.0` is on `b475894`, and `v0.4.0` is the first GitHub
release that holds the driver. The release notes of `v0.3.0`, and the
requests of step 20, waited for Ken. On 2026-09-29, Ken closed this item as
overtaken: `v0.4.0` and `v0.5.0` hold the driver, and the step 20 of
`v0.5.0` in W13 reached `dburl`, `dbmeta` and `usql`. The tag `v0.3.0`
stays, with no GitHub release. dburl D28 added
the scheme `neo4j` on 2026-09-28, and dburl `v0.35.0` holds it.

## W10. Fix the faults that usql found in the SurrealDB driver. Done.

The `dbmeta` session ran `usql` at commit `54c12a4`, which pins `v0.2.0` of
the SurrealDB driver, against 3.3.0 on 2026-09-27. It found two faults:

1. A statement that returns no rows gives a result set with no columns, and
   `tblfmt` `v0.16.0` refused it with "result set has no columns". The
   statement still ran. `tblfmt` settled it on its side in its D31, released
   in `v0.19.1`, which `usql` requires. So D52 stands, and the driver does
   not change (Q12 in [PLAN.md](PLAN.md)).
2. A record id inside an array prints as the fields of `RecordID`, such as
   `[{"Table": "book", "ID": "earthsea"}]`. D70 gives it `MarshalText`, and
   `TestRecordIDText` holds it. That change is committed in `b475894`, and Ken
   accepted D70 on 2026-09-29.

The session also reported that a `usql` user who writes `$1` gets a parse
error from the server, and that `\bind` passes positional arguments, which
the driver refuses by D50. `usql` has no way to pass `sql.Named`. D50 does
not change.

On 2026-09-27 every SurrealDB server of `dbrun` belonged to the owner
`dbimp-w8`, so this item measured nothing on a server. The facts come from
the recordings of 3.3.0, from the source of `usql` and of `tblfmt` v0.16.0,
and from `tblfmt` run on a result set with no rows.

A fix reaches `usql` only in a new release of `dbimp`, by steps 18 to 20 of
[DRIVER.md](DRIVER.md). The first fault needs nothing more, and Ken accepted
D70 on 2026-09-29, so this item is done. The tag `v0.3.0` holds the fix of
the second fault, and `usql` has it through `v0.4.0`.

## W11. Write the InfluxDB driver. Done.

InfluxDB was the next target, by D73. Follow [DRIVER.md](DRIVER.md) from step
2. After InfluxDB come ArangoDB, Databend, TDengine, Apache Pinot,
rqlite, libSQL and then Avatica (D73, D74 and D88). Each one gets a work
item of its own when its turn comes.

Step 3 went in on 2026-09-28 for each target that D73 names, staged for Ken
to review: [INFLUXDB.md](INFLUXDB.md), [CRATEDB.md](CRATEDB.md),
[ARANGODB.md](ARANGODB.md), [DATABEND.md](DATABEND.md),
[TDENGINE.md](TDENGINE.md), [PINOT.md](PINOT.md), [RQLITE.md](RQLITE.md) and
[LIBSQL.md](LIBSQL.md). Each is a draft, with every fact not measured, and
each lists its open questions for Ken at its end. D76 names the Databend,
libSQL and CrateDB drivers ahead of their turn.

Ken decided these on 2026-09-28, and they are staged for his review:

- D75: Flux meets S, so Flux does not rule out InfluxDB 2.
- D77: a JSON result that is an array of objects becomes rows by rule 2 of
  D18. D80 amends it.
- D78: one driver, `influxdb`, serves InfluxDB 1, InfluxDB 2, and InfluxDB 3
  and later, as flavors. The dialect `influxdb` is SQL, on InfluxDB 3 and
  later only. The dialect `influxql` is InfluxQL, on every release. The
  driver does not speak Flux. The key `sqlmode` chooses the language when
  the driver connects.
- D79: the tests run 1.13.1, 2.9.1, 3.9.13 and 3.11.5 in the Tested tier, and
  1.11.8, 2.8.0 and 3.10.6 in the Nightly tier.

On 2026-09-28, dburl D29 added the schemes `influxdb` and `influxql` of
D78, as `a2f1a3a`, which dburl `v0.35.0` holds.

On 2026-09-28, dbmeta D112 and dbmeta D114 added the `dbrun` entries for
InfluxDB 3, and for InfluxDB 1 and 2. `dbmeta` committed them the same day,
so step 4 is done.

Steps 5 to 8 went in on 2026-09-28, staged for Ken to review:

- Step 5a: `testdata/influxdb/features.json` asked Gemini, DeepSeek and four
  clients. Every entry has a verdict from a server.
- Step 6: `testdata/influxdb/requests.json` was recorded on all seven
  releases, as the administrator and, on InfluxDB 1 and 2, as the ordinary
  user. For it, `-ordinary` of the recorder can be empty for a release that
  has no ordinary user, and a request can name its `releases`. A response
  whose body the server closed early is kept with `truncated`, and `Replay`
  closes it early too.
- Step 7: the leads of Gemini and DeepSeek were tested on 1.13.1, 2.9.1 and
  3.11.5.
- Step 8: [INFLUXDB.md](INFLUXDB.md) holds every measured fact.

Ken decided the rest of step 9 on the same day: D80 (the columns of SQL
come from `DESCRIBE`), D81 (a series of InfluxQL is a result set with its
name and its tags), D82 (the DSN), and D83 (chunks only on InfluxDB 1, the
gaps in `statement_id`, and the numbers of InfluxQL).

Steps 10 to 17 went in on 2026-09-28, staged for Ken to review:

- Steps 10 to 13: the package `influxdb/`, its type table and interface
  table in [INFLUXDB.md](INFLUXDB.md), the contract, the replay of every
  recording, and the DSN tests. The fuzz test ran for 60 seconds, with 20
  million inputs, and found nothing.
- Ken decided D85 on the same day: the driver takes INSERT of the `influx`
  shell and sends it to `/write`, and sends DELETE to the server. On
  2026-09-29 he accepted D86, the three fields that let RoundTrip serve
  InfluxDB, and added the rule for the column time to D83.
- Steps 14 and 14a: every integration test passed on all seven releases, one
  at a time, as both principals where the release has an ordinary user.
- Step 15: the job `releases` selects a release of the Staged tier by its
  cadence (dbmeta D120), and fails when a product has no release. It pins dbmeta
  `415e830`, which prints the `url` of D82 for each InfluxDB principal. The
  Test step of the workflow passed with that pin on 1.13.1, 2.9.1 and 3.11.5
  on 2026-09-29.
- Step 17: the row in [TARGETS.md](TARGETS.md) names the package.

The requests of step 16, which step 20 sends after the release that holds
the driver:

1. To `dburl`: the schemes `influxdb` and `influxql` of dburl D29 already
   name the `GoPackage` `github.com/xo/dbimp/influxdb`, and their generators
   write the URL of D82. Please make sure that `RequiresCGO` is false for
   both, and that `GenInfluxQL` keeps the keys `describe`, `chunked`, `rp`
   and `tls` of the URL, which the driver reads (D82 and D83).
2. To `dbmeta`: dbmeta has no model for InfluxDB, so there is nothing to
   measure on the package yet. When a model comes, it reads the driver
   `influxdb`, whose dialect `influxdb` is SQL with an `information_schema`,
   and whose dialect `influxql` has the SHOW statements (D78).
3. To `usql`: add `drivers/influxdb/influxdb.go`, which imports
   `github.com/xo/dbimp/influxdb` for the schemes `influxdb` and `influxql`.
   It reads the version with `influxdb.Version` and the dialect with
   `influxdb.Dialect`, each inside `sql.Conn.Raw` (D78). An InfluxQL result
   has a result set for each series, so it needs `NextResultSet` (D81).

Ken committed steps 1 to 17 on 2026-09-29 as `8548135` and `e29bd29`. The
workflow passed on `e29bd29`, on 1.13.1, 2.9.1, 3.9.13 and 3.11.5, which is
the gate of step 18:
https://github.com/xo/dbimp/actions/runs/36469657061. At Ken's request, the
session tagged `v0.4.0` on `e29bd29` and published it the same day, which is the
gate of step 19: https://github.com/xo/dbimp/releases/tag/v0.4.0. The
requests of step 20 waited for Ken. On 2026-09-29, Ken closed this item as
overtaken: the step 20 of `v0.5.0` in W13 reached `dburl`, `dbmeta` and
`usql`, and `usql` checked the InfluxDB driver of `v0.5.0` on a server.

## W12. Write the CrateDB driver. Dropped.

CrateDB was second in the order after Neo4j (D73). Ken decided on
2026-09-29 that dbimp writes no driver for it (D88). `usql` reaches it with
`pgx` on the PostgreSQL wire protocol, which the `dbmeta` session measured
on 6.4.5 that day, and `dbmeta` reads it through a dialect of its own. Ken
asks the `dbmeta` session for the dialects of CrateDB and CockroachDB
himself. Step 3 of the driver is in [CRATEDB.md](CRATEDB.md), which stays as
the record of why. ArangoDB is the next driver.

## W13. Write the ArangoDB driver. Done.

ArangoDB is the next target, by D73 and D88. Follow [DRIVER.md](DRIVER.md).
Step 1 is done by D73, and step 2 by D75 and the entry of dbmeta D112, which
starts 3.12.12. Step 3 is in [ARANGODB.md](ARANGODB.md). Ken asked on
2026-09-29 for the work to begin.

Steps 4 to 17a went in on 2026-09-29:

- Step 5a and step 6: `testdata/arangodb/features.json` and the recordings of
  3.12.12, as both principals. The recorder now writes a captured value into
  a header too, for `x-arango-trx-id`.
- Step 7: the leads of Gemini and DeepSeek were tested. In the second round,
  on 2026-09-29, DeepSeek gave no answer.
- Step 9: Ken decided D89 to D94, D99, D103, D104 and D106 on 2026-09-29.
  D94 is the convention of every HTTP driver: the secret is the password,
  and `auth=basic|bearer`.
- Steps 10 to 14a: the package `arangodb/`, its tables, the contract, the
  replay tests, the DSN tests, and the integration tests, which passed on
  3.12.12 as both principals.
- Step 15: CI runs `arangodb-3.12.12`. dbmeta printed the `url` of D93 for
  it from 29395a1, and the pin moved to 00d9f72, which holds it. The
  integration tests passed against that commit on 2026-09-29, as both
  principals. `actionlint` passes on the workflow.
- Step 17a: `## Compared with Couchbase` is in ARANGODB.md, and D97 gave
  SurrealDB, Neo4j and InfluxDB the same section. Ken reviewed them before
  step 18 (D97).

Ken committed steps 4 to 17a on 2026-09-29 as `109da2c` and `611397c`. The
workflow passed on `611397c`, on every release of every driver, with
`arangodb-3.12.12` for the first time, which is the gate of step 18:
https://github.com/xo/dbimp/actions/runs/36501952076. At Ken's request, the
session tagged `v0.5.0` on `c6167b3`, on which the workflow passed too, and
published it the same day, which is the gate of step 19:
https://github.com/xo/dbimp/releases/tag/v0.5.0.

Step 20 sent the requests below on 2026-09-29, each naming `v0.5.0`:

- `dburl` checked `GenArangoDB` of `v0.36.0` against `arangodb.ParseDSN`,
  which needs no change, and staged dburl D32, where `arangodb` is no longer
  provisional.
- `usql` staged `drivers/arangodb` and the move to `v0.5.0`, and checked
  each change to the drivers of `v0.4.0` on a server. The error positions of
  Neo4j and the two columns named `name` of InfluxQL, which `usql` found in
  `v0.4.0`, are fixed.
- `dbmeta` has no model for ArangoDB, InfluxDB or Neo4j, and uses only the
  Couchbase driver, at `v0.1.0`. It moves its test module to `v0.5.0`, and
  runs the three releases of Couchbase, after Ken decides on the change that
  it has staged. So this item stays open until `dbmeta` reports.

`dbmeta` moved its test module from dbimp `v0.1.0` straight to `v0.6.0`
instead, on 2026-09-29, as dbmeta `03876ce`, and the three releases of
Couchbase passed there. That closed this item.

The release that holds this driver also carries D95, D96, D100, D105 and
D107, which change the drivers of `v0.4.0`, and the fixes of the review of
D97, such as `BeginTx` of SurrealDB. Step 20 tells `usql` of each:

- The tag of Neo4j now ends each statement (D95).
- The first column of an InfluxQL result set is `measurement` (D96).
- A Neo4j rollback now runs after its context ends (D100).
- An early `Close` of Neo4j rows stops a statement that runs on, and in a
  transaction reads the rest of the answer (D105).
- A failure before the first row no longer wraps `dbimp.ErrIncomplete`,
  which changes Couchbase (D107).

The requests of step 16, which step 20 sends after the release that holds
the driver:

1. To `dburl`: the provisional scheme `arangodb` of dburl D32, which dburl
   `v0.36.0` releases, matches D93. Please check `GenArangoDB` against the
   release: the path is the database, the query passes through, and the
   generator adds no port, because the driver uses 8529 (dburl D34).
2. To `dbmeta`: dbmeta has no model for ArangoDB, so nothing to measure yet.
3. To `usql`: add `drivers/arangodb/arangodb.go`, which imports
   `github.com/xo/dbimp/arangodb` for the scheme `arangodb`. It reads the
   version with `RETURN VERSION()`. A document arrives as one column that
   holds a map, and a projection as its columns (D89).

## W14. Add the key auth to the InfluxDB and Neo4j drivers. Done.

D94 gives every HTTP driver the key `auth=basic|bearer`, and D110 names the
drivers that take it. The InfluxDB and Neo4j drivers of `v0.5.0` send basic
authentication only (D82 and D61). Add the key to both, with `basic` as the
default, and `bearer`, which sends the secret as `Authorization: Bearer`,
and sends no user.

- InfluxDB 3 takes it (measured, in "Requests" of
  [INFLUXDB.md](INFLUXDB.md)). Measure it on InfluxDB 2, which took
  `Authorization: Token` in step 6.
- Neo4j offers Bearer in `Www-Authenticate` (recorded). Measure a Bearer
  token on 5.26.31 and 2026.09.0, which need a server that issues one.

Couchbase stays with basic authentication, and SurrealDB keeps its key
`auth` for the level of the user (D110). Record it in the next release.

It went in on 2026-09-29. Ken decided D116: `auth=bearer` sends `Token` to
InfluxDB 2, which refuses `Bearer`, and `Bearer` to every other release and
to Neo4j. The root package holds `dbimp.SetAuth` and `Query.Auth`, which the
ArangoDB, InfluxDB and Neo4j drivers share. `TestIntegrationBearer` passed on
InfluxDB 1.13.1, 2.9.1 and 3.11.5, where InfluxDB 1 refuses a token that is
not a JWT, and on Neo4j 5.26.31 and 2026.09.0, where the server refuses the
scheme. The accepting path of Neo4j is not measured, because dbrun runs no
identity provider.

## W15. Give every driver the same options. Done.

D109 gives every driver options for one statement, through the machinery
of the root package. Couchbase has them now (D40), and moves to that
machinery. For each driver:

1. Move `Option`, `WithOptions` and the order DSN, then context, then
   argument, into `dbimp.Option[T]`, `dbimp.WithOptions` and
   `dbimp.Resolve`, with a test of the order and of the removal of an
   `Option` argument.
2. Give each driver `WithTimeout`, `WithReadonly`, `WithParameter` and
   `WithDatabase`. Map each one to the setting of its server, measure it,
   and return an error that wraps `dbimp.ErrNotSupported` where the server
   has none. These are the leads, which step 6 of each driver measures:

   | Driver | `WithTimeout` | `WithReadonly` | `WithDatabase` |
   | --- | --- | --- | --- |
   | Couchbase | `timeout` | `readonly` | `query_context` |
   | Neo4j | `maxExecutionTime` on 2026.04 and later | `accessMode: READ` | the path of the request |
   | ArangoDB | `options.maxRuntime` | not known | the path of the request |
   | SurrealDB | not known | not known | the header `Surreal-DB` |
   | InfluxDB | not known | not known | `db` |

3. Give each driver an option for each key of its DSN that can change for
   one statement, with the same meaning (D109).
4. Write the options into the section "Compared with Couchbase" of each
   product document, and into the table of interfaces where they change it.

Record it in the next release. It adds to the API of every driver, and
changes the type of `couchbase.Option`, which stays source compatible.

It went in on 2026-09-29. The root package holds `options.go`, with
`MarshalParams` for the keys of `WithParameter`. The gate
`TestEveryDriverTakesTheCommonOptions` holds the five common functions and
the alias `Option` in each driver, and step 12 of [DRIVER.md](DRIVER.md)
holds the rest. The measurements settled the leads:

| Driver | `WithTimeout` | `WithReadonly` | `WithDatabase` | Options of the DSN |
| --- | --- | --- | --- | --- |
| Couchbase | `timeout` | `readonly` | `query_context` | `WithQueryContext`, `WithScanConsistency`, `WithDurability`, `WithTransactionTimeout` |
| Neo4j | `maxExecutionTime`, in whole seconds, on 2026.04 and later | `accessMode: READ` | the path | `WithCancel` |
| ArangoDB | `options.maxRuntime`, in seconds with a fraction | a read-only transaction only | the path | `WithBatch`, `WithCancel` |
| SurrealDB | `dbimp.ErrNotSupported` | `dbimp.ErrNotSupported` | `Surreal-DB` | `WithNamespace` |
| InfluxDB | `dbimp.ErrNotSupported` | `dbimp.ErrNotSupported` | `db` | `WithRetentionPolicy`, `WithChunked`, `WithDescribe` |

Each product document holds the measurements, and D109 holds what the work
settled. The integration tests passed on Neo4j 5.26.31 and 2026.09.0,
ArangoDB 3.12.12, SurrealDB 2.7.0 and 3.3.0, InfluxDB 1.13.1, 2.9.1 and
3.11.5, and Couchbase 8.0.3.

## W16. Write the Databend driver. Done.

Databend is the next target, by D73 and D88. Ken named it at step 1 on
2026-09-29. Follow [DRIVER.md](DRIVER.md). Step 3 is in
[DATABEND.md](DATABEND.md), and D76 decides that this driver replaces the
Go driver in `usql` and `dburl`.

Step 2 went in on 2026-09-29. `dbrun` starts `databend-1.2.881` and
`databend-1.2.948`, both in the Staged tier with the cadence `tested`, so
step 14 runs both. `POST /v1/query` answered `SELECT version()` on both, as
`root` and as `dbmeta_user`, so R, H and S hold.

Steps 5a to 8 went in on 2026-09-29:

- Step 5a: `testdata/databend/features.json`, from Gemini, DeepSeek,
  databend-go and the Python driver of bendsql.
- Step 6: the recordings of 1.2.881 and 1.2.948, as both principals. The
  recorder gained `follow`, which reads each page by `next_uri`, and it
  escapes a kept value inside a JSON body, for `session.internal`.
- Step 7: DeepSeek gave seven leads, and each was tested. Gemini timed out
  three times and gave no answer.
- Step 8: DATABEND.md holds the measured facts. Each entry of
  `features.json` has a verdict.

Step 9 is D117 to D123, which Ken decided on 2026-09-29. D124, which he
decided the same day, amends D120: a decimal argument goes as a string,
because the server reads a JSON number with a fraction as a `Float64`.

Steps 10 to 17a went in on 2026-09-29:

- Steps 10 to 13: the package `databend/`, its tables, the contract, the
  replay tests, the unit tests of the types and the options, and the DSN
  tests. The fuzz test ran for 60 seconds and found nothing.
- Steps 14 and 14a: the integration tests passed on 1.2.881 and 1.2.948, as
  both principals, with the round trip of every type. They ran with the URL
  of D117, which the entry of dbrun does not print yet (below).
- Step 15: the workflow needs no change but its pin. The `dbmeta` session
  dropped `?sslmode=disable` from the `url` of both principals in dbmeta
  `7907cd9`, on 2026-09-29, and the pin moved there from `00d9f72`. The
  integration tests passed with that `url`, as `dbrun` prints it, on both
  releases, as both principals. The first push, `198a3ed`, ran before the
  move, and its two Databend jobs failed on the key `sslmode`, as expected.
- Step 16: `SELECT version();` gives the version to both principals. The
  requests below wait for the release.
- Step 17a: `## Compared with Couchbase` is in DATABEND.md, and Ken
  reviewed it on 2026-09-29. He decided D125 for the one difference that no
  decision explained: `RowsAffected` returns an error for a statement whose
  result names no count, such as `REPLACE INTO`.

Ken committed steps 2 to 17a on 2026-09-29 as `198a3ed`, and the pin as
`54e9563`. The workflow passed on `54e9563`, on every release of every
driver, with both releases of Databend for the first time, which is the gate
of step 18: https://github.com/xo/dbimp/actions/runs/36566730781. At Ken's
request, the session tagged `v0.6.0` on `54e9563` and published it the same
day, which is the gate of step 19:
https://github.com/xo/dbimp/releases/tag/v0.6.0. The release also holds the
options of W15 (D109) and the key `auth` of W14.

Step 20 sent the requests below on 2026-09-29, each naming `v0.6.0`. This
item stays open until each session reports its change as staged.

`dburl` reported the same day that `GenDatabend` matches `databend.ParseDSN`
of `v0.6.0` with no change, and released it in dburl `v0.37.0` (dburl D39).
`dbmeta` moved to `v0.6.0` as dbmeta `03876ce`, which needed nothing for
Databend.

`usql` staged its switch to `v0.6.0` and dburl `v0.37.0`, and measured it on
both releases. It found that D122 dropped `USE` and `SET` between its
statements, because `database/sql` calls `ResetSession` each time a
connection goes back to the pool. Ken decided D126 the same day, which drops
`ResetSession`. It also found that a DDL statement, whose result names no
count, gives no `RowsAffected` (D125), and `usql` maps that error to 0 in its
driver. D126 needs a release, which this item waits for.

At Ken's request, the session released D126 in `v0.6.1` on `42e2f36`, on
which the workflow passed on every release, and sent it to `usql` the same
day: https://github.com/xo/dbimp/releases/tag/v0.6.1.

`usql` staged its move to `v0.6.1` the same day, with the switch of
`drivers/databend` and dburl `v0.38.0`. On both releases, through `*sql.DB`,
`use system` then `select current_database()` gave `system`, a `SET` read
back, and a transaction worked. That closed this item.

1. To `dburl`: dburl D39, staged, moves the scheme `databend` to
   `github.com/xo/dbimp/databend`. Please check `GenDatabend` against the
   release: the path is the database, the keys `tls`, `auth`, `cancel` and
   `timezone` pass through, and the generator adds no port, because the
   driver uses 8000 (D117).
2. To `dbmeta`: dbmeta has no model for Databend, so nothing to measure
   yet. The `url` of the entry has the form of D117 (step 15).
3. To `usql`: change `drivers/databend/databend.go` to import
   `github.com/xo/dbimp/databend`, which replaces
   `github.com/datafuselabs/databend-go` (D76). It reads the version with
   `SELECT version();`, which works for both principals. The metadata reads
   `information_schema` with `?`, which the driver binds on the server
   (D120).

## W17. Read Databend in Arrow as well as JSON

This item has the lowest priority of the backlog. Ken asked for it on
2026-09-29, in D119. The Databend driver reads JSON only. Arrow is a binary
encoding, so it needs Ken's approval before any code (D13), whether a
package or a reader written here.

- Measure what Arrow carries on each release in the range, from 1.2.899,
  which the Go driver needs: the request with `arrow_result_version_max`
  and `Accept: application/vnd.apache.arrow.stream`, and whether a `Bitmap`
  arrives as bytes, which JSON never gives (D119).
- Add the reader beside the JSON one, through the interface that D119
  shapes after SurrealDB (D108), so the rows code does not change.

## W18. Write the TDengine driver. Dropped.

TDengine is the next target, by D73 and D88. Ken named it at step 1 on
2026-09-29. Follow [DRIVER.md](DRIVER.md). Step 3 is in
[TDENGINE.md](TDENGINE.md). `usql` has no driver for TDengine, so this
driver replaces none (D24), and `dburl` holds the provisional scheme
`tdengine` (dburl D36).

Step 6 measures first the question at the end of TDENGINE.md: whether the
server drops an error that comes after some rows, and whether
`restfulRowLimit` cuts a result with no sign. Either one meets a condition
of "When it cannot be a driver" in DRIVER.md, and then the work stops for
Ken.

Step 2 went in on 2026-09-29: R, H and S hold on 3.3.8.8 and 3.4.2.8, as
both principals. The first measurement of step 6 stopped the work: a query
killed while its result streams ends with a valid body, `code` 0 and a
`rows` that counts only what was sent, on both releases (TDENGINE.md,
Errors). So the server cuts a result short with no sign (D21), and the work
waits for Ken.

Ken asked on 2026-09-29 for the WebSocket interface to be measured first.
It reports the failure that REST loses: after `DROP DATABASE` during the
fetches, the next `fetch` answered `code` 24 on both releases, where REST
ended with `code` 0 and a partial `rows`. Its rows arrive as binary blocks
of taosd, and it is no request of HTTP for each statement (D14). The work
waits for Ken again.

Ken decided the same day that dbimp writes no driver for TDengine, and moved
it to P3 (D127). TDENGINE.md stays as the record of why. Apache Pinot is the
next target.

## W19. Write the Apache Pinot driver. Done.

Apache Pinot is the next target, by D73 and D127. Ken named it at step 1 on
2026-09-30. Follow [DRIVER.md](DRIVER.md). Step 3 is in [PINOT.md](PINOT.md).
`usql` has no driver for Pinot, so this driver replaces none (D24), and
`dburl` holds the provisional scheme `pinot` (dburl D36).

Step 6 measures first two points that can stop the work for Ken: Pinot takes
no insert, update or delete of rows through SQL, which DRIVER.md names as a
stop, and the Broker builds each whole result before it sends it, which
the cursor of `getCursor=true` can page.

Step 2 went in on 2026-09-30: R, H and S hold on 1.4.0 and 1.5.1, as both
principals. The first measurements of step 6 found that the Broker refuses
every insert, update and delete of rows, on both engines, and that the cursor
pages a result (PINOT.md, Parameters). The work waited for Ken, as DRIVER.md
says for a server that refuses one of insert, select, update and delete, and
he decided D128 the same day: the driver runs queries and takes no write.

Steps 5a to 9 went in on 2026-09-30:

- Step 5a: `testdata/pinot/features.json`, from Gemini, DeepSeek, the Go
  client and the JDBC client.
- Step 6: the recordings of 1.4.0 and 1.5.1, as both principals. The
  recorder gained `-second` and `"server": "second"`, so that the setup of
  the script makes its tables and loads its rows through the Controller,
  which the `dbmeta` session publishes as the second port of the entry.
- Step 9 is D128 to D133, which Ken decided on 2026-09-30. He accepted
  D134 the same day, after the commit. It amends D133: the driver cancels
  only a query whose answer has not arrived.

Steps 10 to 17a went in on 2026-09-30:

- Steps 10 to 13: the package `pinot/`, its tables, the contract, the
  replay tests, the unit tests of the literals, the types and the options,
  and the DSN tests. The fuzz test ran for 60 seconds and found nothing.
- Steps 14 and 14a: the integration tests passed on 1.4.0 and 1.5.1, as
  both principals, with the round trip of every type. The type tests load
  their rows through the Controller, with a connector of the tests that
  sends each write of `dbimptest.RoundTrip` there. The ordinary user can
  read only `baseballStats` (dbmeta D112), so it gets HTTP 403 for the
  tables of the tests, and the administrator runs the rest. On 1.4.0 the
  test of the cancel skips, because its Broker has query cancellation off.
- Step 15: the workflow exports `<PRODUCT>_SECOND_ADDRESS` from
  `secondAddress`. The first push, `7f29343`, ran at the pin `7907cd9`, and
  its two Pinot jobs failed, as expected, because that entry of `dbrun` gave
  an `http` URL and no Controller. The `dbmeta` session committed both
  changes as dbmeta `6b24b13` on 2026-09-30, and the pin moved there.
- Step 16: no statement on the Broker gives the version, for either
  principal. `SELECT version()` fails with 700, and `GET /version` of the
  Broker is HTTP 404. The Controller gives it with no user (PINOT.md,
  Principals). The requests below wait for the release.
- Step 17a: `## Compared with Couchbase` is in PINOT.md, and Ken
  reviewed it on 2026-09-30.

Ken committed the type work of W20 and W21 and the pin on 2026-09-30 as
`25131b5`. The workflow passed on `25131b5`, on every release of every
driver, with both releases of Pinot for the first time, which is the gate
of step 18: https://github.com/xo/dbimp/actions/runs/36682954332. Its first
attempt failed on `TestIntegrationErrors` of Databend 1.2.881 only, and a
second attempt of that job passed. At Ken's request, the session tagged
`v0.7.0` on `25131b5` and published it the same day, which is the gate of
step 19: https://github.com/xo/dbimp/releases/tag/v0.7.0.

Step 20 sent the requests below on 2026-09-30, each naming `v0.7.0`. The
request to `dburl` also asks it to make `GenPinot` add no port, because it
adds 8000, and the request to `usql` names the Go types that D136 to D139
change.

`dburl` reported the same day that dburl D43 settles the scheme `pinot`
against `pinot.ParseDSN` of `v0.7.0`, and that `GenPinot` adds no port, so
the driver uses 8099. The 8000 had come from the port of the Broker in the
entry of `dbrun`. It released the change in dburl `v0.39.0`. `dbmeta`
staged its move to `v0.7.0`, which named no type of `dbimp`, and its
Databend and Couchbase models passed on it. `usql` staged `drivers/pinot`
and its move to `v0.7.0` and dburl `v0.39.0`. Through its own binary, the
types of D138 and D139 printed as ISO 8601 on Neo4j, Databend and InfluxDB,
and nothing in `usql` named a type that `v0.7.0` removed. That closed this
item.

Ken committed steps 2 to 17a on 2026-09-30 as `7f29343`. He settled the
`JSON` column that the multi-stage engine names `STRING` the same day with
D135: a value has the Go type that fits the type that the server names.

1. To `dburl`: dbimp D129 settles the provisional scheme `pinot` (dburl
   D36). Please set its `GoPackage` to `github.com/xo/dbimp/pinot` and
   `RequiresCGO` to false. The URL is `pinot://user:password@host:port`,
   with no path, and the driver refuses a path. With no port, the driver
   uses 8099, the port of a Broker, so the generator adds none (dburl D34).
   The keys `tls`, `auth`, `cancel` and `engine` pass through, and any
   other key is refused.
2. To `dbmeta`: dbmeta has no model for Pinot, so there is nothing to
   measure yet. The `url` of the entry has the form of D129, and
   `secondAddress` names the Controller (step 15).
3. To `usql`: `usql` has no driver for Pinot, so this is a new driver:
   `drivers/pinot/pinot.go` imports `github.com/xo/dbimp/pinot`. No
   statement gives the version, so its `Version` cannot run SQL. The driver
   takes no write, and a write fails with the error of the server (D128).

## W20. Map the types of every driver onto one list of kinds. Done.

Ken asked for this on 2026-09-30, after D135. He asked for a table of every
type that every driver supports, with its Go type, for the types that the
root package would share, and for a step of [DRIVER.md](DRIVER.md) that maps
the types of a new driver, with two models, before any code is written.

The first part went in on 2026-09-30:

- D136 fixed the five breaks of D135 that an audit found: the Databend
  geometry follows `geometry_output_format`, the InfluxDB `Duration` is a
  `time.Duration`, a Couchbase value that disagrees with its signature is an
  error, a scan type is `T` for a nullable column, and a type with no Go type
  keeps its text.
- Gemini and DeepSeek reviewed the kinds and the shared types.
- D137 made [TYPES.md](TYPES.md), with the list of kinds and the table of
  every driver, which `TestTheTypeMatrixIsCurrent` writes from the product
  documents, and step 8a of [DRIVER.md](DRIVER.md). The type table of each
  product document has the column Kind, which `TestEveryTypeHasAKind` holds.

Ken reviewed the staged changes, which held the table of
[TYPES.md](TYPES.md), and committed them on 2026-09-30 as `25131b5`. That
closed this item.

## W21. Define the types of D138. Done.

Ken decided D138 on 2026-09-30, and D139, which adds `OffsetTime` and
`Vector`, the same day. This item carries both out.

The work went in on 2026-09-30:

- `civil.go` in the root package holds `Date`, `LocalTime`, `OffsetTime`,
  `LocalDateTime`, `Interval` and `Vector[T]`, with ISO 8601 in both
  directions, and `Assign` converts each one for a `*string`, a `*time.Time`
  and an `sql.Null` of either.
- Neo4j reads and sends the six types, and `neo4j.Time`, `neo4j.Date`,
  `neo4j.LocalTime`, `neo4j.LocalDateTime`, `neo4j.Duration` and
  `neo4j.Vector` are gone.
- Databend reads a `Date`, an `Interval`, a `UInt64` and a `Vector` as a
  `dbimp.Date`, a `dbimp.Interval`, a `uint64` and a `dbimp.Vector[float32]`.
  It sends an interval in the form of the server, because the server takes
  no ISO 8601 part with a sign, and refuses an interval argument with a
  fraction of a microsecond, which the server drops.
- InfluxDB reads a `Date32` and a `Date64` as a `dbimp.Date`, a `Time32` and
  a `Time64` as a `dbimp.LocalTime`, and an `Interval` as a
  `dbimp.Interval`, and sends an interval in the form of the server.
- The integration tests passed on Neo4j 2026.09.0 and 5.26.31, Databend
  1.2.881 and 1.2.948, and InfluxDB 1.13.1, 2.9.1, 3.9.13 and 3.11.5, before
  D139. They ran again on Neo4j and Databend after it.

Ken committed the work on 2026-09-30 as `25131b5`, and approved its
release in `v0.7.0` the same day. The notes of the release say which Go type
changes for a caller of each driver. That closed this item.

## W22. Write the rqlite driver

rqlite is the next target, by D73. Ken named it at step 1 on 2026-09-30,
after the release of `v0.7.0`. Follow [DRIVER.md](DRIVER.md). Step 3 is in
[RQLITE.md](RQLITE.md). `usql` has no driver for rqlite, so this driver
replaces none (D24), and `dburl` holds the provisional scheme `rqlite`
(dburl D36). The `dbmeta` session staged a model for rqlite the same day
(dbmeta D148), which runs through gorqlite until this driver exists.

Steps 2 to 8a went in on 2026-09-30, staged for Ken's review:

- Step 2: `dbrun` starts `rqlite-9.4.5` and `rqlite-10.3.6`, both in the
  Staged tier with the cadence `tested`. `POST /db/query` answered
  `SELECT sqlite_version()` on both, as `admin` and as `dbmeta_user`, so R,
  H and S hold.
- Step 5a: `testdata/rqlite/features.json`, from Gemini, DeepSeek, gorqlite
  and pyrqlite.
- Step 6: the recordings of 9.4.5 and 10.3.6, as both principals. rqlite has
  one namespace, which the `dbmeta` session shares, so each request of the
  script makes and drops its own tables with the prefix `dbimp_`. The first
  run left `PRAGMA foreign_keys = ON` on the shared write connection of
  10.3.6 for four minutes, and the session told `dbmeta`. The script now
  turns it off.
- Step 7: both models were asked twice. Four of their leads proved wrong on
  the server, among them that a read runs on after the client leaves.
- Step 8: RQLITE.md holds the measured facts, and each entry of
  `features.json` has a verdict.
- Step 8a: the type table of RQLITE.md, which both models reviewed. Ken
  reviewed it on 2026-09-30 and decided D140: a column follows the rules of
  affinity of SQLite, a value that does not fit its column keeps the Go type
  of its JSON form, a `DATE` is a `dbimp.Date` and a `DATETIME` a
  `dbimp.LocalDateTime`, and `ANY` is not a supported type.

Step 9 is D141 to D145, which Ken decided on 2026-09-30: the DSN
`rqlite://user:password@host:port` with the keys `tls`, `level` and
`freshness` (D141), one statement a request, a query on `/db/request` and a
count of 0 for a statement that changes no rows (D142), each argument bound
on the server, with a string of the form `x'...'` written as a literal
(D143), no transactions (D144), and `db_timeout` from the deadline of the
context (D145).

Steps 10 to 17a went in on 2026-09-30, staged for Ken's review:

- Steps 10 to 13: the package `rqlite/`, its tables, the contract, the
  replay tests, the unit tests of the types, the arguments and the options,
  and the DSN tests. The fuzz test ran for 60 seconds and found nothing.
- Steps 14 and 14a: the integration tests passed on 9.4.5 and 10.3.6, as
  both principals, 127 on each with none skipped, with the round trip of
  every type. They found that 9.4.5 ignores `db_timeout` for a read on
  `/db/request`. Ken decided D146 the same day: `WithTimeout` ends the
  request at that time too, which stops a read on both releases. They also
  found that a BLOB in a column with no declared type is damaged when it is
  in the first row, and that a read of a table hides a later index from the
  plan, both in RQLITE.md.
- Step 15: the workflow needs no change but its pin. The `url` of the entry
  of `dbrun` is an `http` URL, which the driver refuses. The session asked
  the `dbmeta` session on 2026-09-30 for the form of D141, and it staged
  the change the same day, for Ken's review there. The integration tests
  passed with the `url` that the staged `dbrun` prints, 127 on each release
  with none skipped. The `dbmeta` session committed it as dbmeta `f7e3cbb`
  on 2026-09-30, with its rqlite model, and the pin moved there from
  `6b24b13`.
- Step 16: `SELECT sqlite_version()` gives the version of SQLite to both
  principals, and the header `X-Rqlite-Version` gives the release of
  rqlite on every answer. The requests below wait for the release.
- Step 17a: `## Compared with Couchbase` is in RQLITE.md. Every difference
  that a caller sees has a decision, or is a fact of the server. Ken
  reviewed it on 2026-09-30.

Ken committed steps 2 to 17a and the pin on 2026-09-30 as `af64706`. The
workflow passed on `af64706`, on every release of every driver, with both
releases of rqlite for the first time, which is the gate of step 18:
https://github.com/xo/dbimp/actions/runs/36701186227. At Ken's request, the
session tagged `v0.8.0` on `af64706` and published it the same day, which
is the gate of step 19: https://github.com/xo/dbimp/releases/tag/v0.8.0.
The requests below wait for Ken's word to send them.

1. To `dburl`: dbimp D141 settles the provisional scheme `rqlite` (dburl
   D36). Please set its `GoPackage` to `github.com/xo/dbimp/rqlite` and
   `RequiresCGO` to false. The URL is `rqlite://user:password@host:port`,
   with no path, and the driver refuses a path. With no port, the driver
   uses 4001, so the generator adds none (dburl D34). The keys `tls`,
   `level` and `freshness` pass through, and any other key is refused.
2. To `dbmeta`: the rqlite model of dbmeta D148 runs through gorqlite.
   Please move it to `github.com/xo/dbimp/rqlite`, as dbmeta D101 moved
   Couchbase, and measure it. A number is an `int64` or a `float64` by the
   affinity of its column (D140), where gorqlite gave a `float64`, and the
   driver reads no `/status`.
3. To `usql`: `usql` has no driver for rqlite, so this is a new driver:
   `drivers/rqlite/rqlite.go` imports `github.com/xo/dbimp/rqlite`. Its
   `Version` can run `SELECT sqlite_version()`, which gives the version of
   SQLite, for both principals. The driver has no transactions (D144).
