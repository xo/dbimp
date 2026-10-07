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
on 2026-10-02 from the measurements below, and Ken reviewed it. The code
writes it now (step 10). Run `DBIMP_UPDATE=1 go test -run TestTables
./elasticsearch` to write it.

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

These facts came from the integration tests, on each release, on 2026-10-07
(`TestIntegrationRoundTrip`):

- A `half_float` and a `float` arrive as the shortest text of a 32-bit
  float, such as `6.1035156E-5` for 2 to the power of -14. The driver reads
  that text as a `float64`, so the value is 6.1035156e-05, and not the exact
  32-bit value. A value that has a short exact text, such as 65504.0, 0.5 and
  0.25, comes back as it was stored.
- A `geo_point`, a `geo_shape` and a `shape` arrive as WKT with a decimal
  point in each number, whatever form the document held. A document with
  `POINT (0 0)` reads back as `POINT (0.0 0.0)`, and one with `POINT (-71.34
  41.12)` reads back as it was.
- A `date_nanos` field holds the dates from 1970-01-01 to
  2262-04-11T23:47:16.854775807Z, and the driver reads the last nanosecond.
  A `date` field holds the years 1 to 9999, and its day reads back as the
  day that was stored.
- No field has an interval type. `INTERVAL 1 DAY * v` with a `long` field `v`
  gives an `interval_day` with the value `PT72H` for 3, and NULL for a NULL.
  Every unit gives its own type the same way. The round trip of each interval
  type stores the number and reads the interval that the select makes.
- A `time.Duration` holds at most about 292 years. `INTERVAL 1 DAY *
  1048576` gives `PT25165824H`, which the driver refuses with an error that
  wraps `dbimp.ErrInvalidValue`, for that row.
- A `unsigned_long` stored as a JSON number keeps every digit, and its
  update to a second value and back to the first keeps them too.

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

What the driver does (D167):

- When the context ends, the driver closes the request, and the server
  cancels the task. After a statement whose context ended, no task of
  `indices:data/read/sql` ran for more than 20 seconds on any release
  (`TestIntegrationCancel`, 2026-10-07). The error is the error of the
  context, and never `driver.ErrBadConn`.
- When the caller closes the rows before the end, the driver closes the
  cursor with `POST /_sql/close`. The cursor follows the rows of a page, so
  `Close` reads the rest of the current page, at most 256 KiB and for at most
  5 seconds, to learn it. A page that is larger leaves its cursor to the
  server. Without this read, a cursor stays open until its `keep_alive`
  ends. With it, the count `open_contexts` of `GET /_nodes/stats/indices/search`
  was the same after five statements that the caller closed after 1, 100, 101,
  150 and 249 rows (`TestIntegrationPages`, 2026-10-07).
- When the context ends in the middle of a page, the driver does not know the
  cursor, and closes none.

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

The answer for step 16: `usql` has no driver for Elasticsearch, so it runs no
statement for the version. The driver sends none either. `GET /` gives
`version.number` to the administrator, and gives HTTP 403 to the ordinary
user. `SELECT DATABASE(), USER()` works as both principals, and gives
`docker-cluster` with `elastic` and with `dbmeta_user`. A version that the
ordinary user can read waits for `dbmeta` (Open questions). These facts were
measured on 8.19.22, 9.4.6 and 9.5.3 on 2026-10-07
(`TestIntegrationVersion` and `TestIntegrationPrincipals`). The same tests
show that the ordinary user can page a result on each release.

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

The releases differ in these ways, which the integration tests hold
(`TestIntegrationFeatures`, 2026-10-07):

- A `catalog` that is not a configured cluster gives HTTP 404 with
  `no_such_remote_cluster_exception` on 9.4.6 and 9.5.3. On 8.19.22 it gives
  HTTP 403 with `security_exception`, and the root cause is
  `no_such_remote_cluster_exception`, so the driver reports that type from
  `RootType`.
- `project_routing` is an unknown field on 8.19.22. On 9.4.6 and 9.5.3 it
  fails because cross-project search is off.
- A statement that passes its `request_timeout` gives HTTP 504 on 8.19.22 and
  HTTP 429 on 9.4.6 and 9.5.3 (Errors).

## Interfaces

