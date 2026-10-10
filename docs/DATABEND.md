# Databend

This file holds what is known about the HTTP interface of Databend, for a
driver that D73 places fourth after Neo4j. Its work item comes when its turn
comes. The headings are the template of [DRIVER.md](DRIVER.md).

Steps 5a to 7 measured 1.2.881 and 1.2.948 on 2026-09-29, as `root` and as
`dbmeta_user`. A fact marked "recorded" is in `testdata/databend/`, and
`requests.json` there holds each request. Each section starts with the
measured facts. The facts after them come from the sources of step 3, and
each one that is not measured says so. The sources, each read on 2026-09-27,
are these:

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
- On 2026-09-29, the `dburl` session reported that Ken asked it to move the
  scheme to this driver, staged as dburl D39 and not committed. The
  `GoPackage` becomes `github.com/xo/dbimp/databend`, and the `Driver`, the
  `Dialect` and the aliases `dd` and `bend` stay. `GenDatabend` then writes
  the scheme `databend` for every alias, supplies `localhost` for a URL with
  no host and the port 8000, and passes the user, the path and the query
  through. It waits for step 9 to settle the port, the keys and TLS.
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
  local disk (the image files). H and S hold: `POST /v1/query` answered
  `SELECT version(), 1 + 1 AS two, current_user()` with HTTP 200, `state`
  `Succeeded`, a `schema` and one row, on 1.2.881 and 1.2.948, as `root` and
  as `dbmeta_user` (measured with `curl` on 2026-09-29).
- On 2026-09-29, `dbrun list all` names the releases `databend-1.2.881` and
  `databend-1.2.948`, both in the Staged tier with the cadence `tested`. The
  version of the second is `v1.2.948-nightly-1df0991d1f`. The URL of
  `dbmeta_user` names the database `dbmeta`.
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

Measured on 2026-09-29:

- `POST /v1/query` with `Content-Type: application/json` and `{"sql": ...}`
  runs a statement, as both principals, on both releases (recorded, item 1).
  A body of `text/plain` is HTTP 415 (recorded).
- HTTP Basic works for both principals (recorded). No login is needed first.
- `POST /v1/session/login` with basic authentication and the body `{}` is
  HTTP 500 with "[HTTP-PANIC] Internal server error: request handler
  panicked", and with no body HTTP 400, on both releases (recorded). So the
  driver sends no login.
- `session` in the body can be partial. `{"database": "dbmeta"}` alone sets
  the database, and `{"settings": {...}}` alone sets the settings of the one
  statement (recorded, items 1 and 3).
- A setting that does not exist, such as `dbimp_no_such_setting`, is
  ignored, with no error (recorded, item 3).

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

Measured on 2026-09-29:

- The response holds `schema` and `data`, and `schema` names each column
  with its type, in the order of the statement (recorded, item 2). Two
  columns with one name keep both. A column with no alias is named for its
  expression, such as `1 + 1`. A result with no rows has its `schema`.
- A page holds at most 10,000 rows by default (recorded, item 5). The first
  response is `Starting` with no rows, or `Running` with the first page,
  and the next pages come from `next_uri`, which ends at `final_uri`. A
  result of 30,000 rows came in three pages of 10,000, as both principals,
  on both releases.
- `GET` on `final_uri` answers again after the first time, with no rows
  (recorded, item 5).
- `affect` and `settings` arrive in each response. `settings` names
  `timezone`, `geometry_output_format`, `binary_output_format` and
  `http_json_result_mode` (recorded).
- A statement that changes rows returns one row with a count, such as the
  column `number of rows inserted` (recorded, item 3). `REPLACE INTO` returns
  no rows. `MERGE INTO` returns a count for each action.

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

