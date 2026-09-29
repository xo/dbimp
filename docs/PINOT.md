# Apache Pinot

This file holds what is known about Apache Pinot, for the driver `pinot`,
which D73 places sixth after Neo4j. Its work item is W19.
The headings are the template of [DRIVER.md](DRIVER.md).

Steps 5a and 6 measured 1.4.0 and 1.5.1 on 2026-09-30, as `admin` and as
`dbmeta_user`. A fact marked "recorded" is in `testdata/pinot/`, and
`requests.json` there holds each request. The setup of that script makes its
tables through the Controller, because the Broker takes no write (D128).
Each section starts with the measured facts. The facts after them come from
the sources of step 3, and each one that is not measured says so. The
sources, each read on 2026-09-27, are these:

- "The documents" are the Markdown pages of `github.com/pinot-contrib/pinot-docs`
  on the branch `latest`. "The query API" is
  `reference/api-reference/query-api.md`. "The response format" is
  `reference/api-reference/query-response-format.md`. "The options" is
  `build-with-pinot/querying-and-sql/query-execution-controls/query-options.md`.
  "The null page" is `build-with-pinot/querying-and-sql/null-value-support.md`.
  "The schema reference" is `reference/configuration-reference/schema.md`.
  "The auth page" is `operate-pinot/authentication/basic-auth-access-control.md`.
  "The cancel page" is
  `build-with-pinot/querying-and-sql/query-execution-controls/query-cancellation.md`.
  "The QuickStart page" is `basics/getting-started/quick-start.md`, and "the
  Docker page" is `basics/getting-started/install/docker.md`.
- "The source" is `github.com/apache/pinot` at the tag `release-1.5.1`. The
  files are `PinotClientRequest.java` of the Broker, `BrokerResponse.java`,
  `BrokerResponseNative.java`,
  `ResultTable.java`, `DataSchema.java`, `QueryErrorCode.java`,
  `CommonConstants.java`, `PinotQueryResource.java`,
  `PinotRunningQueryResource.java` and `PinotVersionRestletResource.java` of
  the Controller, `QuickstartRunner.java`, `AuthQuickstart.java` with
  `AuthUtils.java`, and `docker/images/pinot/Dockerfile`.
- "The Go client" is `github.com/startreedata/pinot-client-go` v0.11.0, the
  newest tag, with its package `gormpinot`.
- "The Java client" is `pinot-clients/pinot-java-client` in the source, and
  "the JDBC client" is `pinot-clients/pinot-jdbc-client` at the tag
  `release-1.5.1`, read on 2026-09-30.
- "GitHub" and "Docker Hub" are the releases and the tags that each one
  listed.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`.

The documents on `latest` describe the code after 1.5.1 in some places. A
fact that the source of 1.5.1 lacks says so.

## Summary

- Apache Pinot is a distributed database for analytics (OLAP) that takes
  SQL. The Apache Calcite parser reads the SQL, with the dialect
  `MYSQL_ANSI` (the documents).
- A cluster has ZooKeeper, a Controller, a Broker and a Server (the
  QuickStart page). A query goes to the Broker (the documents). The
  QuickStart command runs every part in one process (the QuickStart page).
- The latest release is 1.5.1, from 2026-06-05, and 1.5.0 came on 2026-04-09
  (GitHub). The documents name 1.5.1 as the current stable release, with the
  image `apachepinot/pinot:1.5.1` (`basics/getting-started/pinot-versions.md`).
- `dburl` has the provisional scheme `pinot`, with the alias `pi` and the
  default port 8000, the port of the broker in the dbmeta container. A
  broker that runs alone listens on 8099. The scheme passes the path and the
  query through (dburl D36, released in dburl `v0.36.0` as dburl D38, read on
  2026-09-29).
  Nothing of it is settled until step 9 decides the name and the URL.
- `usql` has no driver for Pinot, and its `go.mod` names no Pinot package.
- `dbmeta` has no model for Pinot. Its container entry is below.
- R holds, because each Tested release passed `dbrun test` (below). The
  QuickStart image runs every part in one container (TARGETS.md and the
  QuickStart page). The image sets a heap of 4 GB by default (the
  Dockerfile), and that equals the limit of 4 GB in `dbmeta`. So the entry
  sets a smaller heap.
- The integration tests passed on 1.4.0 and 1.5.1 on 2026-09-30, as both
  principals (W19). On 1.4.0, the test of the cancel skips, because its
  Broker has query cancellation off (Cancellation and timeouts).
- H and S hold. `POST /query/sql` on the Broker with
  `SELECT playerName, yearID FROM baseballStats LIMIT 2` answered HTTP 200
  with `resultTable` and two rows, as `admin` and as `dbmeta_user`, on 1.4.0
  and 1.5.1 (measured with `curl` on 2026-09-30).
- TARGETS.md lists Pinot as P1.
- `dbrun` has an entry, staged in `dbmeta` for Ken's review and not committed,
  from dbmeta D112. The `dbmeta` session reported on 2026-09-28 that each
  Tested release passed `dbrun test`: `pinot-1.4.0` and `pinot-1.5.1`. The
  entry sets `JAVA_OPTS` to `-Xms1G -Xmx2G`, and the container used 1.6 GB. It
  writes a configuration of the Broker with `admin` and `dbmeta_user`, and
  `dbmeta_user` can read only the table `baseballStats`, which the QuickStart
  loads. The Broker is on port 8000. A wrong password, or none, gets HTTP
  401.
- The `dbmeta` session staged two changes on 2026-09-30, which wait for
  Ken's approval there. The entry publishes the Controller, on port 9000 in
  the container, as its second port, and `dbrun dsn --json` names it as
  `secondAddress`. The `url` of each principal is
  `pinot://user:password@127.0.0.1:<port of the Broker>` (D129). The
  Controller has no authentication in the entry.