The code writes this table (step 10). Run `DBIMP_UPDATE=1 go test -run
TestTables ./elasticsearch` to write it.

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the credentials. GET / needs the cluster privilege monitor, which the ordinary user lacks. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because the SQL API has no sessions. |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option and a uint64, which the driver refuses above the range of int64 (D167), and hands every other value to database/sql. |
| `driver.QueryerContext` | yes | The server binds each argument from the array params (D167). |
| `driver.ExecerContext` | yes | Exec reads the result to its end. SQL takes no write, so RowsAffected fails (D163). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because Elasticsearch has no transactions (D167). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, and its pages are one result. |
| `driver.RowsColumnTypeScanType` | yes | The answer names the type of each column (D167). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The answer names the type of each column, which the driver writes in upper case, as SYS TYPES does. |
| `driver.RowsColumnTypeLength` | no | The answer names no length. |
| `driver.RowsColumnTypeNullable` | yes | The answer does not say whether a column can be NULL, and every type can be. |
| `driver.RowsColumnTypePrecisionScale` | no | The answer names no precision and no scale, and there is no decimal type. |
<!-- /dbimp:interfaces -->

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

This driver does none of them. It reads each page one token at a time, and
reads the next page only when `Rows.Next` needs it (`TestLargeResult` reads a
result of 64 MiB in 128 pages, and the heap stays under 16 MiB). It gives a
type that it does not know as the decoded JSON value, and logs nothing. It
sends each argument in `params`, and never writes it into the text. It sends
only the keys that the server knows.

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

Ken answered the five questions of step 8 on 2026-10-02, and D167 holds the
answers:

1. The mapping of the types is the table in Types. The day-time intervals are
   `time.Duration`, and `ip` and `version` are strings (D167, item 3).
2. The driver refuses a `uint64` above the range of `int64`, because the
   server cuts it to the largest `long` with no sign (D167, item 5).
3. The driver leaves `field_multi_value_leniency` off, so that no value is lost
   with no sign, and the DSN key lets the caller turn it on (D167, item 9).
4. The ordinary user cannot read the version with `GET /`. The version waits
   for `dbmeta`, which can give `dbmeta_user` the privilege `monitor`. The
   driver sends no request for the version.
5. The page is 1000 rows by default, and the key `fetch_size` changes it
   (D167, item 2). A page cannot pass `index.max_result_window` of an index.

These questions came from steps 10 to 17a. Ken closed questions 6 and 7 on
2026-10-07 (D178). Questions 8 to 13 still wait for Ken:

6. The rows of this driver keep the context of the statement, because the
   request for each next page needs it (`elasticsearch/rows.go`). The driver
   does not store a context anywhere else. Ken decided on 2026-10-07 that hard
   rule 4 of AGENTS.md names the rows of an Elasticsearch query (D178, item
   2).
7. D36 says that `Rows.Close` before the end reads nothing more, and D167
   says that the driver closes the cursor when the caller closes the rows
   early. The cursor follows the rows of its page, so `Close` reads the rest of
   the current page to learn it, at most 256 KiB and for at most 5 seconds
   (Cancellation and timeouts). Ken decided on 2026-10-07 that the driver does
   this, which differs from D36 for this driver (D178, item 3). The
   `keep_alive` default of 45 seconds is not measured.
8. A `float` and a `half_float` arrive as the shortest text of a 32-bit float.
   The driver reads that text as a `float64`, so `6.1035156E-5` is 6.1035156e-05
   and not 2 to the power of -14. The other choice is to read the text as a
   `float32` and widen it, which gives the exact 32-bit value.
9. A day-time interval of more than about 292 years cannot be a
   `time.Duration`, and the driver fails the row with `dbimp.ErrInvalidValue`.
   The other choices are to give a `dbimp.Interval` for such a value, or to
   cap it.
10. `WithDatabase` fails with `dbimp.ErrNotSupported`, because Elasticsearch has
    no database to choose. The cluster is the catalog, and `WithCatalog` sets
    it, as the key `catalog` does. `WithParameter` applies to the first request
    of a statement and not to the request for each next page, which holds the
    cursor, the leniency and the timeout only.
11. A type that the driver does not know is the decoded JSON value, with the
    scan type `any`. The server refuses every type that has no mapping in the
    table, so no recorded column has such a type. The other choice is an error.
