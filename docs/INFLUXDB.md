# InfluxDB

This file holds what is known about InfluxDB, for the driver that D73 names
first after Neo4j. W11 in [BACKLOG.md](BACKLOG.md) is the work, and it
follows [DRIVER.md](DRIVER.md). The headings are the template of that file.

This is the draft of step 3. No server has run for this driver yet, so every
fact here is "not measured" and names its source. R is the exception, because
`dbrun` measured it (Summary). The sources, each read on 2026-09-28,
are these:

- "The Core API reference" is
  `api-docs/influxdb3/core/influxdb3-core-openapi.yaml` in
  `github.com/influxdata/docs-v2`. It names the version v3.11.2.
- "The query guide" is
  `content/shared/influxdb3-query-guides/execute-queries/influxdb3-api.md`,
  and "the parameter guide" is
  `content/shared/influxdb3-query-guides/sql/parameterized-queries.md`, both
  in `docs-v2`.
- "The SQL reference" is `content/shared/sql-reference/_index.md`,
  `data-types.md` and `functions/misc.md` in `docs-v2`.
- "The token guides" are `content/shared/influxdb3-admin/tokens/` and
  `content/influxdb3/enterprise/admin/tokens/resource/` in `docs-v2`.
- "The configuration reference" is
  `content/shared/influxdb3-cli/config-options.md` in `docs-v2`.
- "The Enterprise guides" are `content/influxdb3/enterprise/admin/license.md`
  and `delete-data.md` in `docs-v2`.
- "The v1 API reference" is `api-docs/influxdb/v1/influxdb-oss-v1-openapi.yaml`
  in `docs-v2`, which names the version 1.8.10. "The v1 guides" are
  `content/influxdb/v1/administration/config.md` and
  `authentication_and_authorization.md` in `docs-v2`.
- "The v2 API reference" is
  `api-docs/influxdb/v2/influxdb-oss-v2-openapi.yaml` in `docs-v2`. "The Flux
  note" is `content/flux/v0/future-of-flux.md` in `docs-v2`.
- "The source" is `github.com/influxdata/influxdb` at the tag `v3.11.5`:
  `influxdb3_server/src/http.rs`, `influxdb3_types/src/http.rs` and
  `core/iox_v1_query_api/src/handler.rs`.
- "arrow-json" is `arrow-json/src/writer/mod.rs` on the main branch of
  `github.com/apache/arrow-rs`. The source uses it to write JSON. The release
  of arrow-json that v3.11.5 links was not read.
- "The image documents" are `influxdb/content.md` in
  `github.com/docker-library/docs`, and the start scripts of each release in
  `github.com/influxdata/influxdata-docker/influxdb/`.
- "The dbmeta entry" is `container/influxdb.go` in `dbmeta`. It is staged,
  and not committed.
- "The Go client" is `github.com/InfluxCommunity/influxdb3-go/v2` v2.17.0.
- "GitHub" and "Docker Hub" are the releases and the tags that each one
  listed.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, both
  asked on 2026-09-28.

## Summary

- InfluxDB is a time series database from InfluxData. Its major versions
  speak different languages (the image documents):
  - InfluxDB 1 speaks InfluxQL at `/query`.
  - InfluxDB 2 speaks Flux at `/api/v2/query`, and InfluxQL at `/query`
    through its v1 compatibility API (the v2 API reference).
  - InfluxDB 3 Core and InfluxDB 3 Enterprise speak SQL at
    `/api/v3/query_sql` and InfluxQL at `/api/v3/query_influxql`, and keep
    the v1 `/query` endpoint (the Core API reference). The SQL is the SQL of
    Apache DataFusion (the SQL reference).
- InfluxDB 3 also takes SQL and InfluxQL through Flight SQL over gRPC (the
  Core API reference). The Go client queries only through Flight, and uses
  HTTP only to write (the Go client).
- Flux is in maintenance mode, and InfluxDB 3 does not have it (the Flux
  note). InfluxDB 1 and InfluxDB 2 are in maintenance too (`docs-v2`,
  `content/shared/influxdb3/which-influxdb-3.md`).
