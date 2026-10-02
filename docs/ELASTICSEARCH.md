# Elasticsearch

This file holds what is known about Elasticsearch, for the driver
`elasticsearch` (W28 and D162). The headings are the template of
[DRIVER.md](DRIVER.md).

Steps 5a and 6 measured 8.19.22, 9.4.6 and 9.5.3 on 2026-10-01 and
2026-10-02, as `elastic`, the administrator, and as `dbmeta_user`, an
ordinary user who can read the indices whose names start with `dbmeta`. A fact
marked "recorded" is in `testdata/elasticsearch/`, and the name in quotes
after it is the name of its request in `requests.json` there. The three
releases gave the same answers, except where a line says otherwise. A request
whose name starts with `step 7:` ran on 9.5.3 only. A fact marked "measured"
names how it was measured. Each section starts with the measured facts. The
facts after them come from the sources of step 3, and each one that is not
measured says so. The sources, each read on 2026-10-01, are these:

- "The dbmeta entry" is `container/elasticsearch.go` in `dbmeta`, at its
  commit `724e64e`. It starts the image `docker.io/library/elasticsearch`
  with security on and TLS off. Its `Init` makes the role `dbmeta_role`,
  which can `read` and `view_index_metadata` on the indices `dbmeta*`, the
  user `dbmeta_user` with that role, and the index `dbmeta`.
- "The Go client" is `github.com/elastic/go-elasticsearch` v9.5.2: the
  request of `typedapi/sql/query` and the APIs under `typedapi/sql`.
- "The Python driver" is `github.com/preset-io/elasticsearch-dbapi` at
  commit `2303279`: `es/baseapi.py` and `es/elastic/api.py`.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, asked
  on 2026-10-01 and 2026-10-02.

## Summary

- Elasticsearch is a search engine with a document store. A query reaches it
  in the Query DSL, in ES|QL, or in SQL through `POST /_sql`. The driver
  reads SQL only. The Query DSL and ES|QL fail S ([TARGETS.md](TARGETS.md)).
- R holds. `dbrun` starts `elasticsearch-8.19.22` and
  `elasticsearch-9.5.3`, in the Staged tier with the cadence `tested`, and
  `elasticsearch-9.4.6` with the cadence `nightly` (the dbmeta entry).
  Each one started, and `GET /` gave its version (recorded: "the version").
- H and S hold. `POST /_sql?format=json` answered SQL with HTTP 200 as both
  principals on each release (recorded: "a statement").
- SQL in Elasticsearch reads only. `INSERT`, `UPDATE`, `DELETE`,
  `CREATE TABLE`, `CREATE VIEW` and `BEGIN` fail with a parse error that
  names the statements the parser takes: `DEBUG`, `DESC`, `DESCRIBE`,
  `EXPLAIN`, `SELECT`, `SHOW`, `SYS`, `WITH` and `(` (recorded: "crud:
  insert", "crud: update", "crud: delete", "schema: create table" and
  "begin a transaction"). A write goes through the document API, such as
  `PUT /<index>/_doc/<id>` (recorded: "document: index a document").
- The priority is P1 ([TARGETS.md](TARGETS.md)). Ken gave Elasticsearch and
  OpenSearch a driver each (D162).
- `dburl` has no scheme for Elasticsearch (measured with `grep` in
  `dburl/scheme.go` on 2026-10-01). `usql` has no driver for it (measured
  with `ls usql/drivers` on 2026-10-01).
- `dbmeta` has no model for Elasticsearch. Its entry exists only so that
  `dbrun` can start a server for this driver (the dbmeta entry, dbmeta D118).

## Requests

These facts were recorded on each release:

- A statement is `POST /_sql?format=json` with a JSON object whose `query`
  holds the text (recorded: "a statement"). `GET` with the same body works
  too (recorded: "a GET with a body"). With no `format`, the answer is JSON
  (recorded: "a statement with no format").
