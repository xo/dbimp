# Databend

This file holds what is known about the HTTP interface of Databend, for a
driver that D73 places fourth after Neo4j. Its work item comes when its turn
comes. The headings are the template of [DRIVER.md](DRIVER.md).

This is the draft of step 3. No server has run for this driver yet, so every
fact here is "not measured" and names its source. R is the exception, because
`dbrun` measured it (Summary). The sources, each read on 2026-09-27, are
these:

- "The HTTP document" is `docs/en/developer/10-apis/http.md` in
  `github.com/databendlabs/databend-docs`.
- "The SQL documents" are the pages for `BEGIN`, `CREATE USER` and the data
  types under `docs/en/sql-reference/` in the same repository.
- "The server source" is `src/query/service/src/servers/http/`,
  `src/query/ast/src/ast/param_substitution.rs`,
  `src/query/settings/src/settings_default.rs` and
  `src/common/base/src/headers.rs` in `github.com/databendlabs/databend`, on
  `main`. A fact from the source is what the code says, and not what a server
  did.
- "The image files" are `docker/README.md`, `docker/Dockerfile`,
  `docker/bootstrap.sh` and `docker/query-config.toml` in the same
  repository.
- "The Go driver" is `github.com/datafuselabs/databend-go` v0.9.4, which
  `usql` uses: its code, its `docs/connection.md`, its `README.md` and its
  `tests/`.
- "GitHub" and "Docker Hub" are the releases and the tags that each one
  listed.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`.

## Summary

- The product is Databend, a data warehouse with a dialect of SQL. A server
  is `databend-query`, which keeps its catalog in `databend-meta` (the image
  files).
- The newest release that GitHub does not mark as a prerelease is v1.2.881,
  of 2026-04-17, and the tag `latest` on Docker Hub is the same image. A
  nightly release comes out several times a week, and the newest is
  v1.2.948-nightly, of 2026-09-21. A line of patches also exists, and its
  newest is v1.2.925-patch-13, of 2026-09-03, which GitHub marks as a
  prerelease (GitHub and Docker Hub).
- `dburl` has the scheme with the `Driver` name `databend`, which dburl
  `v0.36.0` calls `Name` (dburl D37), the aliases `dd`
  and `bend`, the generator `GenDatabend`, the `GoPackage`
  `github.com/datafuselabs/databend-go`, the `Deployment`
  `DeploymentServer` and the `Dialect` `databend` (`dburl/scheme.go`).
  `GenDatabend` refuses a URL with no host, and otherwise returns the URL as
  it was typed (`dburl/dsn.go`).
- `usql` has `drivers/databend`, which imports the Go driver at v0.9.4. It
  reads the metadata through `information_schema` with `?` as the
  placeholder, and with functions, indexes, constraints and column
  privileges turned off. It sets no `Version` function, so `usql` runs
  `SELECT version();` for the version (`usql/drivers/drivers.go`).
- `dbmeta` has the dialect constant `Databend` and no model. Its container
  entry is below. dbmeta D66 lists Databend among the products that run
  the real engine in an image, and says that each one is worth a model only
  if somebody asks. `dbmeta/docs/USQL.md`
  says that the `usql` driver serves every command that it counts except
  `\l`, because it has no `CatalogReader`.
- D24 counts a driver that replaces one of `usql` as a way to cut the
  dependencies of `usql`. The Go driver brings Arrow, OpenTelemetry, a
  package for retries, `pkg/errors`, a TOML parser, a UUID package and
  `golang.org/x/mod` (its `go.mod`).
- R holds, because each Tested release passed `dbrun test` (below). The
  image runs the meta service and the query service in one container, on
  local disk (the image files). H and S are not measured. H is likely,
  because the server takes `POST /v1/query` on port 8000 (the HTTP document).
  S is likely, because the statement is SQL.
- `dbrun` has an entry, staged in `dbmeta` for Ken's review and not committed,
  from dbmeta D112. The `dbmeta` session reported on 2026-09-28 that each
  Tested release passed `dbrun test`: `databend-1.2.881` and
  `databend-1.2.948-nightly`, the newest stable and the newest weekly release,
  which Ken chose. `root` has a password through `QUERY_DEFAULT_USER` and
  `QUERY_DEFAULT_PASSWORD`. The DSN has the form of databend-go,
  `databend://...@host:port/default?sslmode=disable`, because that dialect is
  settled. One first start in five after the pull of the image failed, because
  the query server started before the metadata server was the leader (the
  `dbmeta` backlog).

