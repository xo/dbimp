# Adding a driver

Every step needed to add a driver to `dbimp`, in the order to do them. A
coding agent follows this file from the first step to the last. A person can
follow it too.

The rules live in `AGENTS.md`, and the reasons live in [decisions/](decisions/README.md).
This file is the checklist, and it points at both. It follows the shape of
`dbmeta/docs/DIALECT.md`. It takes the lessons that the `dbmeta`, `cql` and
`n1ql` sessions reported, and the review by Gemini and DeepSeek, all on
2026-09-27.

A driver is one deliverable. The measurements, the recorded responses, the
decisions, the code, the tests, the document for the product and the CI job
ship together. A driver that runs a query and has none of the rest is not
nearly finished.

A step is done when its gate is met. The gate is written at the end of the
step. Do not start a step before the gate of the step before it is met. A
gate that names a command is met when that command passes, and not when you
expect that it will pass.

## Where you stop and ask Ken

Stop, and ask Ken, at each of these. Do not decide one yourself, and never
write that Ken accepted something that he did not say in the conversation:

- Which target comes next (step 1).
- A target that fails R, H or S (step 2).
- A licence that must be accepted to run the image, such as the one for Neo4j
  Enterprise (step 4).
- An image that somebody other than the vendor built (step 4).
- A target that meets a condition of "When it cannot be a driver" (step 6).
- A server that refuses one of insert, select, update and delete (step 6).
- The mapping of the types (step 8a), before any code is written.
- Every decision of step 9, including any package that is not the standard
  library or `apd` (D13).
- A feature that the product has in a form that stretches the contract, such
  as a transaction that is not a transaction. Leave it unsupported and write
  down why.
- Any open question at the end of [PLAN.md](PLAN.md) or of a
  `docs/<PRODUCT>.md`.
- The comparison with the first driver (step 17a), and each difference in it
  that no decision explains.
- A commit and a push (step 18), a tag and a release (step 19), and the
  requests to the consumers (step 20). Ken approves each one before you do
  it. His approval of one is not his approval of the next, and his approval
  of the staged changes covers those changes and nothing that you write
  after them.

## Before you write anything

### 1. Ask Ken whether this target is next

[TARGETS.md](TARGETS.md) holds the targets, their priorities and the order
of the work, which Ken set in D73 and D74. The order can change, so ask Ken
whether the next target in it is still next.

Gate: Ken named the target, and his answer is in a decision in
[decisions/](decisions/README.md) or in [BACKLOG.md](BACKLOG.md), with the
date. D73 and D74 name each target in the order.

### 2. Measure the target against R, H and S

D16 holds the three tests. R asks whether `dbrun` can start the product. H
asks whether it takes queries and returns results over HTTP. S asks whether
it has a SQL dialect, or a dialect like SQL. D58 and D75 amend S: a graph
query language that a person types meets it, and so do AQL and Flux.

A target that needs a second container, such as Kafka for ksqlDB, fails R
until `dbrun` can start a pod. A target that is only a cloud service fails R
unless an emulator exists.

Gate: the row of the target in [TARGETS.md](TARGETS.md) names the result of
each test and the priority. If a test fails, stop and ask Ken.

### 3. Read what exists

Read these before you start a server, in this order. Read them, and never
import them (D29):

1. The scheme in `dburl/scheme.go`. The `Name`, the aliases and the DSN
   generator show the URL that `dburl` writes, before any code exists
   (D27). The driver registers the name of its database whatever the scheme
   says, and a scheme with another name is renamed at the move (D28 and
   D30).

   ```bash
   grep -n 'Name: *"<name>"' -B2 -A12 ../dburl/scheme.go
   ```

2. The driver that `usql` uses now, if one exists. Read its `GoPackage` in
   the scheme, the version in `usql/go.mod`, and its file in
   `usql/drivers/<name>/`. Read its `Version` field, which is the statement
   that `usql` runs for the version (step 16).
3. Every fault that is known in that driver. `dbmeta` records them in its
   decisions, and a fork such as `xo/n1ql` records them in its own.
4. What `dbmeta` measured for the product, if it has a model or a container
   entry.
5. Whether other products speak the same interface. If they do, read
   "Flavors" at the end of this file now, because it changes steps 4, 6, 9
   and 14.

Gate: the facts that you found are in a draft of `docs/<PRODUCT>.md`, under
the headings of the template at the end of this file. Each one is marked
"not measured", with its source.

## Standing the server up

### 4. Get the container entry into dbmeta

`dbrun` in `dbmeta` starts every server, and nothing else does (D9). If
`dbrun list` does not name the product, the `dbmeta` session writes its entry
in `dbmeta/container/<product>.go`, by `dbmeta/docs/CONTAINERS.md`. Send it
these facts:

- The image and the tags. `dbmeta/docs/EVALUATION.md` chooses the range of
  releases.
- The port of the HTTP interface. `dbrun` publishes one port.
- An `Init` step that creates an ordinary user as well as the administrator,
  and that is safe to run twice. `container/couchbase.go` and
  `container/vertica.go` in `dbmeta` are the examples.

