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
from D109.

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

## W13. Write the ArangoDB driver

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

## W14. Add the key auth to the InfluxDB and Neo4j drivers

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

## W15. Give every driver the same options

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