12. OpenSearch will share little with this driver. Its answer names the columns
    `schema` and the rows `datarows`, each of its cursors serves once, and its
    errors have another shape (OPENSEARCH.md, Responses and Errors). Nothing
    moved to the root package. `elasticsearch/rows.go` and
    `elasticsearch/errors.go` hold the code that a second driver can take if
    Ken wants it shared: the reader of pages, the bound on the read after
    `Close`, and the reader of the error object.
13. Steps 16 and 20 send three requests, which wait for the release (W28 in
    [BACKLOG.md](BACKLOG.md)). The `url` that `dbrun` prints already has the
    scheme `elasticsearch`, so the tests need no conversion, and the workflow
    needs no change but its comment.

## Integration tests

The integration tests of the driver read `ELASTICSEARCH_DSN`, and
`ELASTICSEARCH_ORDINARY_DSN` for the ordinary user, and skip when
`ELASTICSEARCH_DSN` is empty. Each is the `url` of `dbrun`. The tests ran on
2026-10-07, one server at a time, with `-race`, on a fresh container of each
release:

| Release | Passed | Skipped | Failed |
| --- | --- | --- | --- |
| `elasticsearch-8.19.22` | 362 | 0 | 0 |
| `elasticsearch-9.4.6` | 362 | 0 | 0 |
| `elasticsearch-9.5.3` | 362 | 0 | 0 |

A count is a test or a subtest, and each principal is a subtest. The tests make
their indices as the administrator, with the name of the run as the prefix,
and the prefix starts with `dbmeta` so that the ordinary user can read it.
`TestMain` removes every index of the run, also when a test fails. SQL takes no
write, so the administrator writes through the document API, and each principal
reads through the driver.

The round trip stores every type that `features.json` marks yes in a field of
that type, with the values of DRIVER.md: NULL, the zero value, the smallest and
the largest value, an empty value, a long value, text outside ASCII, and the
last digit of precision. It stores a value as a bound argument, then updates,
reads and deletes it. It cannot store a value as a literal, because SQL has no
`INSERT`, and each literal is logged as skipped inside the test
(`TestIntegrationRoundTrip`). These types need a note on how the round trip
stores them:

- The type `null` has no field. The select is `SELECT NULL AS v`, which gives the
  type `null`, with a row that stores a NULL in a `keyword` field.
- A `date` and a `time` are made from a `date` field with `CAST(v AS DATE)` and
  `CAST(v AS TIME)`, which is how SQL makes them (Types).
- An interval has no field. The round trip stores a number in a `long` field, and
  the select multiplies an interval by it (Types).
- A `datetime` is stored in a `date_nanos` field, to keep every nanosecond.
- A `binary` value is stored as base64, and read as bytes.
- The types that `features.json` marks no, such as `dense_vector` and `nested`,
  have a test of the refusal of the server, and `decimal` shows that a cast to
  `DECIMAL` gives a `DOUBLE`.

The tests also hold the statements of CRUD on three indices, the operations on
a schema, every feature of the survey, the pages and the cursor, the cancel, the
principals and the version, as each principal. The ones that the driver does not
speak, such as the other formats of an answer, the async form and the list of
tasks, go through the HTTP API as the administrator.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was
written on 2026-10-07 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of Elasticsearch from the sections
above.

### The server