## Requests

- A query is `POST /query/sql` on the Broker, with
  `Content-Type: application/json` and a body with the field `sql` (the
  query API). A body that is not JSON, or that has no `sql`, gets HTTP 400
  (the query API).
- The body can also hold `trace` and `queryOptions` (the source).
  `queryOptions` is one string of `key=value` pairs that `;` separates, such
  as `"timeoutMs=5000;useMultistageEngine=true"` (the options).
- `POST /query` on the Broker runs the statement on the multi-stage engine
  (the query API and the source). `GET /query/sql` takes the statement in
  the query string (the source).
- The Controller takes `POST /sql`, with the same body, and sends it on to a
  Broker (the source, `PinotQueryResource.java`). The Broker, and not the
  Controller, authorizes it (the source).
- The multi-stage engine runs joins, window functions and subqueries. The
  single-stage engine is the default (the documents). A statement selects
  the multi-stage engine with `SET useMultistageEngine=true;` before it, or
  with the option in `queryOptions` (the options).
- The Broker can page a result with a cursor: `POST /query/sql?getCursor=true&numRows=N`,
  and then `GET /responseStore/{requestId}/results?offset=...&numRows=...`
  on the same Broker (the query API). The source of 1.5.1 has this.
- `POST /query/sql/validateSyntax` parses a statement and does not run it
  (the query API). The source of 1.5.1 does not have it.
- DDL is `POST /sql/ddl` on the Controller (`build-with-pinot/querying-and-sql/sql-ddl.md`).
  The source of 1.5.1 does not have it, and the branch `master` has it.
  In 1.5.1, a table is made with the JSON of `POST /schemas` and
  `POST /tables` on the Controller (the same page).
- The version is `GET /version` on the Controller (the source). The Broker
  has no version endpoint in the source.
- Authentication is HTTP Basic, in the header `Authorization`, on the
  Controller and on the Broker (the auth page). It is off by default. The
  auth page says that the Swagger page of the Controller also takes
  `Bearer <token>` in the same header, for a deployment that uses bearer
  tokens.
- The Go client sends `X-Correlation-Id` with a new UUID on each request
  (the Go client).

## The DSN

- Step 9 decides the URL (D27 and D35), and with it the default port, which
  is 8000 in the dbmeta container and 8099 for a broker alone. `dburl` has
  the provisional scheme `pinot` (dburl D36).
- The Go client takes a list of Broker addresses, a Controller address, or
  a ZooKeeper path, through its own functions, and not through a DSN (the
  Go client).
- The port of the Broker is 8099 by default (the source, `CommonConstants.java`,
  and the Docker page). The QuickStart puts the Broker on 8000, the
  Controller on 9000 and ZooKeeper on 2123 (the source, `QuickstartRunner.java`).
  The QuickStart page publishes only port 9000. Step 9 decides whether the
  driver talks to the Broker or to the Controller.

## Responses

Recorded on 1.4.0 and 1.5.1:

- `resultTable` comes first, with `dataSchema` and then `rows`, and
  `exceptions` and `partialResult` come after it. So the columns arrive
  before the rows, and an exception arrives after them.
- The columns keep the order of the statement, and a name can appear twice,
  as in `SELECT yearID, yearID`. `SELECT *` gives the columns in the order of
  their names, and not in the order of the schema. That is the order of the
  server, and the driver keeps it (hard rule 3).
- A result with no rows still has `dataSchema`. A failed query has no
  `resultTable`.
- `DISTINCT yearID` with no `LIMIT` gave 10 of 143 rows on the single-stage
  engine, and all 143 on the multi-stage engine.
- `?getCursor=true&numRows=3` gave 3 rows and a `requestId`, and
  `GET /responseStore/<requestId>/results?offset=3&numRows=3` gave the next
  3, as both principals.
- A body with no field `sql` is HTTP 500 with the text "Payload is missing
  the query string field 'sql'", and not HTTP 400.
- A request with `Accept-Encoding: gzip` got a body that is not compressed.
  No response was a redirect.

- The response is one JSON object, with `Content-Type: application/json`
  (the source). The Broker writes the whole result after the query ends,
  with Jackson (the source, `BrokerResponse.java`). A client can still read
  it one token at a time (D25).
- The fields come in a fixed order, and `resultTable` is first. Then come
  `numRowsResultSet`, `partialResult`, `exceptions`, `numGroupsLimitReached`,
  `timeUsedMs`, `requestId`, `clientRequestId`, `brokerId`, `numDocsScanned`,
  `totalDocs` and the other statistics (the source, `BrokerResponseNative.java`).
  So the rows arrive before the exceptions.
- `resultTable` holds `dataSchema` and then `rows`, in that order (the
  source, `ResultTable.java`). `dataSchema` holds `columnNames` and
  `columnDataTypes`, and `rows` is an array of arrays in the order of the
  columns (the response format). So the columns and their types arrive before
  the rows, and rule 1 of D18 applies.
- A statement on the single-stage engine with no `LIMIT` returns 10 rows,
  from `pinot.broker.default.query.limit` (the documents and the source). The
  multi-stage engine does not apply that default (the documents).
- `pinot.broker.query.response.limit` caps the rows, and its default is the
  largest `int` of Java (the source). `maxQueryResponseSizeBytes` and
  `maxServerResponseSizeBytes` cap the size of a response (the options).