Do not start a container by hand. A container started by hand gets a port
that somebody typed, and `dbrun` cannot find it.

Gate: `dbrun list --json` names each release of the product, and `Init` ran
twice on one release with the same result.

```bash
(cd ../dbmeta/test && go run ./cmd/dbrun list --json)
```

### 5. Start it

```bash
(cd ../dbmeta/test && go run ./cmd/dbrun start <release>)
(cd ../dbmeta/test && go run ./cmd/dbrun dsn --json <release>)
```

`dbmeta/docs/DBRUN.md` holds every command of `dbrun`, its output, and the
rules for sharing the machine: start and stop only the servers that you
test, and never stop the server of another session. From dbmeta commit
`ec91128`, each server carries an owner, and `dbrun` refuses to stop or
remove a server of another owner unless you pass `--force`. Never pass it
for a server that is not yours. A server that another session runs is
shared, and `dbrun status` names its owner. The `dsn` field of the
second command is an `http://` URL, and the `url` field is the URL in the
form of `dburl`. Plain `dbrun dsn` prints a table.

Gate: a `curl` request for the version returns HTTP 200 and a body that
`jq .` parses, and the file is saved under `testdata/<driver>/`.

## Measuring the interface

### 5a. Survey the operations, the features and the types

Do this before you measure anything, and before you write any test. The
survey says what to measure in step 6 and what to test in step 14a.

Ask at least two models, such as Gemini and DeepSeek, in separate
conversations. Ask each one the same four questions:

1. Which statements of CRUD the database supports: insert, select, update,
   delete, and any form of its own, such as an upsert or a merge.
2. Which operations on a schema it supports: a table or a collection, a
   primary key, a foreign key, an index, a unique constraint, a view, and a
   default value.
3. Which features are its own, which a driver for another database never
   meets. Examples are `USE KEYS` and `MISSING` in Couchbase, `SAMPLE BY` in
   QuestDB, and object columns in CrateDB.
4. Which native types it has, with the range and the precision of each.

Then read what other drivers claim. Read at least one driver in Go and one
driver in another language, such as the official SDK. Record each feature
that a driver claims.

Write each answer into `testdata/<driver>/features.json`, one entry for each
operation, feature and type. Each entry holds its kind, its name, the
sources that named it, the verdict `not measured`, and no test yet. Record
each model and each driver that you asked, with the date.
`dbimptest.Features` is the form of the file, and [DESIGN.md](DESIGN.md)
describes it.

A model can be wrong, and step 6 settles each entry against the server. Do
not leave out an entry because one model did not name it.

Gate: `features.json` names at least two models and at least one other
driver. It has an entry for each of insert, select, update and delete, and
an entry for every native type that any source named.

### 6. Measure the HTTP interface, and record every response

Send requests to the running server. Send each one as the administrator and
again as the ordinary user. A server can refuse to an ordinary user what it
gives an administrator, and that changes the design. Save each
request and each response under `testdata/<driver>/`, with the status, the
headers and the body, as you send it. Use `curl` for the first look. Then
write the requests into `testdata/<driver>/requests.json`, one or more for
each item, and record them with the command `dbimptest/cmd/record`, once for
each release:

```bash
go run ./dbimptest/cmd/record -dir testdata/<driver> -release <release> \
	-admin <URL of the administrator> -ordinary <URL of the ordinary user>
```

If a release has no ordinary user, leave out `-ordinary`, and the command
records the administrator only. The command sends each request as both principals, through `dbimptest.Recorder`,
and replaces what an earlier run recorded for the same release. A request can
wait, give up after a timeout, run in the background while a later one acts on
it, or send a wrong password. It can send a body of plain text, or its JSON
body as CBOR, and the script can name the headers that every request of one
principal sends, and send each password as a Bearer token. A binary response is kept as base64. Every file comes from a real server, and never
from your memory of the documentation. [DESIGN.md](DESIGN.md) holds the form of
a file and of the manifest.

Measure each of these:

1. How a statement is sent: the endpoint, the method, the content type and
   the authentication.
2. Where the names of the columns and their order come from, and whether they
   arrive before the first row (D18).
3. Every type that the product has, including NULL, a value that is missing
   and a value that is empty. Include the largest integer and the longest
   decimal (D19).
4. Parameters: positional, named, or none. If none, the driver uses the
   parser for placeholders in the root package (D34).
5. A result larger than one page, to find paging, cursors and any cap or
   default limit (D21).
6. An error before any rows, and an error after some rows. Record the status
   of each, because a server can send HTTP 200 with an error in the body.
7. A request cancelled while the result arrives, and any way to cancel on the
   server.
8. A failed authentication, and a request refused for lack of a privilege.
9. The version, by the statement or the endpoint that `usql` uses.
10. Two statements in one request, and a statement with a comment.
11. Transactions, if the product has them (D20).
12. The headers that matter: `Content-Encoding`, a redirect, and a limit on
    the rate of requests, such as HTTP 429.

