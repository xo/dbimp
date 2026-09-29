# Apache Pinot

This file holds what is known about Apache Pinot, for the driver that D73
places sixth after Neo4j. Its work item comes when its turn comes (D73).
The headings are the template of [DRIVER.md](DRIVER.md).

This is the draft of step 3. No server has run for this driver yet, so every
fact here is "not measured" and names its source. R is the exception, because
`dbrun` measured it (Summary). The sources, each read on 2026-09-27, are
these:

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
- "The Java client" is `pinot-clients/pinot-java-client` in the source.
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
- H is likely, and not measured. A query is `POST /query/sql` on the
  Broker, with a JSON body and a JSON response (the query API).
- S is likely, and not measured. The query language is SQL (the documents).
- TARGETS.md lists Pinot as P1.
- `dbrun` has an entry, staged in `dbmeta` for Ken's review and not committed,
  from dbmeta D112. The `dbmeta` session reported on 2026-09-28 that each
  Tested release passed `dbrun test`: `pinot-1.4.0` and `pinot-1.5.1`. The
  entry sets `JAVA_OPTS` to `-Xms1G -Xmx2G`, and the container used 1.6 GB. It
  writes a configuration of the Broker with `admin` and `dbmeta_user`, and
  `dbmeta_user` can read only the table `baseballStats`, which the QuickStart
  loads. The Broker is on port 8000, and the Controller is not published, so
  `GET /version` on the Controller is out of reach. A wrong password, or none,
  gets HTTP 401.

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
- Compression and redirects are not measured.

## Types

Step 10 writes the type table. The names in `columnDataTypes` are the
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
  the JVM. The zone of the image is not measured. The schema reference says
  that a `TIMESTAMP` has the precision of a millisecond.
- `BYTES` is a string of hex digits.
- `STRING` and `JSON` are strings. So a `JSON` column arrives as JSON text
  inside a string.
- `MAP` is a JSON object, which Java copies into a `HashMap`. So the order
  of its keys is not kept.
- Each array type is a JSON array. `TIMESTAMP_ARRAY` is an array of strings.
- How `NaN` and an infinity are written is not measured.
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
  for rule 3 in AGENTS.md and for D18, and step 6 measures it.

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

## Transactions

- Pinot has no transactions. The Go client returns an error from `Begin`,
  because it is read only (the Go client). D20 applies.
- The query API reads data and does not write rows. A DML statement on the
  Broker is `INSERT INTO table FROM FILE uri`, which starts an ingestion task
  on a Minion (`build-with-pinot/ingestion/from-query-console.md`). The same
  page says that an insert of rows is not written yet. Pinot has no `UPDATE`
  and no `DELETE` for rows in the documents that this draft read.

## Errors

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

- A request holds one query. `SET key = value;` statements can come before
  it, and they set options for it (the options and the source).
- The older form `OPTION(key=value)` at the end of a statement also sets
  options (the options).
- `--` comments appear in the SQL of the documents (`querying-pinot.md`).
  DeepSeek says that the parser takes `--` and `/* */`. Step 6 measures both.

## Principals

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
  cluster (the source). Whether the ordinary user has it is not measured.

## Flavors

- No source that this draft read names another product that speaks the
  Broker API. Step 7 asks the models.
- The single-stage and the multi-stage engine are two engines of one
  server, and not flavors. They differ in the default `LIMIT`, in the SQL
  that each one takes, and in the fields of the response (the documents).

## Interfaces

Step 10 writes the interface table from the code.

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

## Second opinions

Step 7 fills this section. For this draft, Gemini and DeepSeek were asked on
2026-09-27 about NULL, the types, parameters, cancellation and statements.
Several calls to each model timed out or closed first. Their answers are
leads:

- NULL: both say that a NULL arrives as JSON `null` with
  `enableNullHandling=true`, and as the default null value without it.
  DeepSeek says that the default is `0` for a number and `""` for a string.
  The schema reference says `Integer.MIN_VALUE` or `Long.MIN_VALUE` for a
  dimension, `0` for a metric, and `"null"` for a string. The documents win,
  and step 6 measures it.
- `TIMESTAMP`: DeepSeek says that it arrives as a JSON number of
  milliseconds since the epoch. Gemini says that it arrives as the string of
  `java.sql.Timestamp`. The source agrees with Gemini.
- `BYTES` is hex text and `BIG_DECIMAL` is a string (DeepSeek). The source
  agrees.
- Parameters: both say that the HTTP API binds none, and that the Java and
  JDBC clients put the values into the text. The source agrees.
- A client that disconnects: Gemini says that the query goes on. DeepSeek
  says that recent releases can cancel it. Step 6 measures it.
- Statements: DeepSeek says that `SET` statements can come before the query,
  and that the parser takes `--` and `/* */` comments. The options agree on
  `SET`.

## Open questions

- None about the memory. The image sets `-Xms4G -Xmx4G` (the Dockerfile),
  and the `dbmeta` entry sets `JAVA_OPTS` to `-Xms1G -Xmx2G`. The container
  used 1.6 GB (Summary).
- `dbrun` publishes one port, and the `dbmeta` entry publishes the Broker on
  8000. The Controller, on 9000, sends a query on to a Broker and also
  serves `GET /version`, and it is out of reach. If step 9 decides that the
  driver needs the Controller, the `dbmeta` session changes the entry.
- None about the ordinary user. The `dbmeta` entry writes a configuration of
  the Broker with `admin` and `dbmeta_user`, and `dbmeta_user` can read only
  the table `baseballStats`, which the QuickStart loads (dbmeta D112).
- The DSN, the Go type of each wire type, and whether the driver sets
  `enableNullHandling=true` on every query are decisions of step 9.
- Pinot takes no insert, update or delete of rows through SQL. DRIVER.md
  says to stop and ask Ken when a server refuses one of them (step 6).
- Which engine the driver uses by default, and how it tells the caller about
  the default `LIMIT` of 10 on the single-stage engine, are decisions of
  step 9.