- `partialResult` is true when Pinot returned part of the result (the
  response format). The multi-stage Lite Mode can cut the result and set it.
- A cursor page has `requestId`, `offset`, `numRows`, `numRowsResultSet`,
  `brokerHost`, `brokerPort` and `expirationTimeMs` (the response format).
  `numRows` is 10000 by default, from `pinot.broker.cursor.fetch.rows` (the
  query API).
- The recordings above hold the facts on compression and redirects.

## Types

`pinot/tables_test.go` writes this table from the code (step 10 and D130).
A NULL of every type is nil. A type that the table does not name, such as
`UNKNOWN`, the type of a `NULL` literal, reads as the decoded JSON value.

<!-- dbimp:types -->
| Wire type | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- |
| INT | `int64` | `int64` | `INT` | yes |
| LONG | `int64` | `int64` | `LONG` | yes |
| FLOAT | `float64, with NaN and the infinities` | `float64` | `FLOAT` | yes |
| DOUBLE | `float64, with NaN and the infinities` | `float64` | `DOUBLE` | yes |
| BIG_DECIMAL | `*apd.Decimal, with every digit` | `*apd.Decimal` | `BIG_DECIMAL` | yes |
| BOOLEAN | `bool` | `bool` | `BOOLEAN` | yes |
| TIMESTAMP | `time.Time, in UTC, to the millisecond` | `time.Time` | `TIMESTAMP` | yes |
| STRING | `string` | `string` | `STRING` | yes |
| JSON | `the decoded JSON value: nil, bool, string, int64, float64, *apd.Decimal, []any or map[string]any. The multi-stage engine names the type STRING, so the value is its text there` | `interface {}` | `JSON` | yes |
| BYTES | `[]byte, from hex` | `[]uint8` | `BYTES` | yes |
| MAP | `map[string]any, of the decoded JSON values` | `map[string]interface {}` | `MAP` | yes |
| INT_ARRAY | `[]any, of int64` | `[]interface {}` | `INT_ARRAY` | yes |
| LONG_ARRAY | `[]any, of int64` | `[]interface {}` | `LONG_ARRAY` | yes |
| FLOAT_ARRAY | `[]any, of float64` | `[]interface {}` | `FLOAT_ARRAY` | yes |
| DOUBLE_ARRAY | `[]any, of float64` | `[]interface {}` | `DOUBLE_ARRAY` | yes |
| BOOLEAN_ARRAY | `[]any, of bool` | `[]interface {}` | `BOOLEAN_ARRAY` | yes |
| STRING_ARRAY | `[]any, of string` | `[]interface {}` | `STRING_ARRAY` | yes |
| TIMESTAMP_ARRAY | `[]any, of time.Time` | `[]interface {}` | `TIMESTAMP_ARRAY` | yes |
<!-- /dbimp:types -->

Recorded on 1.4.0 and 1.5.1, from the table `dbimp_types`, which has a
column of each type and `enableColumnBasedNullHandling`. The two releases
gave the same answers:

- `INT`, `LONG`, `FLOAT` and `DOUBLE` are JSON numbers. `-2147483648`,
  `9223372036854775807` and `-9223372036854775808` keep every digit. A
  `FLOAT` of `-3.4e38` arrives as `-3.4e+38`, and a `DOUBLE` of
  `1.7976931348623157e308` keeps every digit.
- `BIG_DECIMAL` is a string with every digit, such as
  `"12345678901234567890.0123456789"`.
- `BOOLEAN` is a JSON boolean, and `BYTES` is hex, such as `"00ff"`. An empty
  `BYTES` is `""`.
- `TIMESTAMP` is a string in UTC, such as `"2023-11-14 22:13:20.123"` for
  the milliseconds `1700000000123`. A time with no milliseconds ends in `.0`,
  as `"2022-04-15 05:20:00.0"`. So the text has from one to three digits
  after the point.
- `JSON` arrives as a string of JSON text, such as `"{\"k\":[1,null,\"s\"]}"`.
  The multi-stage engine names its type `STRING`, and the single-stage engine
  names it `JSON`. So on the multi-stage engine, the default of D131, a
  driver cannot tell a `JSON` column from a `STRING` column.
- `MAP` is a JSON object, such as `{"a": 1, "b": 2}`, and an empty map is
  `{}`. `m['a']` of a map that lacks the key gives `-2147483648`, the default
  of its `INT` value, and not NULL.
- Each multi-value column is a JSON array, with the types `INT_ARRAY`,
  `LONG_ARRAY`, `FLOAT_ARRAY`, `DOUBLE_ARRAY`, `BOOLEAN_ARRAY`,
  `STRING_ARRAY` and `TIMESTAMP_ARRAY`. A `TIMESTAMP_ARRAY` holds strings in
  the form of `TIMESTAMP`. An empty array that was stored arrives as `null`
  with null handling, and as an array of the default value without it, such
  as `[-2147483648]` or `["null"]`. So an empty array cannot be read back.
- A `BYTES` array can be stored, but every read of it fails with 200
  "Unsupported value type: BYTES for multi-value column".
- The Controller refuses a column of `UUID` or `OBJECT` with HTTP 400, and a
  `BIG_DECIMAL` array with "BIG_DECIMAL columns cannot be of multi-value
  type".
- With `enableNullHandling=true`, a NULL of every type is JSON `null`.
  Without it, a NULL is the default value: `-2147483648`, `"-Infinity"`,
  `"0"`, `false`, `"null"`, `""` for `BYTES`, and `{}` for `MAP`.
- A table with no null handling gives the value of `defaultNullValue` for a
  missing value, even with `enableNullHandling=true`. The table
  `dbimp_defaults` gave `-1` and `"null"`.