Also send at least one request for each entry of `features.json`, and
record it with the item that it belongs to, such as item 3 for a type. Then
set the verdict of the entry to `yes` or `no` from what the server answered.
A verdict of `no` names the recorded file that shows that the server lacks
it: a refusal, or an answer that ignores what was asked. Never set `no` from
the documentation or from a model alone.

List every recorded file in `testdata/<driver>/manifest.json`, with the item
number, the principal, the release and the date. `Recorder.WriteManifest`
writes it. An item that does not apply to the product has an entry with the
reason in `absent`, such as transactions for a product that has none.

Gate: `TestEveryDriverHasItsManifest` passes. The manifest names a file or a
reason for each item, for each principal, from a named release, and every file
under `testdata/<driver>/` is in the manifest. No entry of `features.json` has
the verdict `not measured`. If a condition of "When it cannot be a driver"
holds, stop and ask Ken now.

### 7. Ask two models, then test every answer on the server

Ask at least two models, such as Gemini and DeepSeek, about what step 6 did
not find: a way to bind parameters, transactions, paging, a type, a server
side cancel, a flavor. Then send each lead to the server, and record the
answer in the manifest.

Treat every answer as a lead, not a fact. One model found four real sources for
MariaDB in `dbmeta`, and another invented eight for Trino. In the review of this
file, one model claimed that `database/sql` drops the names of parameters
without `driver.NamedValueChecker`, which is false. A lead that the server did
not show to be true stays "not measured", with the model that gave it.

Add each lead that names an operation, a feature or a type to
`features.json`, and settle its verdict against the server as step 6 does.

Gate: each lead, and what the server answered for it, is in the draft of
`docs/<PRODUCT>.md`, including the leads that proved wrong. No entry of
`features.json` has the verdict `not measured`.

### 8. Write the document for the product

Complete `docs/<PRODUCT>.md` from the template at the end of this file. Add
it to the tables in `AGENTS.md` and `README.md`. Mark each fact "measured",
with the release and the date, or "not measured", with its source. Write
facts only. A choice goes in a decision in step 9.

Each heading of the template is a heading of the document, written as `## `
and the name before the colon, such as `## The DSN`.

Gate: `TestEveryDriverHasItsDocument` passes, every fact in the document is
marked, and the tests for the documents pass.

### 8a. Map the types

Map every type of the product onto the kinds of [TYPES.md](TYPES.md) before
any code is written (D137). A kind is a family of types that means the same
thing in any database, such as an integer or a date, and each kind has one Go
type (D135).

1. List each type that `features.json` marks `yes`, with its name on the wire
   and what step 6 recorded for it: its text or its encoding, NULL, the
   largest and the smallest value, and an empty value.
2. Give each type a kind from [TYPES.md](TYPES.md), and the Go type of that
   kind. A type whose Go type differs from the Go type of its kind needs a
   reason: a type that Go does not have keeps the form that a decision of
   step 9 names (D136). A type that fits no kind needs a new kind, which is a
   decision of its own.
3. Ask at least two models, such as Gemini and DeepSeek, in separate
   conversations, to review the mapping against [TYPES.md](TYPES.md) and
   D135. Give each one the list of step 1 and the kinds. Record each answer,
   and what the server showed about it, under `## Second opinions` of the
   product document.
4. Write the mapping as the type table of `docs/<PRODUCT>.md`, between the
   markers of step 10, in the form that `dbimptest.TypeTable` writes, with
   the column Kind. Write the Go type, the scan type and the database type
   that the code will give.
5. Stop, and show the mapping to Ken, with each type whose Go type differs
   from its kind and each new kind. His review of the mapping is the item 4
   of step 9.

Step 10 then generates the same table from the code. A difference between
the two is a question for Ken, and never a change made to quiet the test.

Gate: `TestEveryTypeHasAKind` passes on the written table, two models are
recorded under `## Second opinions`, and Ken reviewed the mapping in the
conversation.

## Deciding

### 9. Write the decisions

A choice goes in [decisions/](decisions/README.md) as a decision: a file
with the next number and the status `Proposed`, and a row in the index. A
fact stays in `docs/<PRODUCT>.md`. Each driver decides these:

1. The name of the package, which is the name of the database (D26).
2. The one name that it registers with `database/sql`, which is the name of
   the package (D28 and D30). The driver registers no alias. `dburl` keeps
   the aliases.
3. The DSN, which is a standard URL that `net/url` parses (D27). Name the
   scheme, every query key with its default, and what the path means. Refuse
   a key that is unknown or repeated. Keep no form of DSN from an earlier
   driver. The secret is the password of the URL, and never a key of the
   query (D94).
4. The Go type for each wire type, from the mapping of step 8a, which Ken
   reviewed (D135 and D137).
5. Whether NULL and a missing value are one value or two (D18).
6. How parameters are bound. If the server binds none, the escaper for the
   literals of the product (D34).
7. Transactions, or the error that `BeginTx` returns (D20).
8. How the driver reads a result to its end (D21).
9. Whether the server stops a query when the client disconnects, and how it
   is cancelled on the server if it does not (D36).
10. Whether the driver follows a redirect, and to which hosts it sends the
    credentials.
