# TDengine

This file holds what is known about the REST interface of TDengine, for a
driver that D73 places fifth after Neo4j. Its work item comes when its turn
comes. The headings are the template of [DRIVER.md](DRIVER.md).

This is the draft of step 3. No server has run for this driver yet, so every
fact here is "not measured" and names its source. R is the exception, because
`dbrun` measured it (Summary). The sources, each read on 2026-09-27, are
these:

- "The REST reference" is
  `docs/en/10-developer-guide/08-connectors-reference/10-rest-api.mdx` in
  `github.com/taosdata/TDengine`, on the branch `main`.
- "The adapter reference" is
  `docs/en/12-operations-and-tooling/03-components/03-taosadapter.md` in the
  same repository.
- "The SQL reference" is the pages under `docs/en/05-tdengine-sql/` in the
  same repository: `01-datatype.md`, `07-user-and-privilege/`,
  `08-cluster-management/04-recovery.md`, `09-system-info/`,
  `11-appendix/01-escape.md`, `11-appendix/02-limit.md` and the functions in
  `04-data-query/03-function.md`.
- "The Docker documents" are
  `docs/en/04-quick-start/01-download-and-install/01-docker.md` and
  `docs/en/12-operations-and-tooling/02-operations/03-deployment/02-docker.md`.
  "The entrypoint" is `packaging/docker/bin/entrypoint.sh` and
  `packaging/docker/Dockerfile`, in the same repository.
- "The adapter code" is `github.com/taosdata/taosadapter` at commit
  `d06f6d42` on `main`: `controller/rest/restful.go`, `auth.go`,
  `error_resp.go` and `middleware.go`, and `tools/ctools/block.go`,
  `tools/jsonbuilder/stream_float.go` and `tools/layout/time.go`. This is
  the code of the server, and not a document.
- "The Go connector" is `github.com/taosdata/driver-go/v3` v3.8.2, the
  official Go connector. Its REST form is the package `taosRestful`, with
  the decoder in `common/restful.go` and the placeholders in
  `common/sql.go`.
- "The dbmeta entry" is `container/tdengine.go` in `dbmeta`. It is staged,
  and not committed.
- "GitHub" and "Docker Hub" are the releases and the tags that each one
  listed.
- "DeepSeek" is `deepseek-flash`. "Gemini" is `gemini-3.8-flash`, and
  "Gemini Pro" is `gemini-3.1-pro-preview`.

## Summary

- TDengine is a database for time series, with a dialect of SQL. Its REST
  interface is served by taosAdapter, a separate process, on port 6041 (the
  REST reference and the adapter reference).
- The Community Edition is under the AGPL 3.0 licence (GitHub and the
  dbmeta entry). The Enterprise Edition is a separate image, under Flavors.
- The newest release on GitHub is `ver-3.4.1.6`, from 2026-04-30 (GitHub).
  The newest tag of the image `tdengine/tsdb` is `3.4.2.8`, from 2026-08-31
  (Docker Hub). The two sources disagree.
- R: yes. The dbmeta entry names the releases `3.3.8.8` and `3.4.2.8` of the
  image `docker.io/tdengine/tsdb`, both in the Tested tier, and each one
  passed `dbrun test` (below).
- H: yes. `POST /rest/sql` takes a statement and returns JSON (the REST
  reference). Not measured.
- S: yes. The statement is the SQL of TDengine (the SQL reference). Not
  measured.
- The priority is P1 (D17 and [TARGETS.md](TARGETS.md)).
- `dburl` has the provisional scheme `tdengine`, with the alias `td` and the
  default port 6041, the REST port. It passes the path and the query through
  (dburl D36, released in dburl `v0.36.0` as dburl D38, read on
  2026-09-29). Nothing of it
  is settled until step 9 decides the name and the URL.
- `usql` has no driver for TDengine. Its backlog lists
  `github.com/taosdata/driver-go/v3` v3.8.2, from 2026-07-09, as a driver
  that it can add (`usql/docs/BACKLOG.md`, "Tier 3"). So this driver
  replaces no driver of `usql` (D24).
- The Go connector registers `taosRestful` for REST, `taosWS` for the
  WebSocket interface and `taosSql` for the native client, which needs cgo
  through its package `wrapper` (the Go connector).