- `"NaN"`, `"Infinity"` and `"-Infinity"` are JSON strings in a `DOUBLE`
  column.

Recorded on 1.4.0 and 1.5.1, with literals over `baseballStats` on the
multi-stage engine:

- A cast gives the type that it names. `CAST(1700000000123 AS TIMESTAMP)`
  gives `"2023-11-14 22:13:20.0"`, so the cast loses the milliseconds.
- `CAST('-12345678901234567890.01234567890123456789' AS BIG_DECIMAL)` gives
  every digit, and then zeros to 1022 characters.
- `CAST('{"a":1}' AS JSON)` fails with 700 "Unknown identifier 'JSON'".
- `ARRAY[1, 2]` is `INT_ARRAY` and `ARRAY['x', 'y']` is `STRING_ARRAY`.
- A `NULL` literal is JSON `null` with the type `UNKNOWN`. A measurement
  with `curl` on 1.5.1 gave the same without `enableNullHandling=true`.

The round trip of each type (step 14a) ran on 1.4.0 and 1.5.1 on
2026-09-30, and found these facts of the server:

- A `FLOAT` arrives as the text of a float32, which the driver reads as a
  float64. 1.5.1 writes the shortest text, such as `0.1`, and 1.4.0 can
  write more digits, such as `1.17549435e-38` where 1.5.1 writes
  `1.1754944e-38`. Both are the same float32.
- A `NaN` that the Controller loads into a `FLOAT` or a `DOUBLE` reads back
  as NULL. An infinity reads back as `"Infinity"` or `"-Infinity"`.
- A `STRING` keeps 512 characters, the default `maxLength` of a column,
  and the Controller cuts a longer one to 512 with no error.
- An empty array reads back as NULL, as the table of every type showed.
- The Controller refuses to load a `TIMESTAMP` given as a number that fits
  in 32 bits, such as `0` or `-1`, with "Caught exception while reading
  data", and it takes the same milliseconds as text, such as `"0"`.
- The Controller cannot build a segment whose every `MAP` is empty or NULL:
  the load fails with "Index 0 out of bounds for length 0".

These are faults of the loader of the Controller, and not of the Broker. The
tests work around each one, in `pinot/features_integration_test.go`.

The names in `columnDataTypes` are the
values of `ColumnDataType` (the source, `DataSchema.java`): `INT`, `LONG`,
`FLOAT`, `DOUBLE`, `BIG_DECIMAL`, `BOOLEAN`, `TIMESTAMP`, `STRING`, `JSON`,
`MAP`, `BYTES`, `OBJECT`, `INT_ARRAY`, `LONG_ARRAY`, `FLOAT_ARRAY`,
`DOUBLE_ARRAY`, `BOOLEAN_ARRAY`, `TIMESTAMP_ARRAY`, `STRING_ARRAY`,
`BYTES_ARRAY` and `UNKNOWN`. The source writes each value in JSON as
follows (`DataSchema.java`, `format` and `convertAndFormat`):

- `INT`, `LONG`, `FLOAT` and `DOUBLE` are JSON numbers. `BOOLEAN` is a JSON
  boolean.
- `BIG_DECIMAL` is a string, from `toPlainString`, so its digits survive.
- `TIMESTAMP` is a string from `java.sql.Timestamp.toString`, such as
  `2024-01-01 12:30:00.0`. It has no zone, and Java writes it in the zone of
  the JVM. The image writes UTC (recorded, under Types). The schema reference says
  that a `TIMESTAMP` has the precision of a millisecond.
- `BYTES` is a string of hex digits.
- `STRING` and `JSON` are strings. So a `JSON` column arrives as JSON text
  inside a string.
- `MAP` is a JSON object, which Java copies into a `HashMap`. So the order
  of its keys is not kept.
- Each array type is a JSON array. `TIMESTAMP_ARRAY` is an array of strings.
- `NaN` and the infinities are JSON strings (recorded, under Types).
- The schema reference also names `UUID`, written as a lower case string.
  `DataSchema.java` of 1.5.1 has no `UUID`, so it comes after 1.5.1.

NULL depends on the table and on the query (the null page):

- By default a table stores no NULL. Pinot stores the default null value of
  the column in its place. The defaults for a dimension are
  `Integer.MIN_VALUE` for `INT`, `Long.MIN_VALUE` for `LONG`, negative
  infinity for `FLOAT` and `DOUBLE`, `0.0` for `BIG_DECIMAL`, `false` for
  `BOOLEAN`, the epoch for `TIMESTAMP`, `"null"` for `STRING` and `JSON`, and
  an empty array for `BYTES`. A metric defaults to `0` (the schema
  reference).
- A table stores NULL only when its schema sets
  `enableColumnBasedNullHandling`, or its table configuration sets the older
  `nullHandlingEnabled` (the null page).
- A query sees NULL only with the option `enableNullHandling=true`, or when
  the Broker sets `pinot.broker.query.enable.null.handling` (the null page).
  Its default is false (the options).
- So without that option a NULL arrives as a value such as `-2147483648` or
  `"null"`, and a driver cannot tell it from a real value. With it, a NULL
  arrives as JSON `null` (Gemini and DeepSeek). This is the central question
  for rule 3 in AGENTS.md and for D18. The recordings agree, and D130
  sends the option on every query.

## Parameters

- The HTTP API binds no parameters. The body has no field for them (the
  source, `CommonConstants.java`, which names `sql`, `trace`,
  `queryOptions`, `language` and `query`). Gemini and DeepSeek say the same.