11. The flavors that the driver serves, and how it tells them apart.
12. Any binary encoding, which needs Ken's approval for this database (D13).

Gate: each item has a decision in [decisions/](decisions/README.md) that
names this driver, and Ken accepted each one in the conversation. Change a status to `Decided`
only after he said so.

### 10. Write the type table and the interface table

The type table gives, for each wire type, the Go type, the scan type, the
database type name in upper case, whether the column can be NULL, and the
length, the precision and the scale where they apply. Use one Go type for
each wire type in every place: a scan into `*any`, `Rows.Next`, and
`ColumnTypeScanType`, for a column that can hold a NULL too (D135 and
D136). A caller scans a nullable value into `sql.Null[T]`, and a UUID is
`uuid.UUID` (D25), and a decimal is `*apd.Decimal` (D33). The table lives in
the code, as the map of the driver. A test passes the map to
`dbimptest.TypeTable`, which writes the table between the markers
`<!-- dbimp:types -->` and `<!-- /dbimp:types -->` in `docs/<PRODUCT>.md`,
so the two cannot disagree.

The interface table names each optional interface of `database/sql/driver`,
says whether the driver implements it, and says why. Cover at least these:
`DriverContext`, `Connector`, `Pinger`, `SessionResetter`, `Validator`,
`NamedValueChecker`, `QueryerContext`, `ExecerContext`, `ConnPrepareContext`,
`ConnBeginTx`, `RowsColumnScanner`, `RowsNextResultSet`, and each
`RowsColumnType` method. A `Pinger` sends a real request that costs little.
A `SessionResetter` exists only if the connection can hold state on the
server that `database/sql` does not end. A transaction is not such state,
because `database/sql` ends it before it reuses the connection (D102). A test passes the types of the driver and a
reason for each interface to `dbimptest.InterfaceTable`, which learns from
the types whether each one is implemented, and writes the table between
`<!-- dbimp:interfaces -->` and `<!-- /dbimp:interfaces -->`.

Run the tests with `DBIMP_UPDATE=1` to write both tables. Without it, each
test fails when the document holds another table.

The table that the code makes must be the table that step 8a wrote and Ken
reviewed, with the kind of each type from `dbimptest.Kinds`. Run the
test without `DBIMP_UPDATE` first, so that a difference shows.

The type table has one row for each type that `features.json` marks `yes`,
and no other row.

Gate: `TestEveryDriverGeneratesItsTables` passes, every type from step 6 has
a row, and every interface has a reason.

## Writing it

### 11. Write the unit tests first

Make the package with `init`, which registers the driver, and with stubs
that return an error. Then write the unit tests. They run against
`dbimptest.Replay`, which replays the files from step 6. A test reads a
recorded file, and never a response written into the test as a string. The
response then decodes through the real decoder, as `xo/cql` runs the real
encoding of gocql in its fake.

Each driver also calls `dbimptest.RunContract`, with bodies in the form of
its product. It tests the order of the columns, NULL as nil, an error after
some rows, a close before the end, cancellation, a request sent once,
`driver.ErrBadConn` only before the request reached the server, and no
faked transaction. It does not test paging (D21), because each product
pages in its own way, so the driver tests that with its recorded exchanges.
The test that calls `RunContract` must not run in parallel, because each
subtest counts the goroutines of the process. [DESIGN.md](DESIGN.md) holds
the list.

Gate: `TestEveryDriverRunsTheContract` passes, and `go test ./<driver>/...`
compiles and fails only on assertions of the contract. No test is skipped
and no assertion is empty.

### 12. Write the driver

Write the package `github.com/xo/dbimp/<driver>`. Code that a second driver
needs goes in the root package `dbimp` (D4). Code that only this driver needs
stays in its package. Follow D5 to D8, D25 and W4. The driver does all of
these, and a unit test holds each one:

- `init` registers the driver, and nothing else writes global state (D7).
- If the connection implements `driver.NamedValueChecker`, it returns
  `driver.ErrSkip` for a value that it does not take itself, so that
  `database/sql` converts it (W4).
- The `Connector` owns the `http.Client` and its transport, and its `Close`
  releases the idle connections. `Close` on a connection can run twice.
- The driver reads the body with `jsontext.Decoder`, one token at a time, and
  reads the next token only when `Rows.Next` asks for the next row. It never
  reads the whole body into memory with `io.ReadAll` or a similar call (D25).
  A test streams a large result and holds the memory in use to a bound.
- `QueryContext` returns an error from the server before it returns rows,
  including an error in a body sent with HTTP 200. An error that arrives
  after some rows is returned from `Rows.Next`, and from `Rows.Err` through
  `database/sql`.
- When `Rows.Next` reaches the end of the rows, it reads the rest of the
  response, such as an error or a status after the rows, and returns any
  error that it holds (D36).
- `Rows.Close` before the end closes the body and reads nothing more. The
  connection then closes on HTTP/1.1, and a large result is never drained to
  save it. `Rows.Close` after the end closes a body that is at EOF, so that
  the connection goes back to the pool (D36). D90 is the one exception, for
  the first batch of an ArangoDB cursor.