- `dbrun` has an entry, staged in `dbmeta` for Ken's review and not committed,
  from dbmeta D112. The `dbmeta` session reported on 2026-09-28 that each
  Tested release passed `dbrun test`: `tdengine-3.3.8.8` and
  `tdengine-3.4.2.8`, on the image `tdengine/tsdb`. On both releases,
  `dbmeta_user`, with `SYSINFO 0`, can make and drop users, because the
  Community Edition has no `GRANT`. So it has fewer rights than `root` only in
  that it cannot read the state of the server, such as `ins_dnodes`.

## Requests

- A statement is `POST /rest/sql` or `POST /rest/sql/<db>` (the REST
  reference). The body is the statement as plain text. The REST reference
  shows `Content-Type: text/plain` in one example and no content type in
  the others. The adapter code reads the raw body and trims the space
  around it, and does not read the content type.
- `<db>` names the default database for the statement. The REST reference
  says that the interface keeps no state, so `USE` has no effect, and a
  table needs the name of its database unless the path names one. The
  adapter code runs ``use `<db>` `` before the statement and ignores its
  error, so that `CREATE DATABASE` works on that path.
- The query parameters are these (the REST reference, and the adapter code
  where it says so):
  - `tz`, a zone of IANA, such as `America/New_York`, for the times in the
    response.
  - `req_id`, a number that traces the request. The adapter code refuses a
    value that is not an integer with HTTP 400, and makes one when it is
    absent or 0.
  - `row_with_meta`, a boolean, from 3.3.2.0. When it is true, each row is
    an object keyed by the names of the columns, in place of an array.
  - `conn_tz`, `app` and `ip`, which set options of the connection to
    taosd (the adapter code). `conn_tz` wins over `tz`. The Go connector
    sends `app` with the name of its process, and `conn_tz`.
  - `timing=true`, which adds a field `timing` in nanoseconds to the
    response (the adapter code).
  - `token`, a token of TDengine Cloud (the Go connector and the adapter
    code).
- Authentication is by the header `Authorization` (the REST reference):
  - `Basic`, with the user and the password.
  - `Bearer`, with a token that `CREATE TOKEN` makes. It exists from
    3.4.0.0, and the SQL reference calls token management a feature of the
    Enterprise Edition.
  - `Taosd`, with the value that `GET /rest/login/<user>/<password>`
    returns in `desc`. In the adapter code, that value is the user and the
    password, encrypted with DES under a key that is written in the source,
    and then in base64. So a person who has it can read the password.
- The adapter code keeps each value of `Authorization` that it decoded in a
  cache for 30 minutes.
- `GET /-/ping` returns HTTP 200 when taosAdapter is below its limits of
  memory, and HTTP 503 when it is above them (the adapter reference).
- `rejectQuerySqlRegex` makes taosAdapter refuse each statement that matches
  a pattern and does not start with `insert`, with HTTP 403, from 3.3.6.34
  and 3.4.0.0 (the adapter reference).
- The dbmeta entry sends each statement with `curl -d`, as root and as the
  ordinary user, for its `ready` step and for `Init`.

## The DSN

- Step 9 decides the URL (D27 and D35). `dburl` has the provisional scheme
  `tdengine` (dburl D36). D26 names the package for its database, so the
  name is `tdengine`, and `dburl` keeps any alias, such as `td` or `taos`
  (D5 and D28). Not decided.
- The Go connector takes the form of the MySQL driver:
  `user:password@http(host:port)/db?key=value` (`taosRestful/dsn.go`). Its
  keys are `interpolateParams`, `disableCompression`, `readBufferSize`,
  `token`, `skipVerify`, `timezone` and `bearerToken`. It defaults to the
  user `root`, the password `taosdata`, the host `127.0.0.1` and the port
  6041 (`common/const.go`, `common/restful.go` and `connector.go`). D35 does
  not allow that form here.
- The `dsn` field of the dbmeta entry is `http://<user>:<password>@127.0.0.1:<port>`,
  with no path. The `url` field in the form of `dburl` is not known until
  step 9 decides the URL.

## Responses

- A result is one JSON object with `code`, `column_meta`, `data` and `rows`
  (the REST reference). The adapter code writes the fields in that order,
  so the columns arrive before the first row, and rule 1 of D18 applies.
- `column_meta` holds one array for each column: its name, the name of its
  type and its length. For a `DECIMAL`, the type is `DECIMAL(p,s)` and the
  length is 8 or 16 (the REST reference and the Go connector). It holds no
  flag for NULL.
- `data` holds one array for each row, in the order of `column_meta`. With
  `row_with_meta=true`, each row is an object (the REST reference). An
  object repeats the name of each column in each row, and a statement can
  return two columns with the same name. Not measured whether it does.