`databend/tables_test.go` writes this table from the code (step 10, D118
and D119).

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| boolean | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| tinyint | integer | `int64` | `int64` | `INT8` | yes |
| smallint | integer | `int64` | `int64` | `INT16` | yes |
| int | integer | `int64` | `int64` | `INT32` | yes |
| bigint | integer | `int64` | `int64` | `INT64` | yes |
| uint8 | integer | `int64` | `int64` | `UINT8` | yes |
| uint16 | integer | `int64` | `int64` | `UINT16` | yes |
| uint32 | integer | `int64` | `int64` | `UINT32` | yes |
| uint64 | unsigned integer | `uint64` | `uint64` | `UINT64` | yes |
| float | float | `float64` | `float64` | `FLOAT32` | yes |
| double | float | `float64` | `float64` | `FLOAT64` | yes |
| decimal | decimal | `*apd.Decimal` | `*apd.Decimal` | `DECIMAL(38, 10)` | yes |
| date | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| timestamp | timestamp | `time.Time, in the timezone of the session` | `time.Time` | `TIMESTAMP` | yes |
| timestamp_tz | timestamp | `time.Time, with its offset` | `time.Time` | `TIMESTAMP_TZ` | yes |
| interval | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL` | yes |
| string | string | `string` | `string` | `STRING` | yes |
| binary | binary | `[]byte` | `[]uint8` | `BINARY` | yes |
| array | array | `[]any, of the Go types of its elements (D119)` | `[]interface {}` | `ARRAY(INT32 NULL)` | yes |
| map | map | `map[string]any, or map[any]any for a key that is not a String (D119)` | `map[string]interface {}` | `MAP(STRING, INT32 NULL)` | yes |
| tuple | tuple | `[]any, of the Go types of its fields (D119)` | `[]interface {}` | `TUPLE(INT32 NULL, STRING NULL)` | yes |
| variant | json | `the decoded JSON value: nil, bool, string, int64, float64, *apd.Decimal, []any or map[string]any` | `interface {}` | `VARIANT` | yes |
| bitmap | bitmap | `none: reading a value fails with dbimp.ErrNotSupported (D119)` | `interface {}` | `BITMAP` | yes |
| vector | vector | `dbimp.Vector[float32]` | `dbimp.Vector[float32]` | `VECTOR(3)` | yes |
| geometry | geometry | `string, in WKT, which the driver asks for. []byte for WKB, and the decoded value for GeoJSON (D136)` | `string` | `GEOMETRY` | yes |
| geography | geometry | `string, in WKT, which the driver asks for. []byte for WKB, and the decoded value for GeoJSON (D136)` | `string` | `GEOGRAPHY` | yes |
<!-- /dbimp:types -->

Measured on 2026-09-29 (recorded, item 3):

- Every value is a JSON string or `null`. The type of each column is in
  `schema`, with `Nullable(...)` for a column that can be NULL, such as
  `Nullable(Decimal(76, 20))` or `Nullable(Array(Int32 NULL))`.
- An integer keeps its digits from `Int8` to `UInt64`, and a `Decimal(76,
  20)` keeps 76 digits. A float is written as `-3.4e+38`, `Infinity`, `NaN`
  or `-0.0`. A boolean is `1` or `0`.
- In the `display` mode, the default, a `Date` is `2026-09-29`, a
  `Timestamp` is `2026-09-29 12:34:56.123456` in the timezone of the session,
  and a `Timestamp_Tz` is `2026-09-29 12:34:56.123456 +0530`. An `Interval`
  is `1 day 2:00:00.000003` or `-1 month`. In the `driver` mode, a date is
  the days since 1970, a timestamp the microseconds, and a `Timestamp_Tz`
  the microseconds and the offset in seconds, such as
  `1790665496123456 19800`. The other types arrive the same in both modes.
- An `Interval` is a number and a unit for years, months and days, each
  with its own sign, then a clock, such as `1 year 2 months 3 days
  4:05:06.5`, `-1 month -2 days`, `-0:00:00.000001` and `100 days 25:00:00`,
  and zero is `00:00:00` (measured on 1.2.948 on 2026-09-30). The server
  keeps microseconds. It reads its own form as an argument, and ISO 8601
  through `to_interval` too, but not an ISO 8601 part with a sign. It drops a
  fraction of a microsecond with no error, and `'6.5 seconds'` loses its
  fraction where `'0:00:06.5'` keeps it. The driver reads an `Interval` as a
  `dbimp.Interval`, writes an argument in the form of the server, and refuses
  an argument with a fraction of a microsecond (D138).
- A `Date` is a `dbimp.Date`, and a `UInt64` a `uint64`, whatever its value
  (D138). The server gives a positive integer literal above the range of an
  `Int32` the type `UInt64`, and `count(*)`, `numbers` and `nextval` return
  one, so each of them is a `uint64`.
- A `Binary` is hex, such as `00FF`, and base64 with
  `binary_output_format=base64`. A `Geometry` and a `Geography` are GeoJSON,
  and WKT with `geometry_output_format=WKT`. The setting holds inside an
  `Array`, a `Map` and a `Tuple` too: WKT, EWKT, WKB and EWKB there are
  quoted strings, and GeoJSON is bare JSON. WKB and EWKB are hex (measured
  on 1.2.948 on 2026-09-30). The driver decodes each one by D136.
- An `Array`, a `Map` and a `Tuple` arrive as text of SQL, not JSON:
  `[1,NULL,3]`, `{"a":1,"b":NULL}` and `(1,"x")`. A `Variant` arrives as
  JSON text, so its JSON null is the string `null`, where an SQL NULL is
  JSON `null`. A `Vector(3)` is `[1.5,-2.0,3.0]`. The server casts a JSON
  array argument, and its text, to a `Vector` (measured on 1.2.948 on
  2026-09-30). The driver reads a `Vector` as a `dbimp.Vector[float32]`, and
  sends a `dbimp.Vector` as a JSON array (D139).
- A `Bitmap` arrives as the text `<bitmap binary>`, so its value is lost.
  `bitmap_to_array(bm)` gives `[1,3,5]`.
- With `format_null_as_str=1`, a NULL is the string `NULL`, in an `Int32`
  column and in a `String` column alike, so it cannot be told from the text
  `NULL` (recorded). Its default is 0, and a NULL is then JSON `null`.
- `enable_http_handler_result_typed_json`, which DeepSeek named, is no
  setting: the values stay strings (recorded).
- The range of a `Date` is `0001-01-01` to `9999-12-31` on both releases.
  1.2.881 refuses a `Timestamp` of `9999-12-31 00:00:00`, and
  `to_timestamp` clamps a larger value to `9999-12-30 22:00:00`. 1.2.948
  takes `9999-12-31 00:00:00`. A timestamp before year 1 clamps to
  `0001-01-01` on 1.2.881, and fails on 1.2.948 with "timestamp is out of
  range".
- `18446744073709551615 + 1` gives `0`, with no error (recorded).

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

Measured on 2026-09-29 (recorded, item 4), on both releases:

- `params` as a JSON array binds each `?` in order, and as an object each
  `:name`. A number, a string, `null`, `true`, an array and an object bind,
  and `9223372036854775807` and `18446744073709551615` keep their digits. A
  decimal sent as a string and cast with `::DECIMAL(38, 10)` keeps its
  digits.
- A decimal sent as a JSON number goes through a `Float64`:
  `1234567890123456789012345678.0123456789` inserted into a
  `DECIMAL(38, 10)` came back as `1234567890123456752549132460.6797053952`.
  Sent as a JSON string, it kept every digit (measured with `curl`). So the
  driver sends a decimal as a string (D124).
- An object for `?` fails with 1006 "params must be a JSON array for
  positional placeholders (?)". Too few values fail with 1006 "not enough
  parameters". A `?` inside a string or a comment is not a placeholder.

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

Measured on 2026-09-29 (recorded, item 11), as both principals, on both
releases:

- `BEGIN` returns `session` with `txn_state` `Active`, `need_sticky` true,
  and `internal`, which names the last query. Each statement of the
  transaction sends back `database`, `txn_state` and `internal`, and gets a
  new `internal`. A write is not seen outside the transaction before
  `COMMIT`, and `ROLLBACK` removes it.
- A DDL statement commits the transaction. The `ROLLBACK` after it fails
  with 4003 "Transaction timeout: last_query_id ... not found on this
  server", and the write before the DDL stays.
- Any error of a statement aborts the transaction on the server. An error
  while the statement runs, such as 1006 of `SELECT to_uint8(300)`, sets
  `txn_state` to `Fail` (measured with `curl` on both releases). The answer
  to a statement that fails to start, such as one on an unknown table
  (1025), still says `Active` (recorded), and yet the next statement fails
  with 4002 "Current transaction is aborted, commands ignored until end of
  transaction block" (tested). `COMMIT` then succeeds and ends it. So the
  driver ends the transaction on any error of its statements (D122).
- `txn_state` `Active` with no `internal` fails with 5112 "Transaction is
  active but missing server_info".

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

Measured on 2026-09-29 (recorded, item 6), on both releases:

- A statement that fails to start, such as a syntax error or an unknown
  table, is HTTP 200 with `state` `Failed` and `error` `{"code", "message"}`,
  such as 1005 and 1025. The message of 1005 shows the text and a caret.
- An error after rows arrives on a later page: 20,000 rows came in two
  pages, and the next page had HTTP 200, `state` `Failed` and 1006.
- A wrong password is HTTP 401 with 5100 (recorded, item 8). An unknown
  query or page is HTTP 404. A refused privilege is HTTP 200 with 1063
  "Permission denied".

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

### The refused credential (D197)

`errors.Is(err, dbimp.ErrAuthentication)` is true for an `*Error` whose `Code`
is 5100 or whose `HTTPStatus` is 401. The server sends both for a wrong
password: HTTP 401 with 5100 and `Authentication failed: incorrect password`
(recorded: "a wrong password", on both releases).

It is false for code 1063, `Permission denied`, which arrives with HTTP 200
(recorded: "a database that needs a privilege"). It is also false for any
other code, such as 1005. The server source says that HTTP 401 is also the
status for a page of a query that belongs to another user, so such a page
matches too (not measured). The test `TestAuthenticationDatabend` reads each
recorded answer from `testdata/databend/`.

## Cancellation and timeouts

Measured on 2026-09-29 (recorded, item 7), as both principals, on both
releases:

- `GET` on `kill_uri` answers HTTP 200 with an empty body, and the query is
  then `Failed`. A kill of an unknown query is HTTP 404.
- A query that the client left 2 seconds into its `POST` still ran 3
  seconds later, by `GET /v1/query/<id>`. So a disconnect does not stop a
  query, and the driver kills it (D36).
- `max_execute_time_in_seconds` in `session.settings` stops a query with
  1043 "Query aborted due to execution time exceeding maximum limit". Its
  default is 0, no limit. `max_execution_time`, which DeepSeek named, is no
  setting, and a query with it ran for 60 seconds (measured with `curl`).
- A query id that the client names with `X-DATABEND-QUERY-ID` and sends
  again as the same user returned the result of the first query, and for
  another user HTTP 400 "query_id ... already exists" (recorded, item 12).
  1.2.948 refused the same id of the same user with HTTP 400 more than 8
  minutes after its first run.

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

Measured on 2026-09-29 (recorded, item 10), on both releases:

- Two statements in one request fail with 1005. One statement that ends
  with `;` runs.
- `--` and `/* */` are comments. `#` is not, and fails with 1005.
- `SELECT version()` gives `Databend Query v1.2.881-ca29960f5c(...)` and
  `Databend Query v1.2.948-nightly-1df0991d1f(...)` (recorded, item 9).
  `SELECT version();`, which `usql` runs, gives the same through the
  driver, for the administrator and for the ordinary user alike (tested on
  2026-09-29, step 16).