- The Java client and the Go client put each value into the SQL text on the
  client. Each one counts every `?` in the text, including a `?` inside a
  string literal (the Java client and the Go client). A string is quoted
  with `'` and a `'` inside it is doubled.
- So the driver uses the parser for placeholders in the root package (D34),
  and an escaper for the literals of Pinot, which step 9 decides.

Recorded on 1.4.0 and 1.5.1:

- A `?` in the text fails with 450 `InternalError`, "Unsupported RexNode type
  with SqlKind: DYNAMIC_PARAM". A field `parameters` in the body is ignored.
- The literals of D132 match stored values: `s = 'é''"\ x'`,
  `bd = CAST('12345678901234567890.0123456789' AS BIG_DECIMAL)`,
  `by = hexToBytes('00ff')`, `ts = 1700000000123`,
  `l = 9223372036854775807`, `d = 0.1` and `b = true` selected the row of
  those values.
- The Broker refuses every write of rows with 150 `SQLParsingError`:
  `INSERT … VALUES` on the single-stage engine, and after
  `SET useMultistageEngine=true`, `INSERT … SELECT`, `UPDATE`, `DELETE` and
  `CREATE TABLE`. `UPDATE` and `DELETE` fail with "class
  org.apache.calcite.sql.SqlUpdate cannot be cast", which Calcite parsed and
  Pinot cannot run.
- `INSERT INTO baseballStats FROM FILE 's3://x/y'` gives no exception, and a
  result with the columns `tableName` and `taskJobName` and no rows. It
  starts an ingestion task on a Minion, and the image runs none. It inserts
  no row itself.
- The cursor pages a result: `POST /query/sql?getCursor=true&numRows=3` gave
  3 of 7 rows, with `requestId`, `offset`, `numRowsResultSet`, `brokerHost`
  and `brokerPort`, and `GET /responseStore/<requestId>/results?offset=3&numRows=3`
  gave the next 3.

## Transactions

Recorded on 1.4.0 and 1.5.1: `BEGIN` and `START TRANSACTION` fail with 150
"Non-query expression encountered in illegal context". The JDBC client
makes `commit`, `rollback` and `setAutoCommit` do nothing (the JDBC client,
`AbstractBaseConnection.java`), so a caller of it sees a transaction that
does not exist.

- Pinot has no transactions. The Go client returns an error from `Begin`,
  because it is read only (the Go client). D20 applies.
- The query API reads data and does not write rows. A DML statement on the
  Broker is `INSERT INTO table FROM FILE uri`, which starts an ingestion task
  on a Minion (`build-with-pinot/ingestion/from-query-console.md`). The same
  page says that an insert of rows is not written yet. Pinot has no `UPDATE`
  and no `DELETE` for rows in the documents that this draft read.

## Errors

Recorded on 1.4.0 and 1.5.1:

- Every error of a query arrives with HTTP 200, `partialResult` true, no
  `resultTable`, and the header `X-Pinot-Error-Code` with the code of the
  first exception. A query with no error has `X-Pinot-Error-Code: -1`.
- The codes seen are 150 for a parse error, 190 for a table that does not
  exist, 200 for a failure while the query runs, 245 for a join over
  `maxRowsInJoin`, 250 for a timeout, 450 for a `?`, 503 for a cancelled
  query, and 700 for a function that does not exist.
- A function that fails on one segment of three fails the whole query, with
  no rows, on both engines.
- A join with `joinOverflowMode=BREAK` and `maxRowsInJoin=1000` gave one row,
  a count of 1000 that is wrong, with `partialResult` true and no
  exception. So `partialResult` can follow rows, and it is the only sign
  that they are incomplete (D133).
- With the header `Pinot-Use-Http-Status-For-Errors: true`, a table that
  does not exist is HTTP 404 on 1.5.1, and still HTTP 200 on 1.4.0.
- A wrong password is HTTP 401, and a table that the user cannot read is
  HTTP 403 with "Authorization Failed for tables: [dbimp_idx]".


- An error in the query arrives in `exceptions`, a list of objects with
  `errorCode` and `message`, and the HTTP status is 200 by default (the
  source, `PinotClientRequest.java`, and the Go client).
- The response carries the header `X-Pinot-Error-Code`. It is `-1` when the
  query succeeded, and the code of the first exception when it failed (the
  source).
- With the request header `Pinot-Use-Http-Status-For-Errors: true`, the
  Broker sends the HTTP status of the first exception instead (the source).
  For example, `SQLParsingError` (150) is 400, `AccessDenied` (180) is 403,
  `TableDoesNotExistError` (190) is 404, `ExecutionTimeoutError` (250) is
  408, `TooManyRequests` (429) is 429 and `ServerNotResponding` (427) is 503
  (the source, `QueryErrorCode.java`).
- A body that is not JSON, or that has no `sql`, is HTTP 400 (the query
  API). A failure outside the query is HTTP 500 (the source).
- Wrong credentials are HTTP 401, and credentials without the right to a
  table are HTTP 403 (the auth page).
- Because `resultTable` comes before `exceptions`, a response can hold rows
  and then an exception, such as a server that did not answer, with
  `partialResult` true (the source and the response format). The driver
  reads the exceptions after the rows (D36).
- A limit on the rate is the error `TooManyRequests` (429), and a
  `WorkloadBudgetExceededError` has the same code (the source,
  `QueryErrorCode.java`).

## Cancellation and timeouts

Recorded on 1.4.0 and 1.5.1, as both principals:

- `timeoutMs=500` stopped the slow query of the script with 250.
- On 1.5.1, `GET /queries` listed the query of `clientQueryId=dbimp-cancel`,
  and `DELETE /query/dbimp-cancel?client=true` answered HTTP 200. The query
  then ended with 503 "Cancelled on: Server". An unknown id is HTTP 404. The
  ordinary user can list and cancel queries too.
- On 1.4.0, both fail with HTTP 500. `GET /queries` says "Query cancellation
  is not enabled on broker", and the `DELETE` says "Cannot invoke
  \"java.util.Map.entrySet()\" because \"this._clientQueryIds\" is null". So the
  default of `pinot.broker.enable.query.cancellation` is false on 1.4.0, and
  a driver cannot stop a query there. The `timeoutMs` of the query still
  ends it.

Measured on 2026-09-30 with `curl`, as `admin`, on 1.5.1:

- `timeoutMs=500` in `queryOptions` stopped a join that took 1.7 seconds
  after 0.55 seconds, with 250 `ExecutionTimeoutError`.
- `clientQueryId=dbimp-c1` names a query. `GET /queries` on the Broker lists
  the running queries by their `requestId`, with their text.
  `DELETE /query/dbimp-c1?client=true` answered HTTP 200 "Cancelled client
  query", and the query ended 4 seconds in, with 503 "Cancelled". A cancel of
  an id that the Broker does not know is HTTP 404.
- A client that left a query after 2 seconds did not stop it. The query ran
  on until the heap of the server was used up, and the Broker then answered
  nothing (the logs, with `OutOfMemoryError` for the query `dbimp-d1`). So a
  driver cancels a query that it leaves.
- `maxRowsInJoin` bounds a join of the multi-stage engine, and a join above
  it fails with 245.

- `timeoutMs` in the options bounds a query. The default comes from the
  table or from `pinot.broker.timeoutMs`, which is 10000 ms (the options and
  the source).
- The option `clientQueryId` names the query. The Broker cancels it with
  `DELETE /query/{id}?client=true`, and a query with the id that the Broker
  gave it with `DELETE /query/{id}` (the source, `PinotClientRequest.java`).
  That call needs the permission `CANCEL_QUERY` on the cluster. It answers
  404 when the Broker does not know the query (the source).
- The Controller cancels with `DELETE /clientQuery/{clientQueryId}`, and
  with `DELETE /query/{brokerId}/{queryId}` (the source,
  `PinotRunningQueryResource.java`). The cancel page names
  `DELETE /queryClient/{id}` on port 9000 instead. The two disagree.
- The cancel page says that cancellation needs
  `pinot.server.enable.query.cancellation` and
  `pinot.broker.enable.query.cancellation` set to true. The source of 1.5.1
  sets both to true by default. The two disagree.
- The `requestId` of a query arrives only in its response (the source). So a
  client that cancels a running query sets `clientQueryId` before it sends
  the query.
- Whether the Broker stops a query when the client disconnects is not
  measured. Gemini says that it does not, and that the query runs to its end
  or its timeout. DeepSeek says that recent releases can, when cancellation
  is on.

## Statements

Recorded on 1.4.0 and 1.5.1:

- Two statements fail with 150 "SqlNode with executable statement already
  exist". `--` and `/* */` comments work. `SET timeoutMs = 5000;` before a
  query works, and `OPTION(timeoutMs=5000)` at its end works on the
  single-stage engine.
- `CREATE VIEW` fails with 150.
- A window function, a join with the hint `join_strategy='lookup'`, the
  function `LOOKUP` over a table with `isDimTable`, `JSON_EXTRACT_SCALAR`
  and `ID_SET` each return the right rows. `LOOKUP` of a key that the
  table lacks gives `"null"`.
- A table can have a sorted index, an inverted index, a range index, a
  bloom filter, a star-tree index, a text index, a JSON index, an FST
  index, a timestamp index, a geospatial (H3) index and a vector index. A
  query that each one serves returned the right count. The Controller made
  each one from the table configuration, and no SQL makes one.
- The Controller refuses an upsert on an `OFFLINE` table, and a `REALTIME`
  table needs a stream: "Cannot find streamConfigs". A `REALTIME` table with
  a Kafka stream that does not exist timed out after about 60 seconds, as
  measured with `curl`. The image runs no Kafka, so upsert tables and
  real-time tables cannot be tested here.

Measured on 2026-09-30, on 1.5.1: `--` and `/* */` comments work. Two
statements in one request fail with 150. A query with no `LIMIT` returned 10
rows on the single-stage engine, and 97,889 rows, the whole table, after
`SET useMultistageEngine=true`. An error arrives with HTTP 200,
`partialResult` true, and the header `X-Pinot-Error-Code`, such as 190 for a
table that does not exist.

- A request holds one query. `SET key = value;` statements can come before
  it, and they set options for it (the options and the source).
- The older form `OPTION(key=value)` at the end of a statement also sets
  options (the options).
- `--` comments appear in the SQL of the documents (`querying-pinot.md`).
  DeepSeek says that the parser takes `--` and `/* */`. Both work
  (recorded).

## Principals

Recorded on 1.4.0 and 1.5.1: `dbmeta_user` reads `baseballStats` and gets
HTTP 403 for every other table. It can list and cancel the running queries
of the Broker on 1.5.1. The Broker answers `GET /version` with HTTP 404 for
both users. The Controller answers it with the version of each module, such
as `1.5.1-020ff0d0538b2079d4cf4cb2676a191c87c95d4d`, with no user, in the
setup of the script. `SELECT version()` fails with 700. So no statement on
the Broker gives the version.