- `rows` is the count of the rows in `data`.
- A statement that writes returns
  `{"code":0,"column_meta":[["affected_rows","INT",4]],"data":[[n]],"rows":1}`
  (the REST reference and the adapter code).
- The response streams. The adapter code sends
  `Content-Type: application/json; charset=utf-8` and
  `Transfer-Encoding: chunked`, fetches the result from taosd one block at
  a time, and flushes when its buffer holds more than 16352 bytes. So a
  driver can read it one token at a time (D25).
- `restfulRowLimit` caps the rows of a REST result. It is -1 by default,
  which is no cap (the adapter reference). In the adapter code, the result
  ends at the cap with `code` 0, and nothing in the body says that rows are
  missing (D21).
- The response has no paging and no cursor (the REST reference).
- The Go connector sends `Accept-Encoding: gzip` when `disableCompression`
  is false, and reads a gzip body. Its default is true. Not measured
  whether taosAdapter compresses.
- A redirect is not measured. The REST reference writes `curl -L` in each
  example.

## Types

Step 10 writes the type table. The REST reference names these types in
`column_meta`: `NULL`, `BOOL`, `TINYINT`, `SMALLINT`, `INT`, `BIGINT`,
`FLOAT`, `DOUBLE`, `VARCHAR`, `TIMESTAMP`, `NCHAR`, `TINYINT UNSIGNED`,
`SMALLINT UNSIGNED`, `INT UNSIGNED`, `BIGINT UNSIGNED`, `JSON`,
`VARBINARY`, `GEOMETRY`, `DECIMAL(p,s)` and `BLOB`. `BINARY` is another
name for `VARCHAR` (the SQL reference). The SQL reference names no UUID
type.

How each value arrives, from the adapter code, with the REST reference
where it agrees:

- NULL is `null`. The adapter code also writes `null` for a `FLOAT` or a
  `DOUBLE` that is NaN or an infinity. So on the wire a NaN and a NULL are
  one value.
- `BOOL` is `true` or `false`.
- Each integer type is a JSON number. `BIGINT UNSIGNED` reaches 2^64-1 (the
  SQL reference), so it must not pass through `float64` (D19).
- `FLOAT` and `DOUBLE` are JSON numbers, in the shortest form that reads
  back as the same value, and in the form with an exponent below 1e-6 and
  from 1e21.
- `VARCHAR` and `NCHAR` are JSON strings. `VARCHAR` holds single bytes, and
  the SQL reference says to keep it to printable ASCII. Not measured what
  the adapter writes for a byte that is not UTF-8.
- `TIMESTAMP` is a string in the form of RFC 3339, with 3, 6 or 9 digits of
  a second, by the precision of the database: `ms`, `us` or `ns`. It is in
  UTC, or in the zone of `tz` or `conn_tz`. The REST reference says only
  RFC 3339, in UTC unless `tz` is set.
- `DECIMAL` is a JSON string of its digits, with the scale of the column. It
  is `DECIMAL64` with 8 bytes up to a precision of 18, and 16 bytes up to 38
  (the SQL reference). A decimal is an `apd.Decimal` here (D33).
- `VARBINARY`, `GEOMETRY` and `BLOB` are strings of lower case hex, with no
  `\x` (the REST reference). `GEOMETRY` is in the form WKB.
- `JSON` is the JSON text of the value, written into the row as a JSON
  value, and not as a string. It exists only for a tag (the SQL reference).
- The length in `column_meta` is the declared length, such as 4194304 for a
  `BLOB` (the REST reference).

## Parameters

- The REST API binds no parameters (DeepSeek and Gemini). The REST
  reference names none, and the Go connector refuses `Prepare` with the
  error `restful does not support stmt`. So the driver uses the parser for
  placeholders in the root package (D34), with the escaper for the literals
  of TDengine.
- A string is in single quotes, and the escapes are `\'`, `\"`, `\n`,
  `\r`, `\t`, `\\`, `\%` and `\_` (the SQL reference). A backslash before any
  other character is dropped, except in `\x`. `\%` and `\_` stay as two
  characters outside `LIKE`.
- An identifier is in backticks. It keeps its case, and it cannot hold `.`
  (the SQL reference).
- `VARBINARY` and `BLOB` take a literal that starts with `\x` (the SQL
  reference).
- A statement is at most 4194304 characters by default, and `maxSQLLength`
  raises it to 64 MB (the SQL reference).