- Whether one request can hold two statements is not measured. Gemini and
  DeepSeek said that it cannot, and that the server returns an error.
- Comments are not measured.
- The version is `SELECT version()`, which `usql` runs (see Summary).
  `/v1/session/login` returns `version`, and every response carries
  `X-DATABEND-VERSION` (the server source).

## Principals

Measured on 2026-09-29, on both releases:

- `dbmeta_user` has `ALL` on the database `dbmeta`, and runs every
  statement of CRUD there (recorded).
- `RESULT_SCAN(LAST_QUERY_ID())` fails with 1006 in a request with no
  session, which names no last query (recorded). In the session that the
  driver keeps, it fails with 1016 "No cache key found in current session",
  because the server kept no result to scan (tested on 2026-09-29).
- It is refused, with 1063, `CREATE DATABASE`, `system.settings`, a
  sequence, a stage, a task and an aggregating index, which need `Super`, and
  `FLASHBACK`, which needs `Alter` on `*.*`. On 1.2.881 an inverted and an
  ngram index need `Super` too, and 1.2.948 lets it make them (recorded).
- A temporary table needs a session that a cookie keeps: without one it
  fails with "can not use temp table in http handler if cookie is not
  enabled" (recorded), and with a cookie jar and
  `X-DATABEND-CLIENT-CAPS: session_cookie` it works (measured with `curl`).

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