- If step 6 showed that the server keeps running a query after the client
  disconnects, the driver follows the decision that Ken made for it in step 9
  (D36).
- A cancelled context stops the request and the read of the body, and the
  error is `context.Canceled` or `context.DeadlineExceeded`, never
  `driver.ErrBadConn`.
- The driver never sends a request again after it can have reached the
  server. HTTP 429 and HTTP 503 are errors that reach the caller.
- The connector builds its transport with `dbimp.NewTransport` and its
  client with `dbimp.NewClient`, and sends each request with `dbimp.Send`.
  The client has no `Timeout`, and each deadline comes from the context. The
  transport bounds the dial and the TLS handshake, and not the headers of
  the response, because a server can send them only after a long query ends.
- The transport takes a proxy from the environment, and the driver does not
  set `Accept-Encoding` itself, so that the transport decompresses gzip.
- No error message holds a password or a token. Write a URL in an error with
  `url.URL.Redacted`.
- Two queries can run at the same time on one `sql.DB`.
- A test makes sure that no goroutine is left running after each test, with
  `dbimptest.CheckGoroutines`.
- The driver takes the options of D109 through the root package: it declares
  `type Option = dbimp.Option[options]`, `WithOptions`, `WithTimeout`,
  `WithReadonly`, `WithParameter` and `WithDatabase`, and an option for each
  key of its DSN that can change for one statement. `CheckNamedValue` keeps
  an `Option`, and each statement calls `dbimp.Resolve`. An option that the
  server cannot honor fails with `dbimp.Unsupported`, and a value that the
  DSN would refuse fails with `dbimp.ErrInvalidValue`. `WithParameter`
  replaces a key of the driver, through `dbimp.MarshalParams` for a body of
  JSON. Step 6 measures the setting of the server that each option sends,
  and a test with a fake server holds each one.

Gate: `TestNoDriverTouchesGlobalState`, `TestEveryDriverRegistersOneName`
and `TestEveryDriverTakesTheCommonOptions` pass, and
`go test -race -count=2 ./<driver>/...` passes.

### 13. Test the DSN

The DSN is a standard URL, parsed with `net/url` (D27), which the driver reads
with `dbimp.ParseURL` and `dbimp.NewQuery`. Write a test that parses each DSN
from step 9 and formats it again, with `RoundTrip` in its name, and a fuzz test
for the parser, whose name starts with `Fuzz`. Both found real faults in
`xo/cql` on the first day: a host that is an IPv6 address, TLS turned on with no
settings, and an empty host.

Gate: `TestEveryDriverTestsItsDSN` passes, both tests pass, and the fuzz test
ran with `-fuzztime=60s` and found nothing.

## Proving it

### 14. Run the integration tests against every release

A test that needs a server reads `<DRIVER>_DSN`, such as `COUCHBASE_DSN`, and
skips when it is empty (D9). Its name holds the word `Integration`. Write
tests that do these:

- Read every type through `Rows.Scan`, the real path of a caller. A test that
  only runs a query hides a fault in the scan (dbmeta D93).
- Run as the administrator and as the ordinary user.
- Create their own namespace, and remove it at the end.
- Skip a difference between releases or flavors with the reason, rather than
  fail.

Then run them against each release in the tier of the product in `dbmeta`,
one at a time. A release that no `dbmeta` model reads is in the Staged tier,
and its cadence names the tier that it would have (dbmeta D119 and
dbmeta D120). So list the releases of both, and run each release of the
product whose tier or cadence is `tested`:

```bash
(cd ../dbmeta/test && go run ./cmd/dbrun list --json tested staged)
(cd ../dbmeta/test && go run ./cmd/dbrun start <release>)
export <DRIVER>_DSN=$(cd ../dbmeta/test && go run ./cmd/dbrun dsn --json <release> | jq -r '.[0].url')
go test -race -count=1 -run Integration -v ./<driver>/...
(cd ../dbmeta/test && go run ./cmd/dbrun remove <release>)
```

A driver has not run until it has run on every release. A run where every
integration test skipped is not a run.

Gate: the tests pass on every release, no test skipped for a missing DSN, and
`docs/<PRODUCT>.md` names each release with the date.

### 14a. Round trip every operation, feature and type

Test each entry of `features.json` against a real server, in an integration
test, as step 14 runs them. Each entry names its test.

For CRUD, create at least three tables, or collections where the database
has no tables, in the namespace of the test. If the database has foreign
keys, one table refers to another. If it has indexes, one table has an index
that a query uses. On each table, insert rows, select them, update them,
select them again, delete them, and select again to see that they are gone.
Compare each value that a select returns with the value that the test
wrote. A test that only sees no error proves nothing.

For each type that `features.json` marks `yes`, run `dbimptest.RoundTrip`.
It stores values in a column of that type, and never selects
a literal in place of a stored value. For each value, it does these, in
order:

- Inserts the value as a bound argument, and again as a literal.
- Selects each row by its key, and compares the value exactly, and the Go
  type that `database/sql` returns.