## Requests

- A statement is `POST /v1/query` with `Content-Type: application/json`,
  and the statement in `sql` (the HTTP document).
- The body holds these fields (the server source):
  - `sql`, the statement.
  - `session_id`, and `session`, the state of the session (see
    Transactions).
  - `pagination`, with `wait_time_secs`, `max_rows_in_buffer` and
    `max_rows_per_page`.
  - `string_fields`, true by default.
  - `stage_attachment`, which carries a staged file into an `INSERT`.
  - `params`, the values of the parameters (see Parameters).
  - `arrow_result_version_max` and `arrow_features`, which ask for the
    result as Arrow.
- The defaults of `pagination` differ by source. The server source gives
  10 for `wait_time_secs`, 5,000,000 for `max_rows_in_buffer` and 10,000 for
  `max_rows_per_page`. The HTTP document gives 1 for `wait_time_secs`. The
  Go driver documents 10, 5,000,000 and 100,000.
- `session` holds `catalog`, `database`, `role`, `secondary_roles`,
  `settings` (a map of strings), `txn_state`, `need_sticky`,
  `need_keep_alive` and `internal` (the server source). The HTTP document
  also names `keep_server_session_secs`.
- These request headers matter (the server source and the Go driver):
  - `X-DATABEND-QUERY-ID`, an id that the client chooses for the query.
  - `X-DATABEND-TENANT` and `X-DATABEND-WAREHOUSE`, for Databend Cloud.
  - `X-DATABEND-STICKY-NODE`, which sends a request to the node that ran
    the query.
  - `X-DATABEND-ROUTE-HINT`, which the Go driver sends. The response to
    a login carries `X-DATABEND-SESSION-ID`, which the Go driver keeps.
- Authentication is HTTP Basic, or a bearer token (the server source). A
  bearer token is a session token of Databend, a JWT, or a key pair when the
  header `X-DATABEND-AUTH-METHOD: keypair` is present. The server refuses a
  request with two `Authorization` headers, and reads an empty Basic
  password as no password.
- `POST /v1/session/login` returns `version`, `session_id`,
  `server_max_arrow_result_version` and, for a password, a session token and
  a refresh token (the server source). A comment there says that a client is
  encouraged to call it, and that it is not required. The Go driver calls it
  when it connects, and calls `/v1/session/logout` when it closes.
- The server also serves `/v1/session/refresh`, `/v1/session/heartbeat`,
  `/v1/verify`, `/v1/upload_to_stage`, `/v1/streaming_load`,
  `/v1/discovery_nodes`, `/v1/catalog/...`, `/v1/users` and `/v1/roles`
  (the server source). A comment there says that every endpoint except
  `/v1/query` can change without notice, and that a client uses the URIs in
  the response.
- The Go driver asks for Arrow with `arrow_result_version_max` and
  `Accept: application/vnd.apache.arrow.stream`, only when the DSN sets
  `query_result_format=arrow`, and only on a server from 1.2.899 (the Go
  driver). Its default is JSON.

## The DSN

- Step 9 decides the URL (D27 and D35). `dburl` passes the URL through as
  it was typed, so a URL typed with the alias `bend` reaches the driver
  with the scheme `bend` (`dburl/dsn.go` and its tests). D35 needs the scheme
  `databend`.
- The tests of `dburl` use `databend://user:pass@localhost/instance_name?tenant=tn&warehouse=wh`
  and `bend://user:pass@localhost/instance_name?sslmode=disabled&warehouse=wh`
  (`dburl/dburl_test.go`).
- The Go driver reads the path as the database, and the user and the
  password from the URL. It reads these keys: `tenant`, `warehouse`,
  `role`, `access_token`, `access_token_file`, `timeout`,
  `wait_time_secs`, `max_rows_in_buffer`, `max_rows_per_page`, `location`
  or `timezone`, `debug`, `enable_http_compression`,
  `presigned_url_disabled`, `empty_field_as`, `tls_config`, `sslmode`,
  `enable_otel`, `login` and `query_result_format`. It refuses
  `default_format`, `query` and `database`. It sends every other key to the
  server as a setting of the session (the Go driver).
- The Go driver speaks HTTP when `sslmode=disable`, or when the scheme ends
  in `http`. Otherwise it speaks HTTPS. A host with no port gets 80 for HTTP
  and 443 for HTTPS (the Go driver). Its `docs/connection.md` says that
  `disable` is the default of `sslmode`, and its code makes HTTPS the
  default.
