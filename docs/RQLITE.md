# rqlite

This file holds what is known about the HTTP API of rqlite, for the driver
`rqlite`, which D73 places seventh after Neo4j. Its work item is W22. The
headings are the template of [DRIVER.md](DRIVER.md).

Steps 5a and 6 measured 9.4.5 and 10.3.6 on 2026-09-30, as `admin` and as
`dbmeta_user`. A fact marked "recorded" is in `testdata/rqlite/`, and the
name in quotes after it is the name of its request in `requests.json` there.
A fact marked "measured" names how it was measured. The two releases gave
the same answer unless a fact says otherwise. Each section starts with the
measured facts. The facts after them come from the sources of step 3, and
each one that is not measured says so. The sources, each read on 2026-09-27,
are these:

- "The API document" is `content/en/docs/API/api/_index.md` in
  `github.com/rqlite/rqlite.io`, at commit `58779997fc7a`. "The consistency
  document", "the bulk document", "the queue document", "the security
  document", "the rewrite document" and "the FAQ" are the files for Read
  Consistency, Bulk API, Queued Writes, Security, non-deterministic
  functions and the FAQ in the same tree.
- "The server source" is `github.com/rqlite/rqlite` at commit
  `fe6ade752262`, after v10.3.6: `http/service.go`, `http/query_params.go`,
  `http/request_parser.go`, `command/encoding/json.go`, `db/db.go`,
  `db/driver.go`, `store/store.go`, `auth/credential_store.go`,
  `Dockerfile`, `docker-entrypoint.sh` and `CHANGELOG.md`. A fact from it is
  what the code says, and not what a server did.
- "gorqlite" is `github.com/rqlite/gorqlite` at commit `50d445fd0ab9` of
  2026-05-04, with its package `stdlib`. This is the pseudo-version
  `v0.0.0-20260504155303` that the `usql` backlog names.
- "pyrqlite" is `github.com/rqlite/pyrqlite` at commit `a978c9f` of
  2026-01-05, the DB-API driver for Python, read on 2026-09-30.
- "rqlite-go-http" is `github.com/rqlite/rqlite-go-http` at commit
  `b9e674c31e86` of 2026-09-08. It has no tag.
- "The dbmeta entry" is `container/rqlite.go` in `dbmeta`.
- "GitHub" and "Docker Hub" are the releases and the tags that each one
  listed.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, asked
  on 2026-09-27 and again on 2026-09-30.

## Summary

- rqlite is a distributed database. Each node runs SQLite, and the nodes
  agree on each write through Raft (the API document). A client sends SQL
  over HTTP to any node.
- The latest release is 10.3.6, of 2026-09-22 (GitHub and Docker Hub). The
  last release of 9 is 9.4.5, of 2026-03-10 (Docker Hub). The changelog of
  10.0.0 says that it has no breaking change to the API (the server source).
  rqlite is under the MIT licence (GitHub).
