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
[PLAN.md](PLAN.md). Do not mix the two series in one commit message.

## Where the items came from

W1 to W3 came from setting up the repository on 2026-09-27, from the
conventions that `dbmeta` described for the `xo` repositories. W4 came from
the review of the targets on the same day, from what the `cql` session
learned when it rewrote `xo/cql`. W5 came from D23, and from what the `n1ql`
session measured on the Couchbase query service. W6 came from the advice of
the `dbmeta` session on `docs/DRIVER.md`.

## W1. Set up the repository in the xo layout. Done.

Write the three root documents, `docs/PLAN.md` and this file, the lint
configuration, the CI workflow, the tests for the documents and the skills,
and the copies of the two agent skills. See D3, D10, D11 and D12.

Ken reviewed it and it went into the first commit on 2026-09-27.

## W2. Add the first driver

The first driver is Couchbase, and it replaces `xo/n1ql` (D23). W5 is that
work. Do W4 and W6 first. [DRIVER.md](DRIVER.md) holds every step to add a
driver, in order, and this item does not repeat them.

## W3. Run the integration tests in CI

When the first driver exists, add the jobs that `cql/.github/workflows/test.yml`
has: a job that reads the releases from `dbrun list --json --names`, and a
matrix job that starts each release with `dbrun` and runs `go test -run
Integration` with the DSN set. Pin the `dbmeta` commit in the workflow, as in
`cql` D20. The pin is `2b93710` or later, because that commit of `dbmeta` holds
the ordinary Couchbase user (dbmeta D96). Do not write the list of releases in
the workflow.

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

## W5. Write the Couchbase driver

Write the first driver, for the Couchbase query service and SQL++, to D23.
It replaces `xo/n1ql`. Do W4 and W6 first, then follow
[DRIVER.md](DRIVER.md). [COUCHBASE.md](COUCHBASE.md) holds every measured
fact about the query service and every fault of `go_n1ql` that the driver
must not repeat.

[COUCHBASE.md](COUCHBASE.md) does not follow the template of
[DRIVER.md](DRIVER.md) yet. Give it the headings of the template in step 8,
because `TestEveryDriverHasItsDocument` fails once the folder `couchbase`
exists.

The package is `github.com/xo/dbimp/couchbase` (D26). It registers the one
name `couchbase` (D30), and it takes a `couchbase://` URL (D35). Its
integration tests read `COUCHBASE_DSN` (D9).

The `dbmeta` session added the ordinary user on 2026-09-27 (D31, and
dbmeta D96). Two questions about the output of `dbrun dsn --json` remain,
and each is a change to `dbrun`, so it goes to Ken through the `dbmeta`
session once step 9 settles the URL. The first asks how `dbrun` prints the
`couchbase://` URL: as a second field, or in a form for each consumer. The
second asks whether it also prints a DSN for the ordinary user. Until then,
the tests build the URL from the `http://` form.

At the move of step 16, three repositories change. The `n1ql` session listed
them in its W13:

1. `dburl`: the scheme gets the `Driver` name `couchbase`, with `n1ql` as an
   alias, and a new `GoPackage` and `DriverURL`. The `Dialect` becomes
   `couchbase`, so `usql`, `dbtpl` and `dbmeta` change any match on `n1ql` in
   the same release (D30). The `dburl` session writes its generator from the
   parser of this driver, at the version that `usql` pins.
2. `usql`: `drivers/couchbase` imports this driver, changes the prefix of
   its errors, and drops the `strconv.Unquote` in its `Version` function.
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