- The newest tags on GitHub are `v3.11.5`, `v2.9.1` and `v1.13.1`, and
  GitHub also has `v3.12.0-0.rc.2` (GitHub). On Docker Hub, `latest` is the
  same image as `2.9.1`, and `core` is the same image as `3.11.5-core`
  (Docker Hub).
- `dburl` has two schemes for this driver, committed on its `main` as
  `a2f1a3a` and not tagged (dburl D29 and `dburl/scheme.go`). Their default
  port is 8086 with `version=1` or `version=2`, and 8181 otherwise. The scheme `influxdb` has the dialect
  `influxdb`, the aliases `in` and `influx`, and the generator `GenInfluxDB`.
  The scheme `influxql` has the dialect `influxql`, the alias `iq`, and the
  generator `GenInfluxQL`, which returns `influxdb` as the Go driver and
  adds only `sqlmode=disable`. Each names the `GoPackage`
  `github.com/xo/dbimp/influxdb`.
- `usql` has no driver for InfluxDB. Its backlog names InfluxDB and InfluxQL
  among the databases that have a Go API and no `database/sql` driver
  (`usql/docs/BACKLOG.md`, item 157).
- `dbmeta` has no model for InfluxDB. The dbmeta entry names InfluxDB 3 Core
  as the product `influxdb`, with `3.9.13` and `3.11.5` in the Tested tier
  and `3.10.6` in the Nightly tier. It cites dbmeta D112, which is staged and
  not committed.
- R, H and S depend on the flavor. H and S are not measured:
  - R: InfluxDB 3 Core passes, because each Tested release passed `dbrun
    test` (below). InfluxDB 3 Enterprise needs a person to follow a link in
    an email before the server starts (the dbmeta entry and the Enterprise
    guides), so it fails R now. InfluxDB 1 and InfluxDB 2 have images, and
    `dbrun` has no entry for them yet, so R is not measured for them.
  - H: every flavor takes queries over HTTP.
  - S: SQL on InfluxDB 3 meets S. InfluxQL is a dialect like SQL. Ken
    decided on 2026-09-28 that Flux meets S (D75), although a Flux query is
    a chain of functions such as `from(bucket: ...) |> range(start: -5m)`
    (the v2 API reference).
- TARGETS.md has InfluxDB 3 as a P1 target, and says that its JSON output
  can lack a schema. Responses, below, confirms that from the source.
- `dbrun` has an entry, staged in `dbmeta` for Ken's review and not committed,
  from dbmeta D112. The `dbmeta` session reported on 2026-09-28 that each
  Tested release passed `dbrun test`: `influxdb-3.9.13` and `influxdb-3.11.5`,
  with `3.10.6` in the Nightly tier. The entry has no ordinary user, because
  InfluxDB 3 Core has admin tokens only. The admin DSN is
  `http://_admin:<token>@...`, and `/health` needs the token.
- D79 names the releases that the tests run. InfluxDB 1 and InfluxDB 2 need
  their own entries in `dbrun` for `1.13.1` and `2.9.1` in the Tested tier,
  and `1.11.8` and `2.8.0` in the Nightly tier.

## Requests

### InfluxDB 3

- A SQL query is `GET` or `POST` to `/api/v3/query_sql`, and an InfluxQL
  query is `GET` or `POST` to `/api/v3/query_influxql` (the Core API
  reference and the source).
- A `GET` takes `db`, `q`, `format` and `params` in the query string. The
  value of `params` is JSON text (the Core API reference and the source).
- A `POST` takes a JSON body with `db`, `q`, `format` and `params`, where
  `params` is an object (the Core API reference and the source). `db` is
  required for SQL. For InfluxQL, the statement can name the database in
  place of `db`, and the two must agree (the source).
- `format` is `json`, `jsonl` or `json_lines`, `csv`, `pretty` or `parquet`.
  The default is `json` (the Core API reference and the source).