- 10.3.6 runs SQLite 3.53.4, and 9.4.5 runs SQLite 3.51.2 (recorded: "the
  version").
- R holds. `dbrun` starts `rqlite-9.4.5` and `rqlite-10.3.6`, both in the
  Staged tier with the cadence `tested`, on the image
  `docker.io/rqlite/rqlite` (measured with `dbrun list --json all` on
  2026-09-30). So step 14 runs both.
- H and S hold. `POST /db/query` with `["SELECT sqlite_version()"]` answered
  HTTP 200 with the version, as `admin` and as `dbmeta_user`, on both
  releases (measured with `curl` on 2026-09-30).
- TARGETS.md places rqlite in P1.
- `dburl` has the provisional scheme `rqlite`, with the alias `rq` and the
  default port 4001, the HTTP port. It passes the path and the query through
  (dburl D36, released in dburl `v0.36.0` as dburl D38, read on
  2026-09-29). Nothing of it is settled until step 9 decides the name and
  the URL.
- `usql` has no driver for rqlite. Its backlog lists
  `github.com/rqlite/gorqlite/stdlib` at `v0.0.0-20260504155303` as a driver
  that it can add. So a driver here is new to `usql` (D24).
- `dbmeta` staged a model for rqlite on 2026-09-30 (dbmeta D148). It shares
  the statements of the model for `sqlite3`, and runs through the
  `database/sql` driver of gorqlite until this driver exists. The `dbmeta`
  session reported that gorqlite decodes every number as a `float64`, and
  reads `/status` when it opens, which `dbmeta_user` cannot read.
- The dbmeta entry writes the users into a file that the flag `-auth` names.
  `admin` has the permission `all`, and `dbmeta_user` has `query` and
  `execute` and nothing else. The `url` of each principal is
  `http://user:password@127.0.0.1:<port>` (measured with `dbrun dsn --json`
  on 2026-09-30).

## Requests

These facts were recorded on both releases, as both principals:

- `POST /db/query` with a JSON array of statements runs each one as a read
  (recorded: "a query in the body"). `GET /db/query?q=<statement>` gives the
  same answer (recorded: "a query in the URL").
- A body with `Content-Type: text/plain` is one statement (recorded: "a
  query as text"). A body with no content type is read as JSON (recorded:
  "a query with no content type").
- `POST /db/execute` runs each statement as a write, and `POST /db/request`
  runs reads and writes in one request (recorded: "a read and a write on the
  unified endpoint").
- A write on `/db/query` fails with the error
  `attempt to change database via query operation`, in HTTP 200 (recorded:
  "a write on the query endpoint, refused"). So a statement with `RETURNING`
  fails there too (recorded: "crud: returning on query").
- A `SELECT` on `/db/execute` runs, and returns no rows. It returns the
  `last_insert_id` and the `rows_affected` of an earlier write (recorded: "a
  write on the query endpoint").
- A statement with `RETURNING` returns its rows on `/db/execute` and on
  `/db/request` (recorded: "crud: returning on execute" and "crud: returning
  on request").
- `GET /db/execute` and `GET /db/request` answer HTTP 405 (recorded: "a GET
  of execute" and "a GET of request").
- `GET /` answers HTTP 302. 10.3.6 sends it to `/console/`, and 9.4.5 to
  `/status` (recorded: "the root").
- Every response has the headers `X-Rqlite-Version`, such as `v10.3.6`, and
  `X-Rqlite-Served-By`, which names the node (recorded: every file).
  10.3.6 also sends `Www-Authenticate: Basic realm="rqlite"` on every
  response, and 9.4.5 sends it on none, not even on HTTP 401 (recorded:
  every file).
- A flag counts when its key is present, whatever its value.
  `?timings=false` still adds the timings (recorded: "a flag with the value
  false").
- An unknown `level` is `weak`, with no error (recorded: "an unknown
  level").

These facts come from the sources, and are not measured:

- `/db/request` decides whether each statement is a read or a write with
  `sqlite3_stmt_readonly()` (the API document).
- Each statement is a string, or an array whose first element is the SQL and
  whose other elements are the arguments (the API document).
- The query options are keys in the URL (the API document, the consistency
  document and the server source):
  - `level` is `none`, `weak`, `linearizable`, `strong` or `auto`. `weak`
    is the default.
  - `linearizable_timeout` is a duration, 10 seconds by default.
    `freshness` is a duration, and `freshness_strict` needs it.
  - `associative` asks for each row as an object. `blob_array` asks for a
    BLOB as an array of bytes. `qualify_columns` names each column with its
    table.
  - `transaction` runs the statements of the request in one transaction.
    `queue` queues a write, and `wait` with `timeout` waits for the queue.
  - `timings`, `pretty` and `raft_index` add to the response.
  - `db_timeout` bounds the time in the database. `timeout` bounds a request
    that one node forwards to the leader, and is 30 seconds by default.
    `retries` sets how many times a node tries to reach another node.
  - `redirect` asks for a redirect to the leader in place of forwarding.
  - `norwrandom` and `norwtime` stop the rewrite of `RANDOM()` and of the
    functions of time, and `noparse` stops every rewrite.
- Authentication is HTTP Basic. HTTPS and mutual TLS are flags of the server
  (the security document).
- The default port of the HTTP API is 4001, and Raft uses 4002 (the API
  document, `Dockerfile` and `docker-entrypoint.sh`).

## The DSN

- Step 9 decides the URL (D27 and D35). No endpoint names a database (the
  API document), so the path has no meaning yet.
- gorqlite takes `http[s]://[user[:password]@]host[:port]/[?key=value]`,
  with the keys `level`, `disableClusterDiscovery` and `timeout`. An empty
  host is `localhost:4001` (gorqlite `conn.go`). Its scheme is `http` or
  `https`, which D35 does not allow here.
- pyrqlite takes a host, a port, a user and a password as arguments, and no
  URL (pyrqlite `connections.py`).
- The dbmeta entry gives each principal as
  `http://user:password@127.0.0.1:<port>`. The `dbmeta` session will change
  it to `rqlite://user:password@host:port` when the driver exists (its
  message of 2026-09-30).

## Responses

These facts were recorded on both releases, as both principals:

- The body is one JSON object with `results`, and with `time`,
  `sequence_number` or `raft_index` when the request asks for them
  (recorded: "the timings" and "the raft index").
- Each element of `results` is one statement, in the order of the request.
  A read gives `columns`, then `types`, then `values`, an array of arrays in
  the order of `columns` (recorded: "columns in the order of the
  statement"). So the names and the order of the columns arrive before the
  rows, and rule 1 of D18 applies.
- Two columns can have one name (recorded: "two columns with one name"). A
  column with no alias is named by its text, such as `1 + 1` (recorded: "a
  column with no name").
- A read that finds no rows has `columns` and `types`, and no `values`
  (recorded: "no rows").
- A write gives `last_insert_id` and `rows_affected`. Each is left out when
  it is zero (recorded: "an update that changes no rows").
- A statement that changes no rows itself, such as `CREATE TABLE`, `DROP
  TABLE`, `BEGIN` or a `SELECT`, gives the `rows_affected` and the
  `last_insert_id` of the last write before it on the same connection. After
  an `UPDATE` of two rows, a `CREATE TABLE` and a `DROP TABLE` each gave
  `"rows_affected":2` (recorded: "a DDL after an update"). So `rows_affected`
  is right only for `INSERT`, `UPDATE`, `DELETE` and `REPLACE`.
- An empty statement gives `results` with no element (recorded: "an empty
  statement"). A statement of only a comment gives one empty element
  (recorded: "a statement of only a comment").
- `qualify_columns` names each column with its table on 10.3.6, such as
  `dbimp_types.id`. 9.4.5 ignores it, with no error (recorded: "qualified
  columns").
- The associative form sorts the keys of each row, and keeps one of two
  columns with one name, and `types` is an object (recorded: "the
  associative form" and "the associative form with two columns of one
  name"). So that form loses the order of the columns, which hard rule 3
  forbids.
- A result of 20000 rows came in one response, with no paging and no cap
  (recorded: "a result of 20000 rows").
- A request with `Accept-Encoding: gzip` gets no `Content-Encoding` (recorded:
  "a gzip answer").
- `redirect` gave the answer, with no redirect, because the single node is
  the leader (recorded: "a redirect to the leader").
- A queued write gives an empty `results` and a `sequence_number`, with or
  without `wait` (recorded: "a queued insert" and "a queued insert with a
  wait").

These facts come from the server source, and are not measured:

- The server reads every row of every statement into memory, then encodes
  the whole response with `json.Marshal`, and writes it at once
  (`queryStmtWithConn` and `writeResponse`). So a large result costs the
  server its memory, whatever the driver does.
- With `redirect`, a follower answers HTTP 301 with the leader in
  `Location`. The header `X-RQLITE-SERVED-BY` names the node that served the
  request.

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table,
Ken reviewed it on 2026-09-30 (D140), and step 10 will write it from the
code.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| INTEGER | integer | `int64, for each declared type whose name holds INT, such as BIGINT` | `int64` | `INTEGER` | yes |
| REAL | float | `float64, for each declared type whose name holds REAL, FLOA or DOUB` | `float64` | `REAL` | yes |
| TEXT | string | `string, for each declared type whose name holds CHAR, CLOB or TEXT` | `string` | `TEXT` | yes |
| BLOB | binary | `[]byte, from an array of bytes` | `[]uint8` | `BLOB` | yes |
| NUMERIC | number | `int64 for a number with no fraction that fits, and else float64, for each declared type that no other row names, such as DECIMAL(10,2)` | `interface {}` | `NUMERIC` | yes |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| DATE | date | `dbimp.Date, from the RFC 3339 text of the server` | `dbimp.Date` | `DATE` | yes |
| DATETIME | local timestamp | `dbimp.LocalDateTime, from the RFC 3339 text of the server` | `dbimp.LocalDateTime` | `DATETIME` | yes |
| TIMESTAMP | timestamp | `time.Time, in the offset of the server` | `time.Time` | `TIMESTAMP` | yes |
<!-- /dbimp:types -->

The table follows the rules of affinity of SQLite for the declared type
(D140). A value keeps the storage class that SQLite stored it with, whatever
its declared type. So a value whose JSON form cannot have the Go type of its
column, such as `'abc'` in an `INTEGER` column, has the Go type of its JSON
form: a `string`, a `[]byte` from an array, an `int64` or a `float64` from a
number, or a `bool` (D140). `ANY`, the type of a `STRICT` column that takes
every storage class, is not a supported type, and has no row. A value of an
`ANY` column, and of a column of an expression whose type is `""`, reads by
its JSON form in the same way, with the scan type `interface {}` (D140).

These facts were recorded on both releases, as both principals, from a
table with one column of each declared type (recorded: "every type" and
"every type as arrays of bytes"):

- `types` holds the declared type of each column in lower case, as the
  statement wrote it, such as `bigint`, `varchar(10)`, `decimal(10,2)`,
  `uuid` and `any`. A column with no declared type took the type
  `integer` from its first row.
- A column of an expression takes the storage class of its value in the
  first row, such as `integer`, `real` or `text`. If that value is NULL, the
  type is `""` (recorded: "an expression whose first row is NULL"). A column
  whose values differ by row keeps the type of the first row (recorded: "an
  expression whose types differ by row").
- A value keeps the storage class that SQLite stored it with. An `INTEGER`
  column held the `REAL` 1.5 (recorded: "every type with the storage
  class"), and a `DOUBLE` column held the text `'abc'` (recorded: "every
  type").
- An integer is a JSON integer, and keeps every digit from
  -9223372036854775808 to 9223372036854775807. A literal beyond that range is
  a `REAL` (recorded: "an integer beyond the range").
- A `REAL` is a JSON number. A `REAL` with no fraction has none in the JSON:
  the parameter 1.0 came back as `1`, with the type `real` (recorded: "a
  float parameter with no fraction"). The `NUMERIC` 12345678901234567890 is a
  `REAL` in SQLite, and came back as `12345678901234567000`. So only the
  declared type tells an integer from a `REAL`.
- An infinity makes the whole response fail with HTTP 500 and the text
  `json: error calling MarshalJSON for type *http.DBResults: json:
  unsupported value: +Inf` (recorded: "an infinity" and "a negative
  infinity"). `0.0 / 0` is NULL in SQLite (recorded: "a NaN").
- Text is a JSON string, with every character outside ASCII kept (recorded:
  "every type"). NULL is `null`.
- In a `BLOB` column, a `BLOB` is a string in base64 by default, and an
  array of numbers from 0 to 255 with `blob_array`. Text stored in a `BLOB`
  column is a plain string in both forms. So only `blob_array` tells a BLOB
  from text.
- A BLOB from an expression, such as `SELECT x'80ff'`, has the type `text`,
  and arrives as a string in which each byte that is not UTF-8 is U+FFFD,
  with `blob_array` too (recorded: "a blob that is not text" and "literals
  of each class as arrays of bytes"). The driver cannot get those bytes
  back. `SELECT hex(x'80ff')` gives the text `80FF`.
- A `BLOB` in a column whose type is not text, such as `uuid`, is a BLOB as
  in a `BLOB` column (recorded: "every type as arrays of bytes").
- A column with no declared type names the storage class of its first row.
  When that row holds a BLOB, the type is `text`, and the BLOB arrives as
  text with U+FFFD, as the BLOB of an expression does. When the first row
  holds an integer, the same BLOB in a later row arrives as an array of
  bytes (measured with `curl` on 10.3.6 on 2026-09-30). So a column that
  holds BLOBs needs a declared type whose affinity is not text.
- A `BOOLEAN` column gives a JSON `true` or `false`. The stored integer 2 is
  `true` (recorded: "every type").
- A `DATE`, a `DATETIME` and a `TIMESTAMP` column each give RFC 3339 text,
  from the time that the server parsed:
  - `DATE` `'2026-09-30'` is `2026-09-30T00:00:00Z`.
  - `DATETIME` `'2026-09-30 12:34:56.789'`, which names no zone, is
    `2026-09-30T12:34:56.789Z`.
  - `TIMESTAMP` `'2026-09-30T12:34:56.123456789+05:30'` keeps its offset and
    its nine digits.
  - The integer 1727699696 in a `TIMESTAMP` column is
    `2024-09-30T12:34:56Z`, as Unix seconds.
  - Text that does not parse, such as `'not a date'` or `'yesterday'`, is
    `0001-01-01T00:00:00Z`. The driver cannot get that text back.
    `CAST(dt AS TEXT)` in the statement gives the stored text.
- The functions of time give text, and `unixepoch` an integer (recorded:
  "the functions of time").
- A `STRICT` table refuses a value of the wrong storage class, and its `ANY`
  column keeps the class of its value (recorded: "a wrong type in the strict
  table" and "read the strict table").
- A JSON function gives text. `jsonb` gives a BLOB, which arrives as text
  with the damage above (recorded: "a JSON function").
- SQLite has no decimal type and no UUID type. A decimal and a UUID arrive
  as the text, the number or the BLOB that the statement stored.

These facts come from the sources, and are not measured:

- The server runs SQLite through a fork of `mattn/go-sqlite3`,
  `github.com/rqlite/go-sqlite3` v1.51.0 (the server source). In
  `mattn/go-sqlite3`, a column declared `date`, `datetime` or `timestamp` is
  read as a `time.Time`, and a column declared `boolean` as a `bool` (its
  `sqlite3.go`). That matches what the server sent.
- A value of type `[]byte` becomes text when the declared type has the
  affinity of text, and the empty type counts as text (the server source,
  `isTextType`). That matches the BLOB of an expression.

## Parameters

These facts were recorded on both releases, as both principals:

- A statement binds `?` from the elements after its SQL, in order, and
  `?NNN` by its number (recorded: "positional parameters" and "a numbered
  parameter").
- A statement binds `:name`, `$name` and `@name` from an object after its
  SQL (recorded: "named parameters"). One statement can take positional
  values and an object together (recorded: "a positional and a named
  parameter").
- One body can hold a plain statement and a statement with parameters
  (recorded: "a plain and a parameterized statement"). The bulk document
  says that it cannot, and it is wrong.
- A JSON number with no fraction binds as an integer, up to
  9223372036854775807. Beyond that it binds as a `REAL` (recorded: "a large
  integer parameter" and "an integer parameter beyond the range"). A number
  with a fraction or an exponent binds as a `REAL`. The number 1.0 is sent
  as `1.0` and binds as a `REAL` (recorded: "a float parameter with no
  fraction").
- `true` binds as the integer 1 (recorded: "positional parameters").
- An array of numbers binds as a BLOB (recorded: "a blob parameter as
  bytes").
- A string of the form `x'...'` binds as a BLOB, and so does `X'...'`, and
  `x'zz'` binds as text (recorded: "a blob parameter as a hex literal" and
  "a text parameter that looks like hex"). So a driver cannot send text of
  that form as text through a parameter.
- An object as a positional value binds as NULL, with no error (recorded:
  "an object parameter").
- Too few values is an error, `not enough args to execute query: want 2 got
  1`. Too many values is not an error, and the rest are ignored (recorded:
  "too few parameters" and "too many parameters").
- A `?` in a string or in a comment is not a parameter (recorded: "a
  parameter in a string and a comment").

## Transactions

These facts were recorded on both releases, as both principals:

- With `transaction`, the statements of one request succeed or fail
  together. Two inserts of one key left no row (recorded: "what the failed
  transaction left").
- With no `transaction`, each statement stands alone. The statement after a
  failed one still ran (recorded: "what the request with no transaction
  left").
- A `BEGIN` in one request stays open on the server after the request ends.
  A later write request saw the row that the transaction inserted, a read on
  `/db/query` did not, and a `ROLLBACK` in a later request removed it
  (recorded: "a later write request sees the begin", "read after the begin"
  and "read after the rollback").
- Every write, from every client, runs on one SQLite connection on the
  leader (the FAQ). So a `BEGIN` from one client is open for the writes of
  every other client until one of them ends it. A `PRAGMA foreign_keys = ON`
  also stayed on for a later request (recorded: "schema: the pragma of
  foreign keys on the write connection"). In the first recording of
  2026-09-30, which a later one replaced, it stayed on for the writes of
  `dbmeta_user` after `admin` sent it.
- `BEGIN` inside a request with `transaction` fails with `cannot start a
  transaction within a transaction` (recorded: "begin inside a
  transaction").
- `SAVEPOINT` and `ROLLBACK TO` work inside one request (recorded: "read
  after the savepoint").

These facts come from the sources, and are not measured:

- The API document says that the behavior of `BEGIN`, `COMMIT`, `ROLLBACK`,
  `SAVEPOINT` and `RELEASE` is not defined. The FAQ says that they work, but
  are not supported, because a failure during the transaction can leave the
  cluster in a state that is hard to use.
- A queued write is not in a transaction unless the server starts with
  `-write-queue-tx` (the queue document).
- gorqlite returns a transaction whose `Commit` and `Rollback` do nothing
  (gorqlite `stdlib/sql.go`). D20 forbids that.

## Errors

These facts were recorded on both releases, as both principals:

- An error of SQL comes in HTTP 200, as the key `error` in the element of
  the statement, such as `near "SELEC": syntax error` and `no such table:
  dbimp_nosuch` (recorded: "a syntax error" and "an unknown table").
- The statements after a failed one still run, with no `transaction`. So an
  element can hold an error, and the next element rows (recorded: "an error
  after the rows of a statement").
- An error while a statement makes its rows, such as `integer overflow` in
  the fourth row, gives the error and no rows of that statement (recorded:
  "an error inside the rows"). The server reads every row before it answers,
  so an error never comes after some rows of one statement.
- A body that is not JSON answers HTTP 400 with the text `invalid JSON body`,
  and an empty array answers HTTP 400 with `no statements` (recorded: "a
  body that is not JSON" and "an empty array").
- An infinity answers HTTP 500 (Types).
- A wrong password answers HTTP 401 with no body (recorded: "a wrong
  password").
- No response of the recordings was HTTP 429 or HTTP 503.

These facts come from the server source, and are not measured:

- HTTP 503 with the text `leader not found` comes when a node has no leader.
- HTTP 408 comes when a queued write waits past its timeout.

### The refused credential (D197)

`errors.Is(err, dbimp.ErrAuthentication)` is true for an `*Error` whose
`HTTPStatus` is 401, which the server sends with an empty body for a wrong
password (recorded: "a wrong password").

rqlite sends HTTP 401 with the same empty body and the same
`Www-Authenticate` header when a user has no permission for an endpoint
(recorded: "the status", "the nodes", "a backup" and "the readiness").
Nothing in the answer tells the two causes apart. The driver sends only
`/db/execute`, `/db/query` and `/db/request`, so a 401 that it receives is
treated as a refused credential. A user who lacks the `execute` or `query`
permission also gets a 401 from these endpoints (the server source, not
measured), and the driver cannot tell that case from a wrong password.

The test `TestAuthenticationRqlite` reads the recorded wrong password on both
releases, and an HTTP 400 that does not match, from `testdata/rqlite/`.

## Cancellation and timeouts

These facts were measured on both releases:

- A read stops when the client disconnects. A read that runs for about 20
  seconds used one second of processor time on the server when `curl` gave
  up after one second, and nothing in the six seconds after (measured from
  `/proc/<pid>/stat` of `rqlited` on 2026-09-30). The next request answered
  at once (recorded: "a query after the client left").
- `db_timeout` stops a read with the error `query timeout` (recorded: "a
  long query with a time in the database").
- 9.4.5 ignores `db_timeout` for a read on `/db/request`. A read with
  `db_timeout=500ms` ran to its end there, and stopped at 500 ms on
  `/db/query` and `/db/execute`. 10.3.6 stops it on every endpoint
  (measured with `curl` on 2026-09-30). So the driver also ends the request
  at the timeout (D146).
- A read on `/db/request` also stops when the client disconnects, on both
  releases (measured from `/proc/<pid>/stat` of `rqlited` on 2026-09-30).
- `db_timeout` stops a write with the error `execute timeout`, and the write
  leaves nothing (recorded: "a long computed write with a time in the
  database" and "what the long computed write left").
- An insert of 5000000 rows with `db_timeout=1s` completed (recorded: "a
  long write with a time in the database").

These facts come from the sources, and are not measured:

- A write checks the context before it starts, and then goes through Raft
  (the server source, `Store.Execute`). So a write that has started does not
  stop when the client leaves. Step 6 did not measure it, because a write
  holds the one write connection that every client shares.
- rqlite has no request that cancels a running statement (the API
  document).
- By default, SQL does not time out (the API document).

## Statements

These facts were recorded on both releases, as both principals:

- One request holds many statements, one for each element of the array.
- One string that holds two reads runs both, and gives the result of the
  last one only (recorded: "two statements in one string"). One string that
  holds three writes runs all three, and gives the result of the last one
  (recorded: "two writes in one string" and "what the two writes left").
- A statement that ends with a semicolon runs (recorded: "a statement that
  ends with a semicolon"). A comment is part of the name of a column that
  it follows, such as `1 -- after` (recorded: "comments").
- `PRAGMA journal_mode` is refused on 9.4.5 with `attempt to change database
  via query operation`, and gives `wal` on 10.3.6 (recorded: "feature: a
  pragma that is refused").
- After a read of a table, an index that a later write made on it did not
  appear in the plan of a later read, which gave `SCAN`. With no read of the
  table before the index, the plan used it (measured with `curl` on 10.3.6
  on 2026-09-30).
- `pragma_table_xinfo` and `sqlite_schema`, which the `sqlite3` model of
  `dbmeta` reads, work for both principals (recorded: "schema: the table
  valued pragmas").
- Foreign keys are off. `PRAGMA foreign_keys = ON` turns them on for the
  write connection, where they stay on for every client (Transactions).
  Reads run on another connection, where the pragma stays 0 (recorded:
  "schema: the pragma of foreign keys on the read connection").

These facts come from the sources, and are not measured:

- The server rewrites `RANDOM()`, `RANDOMBLOB(N)` and the functions of time
  in a write, and in a `strong` read, before it writes the statement to Raft
  (the rewrite document).
- Foreign keys are on when the server starts with `-fk`
  (`docker-entrypoint.sh`).

## Principals

These facts were recorded on both releases:

- `dbmeta_user` runs every statement of the recordings that `admin` runs,
  on all three endpoints.
- `dbmeta_user` gets HTTP 401 for `/status`, `/nodes`, `/db/backup` and
  `/readyz`, which `admin` reads (recorded: "the status", "the nodes", "a
  backup" and "the readiness").
- Both principals get the version in the header `X-Rqlite-Version` and from
  `SELECT sqlite_version()`, which gives the version of SQLite. rqlite has
  no function `rqlite_version()` (recorded: "the version" and "a function of
  rqlite for the version").

These facts come from the sources, and are not measured:

- The permissions are `all`, `execute`, `query`, `status`, `ready`,
  `backup`, `load`, `snapshot`, `join`, `join-read-only`, `remove`,
  `leader-ops` and `ui` (the security document).
- `/db/execute` needs `execute`, `/db/query` needs `query`, and `/db/request`
  needs both (the security document).
- The user `*` gives its permissions to every request, with or without
  credentials. A node without `-auth` checks nothing (the security
  document).
- No permission is narrower than an endpoint. A user with `execute` can
  create and drop any table (the security document).

## Flavors

- Gemini and DeepSeek name no other product that speaks the HTTP API of
  rqlite.
- libSQL is a target of its own, with its own protocol (TARGETS.md).

## Interfaces

`rqlite/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1 on /db/query, which checks the credentials and needs only the permission query. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because each request stands alone and the driver has no transactions (D144). |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, a uint64 and the civil types of the root package, which the driver writes itself (D143). |
| `driver.QueryerContext` | yes | A query goes to /db/request, so that a write with RETURNING returns its rows (D142). |
| `driver.ExecerContext` | yes | Exec goes to /db/execute, and gives the counts of the server for a statement that counts rows, and 0 for any other (D142). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time, because the server keeps no prepared statement. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because rqlite shares one write connection among every client (D144). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, so an answer has one result (D142). |
| `driver.RowsColumnTypeScanType` | yes | types names the declared type of each column, whose affinity gives the Go type (D140). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | types names the declared type of each column, such as BIGINT or VARCHAR(10). |
| `driver.RowsColumnTypeLength` | no | types names the length as the statement wrote it, such as VARCHAR(10), and SQLite does not keep to it. |
| `driver.RowsColumnTypeNullable` | yes | The answer does not name NOT NULL, and a column of SQLite holds NULL unless its table forbids it. |
| `driver.RowsColumnTypePrecisionScale` | no | types names the precision as the statement wrote it, such as DECIMAL(10,2), and SQLite does not keep to it. |
<!-- /dbimp:interfaces -->

## Faults

These are faults of the Go clients that a driver here does not repeat.

gorqlite, and its package `stdlib`, which registers `rqlite`:

- It fakes a transaction. `Begin` returns a value whose `Commit` and
  `Rollback` do nothing (`stdlib/sql.go`). D20 forbids that.
- It sends `transaction` on every request unless the caller turns it off
  (`conn.go` and `cluster.go`).
- It reads each response whole with `io.ReadAll` (`api.go`), and keeps every
  row in memory (`query.go`).
- It decodes the values with `encoding/json` into `any`, so each number
  becomes a `float64` (`query.go`). An integer larger than 2^53 loses digits
  (D19). The `dbmeta` session saw this on both releases.
- It does not ask for `blob_array`, so a BLOB reaches the caller as a string
  in base64, which it cannot tell from text (`query.go`, and Types).
- It turns a column declared `date` or `datetime` into a `time.Time` in two
  forms of text, and a number into whole seconds (`query.go`, `toTime`).
- It sends a request to each peer in turn after any failure, including a
  status other than 200 after the server ran the statement (`api.go`,
  `rqliteApiCall`). So a write can run twice.
- `Open` reads `/status` with `context.Background` (`cluster.go`), which
  `dbmeta_user` cannot read (Principals), and the driver has no
  `DriverContext` (`stdlib/sql.go`).
- Its default `http.Client` is a package variable with a timeout of 10
  seconds, shared by every connection (`conn.go`). So a long query fails
  whatever its context says.
- Its DSN is an `http` or `https` URL, so its scheme is not the name of the
  driver (D35).
- It refuses a named parameter, which rqlite takes (`stdlib/sql.go`, and
  Parameters).
- A query always goes to `/db/query`, which is read-only, so a write with
  `RETURNING` through `QueryContext` cannot run (`stdlib/sql.go`, and
  Requests).
- It writes a trace to a writer that a package function sets (`gorqlite.go`,
  `TraceOn`). Hard rule 2 forbids that.
- It depends on the standard library only (`go.mod`).

pyrqlite:

- It sends every read as `GET /db/query?q=`, so the statement is in the URL
  (`cursors.py`).
- It writes every value into the text of the statement, with `paramstyle`
  `qmark`, and escapes a string by doubling its quotes (`extensions.py`).
- It decodes a BLOB from base64 only for the declared types that it knows
  (`extensions.py`).

rqlite-go-http, which has no `database/sql` driver:

- It reads each response whole with `io.ReadAll` (`http.go`).
- It decodes numbers with `UseNumber`, so an integer keeps its digits
  (`http.go`).
- Its default `http.Client` has a timeout of 5 seconds (`http.go`).
- It returns no error for a failed statement unless the caller sets
  `PromoteErrors` (`http.go`).
- A balancer that marks a host bad sends the request again to the next host
  after any error of the transport, which can come after the server received
  it (`http.go`, `doRequest`).
- Its `QueryOptions.Timeout` says that it applies in the database, but it
  sends `timeout`, which the server reads as the timeout for forwarding
  (`options.go`, and the server source).

## Second opinions

Step 7 tested each lead on a server. Gemini and DeepSeek were asked the same
questions on 2026-09-27 and again on 2026-09-30. Gemini timed out on most of
the questions of 2026-09-27, and on three of 2026-09-30.

- A `BEGIN` in one request and a `COMMIT` in a later one. On 2026-09-27
  DeepSeek said that rqlite keeps no transaction across requests, and
  Gemini said that it runs in SQLite but is not supported. On 2026-09-30
  both said that one request cannot see the changes of a `BEGIN` of an
  earlier request. The server showed that a later write request sees them
  (Transactions). Both were wrong.
- A client that disconnects. On 2026-09-30 both said that the server does
  not stop a read. The server stopped it (Cancellation and timeouts). Both
  were wrong.
- `db_timeout` on a long write. Gemini said that it only bounds the wait for
  a lock, and DeepSeek said that it stops the statement. The server stopped
  the write with `execute timeout`. DeepSeek was right.
- `rows_affected` of a statement that changes nothing. Both said that each
  statement gets its own count. The server gave the count of the last write
  (Responses). Both were wrong for DDL, and right for an `UPDATE` that
  changes no rows.
- The bytes of a BLOB from an expression. Both said that no flag returns
  them. The server agrees: `blob_array` did not.
- A large result. Both said that the server builds it in memory and has no
  paging. The recordings and the server source agree.
- `SELECT x'00ff'`. On 2026-09-27 DeepSeek said that it arrives in base64
  with the type `blob`. The server gave text with the type `text`.
- The string `"x'41'"` as a parameter. On 2026-09-27 DeepSeek said that it
  binds as text. The server bound it as a BLOB.
- NaN and infinity. On 2026-09-30 DeepSeek said that they arrive as the
  strings `"NaN"` and `"Inf"`. The server stores NaN as NULL, and fails the
  whole response for an infinity.
- `RETURNING`. Both said that it works. It works on `/db/execute` and
  `/db/request`, and fails on `/db/query`.
- Foreign keys. Both said that they are off unless the server starts with
  `-fk`. The server agrees, and a `PRAGMA` turns them on for the shared
  write connection.
- `rqlite_version()`. DeepSeek named it. The server has no such function.
- Other products. Both named none.
- Go clients. On 2026-09-27 DeepSeek named `github.com/rqlite/rqlite-go` and
  `github.com/rqlite/rqlite-go-sql`. The first exists, with its last push on
  2022-01-06, and the second does not exist (GitHub). Gemini named gorqlite,
  rqlite-go-http, and `github.com/goki/rqlite`, which is a driver for GORM,
  with its last push on 2023-12-21 (GitHub).

Step 8a asked both models on 2026-09-30 to review the mapping of the types:

- Both agreed that a `BOOLEAN` is a `bool`, and that a value whose JSON form
  cannot have the Go type of its column keeps the Go type of its JSON form.
  DeepSeek said that this softens D135, and that an error makes
  ordinary SQLite data unreadable.
- Both proposed a kind of its own, `dynamic`, for `ANY` and for the type
  `""` of an expression. Ken did not take it: `ANY` is not a supported type,
  and each value of such a column reads by its JSON form (D140).
- Both agreed that the driver reports a value that the server damaged as the
  server sends it, and that this document says so. DeepSeek added that the
  document names the infinity and the NaN too.
- Both disagreed with a `time.Time` for all three types of time. Each said
  that the `Z` of a `DATETIME` is made up by the server, so a `DATE` is a
  `dbimp.Date`, a `DATETIME` a `dbimp.LocalDateTime`, and a `TIMESTAMP` a
  `time.Time`. Ken took their view (D140).
- Both disagreed that `UUID` is a number. Gemini said that a declared type
  that no rule names is `dynamic`, and DeepSeek said that it is a string.
  DeepSeek also said that `NUMERIC` and `DECIMAL` are `float64`. SQLite
  gives such a type the affinity `NUMERIC`, which stores 12 as an integer,
  and text that is not a number as text (recorded: "every type with the
  storage class"). So the table keeps `number`, and the fallback keeps the
  text of a `UUID` as a `string`. Ken kept `number` (D140).

## Open questions

- None of step 9. Ken decided D140 to D146 on 2026-09-30.
- rqlite has no namespace. The recordings name each table with the prefix
  `dbimp_`. How an integration test keeps its tables apart is a question of
  step 14.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-09-30 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of rqlite from the sections above.

### The server

| | Couchbase | rqlite |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /db/request`, `/db/execute` or `/db/query`, with an array of statements, and the options as keys of the URL (Requests) |
| Database | The key `query_context` of the body | None. rqlite has one database and no namespace |
| Language | SQL++, which is close to SQL | The SQL of SQLite |
| DDL | In SQL++ | In SQL. Each write goes through Raft, on the one write connection of the leader (Transactions) |
| Parameters | `?`, `$1` and `$name` | `?`, `?NNN`, `:name`, `$name` and `@name`, bound by the server. A string of the form `x'...'` binds as a BLOB (Parameters) |
| Framing | One body for the whole result, which does not page | One body for the whole answer, built in memory after the statement ends. No paging (Responses) |
| Columns | `signature`, before the first row | `columns` and `types`, before `values` |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement. The associative form sorts the columns (Responses) |
| Errors | Can come with HTTP 200, after some rows | HTTP 200 with `error` in the result, and no rows of that statement, because the server reads every row first (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, with the declared type of each column. A value keeps its storage class whatever that type is. A BLOB is an array of bytes with `blob_array`, and a date, a time and a boolean come in forms of the server (Types) |
| Cancel | The server stops a query when the client leaves | The server stops a read when the client leaves, and `db_timeout` stops a read and a write, except a read on `/db/request` on 9.4.5 (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | `transaction` makes one request atomic. A `BEGIN` stays open for every client of the server (Transactions) |
| Authentication | Basic, or `creds` in the body | Basic |
| Default port | 8093, or 18093 with TLS | 4001 |

The differences that a caller sees:

- A query has no database, and `WithDatabase` fails (D141).
- A query goes to `/db/request`, so that a write with `RETURNING` returns
  its rows, and needs the permissions `query` and `execute` (D142).
- The server reads every row before it answers, so an error never comes
  after some rows of one statement (D142).
- The driver has no transactions, because a `BEGIN` would be open for every
  other client of the server (D144).
- A statement ends at its deadline on the server, through `db_timeout`, and
  on the client when `WithTimeout` sets it (D145 and D146).

### The driver

| | `couchbase` | `rqlite` |
| --- | --- | --- |
| Size, without tests, on 2026-09-30 | About 1300 lines in 8 files | About 1600 lines in 9 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `User`, `Password`, `Level`, `Freshness` |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, `WithLevel` and `WithFreshness`, through `WithOptions` or an argument (D109). `WithTimeout` sends `db_timeout` and ends the request at that time (D146). `WithReadonly(true)` sends the statement to `/db/query`. `WithDatabase` gives `dbimp.ErrNotSupported`. `WithParameter` sets any key of the URL |
| Arguments | Sent to the server as `args` and `$name` | Sent to the server as the values after the statement, and an object of the named ones. A float is sent with a fraction, and a string of the form `x'...'` is written into the text as a literal (D143) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | `dbimp.ArrayRows` from the root package, after the driver reads `columns` and `types` (`rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the declared type, by the rules of affinity of SQLite, and `ColumnTypeNullable`, which says that every column can be NULL (D140) |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the affinity of the column: `int64`, `float64`, `string`, `[]byte`, `bool`, `dbimp.Date`, `dbimp.LocalDateTime` and `time.Time`. A value of another storage class keeps the Go type of its JSON form (D140) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` from the server for a statement that counts rows, and 0 for any other (D142) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D144) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The request carries the context. Its deadline goes as `db_timeout`, and `WithTimeout` bounds the request too (D145 and D146) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Message}`, which unwraps to `*dbimp.StatusError` for a status that is not 2xx. SQLite gives no code of an error in the answer |
| Authentication | Basic | Basic, and none for a DSN with no user |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, and `LevelNone`, `LevelWeak`, `LevelLinearizable`, `LevelStrong` and `LevelAuto` |

The differences that a caller sees:

- A value has the Go type of the affinity of its column, and a value of
  another storage class keeps the Go type of its JSON form, where Couchbase
  gives JSON shapes (D140).
- A BLOB of an expression, and a date that the server did not parse, reach
  the caller as the server sends them, damaged (Types).
- A `[]byte` argument is sent as an array of bytes, where Couchbase sends it
  as base64 (D44 and D143).
- `RowsAffected` is 0 for a statement that counts no rows, where Couchbase
  counts every statement by `mutationCount` (D142).
- An error has no code, only the message of SQLite (Errors).