- With static Basic auth, the Controller and the Broker each read their
  users from their properties, such as
  `pinot.broker.access.control.principals` (the auth page). A user of the
  Broker can be limited to named tables, and every Broker request is a read
  (the auth page). A user of the Controller has the permissions `CREATE`,
  `READ`, `UPDATE` and `DELETE` (the auth page).
- A user cannot be created by SQL. With static auth, the users are fixed at
  start. With `ZkBasicAuthAccessControlFactory`, an administrator creates
  users in the web console of the Controller, and the first user is `admin`
  with the password `admin` (`operate-pinot/authentication/zkbasicauthaccesscontrol.md`).
- `QuickStart -type AUTH` starts every part with static Basic auth (the
  QuickStart page). It sets the users `admin` with the password `verysecret`,
  `user` with `secret` and only `READ` on the Controller, `service` with
  `verysecrettoo`, and `tableonly` with `secrettoo` and only the table
  `baseballStats` (the source, `AuthUtils.java`). So `user` or `tableonly`
  can be the ordinary user. These passwords are fixed in the code, and not
  `container.Password` of `dbmeta`.
- `GET /version` on the Controller needs the permission `GET_VERSION` on the
  cluster (the source). The Controller of the `dbmeta` entry has no users,
  so the permission is not tested.

## Flavors

- No source that this draft read names another product that speaks the
  Broker API. Step 7 asks the models.
- The single-stage and the multi-stage engine are two engines of one
  server, and not flavors. They differ in the default `LIMIT`, in the SQL
  that each one takes, and in the fields of the response (the documents).

## Interfaces