- The `Accept` header also chooses the format. `format` in the request wins
  over it (the source). The source reads the header first, and it knows
  only `application/vnd.apache.parquet`, `text/csv`, `text/plain`,
  `application/json`, `*/*` and no header. Any other value is HTTP 400,
  even when `format` is present (the source). The Core API reference lists
  `application/jsonl` for `Accept`, and the source refuses it. The two
  disagree.
- The v1 `/query` endpoint takes InfluxQL with `db`, `q`, `epoch`,
  `chunked`, `chunk_size`, `pretty`, `rp`, `u` and `p`, by `GET` or `POST`.
  A `POST` can send them as JSON, as a form, or as the statement alone with
  `Content-Type: application/vnd.influxql` (the Core API reference).
- Authentication is a token. `Authorization: Bearer <token>` works on every
  endpoint. `Authorization: Token <token>` works on the v1 and v2
  endpoints. HTTP Basic with any user name and the token as the password,
  and `u` and `p` in the query string, work on the v1 endpoints (the Core
  API reference and the token guides).
- A token begins with `apiv3_` (the dbmeta entry). The first admin token is
  the operator token, named `_admin` (the token guides).
- Authentication is on by default. `--without-auth` turns it off, and
  `--disable-authz` with `health`, `ping` or `metrics` opens those
  endpoints (the configuration reference).
- A request body is 10 MB at most by default, set by
  `--max-http-request-size`. A larger one is HTTP 413 (the configuration
  reference and the source).
- The default port is 8181 (the configuration reference, the image
  documents and the dbmeta entry).
- Writes are line protocol, at `/api/v3/write_lp`, `/write` and
  `/api/v2/write` (the Core API reference). `/api/v3/configure/database`
  and `/api/v3/configure/table` create and delete a database and a table
  (the Core API reference).

### InfluxDB 1

- A query is `GET` or `POST` to `/query`, with `db`, `q`, `epoch`, `pretty`,
  `chunked`, `u` and `p` (the v1 API reference). `GET` is for `SELECT` and
  `SHOW`. `POST` is for `SELECT ... INTO`, `ALTER`, `CREATE`, `DELETE`,
  `DROP`, `GRANT`, `KILL` and `REVOKE` (the v1 API reference).
- Authentication is off by default. When it is on, a request uses HTTP
  Basic, or `u` and `p` in the query string (the v1 API reference and the v1
  guides).
- The default port is 8086 (the v1 guides).

### InfluxDB 2

- A Flux query is `POST /api/v2/query`, with `org` or `orgID` in the query
  string, and the body as `application/vnd.flux` or as JSON (the v2 API
  reference).
- An InfluxQL query is `/query`, where `db` and `rp` map to a bucket through
  a mapping of the database and the retention policy (the v2 API reference).
- The default port is 8086 (the image documents).

## The DSN

- The name of the package and of the driver is `influxdb` (D78). Step 9
  decides the URL (D27 and D35). dburl D29 says that the form of the URL,
  the port and the authentication are not settled.
- The DSN has the keys `sqlmode` and `version` (D78). `sqlmode` is
  `disable`, `allow`, `prefer` or `require`, and its default is `prefer`.
  `version` is `1`, `2` or `3`, and its default is `3`. The driver reads
  both keys, and sends neither to the server.
- The dbmeta entry gives the URL `http://_admin:<token>@127.0.0.1:<port>`,
  with the name of the token, `_admin`, as the user and the admin token as
  the password. It has no path, so it names no database. Init in the dbmeta
  entry creates the database `dbmeta`.
- Each InfluxDB 3 query needs a database (the Core API reference), so step 9
  decides how the URL names it.

## Responses

### InfluxDB 3

- `json` is one JSON array with one object for each row, such as
  `[{"co":0,"hum":36.2,"room":"Kitchen","temp":23.0,"time":"2022-01-01T09:00:00"}]`
  (the parameter guide). `jsonl` is one such object on each line (the query
  guide). Neither has a list of columns or of types (the source).
- The Core API reference gives the v1 form, `results` with `series`,
  `columns` and `values`, as its example of a SQL response. The query guide,
  the parameter guide and the source give an array of objects. The two
  disagree.