`databend/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the credentials and the database. |
| `driver.SessionResetter` | no | A USE or a SET stays in the session of the connection for as long as it lives, as on MySQL, so nothing needs a reset (D126). |
| `driver.Validator` | no | A connection holds its session in memory, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, a uint64 and a decimal, which the server binds as JSON (D120). |
| `driver.QueryerContext` | yes | The server binds each argument itself, through params (D120). |
| `driver.ExecerContext` | yes | Exec reads the result to its end, and RowsAffected is the count that the result names. |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, bound each time. |
| `driver.ConnBeginTx` | yes | A transaction that the session carries, which a DDL statement ends (D121). |
| `driver.RowsColumnScanner` | yes | A value is decoded from its text when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, so a response has one result. |
| `driver.RowsColumnTypeScanType` | yes | schema names the type of each column (D118). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | schema names the type of each column, such as DECIMAL(38, 10). |
| `driver.RowsColumnTypeLength` | no | schema names no length. |
| `driver.RowsColumnTypeNullable` | yes | schema wraps a type that can be NULL in Nullable. |
| `driver.RowsColumnTypePrecisionScale` | yes | A Decimal names its precision and its scale. |
<!-- /dbimp:interfaces -->

## Faults

These are faults of the server that the tests met (measured on 1.2.881 and
1.2.948, 2026-09-29):

- `UPDATE` of a `TIMESTAMP_TZ` column fails with 1104 "internal error:
  entered unreachable code: unable to merge TimestampTz". An `INSERT` works.
  The round trip of `timestamp_tz` skips its updates, with that reason.
- Inside a nested value, the server escapes a `"` with a backslash, and
  writes a backslash as it is. So a string that ends with a backslash, such
  as `x\`, arrives as `"x\"`, which reads as an escaped quote, and the
  driver reads the rest of the value wrong. The driver reads `\"` as a quote
  always (`types.go`).