- The DSN of the `dbrun` entry has the form of the Go driver,
  `databend://...@host:port/default?sslmode=disable` (Summary).

## Responses

- A response is one JSON object (the HTTP document). The server writes its
  fields in this order: `id`, `session_id`, `node_id`, `state`, `session`,
  `error`, `warnings`, `has_result_set`, `schema`, `data`, `affect`,
  `result_timeout_secs`, `settings`, `stats`, `stats_uri`, `final_uri`,
  `next_uri` and `kill_uri` (the server source). So `schema` arrives before
  the rows of each page, and rule 1 of D18 applies.
- `schema` is a list of objects, each with `name` and `type`, such as
  `{"name": "number", "type": "UInt64"}` (the HTTP document). The Go driver
  parses a type with arguments, such as `Decimal(38, 10)`, and a nullable
  form of each type.
- `data` is a list of rows, and each row is a list of strings (the HTTP
  document). A NULL is JSON `null` (see Types).
- `state` is `Running`, `Succeeded` or `Failed` (the HTTP document). The
  server source also has `Starting`.
- `settings` names `timezone`, `geometry_output_format`,
  `binary_output_format` and `http_json_result_mode`, so a client knows how
  the values were written (the server source).
- `affect` says what a statement changed in the session, such as
  `ChangeSetting` for `SET` and `UseDB` for `USE` (the HTTP document).
- A statement that changes rows returns a column with a name such as
  `number of rows inserted into <db>.<table>` (the server source). The Go
  driver reads the count from the first cell when the name of the first
  column holds `number of rows`.
- Paging is by `next_uri` (the HTTP document and the server source):
  - While the query runs and no rows are ready, `next_uri` is
    `/v1/query/<id>`, which returns the state.
  - When a page is ready, `next_uri` is `/v1/query/<id>/page/<n>`.
  - When the rows are done, or the query failed, `next_uri` is
    `/v1/query/<id>/final`.
  - The response to `final_uri` has no `next_uri`.
  The Go driver stops when `next_uri` is empty or holds `/final`.
- A page is served by the node that ran the query (the server source). The
  Go driver sends `X-DATABEND-STICKY-NODE` with the `node_id` of the first
  response.
- The server closes a query that no client asks for within
  `http_handler_result_timeout_secs`, 60 by default, plus `wait_time_secs`
  (the server source). So a slow reader can lose the rest of a result
  (D21).
- If `X-DATABEND-QUERY-ID` names a query that exists, and the user is the
  same, the server returns the first page of that query and does not start
  it again. For another user it answers HTTP 400 (the server source). The Go
  driver sends the same id when it sends a `POST` again.
- `max_result_rows` is 0 by default, which means no limit (the server
  source).
- Each response carries `X-DATABEND-QUERY-ID`, `X-DATABEND-QUERY-STATE`,
  `X-DATABEND-QUERY-PAGE-ROWS` and `X-DATABEND-VERSION` (the server source).
- Compression is not measured. The Go driver sends
  `enable_http_compression` as a setting.

## Types

Step 10 writes the type table. These are the facts so far:

- The types are `BOOLEAN`, `BINARY`, `VARCHAR` or `STRING`, `TINYINT`,
  `SMALLINT`, `INT`, `BIGINT`, `FLOAT`, `DOUBLE`, `DECIMAL`, `DATE`,
  `TIMESTAMP`, `TIMESTAMP_TZ`, `INTERVAL`, `ARRAY`, `TUPLE`, `MAP`,
  `VARIANT`, `BITMAP`, `VECTOR`, `GEOMETRY` and `GEOGRAPHY` (the SQL
  documents). The Go driver also names `UInt8`, `UInt16`, `UInt32` and
  `UInt64`.
- A `DECIMAL` has a precision of up to 38 digits in 16 bytes, or up to 76
  digits in 32 bytes. A `TIMESTAMP` has microseconds, and the session
  timezone sets how it is written (the SQL documents). The setting
  `timezone` is `UTC` by default (the server source).
- Every value in `data` is a string (the HTTP document), so no number
  passes through a float64 on the wire (D19).
- A NULL is JSON `null` when the setting `format_null_as_str` is 0, and the
  string `"NULL"` when it is 1. Its default is 0 (the server source). The
  default changed from 1 to 0 on 2025-05-28, in `databend` commit
  `a010d96594`, which v1.2.881 holds (GitHub). The tests of the Go driver
  set it to 0 before they read a NULL.