- A NULL is not written. The key is left out of the object (the source and
  arrow-json). The source builds its writers with the defaults of
  arrow-json, and arrow-json says that its writers leave out a key whose
  value is null.
- A result with no rows is `[]` for `json`, and an empty body for `parquet`
  (the source). So neither format names the columns of an empty result.
  `tblfmt` writes such a result set as `psql` does from `v0.19.1`
  (tblfmt D31, and Q12 in [PLAN.md](PLAN.md)). Whether `csv` writes its header for
  an empty result is not measured.
- By rule 2 of D18, the columns of `json` and `jsonl` are the keys of the
  first row (D77). A NULL in the first row removes its column, and the same
  key in a later row is then an error. If step 6 shows this, Ken decides
  whether it holds (D77).
- `csv` writes a header row before the first batch of rows, and no header
  after it (the source). It has no types.
- `json`, `jsonl` and `csv` stream. The server writes each batch of rows as
  the query makes it (the source). `pretty` and `parquet` read the whole
  result into the memory of the server before they send it (the source).
- The status and the content type are sent before the first row (the
  source).
- There is no paging and no cursor over HTTP (the Core API reference). No
  default `LIMIT` is written in the documents that were read.
- On Core, `--query-file-limit` caps a query at 432 Parquet files by
  default. With the default `gen1-duration` of 10 minutes, that is about 72
  hours of data. A query over the limit is an error (the configuration
  reference).
- The v1 `/query` on InfluxDB 3 gives the v1 form, below, and `chunked` with
  a `chunk_size` of 10,000 by default (the Core API reference and the
  source).

### InfluxDB 1

- A response is `{"results": [...]}`, with one entry for each statement. An
  entry holds `statement_id`, and either `series` or `error`. Each series
  holds `name`, `columns` and `values`, one array for each row. A NULL is
  `null` in `values` (the v1 API reference). So the columns arrive before
  the rows of a series, and rule 1 of D18 applies.
- `chunked=true` streams the result in chunks by series, or every 10,000
  points. A number sets the size of a chunk (the v1 API reference).
- `max-row-limit` caps a result that is not chunked. Its default is 0, which
  is no limit. Over the limit, the response holds `"partial":true` (the v1
  guides).
- `Accept: application/csv` gives CSV (the v1 API reference).

### InfluxDB 2

- `/api/v2/query` answers Flux in CSV, with the columns `result`, `table`,
  `_start`, `_stop`, `_time`, `_value` and the tags (the v2 API reference).
  It takes `Accept-Encoding: gzip` (the v2 API reference).
- `/query` gives the v1 form, and its example holds `"partial":true` for a
  chunk (the v2 API reference).

## Types

Step 10 writes the type table. These are the SQL types of InfluxDB 3, with
their Arrow types (the SQL reference):

- `STRING`, `CHAR`, `VARCHAR` and `TEXT` are `Utf8`.
- `BIGINT` is `Int64`, from -9223372036854775808 to 9223372036854775807.
- `BIGINT UNSIGNED` is `UInt64`, from 0 to 18446744073709551615.
- `DOUBLE` is `Float64`, and `FLOAT` and `REAL` are `Float32`. A float field
  is stored as `Float64`.
- `TIMESTAMP` is `Timestamp(Nanosecond, None)`, a time with nanoseconds and
  no zone.
- `INTERVAL` is `Interval(MonthDayNano)`.
- `BOOLEAN` is `Boolean`.
- A tag column is `Dictionary(Int32, Utf8)` (the query guide).
- UUID, BLOB, CLOB, BINARY, VARBINARY, ARRAY, ENUM, SET, DATETIME and BYTEA
  are not supported, with others (the SQL reference).

How each type arrives in JSON:

- A time arrives as a string with no zone, such as
  `"2025-02-25T20:19:34.984098"` (the Core API reference).
- Whether an `Int64` and a `UInt64` arrive as JSON numbers is not measured.
  If they do, a `UInt64` above 2^53 needs D19.
- arrow-json writes binary data in hex by default (arrow-json).
- A missing key is a NULL, above. JSON gives no way to tell a NULL from a
  column that a row lacks.