- `POST /v1/session/login` with basic authentication is HTTP 500, with
  "request handler panicked" (recorded, item 1).
- A `Bitmap` arrives as the text `<bitmap binary>` in every setting (Types).

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

Step 7 asked both models again on 2026-09-29, about what step 6 left open.
Gemini timed out three times and gave no answer. DeepSeek gave these leads,
each tested on both releases:

- Typed JSON through `enable_http_handler_result_typed_json`. Wrong: no such
  setting exists, and the values stay strings (recorded, item 3).
- A bitmap through `bitmap_to_array`. Right (recorded, item 3).
- A login needs no body. Wrong: no body is HTTP 400, and `{}` is HTTP 500
  (recorded, item 1).
- A disconnect during the `POST` cancels the query. Wrong: the query still
  ran 3 seconds later (recorded, item 7).
- A timeout through `max_execution_time`. Wrong: the setting is
  `max_execute_time_in_seconds` (recorded, item 7).
- A query id is kept for `http_handler_result_timeout_secs`, 60 seconds.
  Not settled: 1.2.948 refused an id more than 8 minutes after its first
  run (recorded, item 12).
- A temporary table needs a session kept by a cookie. Right, with
  `X-DATABEND-CLIENT-CAPS: session_cookie` (measured with `curl`).

## Open questions

Step 9 answers the questions below in D117 to D123 of
[decisions/](decisions/README.md), which Ken decided on 2026-09-29.

- None about the name. The scheme `databend` of `dburl` and the name
  `databend` belong to the Go driver, which `usql` ships, and the second
  `sql.Register` of `databend` panics if both are linked. Ken decided on
  2026-09-28 that this driver replaces the Go driver in `usql` and `dburl`,
  so the two are never linked together (D76).
- None about the aliases of `dburl`. dburl D39, staged on 2026-09-29, makes
  `GenDatabend` write the scheme `databend` for every alias (Summary).
- None about the releases. The range is the newest stable release,
  v1.2.881, and the newest weekly release, 1.2.948, which `dbrun` lists in
  the Staged tier with the cadence `tested` (Summary).
- None about the parameters of an older release. `params` needs v1.2.881 or
  later, and the range of dbmeta D112 has no older release.
- A DDL inside a transaction commits it, and the next statement of the
  transaction fails with 4003 (Transactions). How `BeginTx` answers that is
  a decision of step 9 (D20).
- JSON or Arrow. Arrow is a binary encoding, and it needs Ken's approval for
  this driver (D13). JSON gives every type except the value of a `Bitmap`.
- How the driver reads an `Array`, a `Map` and a `Tuple`, which arrive as
  text of SQL and not JSON, and a `Bitmap`, which arrives as
  `<bitmap binary>` (Types). A decision of step 9.
- Whether a connection keeps a session with a cookie, which a temporary
  table needs (Principals). A decision of step 9.
- The URL: the default port, the keys, and how TLS is chosen. The `dburl`
  session asked to hear it when step 9 settles it (Summary).
- None about the entry in `dbrun`. The `dbmeta` session wrote it from
  dbmeta D112, and each Tested release passed `dbrun test`.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-09-29 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of Databend from the sections above.

### The server