- Updates the value to a second value, selects it, and compares again.
- Deletes the row, and selects it to see that it is gone.

The values for each type are NULL, the zero value, the smallest and the
largest value, an empty value, a long value, and a value that tests the
type: text with characters outside ASCII for a string, a time zone and the
smallest step of time for a time, and the last digit of precision for a
number.

For each feature of the database that `features.json` marks `yes`, write a
test that uses it and compares what it returns. For each entry marked `no`,
write a test that sends the operation and expects the refusal of the server,
or shows that the server ignores it. If the server does it, the verdict was
wrong, and the test fails.

Run every test as the administrator and as the ordinary user. If the
ordinary user cannot create a table, the administrator creates it and the
ordinary user runs the rest, and the test says so. If the database updates
an index after a write, as Couchbase does, use the option of the driver that
waits for the write, such as `scan_consistency`, or poll with a limit on the
time. Never sleep for a fixed time.

Drop every table and every namespace that a test creates, even when the test
fails, and select at the end to see that nothing is left.

A database can refuse part of CRUD by design. A store that only appends has
no update or delete, and Couchbase has no foreign key. Mark each such entry
`no` with its recording, test the refusal, and test the form that the
database has in its place, such as an upsert or dropping a partition, as a
feature of its own.

Gate: `TestEveryDriverHasItsFeatures` passes. Every entry of `features.json`
names a test that exists, the test of each type calls `dbimptest.RoundTrip`,
and every integration test passes on every release as both principals.

### 15. Add the CI job

The jobs of W3 are in `.github/workflows/test.yml`, and a new driver adds
no job. The job `releases` reads the products from the folders under
`testdata/`, and the releases from `dbrun list --json`, and never from names
written in the workflow (dbmeta D69). A push runs the releases that `dbmeta`
calls tested, and the nightly run adds the ones it calls nightly. A release
in the Staged tier runs by its cadence (dbmeta D120). The workflow checks
out a pinned commit of `dbmeta` from its main branch, as in `cql` D20 and
`n1ql` D28. If the driver needs a change in `dbrun`, such as the `url` of
its DSN, move the pin to the commit of `dbmeta` that holds it.

Gate: `actionlint` passes on the workflow, and
`TestTheWorkflowNamesNoRelease` passes. The run on a push is the gate of
step 18.

## Handing it over

### 16. Write the requests to the consumers

First run the statement that `usql` runs for the version on this driver, as
the administrator and as the ordinary user, and record what each one gets in
`docs/<PRODUCT>.md`. If the ordinary user gets nothing, say so. `usql` reads
`v$instance` for Oracle, which an ordinary user cannot see, and nobody
noticed until `dbmeta` measured it.

Then write the request to each consumer. The move is one deliverable across
three repositories, and each repository follows its own rules. Each session
changes its own repository, and you never edit its files. Do not send a
request yet. A request names a release of this repository, so step 20 sends
it after step 19 publishes that release:

1. The `dburl` session sets the `GoPackage` of the scheme to
   `github.com/xo/dbimp/<driver>`, sets `RequiresCGO` to false (D5 and D14),
   and makes its generator write the URL of D27. If the `Name` of the scheme
   differs from the name of the package, it renames the scheme and keeps the
   old name as an alias (D30). A second scheme for the driver, for a flavor
   or a second dialect, keeps its own `Name` and `Dialect`, and its
   generator returns the registered name of the package (D98).
2. The `dbmeta` session measures its model on the new package, as dbmeta D93
   did when Cassandra moved to `xo/cql`. `dbmeta` passes before a release of
   `usql` uses the driver.
3. The `usql` session changes `usql/drivers/<name>/<name>.go` to import the
   driver, and removes any code that worked around a fault of the old driver.
   The `Version` function for Couchbase called `strconv.Unquote`, which is one
   example.

Gate: the document holds both answers for the version, and the work item
holds the text of each request.

### 17. Write it down

- Mark the target done in [TARGETS.md](TARGETS.md): its row names the
  package, as `` `github.com/xo/dbimp/<driver>` ``.
- Make `docs/<PRODUCT>.md` complete.
- Mark the work item done in [BACKLOG.md](BACKLOG.md) after step 20.

Gate: `TestEveryDriverIsATarget`, the tests for the documents, and every
test in the table at the end of this file pass, except
`TestEveryDriverIsComparedWithTheFirst`, which is the gate of step 17a.
Stage the work.

### 17a. Compare it with the first driver

`couchbase` is the first driver, and every later driver is compared with it
(D97). This is the last review before the commit of step 18. Write it after
step 17, from the staged code and documents, and write it again when either
one changes.

Add the section `## Compared with Couchbase` at the end of
`docs/<PRODUCT>.md`, with two parts:

- `### The server`: a table of the interface of Couchbase against the
  interface of the product. It has a row for each of these: the request, how
  a database is chosen, the language, DDL, parameters, the framing of a
  result, where the columns come from and their order, errors, types, cancel,
  transactions, authentication and the default port.