- A request with no `Content-Type` fails with HTTP 406 (recorded: "no
  content type"). A body that is not JSON fails with HTTP 400 (recorded: "a
  body that is not JSON"). A field of the body that the server does not know
  fails with HTTP 400, `unknown field [nope]` (recorded: "an unknown field").
- A setting goes in the body. `field_multi_value_leniency` in the query
  string fails with `contains unrecognized parameter`, and so does `query`
  (recorded: "a setting in the query string" and "a statement in the query
  string").
- `format` also takes `txt`, `csv`, `tsv`, `yaml`, `cbor` and `smile`
  (recorded: "a statement as text" and the requests after it). `nope` fails
  with HTTP 400 (recorded: "an unknown format").
- `columnar: true` gives `values`, one array for each column, in place of
  `rows` (recorded: "a statement in columns").
- `mode: jdbc` with a `version` fails with HTTP 403, `current license is
  non-compliant for [jdbc]`. The basic license of the image does not allow
  it (recorded: "a statement in the jdbc mode").
- The body takes `fetch_size`, `cursor`, `params`, `time_zone`,
  `field_multi_value_leniency`, `filter` with the Query DSL,
  `runtime_mappings`, `catalog`, `request_timeout`, `page_timeout`,
  `keep_alive`, `wait_for_completion_timeout`, `keep_on_completion` and
  `allow_partial_search_results` (recorded under items 3 to 7, each by
  name). `index_using_frozen`, which the Go client sends, fails with
  `unknown field [index_using_frozen] did you mean [index_include_frozen]?`.
  `project_routing` fails with `[project_routing] is only allowed when
  cross-project search is enabled` (recorded: "step 7: index_using_frozen"
  and "step 7: project_routing").
- `POST /_sql/translate` gives the Query DSL of a statement, and does not run
  it (recorded: "translate a statement").
- Authentication is HTTP basic. A wrong password fails with HTTP 401 and
  `WWW-Authenticate` for `Basic` and for `ApiKey` (recorded: "a wrong
  password").
- Each answer has the headers `X-Elastic-Product: Elasticsearch` and
  `Took-Nanos` (recorded).

These facts come from the sources, and are not measured:

- The body takes `index_include_frozen` (the server, in the message above).
- A client can authenticate with an API key, as `Authorization: ApiKey
  <key>`, or with a bearer token (Gemini).

## The DSN

- `dburl` has no scheme, so step 9 decides the URL (D27 and D35).
- The dbmeta entry gives each principal as `http://user:password@host:port`,
  with the port 9200 inside the container. `dbrun` publishes it on a port of
  its own (measured with `dbrun dsn --json` on 2026-10-02).
- The Go client takes a list of addresses, a user and a password, an API
  key, a service token and a cloud id (the Go client, not measured).

## Responses

These facts were recorded on each release:

- An answer is one JSON object: `columns`, an array of `name` and `type`,
  then `rows`, an array of arrays in the order of `columns`, and `cursor` if
  more rows follow (recorded: "a statement" and "the first page of 100").
  So the columns arrive before the first row, and rule 1 of D18 applies.
- The columns keep the order of the statement (recorded: "the order of the
  columns"). `SELECT *` gives the columns sorted by name, because the
  mapping holds them so (recorded: "select star sorts the columns" and
  "every type"). Two columns can have one name (recorded: "two columns with
  one name"). A result with no rows has `columns` and an empty `rows`
  (recorded: "an empty result").
- The type of a column is the name of a mapping type or of an SQL type in
  lower case, such as `integer`, `keyword`, `datetime` or
  `interval_day_to_second`. A `constant_keyword` and a `wildcard` column
  arrive as `keyword`, a `match_only_text` as `text`, and a `date_nanos` as
  `datetime` (recorded: "every type"). See Types.
- `SELECT *` leaves out an object, a nested field, a `dense_vector`, a
  `flattened`, a range, a `point` and an `aggregate_metric_double`, and gives
  each subfield of an object as a column of its own, such as `o.a`
  (recorded: "every type"). A subfield of a text, such as `t.raw`, does not
  appear in `SELECT *`, and a statement can name it (recorded: "a subfield of
  a text").
- The default page is 1000 rows. 250 rows came in one answer with no
  `cursor` (recorded: "the default page").
- `fetch_size` sets the rows of a page. With 100, the first answer has
  `columns`, 100 rows and a `cursor` (recorded: "the first page of 100").
  `POST /_sql?format=json` with `{"cursor": ...}` gives the next page, with
  `rows` and a new `cursor`, and no `columns` (recorded: "the second page").
  The last page has no `cursor` (recorded: "the last page").
- A cursor can be sent twice while its search lives, and gives the same page
  again (recorded: "the second page again"). Once the last page was read,
  the server has closed it, and a cursor of that search fails with HTTP 404,
  `search_context_missing_exception` (recorded: "a cursor after the last
  page"). A body with a `cursor` and a `query` reads the cursor (recorded:
  "a cursor with the query"). A cursor that is not base64 fails with HTTP 400
  (recorded: "a cursor that is not one").
- `POST /_sql/close` with a cursor answers `{"succeeded":true}`, and
  `{"succeeded":false}` the second time. A closed cursor fails with HTTP 404
  (recorded: "close a cursor", "close a cursor again" and "a closed
  cursor").
- `fetch_size` of 0 fails with HTTP 400 (recorded: "a fetch size of zero").
  `GROUP BY`, `LIMIT` and `PIVOT` page too (recorded: "a group by in pages",
  "a limit across pages" and "a pivot in pages").
- `index.max_result_window` caps a page, not a result. On an index whose
  window is 100, a page of 1000 rows and a `LIMIT 200` fail with HTTP 400,
  `Result window is too large`. Pages of 100 read all 250 rows. A `GROUP BY`
  read all 250 groups in one answer (recorded: "a page larger than the
  result window" and the requests after it). The default window is 10000,
  so a `fetch_size` of 100000 fails (recorded: "a fetch size above the
  default result window").
- `page_timeout` of `1s` did not end a cursor that was read three seconds
  later (recorded: "a cursor after its page timeout").
- `LIMIT` and `TOP` work. `OFFSET` is a parse error (recorded: "a limit
  across pages", "top" and "an offset").
- The async form: `wait_for_completion_timeout` of `0s` with
  `keep_on_completion` answers `id`, `is_partial`, `is_running` and an empty
  `rows`, with no `columns` (recorded: "async: a statement").
  `GET /_sql/async/<id>?format=json` gives the result. `GET
  /_sql/async/status/<id>` gives `completion_status`. `DELETE
  /_sql/async/delete/<id>` answers `acknowledged`, and the result is then
  gone with HTTP 404 (recorded: "async: the result" and the requests after
  it). An async result larger than `fetch_size` has `columns`, its first
  page, `id` and a `cursor` for the next page (recorded: "async: a result
  in pages").
  `POST /_sql/async/execute` fails with HTTP 405 (recorded: "the async
  execute path").
- A request with `Accept-Encoding: gzip` gets `Content-Encoding: gzip`
  (recorded: "a gzip answer"). No answer was a redirect, and no answer was
  HTTP 429 for the rate of requests (recorded).

These facts come from the sources, and are not measured:

- A cursor of a statement with no `ORDER BY` reads a scroll, and one with
  `ORDER BY` reads a point in time with `search_after` (Gemini).
- `keep_alive` is 45 seconds by default (Gemini).
- `search.max_buckets` caps the groups of a `GROUP BY` (Gemini and
  DeepSeek).

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table
on 2026-10-02 from the measurements below, for Ken to review. No code
writes it yet (step 10).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| null | null | `nil` | `interface {}` | `NULL` | yes |
| boolean | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| byte | integer | `int64` | `int64` | `BYTE` | yes |
| short | integer | `int64` | `int64` | `SHORT` | yes |
| integer | integer | `int64, and a number with a fraction of zero, such as 100.0 from HISTOGRAM, as its integer` | `int64` | `INTEGER` | yes |
| long | integer | `int64` | `int64` | `LONG` | yes |
| unsigned_long | unsigned integer | `uint64, whatever its value (D138)` | `uint64` | `UNSIGNED_LONG` | yes |
| half_float | float | `float64` | `float64` | `HALF_FLOAT` | yes |
| float | float | `float64` | `float64` | `FLOAT` | yes |
| double | float | `float64, with NaN and the infinities from their strings` | `float64` | `DOUBLE` | yes |
| scaled_float | float | `float64, rounded by the scaling factor of the mapping` | `float64` | `SCALED_FLOAT` | yes |
| keyword | string | `string` | `string` | `KEYWORD` | yes |
| text | string | `string` | `string` | `TEXT` | yes |
| binary | binary | `[]byte, from base64` | `[]uint8` | `BINARY` | yes |
| ip | string | `string, because SQL names it VARCHAR` | `string` | `IP` | yes |
| version | string | `string, because SQL names it VARCHAR` | `string` | `VERSION` | yes |
| datetime | timestamp | `time.Time, with the offset of time_zone and every digit of the second` | `time.Time` | `DATETIME` | yes |
| date | date | `dbimp.Date, from the date before the T` | `dbimp.Date` | `DATE` | yes |
| time | time of day with offset | `dbimp.OffsetTime, in milliseconds` | `dbimp.OffsetTime` | `TIME` | yes |
| geo_point | geometry | `string, in WKT` | `string` | `GEO_POINT` | yes |
| geo_shape | geometry | `string, in WKT` | `string` | `GEO_SHAPE` | yes |
| shape | geometry | `string, in WKT` | `string` | `SHAPE` | yes |
| interval_year | interval | `dbimp.Interval, from the months of the ISO period` | `dbimp.Interval` | `INTERVAL_YEAR` | yes |
| interval_month | interval | `dbimp.Interval, from the months of the ISO period` | `dbimp.Interval` | `INTERVAL_MONTH` | yes |
| interval_year_to_month | interval | `dbimp.Interval, from the months of the ISO period` | `dbimp.Interval` | `INTERVAL_YEAR_TO_MONTH` | yes |
| interval_day | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_DAY` | yes |
| interval_hour | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_HOUR` | yes |
| interval_minute | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_MINUTE` | yes |
| interval_second | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_SECOND` | yes |
| interval_day_to_hour | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_DAY_TO_HOUR` | yes |
| interval_day_to_minute | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_DAY_TO_MINUTE` | yes |
| interval_day_to_second | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_DAY_TO_SECOND` | yes |
| interval_hour_to_minute | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_HOUR_TO_MINUTE` | yes |
| interval_hour_to_second | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_HOUR_TO_SECOND` | yes |
| interval_minute_to_second | duration | `time.Duration, from the ISO duration in hours` | `time.Duration` | `INTERVAL_MINUTE_TO_SECOND` | yes |
<!-- /dbimp:types -->

The wire type is the `type` of a column. The database type is the name that
`SYS TYPES` gives (recorded: "step 7: the SQL types"). The `date`, `time` and
interval types are SQL types only. A column of an index never has one, and a
statement makes one with `CAST`, a literal or a function. `SYS TYPES` also
names `UNSUPPORTED`, `NESTED` and `OBJECT`, which no column can have,
because the server refuses to select them (below).

These facts were recorded on each release, from the index `dbmeta_types`,
which has one field of each mapping type (recorded: "every type"):

- A NULL is JSON `null`. A field that a document does not have, and a field
  set to `null`, both arrive as `null`. So NULL and a missing value are one
  value. An empty object gives `null` for each subfield. A
  `constant_keyword` gives its value in every row, also in a document with
  no fields.
- `byte`, `short`, `integer` and `long` are JSON numbers, and a `long` keeps
  every digit from -9223372036854775808 to 9223372036854775807.
  `HISTOGRAM(n, 100)` of an `integer` column gives the type `integer` and
  the values `0.0`, `100.0` and `200.0` (recorded: "a histogram of an
  integer").
- `unsigned_long` is a JSON number, and keeps every digit to
  18446744073709551615. `SYS TYPES` says that it is unsigned, with 20
  digits. `ul + 1` of the largest value fails with HTTP 500, `unsigned_long
  overflow` (recorded: "step 7: an unsigned_long that overflows").
- A `double` keeps 1.7976931348623157E308 and 4.9E-324. NaN and the infinity
  arrive as the strings `"NaN"` and `"Infinity"` (recorded: "a double that
  is not a number"). A `float` keeps 3.4028235E38 and -1.4E-45, and a
  `half_float` 65504.0.
- A `scaled_float` with the factor 100 gives 1234.57 for 1234.5678. `SYS
  TYPES` names it with the type number of `DOUBLE`.
- A `binary` is a string in base64, such as `"AP8="`.
- `ip` and `version` are strings, such as `"2001:db8::1"` and
  `"1.2.3-beta"`. `DESCRIBE` names both `VARCHAR` (recorded: "schema:
  describe").
- A `datetime` is ISO 8601 text: `"2026-10-01T07:04:56.789Z"` for a `date`
  field, which keeps milliseconds, and `"2026-10-01T12:34:56.123456789Z"`
  for a `date_nanos` field. The text has the offset of `time_zone`, such as
  `"2026-10-01T12:34:56.789+05:30"`, and is in UTC without it (recorded: "a
  time zone with an offset" and "a time zone with a name").
- A `date` is the text of midnight in `time_zone`, such as
  `"2026-10-01T00:00:00.000Z"`, or `"2026-10-01T00:00:00.000+05:30"`. The
  year -1 is `"-0001-01-01T00:00:00.000Z"` (recorded: "the edges of a
  date" and "literals of each SQL type").
- A `time` is text with an offset, such as `"12:34:56.789Z"`. It keeps
  milliseconds, so the time of a `date_nanos` loses its nanoseconds
  (recorded: "step 7: a date_nanos with a time zone").
- `geo_point`, `geo_shape` and `shape` are WKT, such as
  `"POINT (-71.34 41.12)"`.
- An interval of years and months is an ISO 8601 period, such as `"P1Y"`,
  `"P1Y2M"` and `"P-1Y-2M"`. An interval of days and time is an ISO 8601
  duration with no days, such as `"PT48H"` for `INTERVAL 2 DAY`,
  `"PT26H3M4.5S"` for `INTERVAL '1 02:03:04.5' DAY TO SECOND`, and
  `"PT-24H"` (recorded: "every interval").
- `CAST(1.5 AS DECIMAL)` gives a `double`. There is no decimal type
  (recorded: "step 7: a decimal").
- A literal that does not fit a `long`, or a `double` above its range, fails
  with HTTP 400, `Number [...] is too large` (recorded: "a number too large"
  and "a double too large").
- `AVG` gives a `double`, and `SUM` and `COUNT` a `long` (recorded: "the
  types of aggregates").

These facts were recorded about fields that are not one value:

- A field that holds an array in a document fails the statement with HTTP
  400, `Arrays (returned by [i]) are not supported` (recorded: "a
  multi-valued field"). With `field_multi_value_leniency: true`, the first
  value arrives, such as 1 for `[1, 2, 3]` (recorded: "a multi-valued field
  with leniency").
- An object cannot be selected. `SELECT o` fails with `Cannot use field [o]
  type [object] only its subfields`, and `SELECT o.a, o.s` works (recorded:
  "an object" and "the fields of an object").
- A nested field cannot be selected either. A subfield of it gives one row
  for each nested object, such as two rows for a document with two, and no
  row for a document whose nested array is empty (recorded: "a nested field"
  and "the fields of a nested field").
- `dense_vector`, `flattened`, `integer_range`, `point` and
  `aggregate_metric_double` fail with `Cannot use field [...] with
  unsupported type` (recorded: "a dense_vector", "a flattened field", "a
  range and a point" and "an aggregate_metric_double").
- A field of the type `alias` gives the type and the value of its target
  (recorded: "the alias field").
- `_id` and `_index` are unknown columns (recorded: "the id of a document"
  and "the index of a document").


## Parameters

These facts were recorded on each release:

- A statement names each parameter as `?`, and `params` holds a JSON array
  of plain values in their order (recorded: "positional parameters").
- The server gives each value a type from its JSON kind: a whole number is
  an `integer` or a `long`, a fraction a `double`, a string a `keyword`, a
  boolean a `boolean`, and `null` the type `null` (recorded: "parameters of
  every JSON kind").
- The number 18446744073709551615 arrives as the `long`
  9223372036854775807, with no error. The server cuts it to the range of a
  `long` (recorded: "parameters of every JSON kind").
  `CAST(? AS UNSIGNED_LONG)` of the string `"18446744073709551615"` keeps
  it, and `CAST(? AS DATE)` and `?::INTEGER` of a string work (recorded: "a
  cast of a parameter").
- A string compares with a `datetime` column, and keeps nanoseconds: `dn >
  ?` with `"2026-10-01T12:34:56.123456788Z"` found the row of
  `...123456789Z` (recorded: "a time as a parameter" and "step 7: a
  parameter for a datetime column").
- A parameter as an object with `type` and `value` fails with HTTP 400,
  `[params] must be an array where each entry is a single field (no objects
  supported)` (recorded: "a parameter with its type"). `params` as an
  object, for `:x`, fails with HTTP 400 (recorded: "named parameters"). An
  array as a value fails with HTTP 400 (recorded: "an array as a
  parameter").
- Too few values fail with HTTP 400, `Not enough actual parameters`. A value
  too many is ignored, with no error (recorded: "too few parameters" and
  "too many parameters").
- A `?` in a string literal is not a parameter (recorded: "a question mark
  in a literal"). `?` works in `IN`, and is a parse error in `LIMIT`
  (recorded: "parameters in IN" and "a parameter in LIMIT").

These facts come from the sources, and are not measured:

- The `jdbc` mode takes a parameter with its type (Gemini). The license of
  the image refuses that mode (above).
- The Python driver writes each parameter into the text itself (the Python
  driver, `apply_parameters`).

## Transactions

- Elasticsearch has no transactions. `BEGIN` and `COMMIT` fail with HTTP
  400, a parse error (recorded: "begin a transaction" and "commit").
- Each write through the document API acts on one document or one query, and
  is visible to SQL after a refresh (recorded: "document: index a document"
  and "crud: select", which the setup refreshes with `refresh=true`).

## Errors

These facts were recorded on each release:

- An error is a JSON object, `{"error": {"root_cause": [...], "type": ...,
  "reason": ...}, "status": ...}`, and `status` is the HTTP status of the
  answer (recorded: "a syntax error").
- A parse error, an unknown index and an unknown column give HTTP 400, with
  `parsing_exception` or `verification_exception` (recorded: "a syntax
  error", "an unknown index" and "an unknown column"). A division by zero
  gives HTTP 500, `arithmetic_exception` (recorded: "a division by zero").
- No error arrived with HTTP 200. An answer either holds rows or an error.
- An error after some rows arrives on a later page. With `fetch_size` 2,
  the first two pages of `dbmeta_types` gave rows, and the third failed with
  HTTP 400, `Arrays (returned by [i]) are not supported`, at the document
  that holds an array (recorded: "an error on a later page" and the two
  requests after it). Without paging, the same statement fails before any
  row (recorded: "an error in the first page").
- A missing cursor or a closed one gives HTTP 404,
  `search_context_missing_exception`. The text of its reason differs
  between 8.19.22 and 9.x (recorded: "a closed cursor").
- `request_timeout` that ends gives `search_timeout_exception`, with HTTP 504
  on 8.19.22 and HTTP 429 on 9.4.6 and 9.5.3 (recorded: "a request
  timeout"). So a HTTP 429 does not always mean a limit on the rate.
- A wrong password gives HTTP 401, and a missing privilege HTTP 403 with
  `security_exception` (recorded: "a wrong password" and, as `dbmeta_user`,
  "document: index a document").
- A method that a path does not take gives HTTP 405, and a path with no
  handler gives HTTP 400, `no handler found for uri` (recorded: "the async
  execute path" and "the SQL path of OpenSearch").

## Cancellation and timeouts

These facts were recorded on each release, with a statement that runs a
script in its `filter` for about two seconds:

- When the client leaves, the server cancels the task of the statement. 200
  milliseconds after the client gave up, 8.19.22 and 9.4.6 listed the task
  `indices:data/read/sql` with `cancelled: true`, and 9.5.3 listed no task
  (recorded: "a slow statement that the client leaves" and "the tasks after
  the client left").
- A statement can carry the header `X-Opaque-Id`, and `GET /_tasks` shows it
  on its task. `POST /_tasks/_cancel?actions=indices:data/read/sql` cancels
  it, and the statement then fails with HTTP 400, `task_cancelled_exception`
  (recorded: "the tasks while the statement runs", "cancel the statement on
  the server" and "a slow statement that the server cancels"). The ordinary
  user cannot list or cancel a task, which needs the cluster privilege
  `monitor` or `manage` (recorded as `dbmeta_user`).
- `DELETE /_sql/async/delete/<id>` of a running async statement cancels its
  task (recorded: "async: delete the slow statement" and "the tasks after
  the delete"). `POST /_sql/async/<id>/close` does not exist (recorded: "the
  async close path").
- `request_timeout` ends a statement on the server (recorded: "a request
  timeout"). See Errors for its status.

## Statements

- Two statements in one text fail with HTTP 400, a parse error at the `;`.
  A `;` at the end of one statement fails too (recorded: "two statements"
  and "a semicolon at the end").
- `--` and `/* */` comments work, also at the end of the text (recorded:
  "comments").
- A subquery in `FROM` works. A `JOIN` fails with `Queries with JOIN are not
  yet supported`, and a common table expression reads its name as an index
  (recorded: "step 7: a subquery", "step 7: a join" and "step 7: a common
  table expression").
- `SHOW TABLES`, `SHOW COLUMNS`, `DESCRIBE`, `SHOW FUNCTIONS`, `SHOW
  CATALOGS`, `SYS COLUMNS` and `SYS TYPES` give the catalog. A query of
  `information_schema` is a parse error (recorded under item 1).
- `SHOW TABLES` names an alias as a `VIEW` of the kind `ALIAS`, and an alias
  with a filter gives only the rows of its filter (recorded: "schema: show
  tables" and "schema: an alias with a filter").
- A quoted pattern names several indices, such as `"dbmeta_p*"` (recorded:
  "step 7: an index pattern"). `catalog` with the name of the cluster works,
  and another name fails with HTTP 404, `no such remote cluster` (recorded:
  "step 7: the local catalog" and "step 7: a remote catalog").
- `PIVOT`, `HISTOGRAM`, `MATCH` with `SCORE()`, the ODBC escapes such as
  `{d '2026-10-01'}`, and the geo functions such as `ST_AsWKT` work
  (recorded: "a pivot in pages", "a histogram of an integer", "the score",
  "the escapes of ODBC" and "geo functions").

## Principals

These facts were recorded as `dbmeta_user` on each release:

- The ordinary user runs SQL on the indices `dbmeta*`, and the async form
  (recorded: "a statement" and "async: a statement").
- An index outside `dbmeta*` is unknown to the ordinary user: `SELECT a FROM
  dbimp_secret` fails with HTTP 400, `Unknown index [dbimp_secret]`, and
  `SHOW TABLES` does not name it (recorded: "an index that the user cannot
  read" and "show tables names what the user can read").
- Each write of the document API fails with HTTP 403 (recorded: "document:
  index a document" and the writes after it).
- `GET /`, which gives the version, fails with HTTP 403, `action
  [cluster:monitor/main] is unauthorized` (recorded: "the version"). The
  status of an async statement and the list of tasks fail the same way
  (recorded: "async: the status" and "the tasks after the client left").
  The ordinary user can delete its own async statement (recorded: "async:
  delete").
- SQL has no function for the version. `DATABASE()` gives the name of the
  cluster and `USER()` the user, to both principals (recorded: "the database
  and the user").

## Flavors

- OpenSearch forked from Elasticsearch 7.10, and gets a driver of its own
  (D162). Elasticsearch has no `/_plugins/_sql`, the SQL path of OpenSearch
  (recorded: "the SQL path of OpenSearch").
- `POST /_query` answers ES|QL with `columns` and `values` (recorded:
  "ES|QL"). ES|QL fails S ([TARGETS.md](TARGETS.md)), and the driver does
  not speak it.
- `GET /` names the product in `X-Elastic-Product` and the release in
  `version.number`, to the administrator only (recorded: "the version").
- Elastic Cloud runs the same API as a service, and its serverless form
  adds `project_routing` (the Go client and the server message under
  Requests, not measured).

## Interfaces

No code exists yet. Step 10 writes this table from the code.

## Faults

`usql` has no driver for Elasticsearch, so no driver of `usql` has faults to
avoid. The other drivers that step 5a read have these faults, which this
driver must not repeat:

- The Python driver reads every page of a result into memory before it
  returns the first row (`fetch_remaining_pages` in `es/elastic/api.py`).
- The Python driver gives a type that it does not know as a string, and logs
  a warning (`get_type` in `es/baseapi.py`). D135 and hard rule 2 refuse
  both.
- The Python driver writes the parameters into the text (`apply_parameters`).
- The Go client sends `index_using_frozen`, which the server refuses
  (recorded: "step 7: index_using_frozen").

## Second opinions

Step 5a asked Gemini and DeepSeek the four questions on 2026-10-01. Step 7
asked both what step 6 did not find on 2026-10-01, and each lead went to
the server. A lead that the server did not show stays "not measured".

- CRUD. Both said that SQL reads only, and that a write goes through the
  document API. The server agrees. DeepSeek gave the error as `Unsupported
  statement INSERT`. The server gives a parse error.
- Schema. Both said that SQL has no DDL, and that an alias stands for a
  view. The server agrees. DeepSeek said that `information_schema` and
  `_id` exist. Both fail.
- Paging. Both named `fetch_size`, the cursor and `/_sql/close`. DeepSeek
  said that a `fetch_size` of 0 reads everything. It fails. Gemini said that
  a `GROUP BY` does not page. It pages. Gemini said that a cursor reads past
  `index.max_result_window`. It does, and a page cannot be larger than the
  window.
- Parameters. DeepSeek said that `params` as an object binds `:x`. It
  fails. Both proposed `CAST(? AS ...)`, which works, and DeepSeek `?` in
  `LIMIT`, which fails.
- Types. Gemini said that a large `unsigned_long` overflows. As a value it
  keeps every digit, and as a parameter it is cut to a `long`. DeepSeek said
  that `unsigned_long`, `TIME` and a decimal cannot be used. The first two
  work, and `DECIMAL` is a `double`. DeepSeek said that a multi-valued field
  arrives as an array and a nested field as an array. The server refuses
  both. Both said that a geo type arrives as WKT. It does.
- Statements. DeepSeek said that `JOIN` works in a restricted form. It
  fails.
- Async. DeepSeek named `POST /_sql/async/execute` and `POST
  /_sql/async/<id>/close`, and Gemini `DELETE /_sql/async/<id>`. The first
  two fail. The path that works is `DELETE /_sql/async/delete/<id>`.
- Cancel. Both proposed `X-Opaque-Id` with `POST /_tasks/<id>/_cancel`.
  Cancelling by action works for the administrator.
- Flavors. Gemini named CrateDB, which has `POST /_sql` with another shape.
  It is another product, which has no driver here (D88). DeepSeek named the
  OpenSearch SQL plugin, which Elasticsearch does not have.
- The jdbc mode and typed parameters. Gemini named both. The mode needs a
  license that the image does not have.

Step 8a asked both models on 2026-10-02 to review the mapping of the types
against [TYPES.md](TYPES.md) and D135:

- Both said that the day-time intervals are an exact length of time, the
  kind duration with `time.Duration`, and not the kind interval, because the
  server writes them as hours with no days and no months. The table follows
  them. The year-month intervals stay `dbimp.Interval`.
- Both agreed with the integers, the floats, the strings, the binary, the
  timestamp and the geometry as WKT. DeepSeek said that the driver reads
  `100.0` in an integer column as an integer only when its fraction is
  zero, and fails otherwise.
- DeepSeek said that `ip` and `version` are strings, because `DESCRIBE`
  names them `VARCHAR`. Gemini preferred `netip.Addr` for `ip`, in a second
  answer that had lost the context of the rule.
- DeepSeek said that a `date` comes from the text before the `T`, and never
  through a `time.Time` in UTC, because the offset of `time_zone` can move
  the day. Gemini proposed `time.Time` at midnight, which D138 replaced
  with `dbimp.Date`.
- DeepSeek said that `uint64` is not a value of `driver.Value`. D138 decided
  that an unsigned integer of 64 bits is a `uint64`, as the Databend and
  InfluxDB drivers return it.

## Open questions

Each of these waits for Ken, at step 9 or before:

1. The mapping of the types (step 8a). The day-time intervals are
   `time.Duration`, where the `INTERVAL` of Avatica is a `dbimp.Interval`.
   `ip` and `version` are strings.
2. A `uint64` argument above the range of `int64` is cut to a `long` by the
   server with no error. The driver can send it as a string in
   `CAST(? AS UNSIGNED_LONG)`, write it as a literal, or refuse it.
3. A field with several values fails the statement unless
   `field_multi_value_leniency` is true, and then gives the first value.
   Whether the driver sets it, and whether a caller can, is a decision.
4. The ordinary user cannot read the version with `GET /`. `usql` needs a
   statement for the version (step 16).
5. The server cuts no result short, and gives each page with a cursor. A
   page cannot pass `index.max_result_window`, so the size of a page is a
   decision.