InfluxDB 1 has float, integer, string, boolean and time values (not
measured, from the example of the v1 API reference, which shows floats,
strings, `null` and RFC 3339 times). `epoch` turns a time into a number in
`ns`, `u`, `ms`, `s`, `m` or `h` (the v1 API reference).

## Parameters

- InfluxDB 3 binds named parameters, written `$name`, for SQL and for
  InfluxQL. The values go in `params`, an object of names and values (the
  parameter guide and the Core API reference).
- A value can be null, a boolean, a `u_int64`, an `int64`, a `float64` or a
  string. A time is a string (the parameter guide).
- A parameter works only in a `WHERE` predicate. It does not work in
  `SELECT`, in `GROUP BY`, as the argument of a function, as a name, or as
  an interval (the parameter guide).
- A parameter with no value is an error (the parameter guide).
- There are no prepared statements (the parameter guide).
- The v1 `/query` of InfluxDB 1 takes `params`, as JSON text, with `$key` in
  the statement (the v1 API reference).
- Flux takes `params` in the JSON body, which the query reads as
  `params.name` (the v2 API reference).

## Transactions

- InfluxDB 3 has no transactions (Gemini and DeepSeek). The SQL reference
  gives only the syntax of `SELECT`, with `WITH`, `JOIN`, `WHERE`, `GROUP
  BY`, `HAVING`, `UNION`, `ORDER BY` and `LIMIT`.
- SQL on InfluxDB 3 does not insert, update or delete (Gemini and DeepSeek).
  Rows arrive by line protocol (the Core API reference). On Core, a delete
  drops a whole table or database (the Core API reference). On Enterprise
  from 3.11, `influxdb3 delete rows` deletes rows by time and tag. The delete
  is asynchronous and takes effect up to 24 hours later by default (the
  Enterprise guides).
- InfluxDB 1 has `DELETE` and `DROP` in InfluxQL, and no transactions (the
  v1 API reference).

## Errors

### InfluxDB 3

The source maps these errors to these statuses:

- A plan or parse error of DataFusion is HTTP 400. A feature that
  DataFusion does not implement is HTTP 405. An error while the query runs
  is HTTP 500.
- A database that does not exist is HTTP 404, with the body
  `{"error": "..."}`.
- A failed authentication is HTTP 401, and a refused permission is HTTP 403.
  Both have an empty body.
- A request that is too large is HTTP 413. A limit on requests is HTTP 429.
- A missing parameter, bad JSON, and an unknown `Accept` are HTTP 400. An
  unknown `Content-Type` is HTTP 415.
- InfluxQL at `/api/v3/query_influxql` with more than one statement is HTTP
  400.
- Many errors have a plain text body, and some have the JSON object
  `{"error": ..., "data": ...}` (the source and the Core API reference).

The status is sent before the rows (the source), so an error after some rows
cannot change it. How the body ends then is not measured.

The v1 `/query` on InfluxDB 3 answers a statement that does not parse with
HTTP 200 and the error in the body. It gives each failed statement an error
in its entry of `results` (the source).

### InfluxDB 1

A syntax error is HTTP 400 with `{"error": "error parsing query: ..."}`, and
a failed authentication is HTTP 401 with `{"error": "authorization failed"}`
(the v1 API reference). An entry of `results` can hold `error` in a response
with HTTP 200 (the v1 API reference). The v1 API reference and the source of
InfluxDB 3 disagree on the status of a parse error.

## Cancellation and timeouts

- InfluxDB 3 has no endpoint to cancel a query in the Core API reference.
  `system.queries` has a column `cancelled` (the query guide).
- Whether InfluxDB 3 stops a query when the client disconnects is not
  measured. Gemini and DeepSeek say that it does.
- InfluxDB 1 has `KILL QUERY` (the v1 API reference). `query-timeout` kills
  a query after a time, and its default is `0s`, which is no limit (the v1
  guides).

## Statements

- `/api/v3/query_influxql` takes one statement (the source). Whether
  `/api/v3/query_sql` takes more than one is not measured. Gemini and
  DeepSeek say that it takes one.