- The setting `http_json_result_mode` is `display` by default, or `driver`
  (the server source). It came on 2026-04-01, in pull request 19639, which
  v1.2.881 holds (GitHub). In `driver` mode, a `DATE` and a `TIMESTAMP`
  arrive as a number in a string, a `TIMESTAMP_TZ` arrives as microseconds
  and an offset, and a `BINARY` arrives as hex. In `display` mode, a
  `BINARY` follows `binary_output_format`, which is `hex` by default (the
  pull request and the server source).
- The Go driver reads a `Timestamp` with the layout
  `2006-01-02 15:04:05.999999` in the timezone of the response, a `Date`
  with `2006-01-02`, and a `Binary` from hex, base64 or UTF-8. It returns a
  `Decimal`, each number, a `Boolean`, an `Array`, a `Map`, a `Tuple` and a
  `Variant` as the string of the server (the Go driver).

## Parameters

- The server binds parameters from `params` in the body (the server
  source). A JSON array binds each `?` in order. A JSON object binds each
  `:name`. The kind of `params` must match the kind of placeholder, or the
  server returns an error.
- A JSON `null` becomes `NULL`, a boolean a boolean, an integer an integer,
  another number a `Float64`, a string a string, and an array or an object
  a call to `parse_json` (the server source). So a decimal sent as a JSON
  number becomes a `Float64`, and a decimal sent as a string keeps its
  digits.
- This came on 2026-03-25, in `databend` commit `11f15d0834`, which v1.2.881
  holds (GitHub). A release before it has no `params`, and there the driver
  needs the escaper of D34.
- The Go driver does not send `params`. It writes each argument into the
  text of the statement (the Go driver).

## Transactions

- `BEGIN`, `COMMIT` and `ROLLBACK` exist from v1.2.371 (the SQL documents).
- A `BEGIN` inside a transaction is ignored, and a `COMMIT` outside one is
  ignored, with no error (the SQL documents).
- A DDL statement inside a transaction commits the transaction. The
  statements after it run one at a time until the next `BEGIN` (the SQL
  documents). This is a transaction that stops being one, and D20 applies.
- The client carries the transaction. Each response holds `session`, and
  the next request sends it back (the HTTP document and the server source).
  `session.txn_state` is `AutoCommit`, `Active` or `Fail` (the SQL documents
  and the server source).
- When `txn_state` is `Active`, the server looks for the last query of the
  transaction on its own node, by `session.internal.last_query_ids`. If it
  does not find it, it fails with a timeout of the transaction (the server
  source). So every statement of a transaction goes to one node, and
  `need_sticky` says so.
- A statement that fails to start inside an active transaction sets
  `txn_state` to `Fail` (the server source).
- An idle transaction ends after `idle_transaction_timeout_secs` (the server
  source). Its default is not read.
- Isolation levels are not named in the sources.

## Errors

- A statement that fails to start, such as one with a syntax error, gets
  HTTP 200 with `state` set to `Failed` and `error` set (the server source).
  Gemini and DeepSeek said the same.
- `error` holds `code`, `message` and, if there is one, `detail` (the
  server source).
- An error outside a statement gets its own status and the body
  `{"error": {"code": <the HTTP status>, "message": ...}}` (the server
  source).
- The statuses are these (the HTTP document and the server source):
  - 200 for success, and also for a statement that failed.
  - 400 for a request that is not valid, a page of a query that is closed,
    and a query id of another user.
  - 401 for a failed authentication, an unknown user, and a page of a
    query of another user.
  - 404 for an unknown query or page.
  - 500 for an error of the server.
- An error after some rows arrives in the body of a later page. The Go
  driver reads `error` on each page. The status of such a page is not
  measured.
- A limit on the rate of requests is not measured.
- Which errors mean that the statement did not reach the server is not
  measured. The client id of a query lets a driver send a `POST` again
  without running the statement twice (the server source). D8 decides what
  that allows.

## Cancellation and timeouts

- `GET` or `POST` on `kill_uri`, `/v1/query/<id>/kill`, cancels the query.
  It answers HTTP 200 with an empty body, or 404 for an unknown query (the
  HTTP document and the server source).
- `final_uri`, `/v1/query/<id>/final`, closes the query and frees its
  resources (the HTTP document).
- The server does not stop a query when the client disconnects (Gemini and
  DeepSeek). The server source closes a query that no client asks for after
  the result timeout (see Responses). So a driver sends `kill_uri` when its
  context ends (D36).

## Statements

- Whether one request can hold two statements is not measured. Gemini and
  DeepSeek said that it cannot, and that the server returns an error.