- The WebSocket interface binds parameters (the adapter reference,
  DeepSeek and Gemini). It is not a request over HTTP for each statement,
  as Flavors says.

## Transactions

- TDengine has no `BEGIN`, `COMMIT` or `ROLLBACK` for a user (DeepSeek and
  Gemini). The pages of the SQL reference that this draft read name none.
  The Go connector refuses `BeginTx` with
  `restful does not support transaction`.
- `SHOW TRANSACTIONS` and `KILL TRANSACTION` exist (the SQL reference). Not
  measured whether they concern a statement of a user.
- So `BeginTx` returns an error (D20).

## Errors

- An error is `{"code": <n>, "desc": "<text>"}` (the REST reference). The
  adapter code adds `timing`, in nanoseconds, to each error that it writes.
  The code is masked to 16 bits.
- HTTP 200 carries most errors of taosd by default (the REST reference, the
  adapter code and the dbmeta entry, which says that an error still answers
  HTTP 200). The setting `httpCodeServerError`, from 3.0.3.0, maps them to
  400, 401, 502, 503 and 500 (the REST reference).
- Whatever the setting, the adapter code sends these:
  - HTTP 400 for a bad query parameter or an empty body.
  - HTTP 401 for a missing, malformed or unknown `Authorization` header.
  - HTTP 403 for a statement that `rejectQuerySqlRegex` refuses, and for an
    address that the allow list refuses.
  - HTTP 503 when taosAdapter is above its limits of memory, when the pool
    of connections to taosd times out or is full, and when the limit of
    concurrent queries is reached (the adapter reference, from 3.3.6.29 and
    3.3.8.3).
- A wrong password: the REST reference says HTTP 401 even with
  `httpCodeServerError` false. The adapter code sends the error of the
  connection through the same path as an error of taosd, which is HTTP 200
  with that setting false. The two disagree.
- An error after some rows is lost. The adapter code writes `code` 0 before
  the rows. When a fetch from taosd fails, it stops, closes `data`, writes
  `rows` with the count so far, and closes the object. So the body is valid
  JSON that looks complete (D21). DeepSeek said that the client sees JSON
  that is cut short, which the code does not show.
- The error codes that mean a statement did not parse include `0x0216`,
  `0x021B` and `0x2600`. The codes of authentication include `0x0357` (the
  REST reference).
- The adapter code has a response for HTTP 429. Not measured where it is
  sent.

## Cancellation and timeouts

- The loop that fetches the result does not read the context of the HTTP
  request (the adapter code). When the client leaves, the next flush fails,
  the loop stops, and the adapter frees the result in taosd. A query that
  has not returned its first block yet does not stop. Not measured whether
  taosd stops a query when its result is freed.
- DeepSeek and Gemini said that a client that leaves does not stop the
  query on the server.
- `SHOW QUERIES`, or `performance_schema.perf_queries`, lists the running
  queries with `kill_id`, `query_id`, `conn_id`, `user`, `app` and `sql`.
  `KILL QUERY '<kill_id>'` stops one (the SQL reference). Not measured
  whether `query_id` is the `req_id` of the request.
- In the Enterprise Edition from 3.4.0.0, `SHOW QUERIES` and `KILL QUERY`
  are privileges of their own (the SQL reference). Not measured whether an
  ordinary user of the Community Edition with `SYSINFO 0` can run either.
- The adapter has no timeout for a statement. `pool.waitTimeout`, 60
  seconds by default from 3.3.3.0, bounds the wait for a connection to
  taosd, and `request.default.queryWaitTimeout`, 900 seconds, bounds the
  wait for the limit of concurrent queries (the adapter reference).

## Statements

- The adapter code passes the whole body to taosd as one call. The sources
  disagree about several statements in one body. DeepSeek said that they
  run in order and that the response holds the result of the last one.
  Gemini said that one body takes one statement.
- DeepSeek and Gemini said that taosd takes the comments `--` and `/* */`.
  Not measured.
- A statement that is empty after the space is trimmed is HTTP 400 (the
  adapter code).

## Principals

- The administrator is `root`. Its password is `taosdata`, or the value of
  `TAOS_ROOT_PASSWORD` on the first start of the image (the entrypoint and
  the Docker documents).
- `CREATE USER <name> PASS '<password>' SYSINFO 0` makes a user that cannot
  read the state of the server, such as the nodes and the configuration (the
  SQL reference). A password is 8 to 255 characters, and must mix three
  kinds of character unless `enableStrongPassword` is 0.