- The v1 `/query` takes statements separated by `;`, and answers each one in
  `results` with its `statement_id` (the v1 API reference and the source).
- SQL comments are `--` to the end of the line, and `/* */` (the SQL
  reference).

## Principals

- InfluxDB 3 Core has admin tokens and no users. A token with fewer rights
  is an Enterprise feature (the dbmeta entry and the token guides). So Core
  has no ordinary user.
- InfluxDB 3 Enterprise has resource tokens. A database token grants `read`,
  `write` or both on named databases, or on every database with `*`, such as
  `db:DATABASE1,DATABASE2:read,write`. A system token reads the information
  of the server (the token guides).
- InfluxDB 1 has admin users and users that are not admins. An admin grants
  `READ`, `WRITE` or `ALL` on a database to a user that is not an admin. A new
  user that is not an admin can reach no database (the v1 guides). The image
  of InfluxDB 1 takes `INFLUXDB_ADMIN_USER`, `INFLUXDB_USER`,
  `INFLUXDB_READ_USER`, `INFLUXDB_WRITE_USER` and `INFLUXDB_HTTP_AUTH_ENABLED`
  (the image documents).
- InfluxDB 2 has users and authorizations at `/api/v2/users` and
  `/api/v2/authorizations` (the v2 API reference). Its image sets the first
  user, organization, bucket and admin token with `DOCKER_INFLUXDB_INIT_*`
  (the image documents).
- The version on InfluxDB 3 is `GET /ping`. It answers with the headers
  `x-influxdb-version` and `x-influxdb-build`, which is `Core` or
  `Enterprise`, and a JSON body with `version`, `revision` and `process_id`
  (the Core API reference). The source also writes the name of the product
  in the body. `/ping` needs a token by default (the Core API reference).
- The SQL function `version()` gives the version of DataFusion, and not the
  version of InfluxDB (the SQL reference).
- On InfluxDB 1, `/ping` answers HTTP 204 with `X-Influxdb-Version` and
  `X-Influxdb-Build`, which is `OSS` or `ENT` (the v1 API reference).

## Flavors

Ken decided on 2026-09-28 that the driver serves InfluxDB 1, InfluxDB 2, and
InfluxDB 3 and later, as flavors (D78).

- InfluxDB 3 Core: the image `docker.io/library/influxdb`, with the tags
  `3.9.13-core`, `3.10.6-core` and `3.11.5-core` (Docker Hub and the dbmeta
  entry). The port is 8181. The dbmeta entry writes the admin token to a
  file, starts the server with `--admin-token-file`, and caps its memory to
  fit the 4 GB limit of `dbmeta`. SQL and InfluxQL.
- InfluxDB 3 Enterprise: the same image, with tags such as
  `3.11.5-enterprise` (Docker Hub). The documents that were read do not name
  its port. A trial
  or home licence needs a person to follow a link in an email (the
  Enterprise guides and the dbmeta entry). SQL and InfluxQL, with resource
  tokens and the delete of rows.
- InfluxDB 2: the same image, with tags such as `2.9.1` and `2.8.0` (Docker
  Hub). The port is 8086. Flux, and InfluxQL through `/query`. The driver
  speaks only InfluxQL to it (D78).
- InfluxDB 1: the same image, with tags such as `1.13.1`, `1.12.4` and
  `1.11.8` (Docker Hub). The port is 8086. InfluxQL.
- The header `X-Influxdb-Build` of `/ping` names `Core` or `Enterprise` on
  InfluxDB 3 and `OSS` or `ENT` on InfluxDB 1 (the Core API reference and
  the v1 API reference). What InfluxDB 2 answers is not measured.
- With `sqlmode` `prefer` or `require`, the driver tells the flavors apart
  by the header `X-Influxdb-Version` of `GET /ping`. With `disable` or
  `allow`, it sends no ping, and the key `version` names the release (D78).

## Interfaces

Step 10 writes the interface table from the code.

## Faults

`usql` has no driver for InfluxDB, so there is no driver in use whose faults
this driver must not repeat. These facts about the other clients are for step
5a:

- The Go client queries through Flight SQL, and needs
  `github.com/apache/arrow-go/v18`, `google.golang.org/grpc` and
  `google.golang.org/protobuf` (its `go.mod`). D13 allows none of them
  without the approval of Ken.
- `github.com/influxdata/influxdb1-client` was last pushed on 2024-03-12
  (GitHub). Its code was not read.

## Second opinions

Step 7 tests these leads on the server. Until then, each one is a lead.

- Gemini and DeepSeek say that a NULL in `json` leaves out its key. The
  source and arrow-json agree.
- Gemini says that `Int64` and `UInt64` arrive as JSON numbers, a time as a
  string with no zone, such as `"2024-01-01T12:00:00.123456789"`, and a tag
  as a string. DeepSeek says that a time arrives with a `Z`. The example in
  the Core API reference has no zone, so DeepSeek disagrees with it.
- Gemini says that `format=parquet` keeps the schema of a result with no
  rows. The source sends an empty body when the query gives no batch, so it
  disagrees.
- DeepSeek says that `format=arrow` and `Accept:
  application/vnd.apache.arrow.stream` give an Arrow stream. The source has
  no Arrow format, and it answers an unknown `Accept` with HTTP 400, so it
  disagrees. DeepSeek also says that `csv` gives the names and not the
  types, which agrees with the source.
- DeepSeek writes a database token as `db:read:<database>`. The token guides
  write `db:<databases>:<actions>`, so it disagrees.
- Gemini and DeepSeek say that SQL has no `INSERT`, `UPDATE`, `DELETE` or
  transactions, that `/api/v3/query_sql` takes one statement, that the
  server stops a query when the client disconnects, and that there is no
  `KILL QUERY`.
- Gemini and DeepSeek say that Core has only admin tokens, as the token
  guides say.
- Gemini says that `SHOW DIAGNOSTICS` needs an admin on InfluxDB 1, that
  InfluxDB 2 has no `SHOW DIAGNOSTICS`, `SHOW QUERIES` or `KILL QUERY`, and
  that the InfluxQL of InfluxDB 2 needs a mapping of the database and the
  retention policy, with a default of `<bucket>/autogen`.

## Open questions

[PLAN.md](PLAN.md) has no entry for these yet. Each one is for Ken.

- Ken decided on 2026-09-28 which versions and languages the driver serves
  (D78): one driver with two dialects, `influxdb` for SQL on InfluxDB 3 and
  later and `influxql` for InfluxQL on every release, no Flux, and the key
  `sqlmode`, whose default `prefer` chooses by the release. `dburl` holds
  the two dialects as the schemes `influxdb` and `influxql` (dburl D29).
  D79 names the releases that the tests run.
- Ken decided on 2026-09-28 how SQL on InfluxDB 3 gets its columns: from
  the keys of the first object of `json`, in order, by rule 2 of D18 (D77).
  D77 names what step 6 measures first. The question as it stood: `json`
  and `jsonl` leave out a
  NULL and name no column, so D18 cannot supply the columns. The other ways
  are the header of `csv`, which has no types and cannot tell a NULL from an
  empty string, `parquet`, which the server builds whole in memory and which
  is a binary encoding under D13, the v1 `/query` with InfluxQL, which names
  its columns, and Flight SQL over gRPC, which needs packages under D13. This
  is a question of step 6 and of "When it cannot be a driver".
- SQL on InfluxDB 3 does not insert, update or delete. Writes are line
  protocol, and Core deletes only a whole table or database. Step 6 stops
  and asks Ken when the server refuses one of insert, select, update and
  delete.
- Core has no ordinary user, and Enterprise fails R now. InfluxDB 1 and
  InfluxDB 2 have ordinary users. Step 6 needs both principals.
- None about Flux and S: Ken decided on 2026-09-28 that Flux meets S
  (D75). The driver serves InfluxDB 2 through InfluxQL, and does not speak
  Flux (D78).
- On Core, the default `--query-file-limit` caps a query at about 72 hours of
  data. Step 4 decides whether the dbmeta entry changes it.