- Comments are not measured.
- The version is `SELECT version()`, which `usql` runs (see Summary).
  `/v1/session/login` returns `version`, and every response carries
  `X-DATABEND-VERSION` (the server source).

## Principals

- The image adds the user `root` with no password, unless both
  `QUERY_DEFAULT_USER` and `QUERY_DEFAULT_PASSWORD` are set. Then it adds
  that user, with the type `double_sha1_password`, and adds no `root` (the
  image files). Both users come from the configuration file, not from SQL.
- An ordinary user comes from SQL (the SQL documents):

  ```sql
  CREATE ROLE <role>;
  GRANT SELECT, INSERT ON <database>.* TO ROLE <role>;
  CREATE OR REPLACE USER <name> IDENTIFIED BY '<password>' WITH DEFAULT_ROLE = '<role>';
  GRANT ROLE <role> TO <name>;
  ```

  `CREATE OR REPLACE USER` is a form that step 4 can run twice. Whether
  `CREATE ROLE` and `GRANT` can run twice is not measured.
- Gemini and DeepSeek said that a new user has no privileges, and that it
  can run `SELECT version()`.
- What an ordinary user cannot read is not measured.

## Flavors

- Databend Cloud speaks the same interface, with the headers
  `X-DATABEND-TENANT` and `X-DATABEND-WAREHOUSE` (the HTTP document). It is
  a cloud service, so it fails R.
- No other product that speaks `/v1/query` is known.
- The server also has a MySQL interface on port 3307, a ClickHouse HTTP
  interface on port 8124 and Flight SQL on port 8900 (the image files). Each
  is another interface, and not a flavor.

These are the facts that `dbmeta` needs for a `dbrun` entry (the image
files, Docker Hub and the Go driver):

- The image is `docker.io/datafuselabs/databend`. Each tag is the release
  with a `v`, such as `v1.2.881`. Each recent tag has a `linux/amd64` build.
- The HTTP port is 8000. The admin API on port 8080 answers
  `/v1/health`, which the tests of the Go driver use for readiness.
- The image runs `databend-meta` and `databend-query` in one container,
  with the data on local disk under `/var/lib/databend`. MinIO runs only when
  `MINIO_ENABLED` is set. The image declares the volumes
  `/var/log/databend`, `/etc/databend`, `/var/lib/databend` and
  `/var/lib/minio`.
- When `QUERY_CONFIG_FILE` is not set, the start script copies a new
  configuration on each start. It waits one second for the meta service.
- The image holds `bash` and `curl`, so `Init` can send SQL to
  `http://localhost:8000/v1/query`.
- The tests of the Go driver run a meta container, a MinIO container,
  several query containers and nginx. They take the image
  `datafuselabs/databend-query` at the tag `nightly`. The configuration
  there has `root` with no password and `databend` with the password
  `databend`.

## Interfaces

Step 10 writes the interface table from the code.

## Faults

These are faults of the Go driver that a driver here does not repeat (the
Go driver):

- It registers the names `databend` and `lake` (`driver.go`), where hard
  rule 1 allows one.
- It keeps a logger in a package variable that writes to standard output,
  and `ParseDSN` logs its error there (`driver.go`, `log.go` and `dsn.go`).
  It prints a line with `fmt.Printf` when a query is cancelled
  (`client.go`). Hard rule 2 forbids both.
- It reads each response whole with `io.ReadAll` (`client.go` and
  `arrow.go`).
- `Exec` reads every page of the result into memory before it returns
  (`client.go`).
- It calls `context.Background` in `Open`, in `Stmt.Exec` and `Stmt.Query`,
  and to kill a query, and `context.TODO` to load a token (`driver.go`,
  `stmt.go`, `client.go` and `rows.go`). A connection keeps a context in a
  struct (`connection.go`).
- In JSON, each number, a boolean, a decimal, an array, a map, a tuple and a
  variant reach `database/sql` as a string, while `ColumnTypeScanType` names
  another type, such as `int64` (`columntype.go`). An array or a variant is
  text in place of a value (D8).
- It never tells its parser that `format_null_as_str` is 1. On a server
  where it is 1, which was the default before 2025-05-28, a NULL in a
  nullable column that is not a string arrives as the string `NULL`
  (`columntype.go`).
- It reads a `Timestamp` with one layout of the `display` mode. A DSN key
  that sets `http_json_result_mode=driver` passes through as a setting, and
  then a timestamp arrives as a number that the layout does not read
  (`dsn.go` and `columntype.go`).