- `### The driver`: a table of the package `couchbase` against the new
  package. It has a row for each of these: the size, rounded to the hundred
  lines and dated, because no test keeps it current, `Config`, options for
  one statement, arguments, rows, the types of the columns, values, the
  result of `Exec`, transactions, the reset of a session, cancel, errors,
  authentication, and anything else that the package exports.

Under each table, list each difference that a caller of `database/sql` sees,
and name the decision that gives its reason. A difference that no decision
explains is a question for Ken. He decides whether the new driver changes,
or a decision records why it differs. Cite a fact of the server by its
measurement, as the rest of the document does, and a fact of the code by its
file.

Then show the comparison to Ken, with the list of the differences that no
decision explains.

Gate: `TestEveryDriverIsComparedWithTheFirst` passes, and Ken reviewed the
comparison in the conversation. His review is not his approval of step 18.

## Before you call it done

```bash
gofmt -l . && go vet ./... && go build ./... && go test -race -count=2 ./...
golangci-lint run ./...
```

`gofmt -l .` must print nothing. Then stage the work and stop. Ken reviews
the staged changes, with the comparison of step 17a. Steps 18 to 20 follow only when he approves each one.

## Releasing it

### 18. Commit and push

Commit the staged changes and push them only when Ken says so. If you change
anything after he reviewed it, stage it and ask him again. Write the commit
message by the `simple-english` skill, and do not mix the decisions and the
work items in one message.

Gate: the workflow passes on the push, and the URL of the run is in the work
item. If it fails, fix it, stage the fix, and ask Ken again before the next
commit.

### 19. Tag and publish the release

When the workflow has passed on the commit, ask Ken for the version and for
his approval to tag it. The first release of the module was `v0.1.0`, and
`git tag --list 'v*'` names every release. Tag that commit with the version
that Ken named, and push the tag:

```bash
git tag -a <version> -m "<version>"
git push origin <version>
```

Write the release notes from the commits since the last tag, by the
`simple-english` skill. Name each driver that the release adds or changes,
what a consumer must change, and each fault that it fixes. Show them to Ken.
When he approves them, publish the release:

```bash
gh release create <version> --title <version> --notes-file <notes>
```

Gate: `gh release view <version>` shows the release as public, and its URL
is in the work item.

### 20. Send the requests to the consumers

When the release is public, send each request of step 16 to its session, and
name the tag in it, so that each consumer pins the release and not a commit.
Tell each session what changed for it, and what it must measure before it
releases.

Gate: each session reports that its change is staged.

## When it cannot be a driver

Some targets cannot be a driver. To find that out is a result, not a failure.
A target cannot be a driver when one of these is true:

- The server gives no way to learn the columns and their order before the
  first row, and the rules of D18 cannot supply them.
- The result cannot be read one row at a time, and it can be too large to
  hold in memory.
- The server cuts a result short and gives no way to read the rest (D21).
- The only interface needs a package that Ken refused (D13).

Record the evidence in `docs/<PRODUCT>.md`. Write a decision in
[decisions/](decisions/README.md) that says why, and move the target to P3 in
[TARGETS.md](TARGETS.md). A later reader will ask why the target is missing.

## Flavors

Some products speak the same interface as another product. DynamoDB and
ScyllaDB Alternator, Trino and Presto, and Elasticsearch and OpenSearch are
examples. One driver serves the products of such a group, and these steps
change:

- Step 4: each flavor needs its own container entry, because each one has its
  own releases and port.
- Step 6: measure every item on each flavor. A difference goes in the
  document under Flavors.
- Step 9: the driver tells the flavors apart from what the server says, such
  as the version statement, and never from the DSN alone. The version
  statement runs on each flavor and says which one answered (dbmeta D91).
  D78 differs for InfluxDB. With `sqlmode` set to `disable` or `allow`, the
  driver sends no request to learn the release, and the key `version` of
  the DSN names it.
- Step 14: a test skips a difference between flavors with the reason, and
  every flavor runs in the matrix.
- Steps 5a and 14a: `features.json` records a verdict for each flavor, and a
  feature that one flavor lacks has a test of its refusal on that flavor.
- Step 16: a flavor that `dburl` names with a scheme of its own reaches the
  one driver by its registered name, with its own `Dialect` (D98).

## The template for a product document

Each heading below is a section of `docs/<PRODUCT>.md`. Mark every fact
"measured", with the release and the date, or "not measured", with its
source.

1. Summary: the product, the releases measured, their names in `dbrun`, the
   result of R, H and S with the priority, the scheme in `dburl` with its
   `Name`, aliases and generator, and the driver that `usql` uses now.
2. Requests: the endpoints, methods, content types and authentication, and
   how a statement is sent.
3. The DSN: the URL that `dburl` writes (D27), every query key with its
   default, what the path means, and examples.
4. Responses: the framing, and whether it streams. Where the names of the
   columns and their order come from. How a row is encoded. Paging, and every
   cap or default limit. Compression and redirects.
5. Types: the table of step 8a, which step 10 generates, with the kind of
   each type from [TYPES.md](TYPES.md). NULL, missing and empty values. The range
   of numbers. How a decimal, a time, a binary value and a UUID arrive.
