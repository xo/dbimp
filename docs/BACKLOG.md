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
in D73.

## W1. Set up the repository in the xo layout. Done.

Write the three root documents, `docs/PLAN.md` and this file, the lint
configuration, the CI workflow, the tests for the documents and the skills,
and the copies of the two agent skills. See D3, D10, D11 and D12.

Ken reviewed it and it went into the first commit on 2026-09-27.

## W2. Add the first driver

The first driver is Couchbase, and it replaces `xo/n1ql` (D23). W5 is that
work. Do W4 and W6 first. [DRIVER.md](DRIVER.md) holds every step to add a
driver, in order, and this item does not repeat them.

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

## W4. Design the root package before the first driver

Write `docs/DESIGN.md`, and add it to the tables in `CLAUDE.md` and
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
(D37).

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
dburl D25.

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

## W6. Write the tests that hold DRIVER.md

Write the gates that hold the steps of [DRIVER.md](DRIVER.md), before the
first driver is done. Each one fails with a message that names the step that
was skipped. Where a table can be made from the code, generate it and test
only that the file is current. The `dbmeta` session proposed the list, from
what its own gate tests caught.

The gates are in `gates_test.go`, and "The tests that tell you what you
forgot" in [DRIVER.md](DRIVER.md) lists them. The repository has no driver
yet, so each gate passes with nothing to read. `gates_self_test.go` makes
sure that each gate passes a complete driver and fails an incomplete one.
The code and the design of W4 went in with the gates, on 2026-09-27, staged
for Ken to review.

## W7. Add the survey of features and the round trip of types

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
[DRIVER.md](DRIVER.md) lists the gate.

Two ideas of the models were not taken. A gate on the history of git, which
makes sure that the survey came before the tests, breaks when a branch is
rebased or squashed, and Ken reviews the order of the work anyway. A linter
that looks for a comparison after each `Scan` is a guess, and the helper
does the comparing itself.

## W8. Write the SurrealDB driver

Ken named SurrealDB as the second target on 2026-09-27 (Q1 in
[PLAN.md](PLAN.md)). Follow [DRIVER.md](DRIVER.md) from step 2.
[SURREALDB.md](SURREALDB.md) holds every fact about its HTTP interface.

`dbrun` starts 2.7.0 and 3.3.0 in the Tested tier, and 3.1.6 and 3.2.4 in
the Nightly tier, from dbmeta D103 at commit `78f1e44`, with the ordinary
user `dbmeta_user`. `dburl` has no scheme for SurrealDB, and `usql` has no
driver for it.

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
   `surrealdb://user:pass@host:port/<namespace>/<database>`, with the default
   port 8000, with TLS or without, and passes the keys `tls`, `auth` and
   `encoding` through (D48, D49 and D51). Aliases such as `surreal` are for
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
step 18. At Ken's request, `v0.2.0` was tagged on `82c1902` the same day. It holds the
driver, and `dburl` released the scheme `surrealdb` in `v0.34.0`, checked
against `v0.2.0` (dburl D26). The release notes of step 19 and the requests
of step 20 wait for Ken.

## W9. Write the Neo4j driver

Ken named Neo4j as the third target on 2026-09-27 (Q1 in
[PLAN.md](PLAN.md)). Cypher meets S by D58, so it is P1. `dbrun` runs the
Enterprise Edition under the evaluation agreement (D59). Follow
[DRIVER.md](DRIVER.md).

`dburl` has no scheme for Neo4j yet, and Ken chose its aliases `nj`, `neo`
and `n4j`. `usql` has no driver for it. The `dbmeta` session was asked for the entry of step 4 on 2026-09-27, with the
facts below, which this session read and did not measure:

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
- Step 8: [NEO4J.md](NEO4J.md) is complete, apart from the type table and
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
  folders under `testdata/`. `DBMETA_COMMIT` moves to `19a8a9a`, which names
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
   `neo4j://user:pass@host:port/<database>`, with the default port 7474, or
   7473 with `tls=true`, and passes the keys `tls` and `cancel` through
   (D61 and D67). A path that is empty means the database `neo4j`. The tools
   of Neo4j write `neo4j://host:7687` for Bolt, which this driver does not
   speak.
2. `usql`: add `drivers/neo4j`, which imports the driver. Its `Version` runs
   `CALL dbms.components() YIELD name, versions WHERE name = 'Neo4j Kernel'
   RETURN versions[0]`, which the ordinary user can run (recorded). A
   statement with no `RETURN`, such as `CREATE (:A)`, gives a result set with
   no columns, which `tblfmt` refuses, as Q12 says for SurrealDB.
3. `dbmeta`: nothing to change now. `dbrun` names the URL of Neo4j from
   dbmeta D109. A model for Neo4j is a question for Ken.

## W10. Fix the faults that usql found in the SurrealDB driver

The `dbmeta` session ran `usql` at commit `54c12a4`, which pins `v0.2.0` of
the SurrealDB driver, against 3.3.0 on 2026-09-27. It found two faults:

1. A statement that returns no rows gives a result set with no columns, and
   `tblfmt` refuses it with "result set has no columns". The statement still
   runs. Q12 in [PLAN.md](PLAN.md) holds the options. It waits for Ken,
   because two of them change `tblfmt` or `usql`, and the third replaces
   D52.
2. A record id inside an array prints as the fields of `RecordID`, such as
   `[{"Table": "book", "ID": "earthsea"}]`. D70 proposes `MarshalText`, and
   `TestRecordIDText` holds it. That change is staged.

The session also reported that a `usql` user who writes `$1` gets a parse
error from the server, and that `\bind` passes positional arguments, which
the driver refuses by D50. `usql` has no way to pass `sql.Named`. D50 does
not change.

On 2026-09-27 every SurrealDB server of `dbrun` belonged to the owner
`dbimp-w8`, so this item measured nothing on a server. The facts come from
the recordings of 3.3.0, from the source of `usql` and of `tblfmt` v0.16.0,
and from `tblfmt` run on a result set with no rows.

A fix reaches `usql` only in a new release of `dbimp`, by steps 18 to 20 of
[DRIVER.md](DRIVER.md).

## W11. Write the InfluxDB driver

InfluxDB is the next target, by D73. Follow [DRIVER.md](DRIVER.md) from step
2. InfluxDB 3 speaks SQL and InfluxQL over HTTP, InfluxDB 1 speaks InfluxQL,
and InfluxDB 2 speaks Flux (not measured). Step 9 decides which releases and
which languages the driver serves. After InfluxDB come CrateDB, ArangoDB,
Databend, TDengine, Apache Pinot, rqlite and libSQL, each a work item of its
own when its turn comes.