| | Couchbase | Elasticsearch |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /_sql?format=json`, with `query`, `params` and the settings in the body. A later page sends `cursor` in place of `query` (Requests) |
| Database | The key `query_context` of the body | None. A statement names its indices, and `catalog` names a cluster (Statements) |
| Language | SQL++, which is close to SQL | SQL of its own, which reads only: `SELECT`, `SHOW`, `DESCRIBE` and `SYS` (Requests) |
| DDL | In SQL++ | None. A write goes through the document API, and every statement of DDL is a parse error (Statements) |
| Parameters | `?`, `$1` and `$name` | `?` only, from a JSON array of plain values. The server types each value from its JSON kind (Parameters) |
| Framing | One body for the whole result, which does not page | One object for each page, with a `cursor` for the next page, and no `columns` after the first (Responses) |
| Columns | `signature`, before the first row | `columns`, before the first row, with the type of each column. A repeated name stays (Responses) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement. `SELECT *` sorts the columns by name (Responses) |
| Errors | Can come with HTTP 200, after some rows | A status that is not 2xx with a JSON object. Never HTTP 200. After some rows, the error is the answer to the request for a later page (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, with a type name for each column. A time is ISO 8601 text, an interval is an ISO 8601 text, binary is base64 and a geometry is WKT (Types) |
| Cancel | The server stops a query when the client leaves | The server cancels the task when the client leaves. A cursor stays open until it is closed or its `keep_alive` ends (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None. `BEGIN` is a parse error (Transactions) |
| Authentication | Basic, or `creds` in the body | Basic, or an API key in `Authorization: ApiKey` (Requests) |
| Default port | 8093, or 18093 with TLS | 9200, with and without TLS |

The differences that a caller sees:

- Every write fails with the parse error of the server, because SQL takes none
  (D163).
- A statement has no database, and `WithDatabase` fails (D167).
- A result comes in pages, and an error on a later page fails the rows with
  `dbimp.ErrIncomplete` after exactly the rows that arrived (D167).
- The server cancels a statement when the client leaves, so the driver only
  closes the request. The driver closes the cursor of rows that the caller
  closes early (D167).
- A field that holds several values fails the statement, unless the caller sets
  `field_multi_value_leniency`, which then loses the other values (D167).
- An object and a nested field cannot be selected as a column. A statement names
  their subfields (Types).

### The driver

| | `couchbase` | `elasticsearch` |
| --- | --- | --- |
| Size, without tests, on 2026-10-07 | About 1300 lines in 8 files | About 1600 lines in 8 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `Auth`, `User`, `Password`, `FetchSize`, `TimeZone`, `FieldMultiValueLeniency`, `Catalog` (D167) |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, `WithFetchSize`, `WithTimeZone`, `WithFieldMultiValueLeniency` and `WithCatalog`, through `WithOptions` or an argument (D109). `WithTimeout` sends `request_timeout`. `WithReadonly` changes nothing, because every statement is read-only. `WithDatabase` gives `dbimp.ErrNotSupported`. `WithParameter` sets any key of the body of the first request |
| Arguments | Sent to the server as `args` and `$name` | Sent as `params`, a JSON array of plain values. A `time.Time` is a string in ISO 8601 in UTC. A named argument, a `[]byte`, a `uint64` above `int64`, `NaN` and an infinity are refused (`elasticsearch/types.go` and `elasticsearch/conn.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | `dbimp.ArrayRows` for each page, and a reader of the members of the page around it, which follows the cursor (`elasticsearch/rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName`, the type in upper case, `ColumnTypeScanType` and `ColumnTypeNullable`. Every column can be NULL |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column, as the type table says: `uint64` for `unsigned_long`, `time.Time`, `dbimp.Date`, `dbimp.OffsetTime`, `dbimp.Interval` for a period of months, `time.Duration` for a length of time, `[]byte` and `string` for a geometry (D135 and D167) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` return `dbimp.ErrNotSupported`, because SQL changes no rows (D163) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D167) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The same. The server cancels the task. Rows that the caller closes early close the cursor with `POST /_sql/close` and a limit of 5 seconds (D167) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Type, Reason, RootType, RootReason}`, which unwraps to `*dbimp.StatusError` |
| Authentication | Basic | Basic, or an API key from the password with `auth=apikey`. The driver follows no redirect, so the credentials go to the host of the DSN only (D167) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, `Config`, `ParseDSN` and `NewConnector` |

The differences that a caller sees:

- A value keeps its type, a time, an interval and a `uint64` too, where
  Couchbase gives JSON shapes (D135 and D167).
- The ten day-time intervals are a `time.Duration`, and the year-month intervals
  are a `dbimp.Interval` (D167, item 3).
- `RowsAffected` always returns an error, where Couchbase counts every
  statement by `mutationCount` (D163).
- `WithReadonly` succeeds and does nothing, where Couchbase sends `readonly`,
  because SQL takes no write (D163).
- The rows keep the context of the statement for the next page, where the rows
  of Couchbase hold none (Open questions, 6).
- `Close` before the end can read the rest of the current page to close the
  cursor (Open questions, 7).