- `GRANT` works only in the Enterprise Edition. In the Community Edition it
  does nothing before 3.4, and from 3.4 it is an error (the SQL reference
  and the dbmeta entry). `CREATEDB` is a feature of the Enterprise Edition
  from 3.3.2.0 (the SQL reference).
- The dbmeta entry makes the ordinary user `dbmeta_user` with `SYSINFO 0`,
  and that user makes the database `dbmeta` itself. It says that on 3.3.8.8
  and 3.4.2.8 the ordinary user can make and drop users, and cannot read
  `ins_dnodes`, measured by the `dbmeta` session on 2026-09-28. So in the
  Community Edition the ordinary user differs from root only in the state
  of the server.
- DeepSeek said that an ordinary user of the Community Edition needs a
  `GRANT` to make a database. The SQL reference and the dbmeta entry
  disagree.
- The version is `SELECT SERVER_VERSION()` (the SQL reference), and the
  `ready` step of the dbmeta entry runs it as root. `usql` has no
  statement for the version of TDengine. Not measured for the ordinary
  user. DeepSeek said that any user can run it.

## Flavors

- The Community Edition is the image `tdengine/tsdb`. Its newest tags are
  `3.4.2.8`, from 2026-08-31, and `3.3.8.8`, from 2025-12-02 (Docker Hub).
  The Docker documents say that the image `tdengine/tdengine` took this name
  from 3.3.7.0. That image stopped at `3.3.6.13`, from 2025-06-30 (Docker
  Hub).
- The Enterprise Edition is the image `tdengine/tsdb-ee`, whose newest tag
  is `3.4.2.13`, from 2026-09-27 (Docker Hub). The Docker documents start it
  with no licence step. Not measured whether it needs a licence to run.
  HTTPS on taosAdapter, tokens, `CREATEDB` and `GRANT` are features of the
  Enterprise Edition (the adapter reference and the SQL reference).
- TDengine Cloud takes the query parameter `token` (the Go connector). It
  is only a cloud service, so it fails R.
- taosAdapter runs in both images by default. `TAOS_DISABLE_ADAPTER=1` turns
  it off (the entrypoint). The image exposes 6030 for taosd, 6041 for
  taosAdapter, 6043 for taosKeeper and 6060 for taosExplorer (the
  entrypoint). The dbmeta entry turns off taosKeeper, taosExplorer and the
  reports to the vendor.
- The WebSocket interface of taosAdapter is `GET /ws` on port 6041, and
  `/rest/ws` for an older form (the adapter code). It runs SQL, binds
  parameters, writes without a schema and subscribes to data (the adapter
  reference). The Go connector reaches it through `taosWS`, which needs
  `gorilla/websocket`. It is a connection of WebSocket, and not a request
  over HTTP for each statement (D14).
- Step 9 decides how the driver tells the editions apart from what the
  server says (DRIVER.md, "Flavors").

## Interfaces

Step 10 writes the interface table from the code.

## Faults

`usql` uses no driver for TDengine. These are faults of the REST form of
the Go connector, `taosRestful`, which a driver here does not repeat (the Go
connector):

- It registers `taosRestful`, and its DSN is the form of the MySQL driver
  and not a URL whose scheme is the name of the driver (D28 and D35). A key
  that it does not know goes into `Params` and is ignored, and a pair with
  no `=` is skipped (`dsn.go`).
- `Open` calls `Connect` with `context.Background` (`driver.go`).
- `Connect` writes its defaults into the `Config` that every connection
  shares (`connector.go`). Two connections that open at the same time write
  it together.
- Each connection builds its own `http.Transport`, and `Close` sets its
  fields to nil and never releases the idle connections (`connection.go`).
- `Ping` returns nil and sends nothing (`connection.go`).
- `QueryContext` decodes the whole result into a slice before it returns
  the rows, and `Rows.Close` does nothing (`common/restful.go` and
  `rows.go`). On a status that is not 200, it reads the whole body with
  `ioutil.ReadAll`, and after a result it drains the rest of the body with
  `ioutil.ReadAll` (`connection.go`).
- Its errors do not wrap: `fmt.Errorf("server response: %s - %s", ...)` puts
  the body into the text, and `errors.New("wrong result")` names nothing
  (`connection.go`).
- A `JSON` value is returned as its raw bytes, and it is read before the
  test for NULL. So a NULL tag becomes the bytes `null`, and not nil (D8 and
  `common/restful.go`).