`pinot/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the credentials. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because Pinot has no sessions. |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, a uint64 and a decimal, which the driver writes as literals of their own (D132). |
| `driver.QueryerContext` | yes | The driver writes each argument into the text as a literal, because the Broker binds none (D132). |
| `driver.ExecerContext` | yes | Exec reads the result to its end. The Broker takes no write, so RowsAffected fails (D128). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments written in each time. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because Pinot has no transactions (D133). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, so an answer has one result. |
| `driver.RowsColumnTypeScanType` | yes | dataSchema names the type of each column (D130). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | dataSchema names the type of each column, such as LONG or INT_ARRAY. |
| `driver.RowsColumnTypeLength` | no | dataSchema names no length. |
| `driver.RowsColumnTypeNullable` | yes | Every column can be NULL, because each query sends enableNullHandling=true (D130). |
| `driver.RowsColumnTypePrecisionScale` | no | dataSchema names no precision and no scale, and a BIG_DECIMAL has any scale. |
<!-- /dbimp:interfaces -->

## Faults

`usql` has no Pinot driver, so these are the faults of the Go client, which
a driver here does not repeat (the Go client):

- Its package `pinot` is not a `database/sql` driver. Its package
  `gormpinot` makes a `driver.Connector` for GORM, and it registers no name
  with `database/sql`.
- It takes no context. It builds each request with `http.NewRequest`, and a
  timeout is only the `Timeout` of the `http.Client`
  (`jsonAsyncHTTPClientTransport.go`). `gormpinot` looks at the context once
  before the request and never again, and its `Stmt.Exec` and `Stmt.Query`
  call `context.Background` (`gormpinot/driver.go`).
- It reads each response whole with `io.ReadAll` (`jsonAsyncHTTPClientTransport.go`
  and `controllerBasedBrokerSelector.go`).
- A NULL becomes a zero value. `GetInt`, `GetLong`, `GetFloat` and
  `GetDouble` return 0 for a value that is not a number, and they log the
  error rather than return it (`response.go`).
- `gormpinot` turns `BYTES` into the bytes of its hex text, and not into the
  decoded bytes. It turns an array or a map into text with `fmt.Sprintf`
  (`gormpinot/rows.go`). That is text in place of a value.
- It logs with `logrus` (the Go client), where AGENTS.md says that a package
  logs nothing.
- It counts every `?` in the SQL as a parameter, including one in a string
  literal (`connection.go`). It writes a `time.Time` in its own zone, with
  no zone in the text (`connection.go`).
- A response with a status other than 200 loses its body, and some errors
  are wrapped with `%v` and not `%w` (`jsonAsyncHTTPClientTransport.go` and
  `connection.go`). `gormpinot` returns only the first exception.
- It depends on gRPC, protobuf, Apache Arrow, ZooKeeper, `logrus`, GORM and
  several compression libraries (`go.mod`), where D13 allows the standard
  library, apd and a binary encoding that Ken approves.
- It takes no DSN, so no scheme names it (D35).
- It picks a Broker for each query from the name of the table, which
  `gormpinot` finds by parsing the SQL (`gormpinot/rows.go`).

The JDBC client has one fault that a driver here does not repeat: its
`commit`, `rollback` and `setAutoCommit` do nothing, so a caller sees a
transaction that does not exist (`AbstractBaseConnection.java`). The driver
here returns `dbimp.ErrNotSupported` from `BeginTx` (D133).

## Second opinions

Step 7 fills this section. For this draft, Gemini and DeepSeek were asked on
2026-09-27 about NULL, the types, parameters, cancellation and statements.
Several calls to each model timed out or closed first. Their answers are
leads:

- NULL: both say that a NULL arrives as JSON `null` with
  `enableNullHandling=true`, and as the default null value without it.
  DeepSeek says that the default is `0` for a number and `""` for a string.
  The schema reference says `Integer.MIN_VALUE` or `Long.MIN_VALUE` for a
  dimension, `0` for a metric, and `"null"` for a string. The recordings
  agree with the documents.
- `TIMESTAMP`: DeepSeek says that it arrives as a JSON number of
  milliseconds since the epoch. Gemini says that it arrives as the string of
  `java.sql.Timestamp`. The source agrees with Gemini.
- `BYTES` is hex text and `BIG_DECIMAL` is a string (DeepSeek). The source
  agrees.
- Parameters: both say that the HTTP API binds none, and that the Java and
  JDBC clients put the values into the text. The source agrees.
- A client that disconnects: Gemini says that the query goes on. DeepSeek
  says that recent releases can cancel it. The measurement agrees with
  Gemini: the query ran on until the heap was used up.
- Statements: DeepSeek says that `SET` statements can come before the query,
  and that the parser takes `--` and `/* */` comments. The options agree on
  `SET`.

## Open questions

None. Ken settled the `JSON` column that the multi-stage engine names
`STRING` on 2026-09-30: the driver follows the type that the server names,
so a caller gets the text of that column by default, and the decoded value
with `engine=single` (D130 and D135).

These were open at step 3, and step 9 settled them: the DSN (D129), the Go
types and NULL (D130), the engine and its limit of 10 rows (D131), and the
driver that takes no write (D128). The `dbmeta` entry sets a smaller heap,
and it publishes the Controller as its second port (Summary).

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-09-30 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of Pinot from the sections above.

### The server

| | Couchbase | Apache Pinot |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /query/sql` on the Broker, with `sql` and `queryOptions` |
| Database | The key `query_context` of the body | None. Pinot has tables and no databases |
| Language | SQL++, which is close to SQL | SQL, parsed by Calcite, on two engines (Requests) |
| DDL | In SQL++ | None in SQL. The Controller makes a table from JSON, and the Broker refuses every write (D128) |
| Parameters | `?`, `$1` and `$name` | None. A `?` fails with 450 (Parameters) |
| Framing | One body for the whole result, which does not page | One body for the whole result, which the Broker writes after the query ends. A cursor can page it (Responses) |
| Columns | `signature`, before the first row | `dataSchema`, with a type for each column, before the rows |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement. `SELECT *` gives the order of the names |
| Errors | Can come with HTTP 200, after some rows | HTTP 200 with `exceptions` and no rows, or rows and then `partialResult` (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, with a type in `dataSchema`. A decimal and a time are strings, and bytes are hex (Types) |
| Cancel | The server stops a query when the client leaves | The query runs on when the client leaves, and `DELETE /query/<id>?client=true` stops it on 1.5.1. 1.4.0 has cancellation off (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None. `BEGIN` fails with 150 (Transactions) |
| Authentication | Basic, or `creds` in the body | Basic, or a Bearer token |
| Default port | 8093, or 18093 with TLS | 8099, with TLS too. The QuickStart image puts its Broker on 8000 |

The differences that a caller sees:

- Every write fails with the error of the server, because the Broker takes
  none (D128).
- A query has no database, and `WithDatabase` fails (D129).
- The driver writes each argument into the text as a literal, because the
  Broker binds none (D132).
- The server does not stop a query when the client leaves, so the driver
  cancels it when its context ends before the answer (D133 and D134).
- An engine is chosen for each query, and the single-stage engine cuts a
  result with no `LIMIT` at 10 rows (D131).

### The driver

| | `couchbase` | `pinot` |
| --- | --- | --- |
| Size, without tests, on 2026-09-30 | About 1300 lines in 8 files | About 1300 lines in 8 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `User`, `Password`, `Auth`, `Cancel`, `Engine` |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, `WithCancel` and `WithEngine`, through `WithOptions` or an argument (D109). `WithTimeout` sends `timeoutMs`. `WithReadonly` changes nothing, because every statement is read-only. `WithDatabase` gives `dbimp.ErrNotSupported`. `WithParameter("queryOptions")` replaces every query option of the driver |
| Arguments | Sent to the server as `args` and `$name` | Written into the text by the parser for placeholders of the root package (D34 and D132). A named argument is refused, and so are `NaN` and an infinity |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | `dbimp.ArrayRows` from the root package, after the driver reads `dataSchema` (`rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName`, `ColumnTypeScanType` and `ColumnTypeNullable`, from `dataSchema`. Every column can be NULL (D130) |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column: `int64`, `float64`, `*apd.Decimal`, `bool`, `string`, `[]byte` from hex, `time.Time` in UTC, `[]any` for an array, `map[string]any` for a `MAP`, and the decoded value of `JSON` (D130) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` return `dbimp.ErrNotSupported`, because Pinot changes no rows (D128) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D133) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The driver names each query with `clientQueryId`, and cancels it when the context ends before the answer, with `cancel=kill` (D133 and D134) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Code, Message, Exceptions}`, which unwraps to `*dbimp.StatusError` for a status that is not 2xx, and `ErrPartial` for `partialResult` with no exception |
| Authentication | Basic | `auth=basic` or `auth=bearer` (D94) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Exception`, `ErrPartial`, and `CancelKill`, `CancelNone`, `EngineMulti`, `EngineSingle`, `AuthBasic` and `AuthBearer` |

The differences that a caller sees:

- A value keeps its type, a time and a decimal too, where Couchbase gives
  JSON shapes (D130).
- A `JSON` column is text on the multi-stage engine, and a decoded value on
  the single-stage engine, because only the second names its type (D135).
- A `[]byte` argument is written as `hexToBytes`, where Couchbase sends it
  as base64 (D44 and D132).
- `WithReadonly` succeeds and does nothing, where Couchbase sends
  `readonly`, because Pinot takes no write (D128).
- `RowsAffected` always returns an error, where Couchbase counts every
  statement by `mutationCount` (D128).
- A result that `partialResult` marks fails with `ErrPartial` after its
  rows (D133).