- It writes each argument into the text of the statement
  (`interpolate.go`). It writes a boolean as `1` or `0`. Its scanner for
  placeholders knows single quotes only, and not comments or double quotes,
  and its two passes treat a backslash differently. It drops the error of
  its encoder.
- It skips the arguments when the first one is nil, and sends the `?`
  unchanged. A prepared statement whose first argument is nil calls
  `reflect.TypeOf(nil).Kind()`, which panics (`interpolate.go`).
- `Commit` sends nothing when the server returned no `txn_state`, and
  reports success (`transaction.go`). D20 forbids a fake transaction.
- `BeginTx` ignores the isolation and the read only flag of
  `driver.TxOptions`, with no error (`connection.go`).
- It takes any scheme, and its DSN keeps a form of its own. It
  reads `sslmode=disabled`, as in the tests of `dburl`, as HTTPS, because
  only `disable` means HTTP (`dsn.go`).
- It needs Arrow, OpenTelemetry, `avast/retry-go`, `pkg/errors`, a TOML
  parser, a UUID package and `golang.org/x/mod` (`go.mod`), where D13 allows
  the standard library, apd and a binary encoding that Ken approves.
- Its `VERSION` file in v0.9.4 says `0.9.3`, and it sends that in its
  `User-Agent` (`VERSION` and `client.go`).

## Second opinions

Gemini and DeepSeek were asked on 2026-09-27, before any server ran. Gemini
returned empty answers and timed out at first, and then answered with a
short system prompt. Each lead is not measured.

- Two statements in one request. Both said that the server refuses them.
- Cancellation. Both said that the client uses the kill URI, and that a
  disconnect does not stop the query. DeepSeek named `POST`, and the server
  source takes `GET` and `POST`.
- Transactions. Both said that the client sends `session` back. DeepSeek
  named the state `Explicit`, and the SQL documents and the server source
  name `Active`.
- A DDL inside a transaction. Gemini said that it commits the transaction,
  which the SQL documents say. DeepSeek said that the server refuses it,
  which the SQL documents contradict.
- `params`. Gemini said that `/v1/query` has no field for parameters.
  DeepSeek said that `params` is a list of strings for `?` from v0.9.0. The
  server source has `params` as any JSON value, an array for `?` or an
  object for `:name`, from 2026-03-25. Both models were wrong.
- The status of a syntax error. Both said HTTP 200 with the error in the
  body, which the server source agrees with.
- A failed authentication. DeepSeek said 401, which the server source agrees
  with.
- An unknown query id. DeepSeek said 404, which the server source agrees
  with.
- NULL. Both said JSON `null` by default and `"NULL"` with
  `format_null_as_str`, which the server source agrees with.
- The image. DeepSeek said that it runs the meta and the query service in
  one container, which the image files agree with. Both said that the
  default user is `root` with no password, which the image files agree
  with.
- A new user. Both said that it has no privileges and can run
  `SELECT version()`.
- A retry. Both said that the same `X-DATABEND-QUERY-ID` makes a `POST`
  safe to send again. The server source agrees, for the same user.
- The version. DeepSeek said `SELECT version();`, which `usql` runs.

## Open questions

None of these has an entry in [PLAN.md](PLAN.md) yet. Ken decides each one.

- None about the name. The scheme `databend` of `dburl` and the name
  `databend` belong to the Go driver, which `usql` ships, and the second
  `sql.Register` of `databend` panics if both are linked. Ken decided on
  2026-09-28 that this driver replaces the Go driver in `usql` and `dburl`,
  so the two are never linked together (D76).
- The aliases of `dburl`. `GenDatabend` passes the URL through as it was
  typed, so the alias `bend` reaches the driver as the scheme `bend`, which
  D35 refuses. `dburl` owns that change (D5).
- None about the releases. The range is the newest stable release,
  v1.2.881, and the newest weekly release, v1.2.948-nightly, both in the
  Tested tier, by the rule that Ken chose in dbmeta D112.
- None about the parameters of an older release. `params` needs v1.2.881 or
  later, and the range of dbmeta D112 has no older release.
- A DDL inside a transaction commits it. How `BeginTx` answers that is a
  decision of step 9 (D20).
- JSON or Arrow. Arrow is a binary encoding, and it needs Ken's approval for
  this driver (D13).
- None about the entry in `dbrun`. The `dbmeta` session wrote it from
  dbmeta D112, and each Tested release passed `dbrun test`.