| | Couchbase | Databend |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /v1/query`, with `sql`, `session` and `params` |
| Database | The key `query_context` of the body | `session.database` of the body |
| Language | SQL++, which is close to SQL | SQL |
| DDL | In SQL++ | In SQL, and a DDL statement commits a transaction (Transactions) |
| Parameters | `?`, `$1` and `$name` | `?` from an array, or `:name` from an object, never both (Parameters) |
| Framing | One body for the whole result, which does not page | Pages of at most 10,000 rows, each one a `GET` of `next_uri` (Responses) |
| Columns | `signature`, before the first row | `schema`, with a type for each column, before the rows of each page |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement |
| Errors | Can come with HTTP 200, after some rows | HTTP 200 with `error`, before the rows or on a later page (Errors) |
| Types | JSON. No date, decimal, UUID or binary | Text for every value, with a type in `schema`, and an array, a map and a tuple as text of SQL (Types) |
| Cancel | The server stops a query when the client leaves | The query runs on when the client leaves, and `kill_uri` stops it (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | `BEGIN` in SQL, carried by `session` in each request |
| Authentication | Basic, or `creds` in the body | Basic, or a Bearer token |
| Default port | 8093, or 18093 with TLS | 8000 |

The differences that a caller sees:

- A result arrives in pages, and the driver asks for each one when the rows
  need it (D123).
- The server does not stop a query when the client leaves, so the driver
  kills it (D123).
- A DDL statement ends a transaction, and so does any error, and `Commit`
  then says so (D121 and D122).
- `USE` and `SET` stay on the connection for as long as it lives (D122 and
  D126), where Couchbase keeps no session.

### The driver

| | `couchbase` | `databend` |
| --- | --- | --- |
| Size, without tests, on 2026-09-29 | About 1300 lines in 8 files | About 2400 lines in 11 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Database`, `Cancel`, `Timezone`, `Auth` |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, `WithCancel` and `WithTimezone`, through `WithOptions` or an argument (D109 and D117). `WithTimeout` sends `max_execute_time_in_seconds`. `WithReadonly` gives `dbimp.ErrNotSupported`. An option of one statement does not stay in the session (D122) |
| Arguments | Sent to the server as `args` and `$name` | Sent in `params`: an array for `?`, or an object for `:name`, never both (D120). A decimal goes as a string (D124). A `[]byte` is refused |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | A reader of its own (`rows.go` and `page.go`), which follows `next_uri` one page at a time with the context of `QueryContext` (D123). The page reader is an interface of three methods, after SurrealDB (D108 and D119) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName`, `ColumnTypeScanType`, `ColumnTypeNullable` and `ColumnTypePrecisionScale`, from `schema` (D118) |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | Every value arrives as text, and the driver decodes it by its type: `int64`, `float64`, `*apd.Decimal`, `bool`, `[]byte`, `time.Time`, and `[]any` and maps for the nested types (D118 and D119). A `Bitmap` fails with `dbimp.ErrNotSupported` |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from the columns named `number of rows ...`, or `dbimp.ErrNotSupported` when the result names none, as for `REPLACE INTO`. `LastInsertId` returns `dbimp.ErrNotSupported` |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` sends `BEGIN`, and the session carries the transaction (D121). A DDL statement or any error ends it, and `Commit` then returns why (D121 and D122). `ReadOnly` gives `dbimp.ErrNotSupported` |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. `USE` and `SET` stay on the connection for as long as it lives (D126) |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The driver names each query, and kills it when the context ends or the rows close early, with `cancel=kill` (D123) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Code, Message}`, which unwraps to `*dbimp.StatusError` for a status that is not 2xx |
| Authentication | Basic | `auth=basic` or `auth=bearer` (D94) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, and `CancelKill`, `CancelNone`, `AuthBasic` and `AuthBearer` |

The differences that a caller sees:

- The columns have a database type and a scan type from `schema` (D118),
  as Couchbase has from its signature.
- A value keeps its type, a time and a decimal too, where Couchbase gives
  JSON shapes (D118). A nested value is decoded from text of SQL (D119).
- A `Bitmap` cannot be read at all, because JSON carries none of its bytes
  (D119).
- Positional and named arguments cannot be mixed in one statement (D120).
- A `[]byte` argument is refused, where Couchbase sends it as base64 (D44),
  because `params` has no form for bytes (D120).
- `WithReadonly` gives `dbimp.ErrNotSupported`, where Couchbase sends
  `readonly` (D117).
- `RowsAffected` returns an error for a statement whose result names no
  count, such as `REPLACE INTO`, where Couchbase counts every statement by
  `mutationCount` (D125).