6. Parameters: positional, named or none, and the rule if there are none.
7. Transactions: what exists, or the error that `BeginTx` returns.
8. Errors: the status codes, the body, an error with HTTP 200, an error after
   some rows, a limit on the rate, and which errors mean that the request did
   not reach the server.
9. Cancellation and timeouts: what happens when the client disconnects, and
   whether the server can cancel a query.
10. Statements: several in one request, splitting, and comments.
11. Principals: what an ordinary user can and cannot do or read, including
    the version.
12. Flavors: other products behind the same interface, and how the driver
    tells them apart.
13. Interfaces: the table from step 10.
14. Faults: the faults of the driver that `usql` uses now, which this driver
    must not repeat.
15. Second opinions: each lead from step 7, and what the server said.
16. Open questions: each one that waits for Ken, or a pointer to its entry
    in [PLAN.md](PLAN.md).

## The tests that tell you what you forgot

These tests exist so that an unfinished driver fails, rather than passing
quietly. Each gate reads every driver folder, and each problem that it finds
names the step to finish. `gates_self_test.go` holds the gates themselves:
`TestTheGatesPassACompleteDriver` builds a complete driver in a repository of
its own and fails if a gate finds a problem, and
`TestTheGatesCatchAnIncompleteDriver` builds an incomplete one and fails if a
gate finds nothing.

| Test | Fails when |
| --- | --- |
| `TestEveryDriverIsATarget` | the row of a driver in `TARGETS.md` does not name its package, or `TARGETS.md` names a package that has no folder (step 17) |
| `TestEveryDriverHasItsDocument` | a driver has no `docs/<PRODUCT>.md`, or it lacks a heading of the template (step 8) |
| `TestEveryDriverHasItsManifest` | the manifest lacks an item or a principal, names a file that does not exist, or misses a file under `testdata/<driver>/` (step 6) |
| `TestEveryTypeHasAKind` | a type table of a product document has a type whose kind `TYPES.md` does not name (step 8a) |
| `TestTheTypeMatrixIsCurrent` | the table of every driver in `TYPES.md` is not the one that the type tables of the product documents make (step 8a) |
| `TestEveryDriverGeneratesItsTables` | the document lacks the markers of a table, or the tests do not call `TypeTable` and `InterfaceTable` (step 10) |
| `TestEveryDriverRunsTheContract` | the tests of a driver do not call `dbimptest.RunContract` (step 11) |
| `TestNoDriverTouchesGlobalState` | a driver assigns to `http.DefaultTransport`, `http.DefaultClient` or a package variable outside `init`, or calls `io.ReadAll` (step 12) |
| `TestEveryDriverRegistersOneName` | a driver calls `sql.Register` more than once, outside `init`, or with a name that is not its folder (step 12) |
| `TestEveryDriverTestsItsDSN` | a driver has no fuzz test, or no round trip test, for its DSN (step 13) |
| `TestEveryDriverIsComparedWithTheFirst` | the document of a driver other than `couchbase` has no `## Compared with Couchbase`, or that section lacks `### The server` or `### The driver` (step 17a) |
| `TestEveryDriverHasItsFeatures` | the survey asked fewer than two models or no other driver, lacks a statement of CRUD, leaves an entry not measured, marks one `no` with no recorded refusal, names a test that does not exist, has a type whose test does not call `RoundTrip`, or disagrees with the type table (steps 5a, 6, 10 and 14a) |
| `TestTheWorkflowNamesNoRelease` | a workflow names a release, or runs the integration tests without reading the releases from `dbrun` (step 15) |
| `TestEveryDocumentIsInTheTable` | a document in `docs/` is missing from `AGENTS.md` or `README.md` |
| `TestEveryDecisionReferenceExists` | a document points at a decision that does not exist |
| `TestTheDecisionIndexIsComplete` | a decision has no row in `docs/decisions/README.md`, or its row differs from its file |
| `TestAnAmendmentPointsBothWays` | a decision amends another, and the two do not name each other in their status |
| `TestTheRootHoldsFourDocuments` | a Markdown file other than `README.md`, `AGENTS.md`, `CLAUDE.md` and `CONTRIBUTING.md` is in the root |
| `TestClaudeImportsAgents` | `CLAUDE.md` is a link, or holds anything but `@AGENTS.md` |
| `TestEveryTestNameInTheDocsExists` | a document names a test that does not exist |
| `TestEveryLinkResolves` | a link points at a file that does not exist |
| `TestSkillsAreCopies` | a skill in `skills-lock.json` is a link, or its folders under `.agents/skills` and `.claude/skills` differ (D11) |

`TestNoDriverTouchesGlobalState` finds an assignment to a package variable
by a heuristic. It counts an assignment to a name that the function does not
declare itself, so a driver that shadows a package variable in a closure can
pass it. Review the code as well.

Where a table is made from something that the code already knows, generate
it, and test only that the file is current. Generation is stronger than a
test, because a stale table cannot exist. Never write a count in prose unless
a test or a generator makes sure that it is correct.