- A `DECIMAL` is returned as a string (`common/restful.go`).
- The decoder of hex panics on a character outside `0-9` and `a-f`
  (`hexCharToDigit` in `common/restful.go`).
- The decoder of `data` indexes the types that `column_meta` gave, so it
  depends on `column_meta` first in the object (`common/restful.go`).
- `ExecContext` asserts that the count of rows is an `int32`, and panics if
  it is not (`connection.go`).
- `ColumnTypeLength` always returns false for `ok`, because it never sets
  it (`rows.go`).
- It puts the placeholders into the text itself, and `interpolateParams` is
  true by default (`dsn.go` and `common/sql.go`). It writes a `string` and a
  `[]byte` with no quotes and no escapes, which lets a value change the
  statement. It writes a float with `%f`, which keeps six digits after the
  point. It counts each `?` in the text, including a `?` in a literal or a
  comment.
- It puts the token of TDengine Cloud into the query of the URL with no
  escape, and it sends the name of its process from `os.Args` as `app`
  (`connection.go` and `common/process.go`).
- It sets `Accept-Encoding` itself when compression is on (`connection.go`).
- It depends on `google/uuid`, `gorilla/websocket` and
  `json-iterator/go` (`go.mod`), where D13 allows the standard library and
  apd.
- It takes the zone of a time from its key `timezone`, and not from the
  server (`dsn.go`).

## Second opinions

Step 7 tests each lead on the server. These are the leads from the models,
asked on 2026-09-27, with where each one disagrees with a source:

- Several statements in one body: DeepSeek said that they run in order and
  that the response holds the last result. Gemini said that a body takes one
  statement. The two disagree.
- Comments: DeepSeek and Gemini said that `--` and `/* */` work.
- Parameters: DeepSeek and Gemini said that REST binds none and that the
  WebSocket interface binds `?` through STMT. This agrees with the Go
  connector.
- Transactions: DeepSeek and Gemini said that none exist.
- Cancellation: DeepSeek and Gemini said that a client that leaves does not
  stop the query, and that `SHOW QUERIES` and `KILL QUERY` stop it.
  DeepSeek said that `KILL QUERY` needs root or a system privilege, and
  gave `KILL QUERY <query_id>`. The SQL reference gives
  `KILL QUERY '<kill_id>'`.
- An error after some rows: DeepSeek said that the body is cut short. The
  adapter code writes a complete body and drops the error. Gemini Pro did
  not know.
- Values: DeepSeek said that NaN is the string `"NaN"`, that a `JSON` tag is
  a string that holds JSON text, and that `VARBINARY` is base64. The adapter
  code writes `null` for NaN and the JSON value itself, and the REST
  reference and the adapter code write hex. Gemini Pro said that NaN is
  `null`, which agrees with the adapter code. It also said that a
  nanosecond time is `YYYY-MM-DD HH:MM:SS.nnnnnnnnn`, and the adapter code
  writes RFC 3339 with a `T`. DeepSeek said that `DECIMAL` is a string,
  which agrees with the adapter code.
- Principals: DeepSeek said that an ordinary user of the Community Edition
  needs a `GRANT` to make a database. The SQL reference says that `GRANT`
  does nothing there, and the dbmeta entry makes the database as the
  ordinary user.
- The token of `/rest/login`: Gemini Pro said that it is the base64 of
  `user:password`. The adapter code encrypts it with DES under a fixed key
  before base64. Both say that a person can read the password from it.
- Gemini timed out on several questions and gave no answer to them.

## Open questions

- An error after some rows is lost, and `restfulRowLimit` cuts a result
  with no sign (the adapter code). If step 6 shows either on the server, the
  condition of "When it cannot be a driver" about a result cut short
  applies, unless the driver reads a sign that this draft did not find. Ask
  Ken at step 6.
- A NaN and a NULL are one value on the wire (the adapter code). Whether
  the driver returns nil for both is a decision of step 9 (D8).
- The WebSocket interface binds parameters and reports an error of a fetch
  (the adapter code), but it is not a request over HTTP for each statement
  (D14). Whether it is in scope is a question for Ken, and not for this
  draft.
- Whether the Enterprise Edition is a flavor, and whether its image needs
  a licence, are questions of steps 2 and 4.
- None about the dbmeta entry: each Tested release passed `dbrun test`, and
  dbmeta D112 now exists, staged and not committed.
- The name of the package, the URL of the DSN, and what its path names are
  decisions of step 9. `dburl` holds a provisional scheme until then (dburl
  D36).
