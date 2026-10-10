# Amazon Athena

This file holds what is known about Amazon Athena over its JSON API, for a
possible driver `athena` (W37 and D184). The headings are the template of
[DRIVER.md](DRIVER.md). A fact is "recorded", with the name of its request in
quotes, "measured", with how and when, or "not measured", with its source.

Step 6 recorded the service on 2026-10-10 in the region `us-east-1`, as one
login: an IAM user with the right to run queries in one workgroup and to write
to one bucket. The recorder signed each request with SigV4. Step 6 ran five times on
that day. The first pass had 255 requests. The second pass had 391 requests,
which were the 255 of the first pass, with the same names, and 136 new ones. The
third pass had 446 requests, which were the 391 of the second pass, with the
same names, and 55 new ones. The fourth pass had the same 446 requests, with a
new target for `UNLOAD`, a table in the two requests for result reuse, and the
ids of `SELECT` statements in the requests for statistics and for a batch get.
The folder holds the fifth pass only. It has 473 requests, which are the 446 of
the fourth pass and 27 new ones for the federated catalog. The fifth pass ran
after `dbsetup` made the Lambda connector and the data catalog `dbimp-cw`, and
gave the user the rights `athena:GetDataCatalog` and for the Lambda function.
The request that named a catalog that does not exist kept its recording under
the new name "a federated query on a catalog that does not exist". The IAM user
of the second to the fifth pass also had the rights for `athena:CreatePreparedStatement`,
`athena:GetPreparedStatement`, `athena:DeletePreparedStatement`,
`athena:ListDatabases`, `athena:ListQueryExecutions`,
`athena:GetQueryRuntimeStatistics`, `athena:BatchGetQueryExecution` and for
the Glue partitions and `glue:UpdateTable`. The recordings are under
`testdata/athena/`, with 446 requests in `requests.json` and one file for each
response. The recorder runs the requests of the phase `setup` first and the
requests of the phase `teardown` last, so the file numbers do not follow the
order of the list in `requests.json`. A name is the safe way to find a request.
The files 1 to 419 hold the same requests, in the same order, as the files 1 to
419 of the fourth pass, except that the files 400 and 401 hold the renamed
request. The files 420 to 446 hold the new requests of the federated catalog,
and the files 447 to 473 hold the teardown. The account id is `111122223333` in every file, and the recorder
redacts each id of a query execution that it captured, so a message can hold
`{{id8}}`. The workgroup is `dbimp`, the Glue database is `dbimp_test`, and the
bucket is `dbimp-athena-111122223333-us-east-1`. Athena has no release that a
person picks. The effective engine was "Athena engine version 3" (recorded:
"the workgroup").

The names of the requests in quotes are the names in `requests.json` (item 11
holds the new ones, and the third to the fifth pass added only to item 11). A request
that has two or more steps has names that end with ": the execution" for the
`GetQueryExecution` call and ": the results" for the `GetQueryResults` call.

Sources, each read on 2026-10-10:

- "athenadriver" is `github.com/uber/athenadriver` v1.1.15, the driver that
  `usql` uses, read from the cache of Go modules.
- "The SDK" is `github.com/aws/aws-sdk-go` v1.55.8, `service/athena`, read from
  the cache of Go modules. The module `aws-sdk-go-v2/service/athena` is not in
  the cache.
- "The quotas page" is the page of the AWS documentation named "Service Quotas"
  of Athena, fetched on 2026-10-10. "The pricing page" is
  `aws.amazon.com/athena/pricing`, fetched on 2026-10-10.
- "Gemini" is `gemini-3.1-pro-preview` and "Qwen" is `qwen3-coder-plus`, each
  asked on 2026-10-10. Kimi K3, Qwen Max and DeepSeek Pro timed out on every
  try on the same day.

## Summary

- Product: Amazon Athena, a service that runs SQL over data in Amazon S3 and
  in the AWS Glue Data Catalog. The engine is Trino, which Athena calls
  "Athena engine version 3" (recorded: "the workgroup"). Hive DDL runs for
  tables of files, and Iceberg tables take `INSERT`, `UPDATE`, `DELETE` and
  `MERGE`, with the count of changed rows in `UpdateCount` (recorded: "an
  insert into the iceberg table", "an update of the iceberg table").
- R: fails. Athena is a hosted service with no emulator, and `dbrun` has no
  entry for it. The measurement needs a real account and its credentials. This
  is the same shape as Snowflake (D182). Whether CI can hold a key pair and pay
  for each query is decided, D192 item 2: the integration tests run only where a
  person supplies the account.
- H: holds. Every call is `POST /` over HTTPS with a JSON body and a JSON answer
  (recorded: "a select"). No binary encoding is needed.
- S: holds. The statements are SQL of Trino and Hive DDL. They answered `SELECT`,
  `INSERT`, `UPDATE`, `DELETE`, `MERGE`, `CREATE TABLE`, `CREATE TABLE AS
  SELECT`, `CREATE VIEW`, `DROP TABLE`, `MSCK REPAIR TABLE`, `PREPARE`,
  `EXECUTE`, `SHOW`, `DESCRIBE`, `EXPLAIN` and `INFORMATION_SCHEMA` queries
  (recorded: see Statements).
- Whether it can be a driver: yes. No condition of "When it cannot be a driver"
  in [DRIVER.md](DRIVER.md) holds. The columns arrive in the first page, even
  for a result with no rows (recorded: "columns of a result with no rows"). A
  result is read in pages of at most 1000 rows, which is also the size when the
  request leaves out `MaxResults`, and the pages ran to the end for 2500 rows
  (recorded: "a result of 2500 rows", "the second page", "the third page", "the
  default page"). The server cuts nothing short. The signature of a request is written
  here, with the standard library. The facts that make it costly are these:
  every statement is asynchronous, so it takes at least three calls
  (recorded: "a select", "a select: the execution", "a select: the results"),
  the first row of the first page of a `SELECT` is a header row, every value is
  text, a container type has text that is not JSON, and a timestamp with a
  named zone has the name of the zone and no offset (see Types).
- Condition at risk: "The result cannot be read one row at a time". A page is a
  JSON body of at most 1000 rows, so the driver reads a page whole and then a
  row at a time. The size of a page for very wide rows is not measured. The
  facts of the second to the fifth pass that move the design are these: the
  same `ClientRequestToken` with the same query returns the same
  `QueryExecutionId`, a NULL and an empty string differ on the wire,
  `INFORMATION_SCHEMA`, `CREATE TABLE AS SELECT` and a prepared statement all
  work, `StopQueryExecution` on a running query ends it in the state
  `CANCELLED` (recorded: "stop the minute statement", "the minute statement
  after the stop"), and the text of an array, a map and a row has no escape
  for a comma or a quote (recorded: "commas and quotes in an array, a map and
  a row: the results"). `UNLOAD` to a new prefix succeeds (recorded: "unload"),
  and a second identical `SELECT` with result reuse reports a reuse (recorded:
  "a result reuse", "a result reuse again").
- Scheme in `dburl`: `Name` is `athena`, the aliases are `awsathena`, `s3` and
  `aws`, the generator is `GenSchemeHost("athena")`, the `Dialect` is `athena`,
  and `GoPackage` is `github.com/xo/dbimp/athena` (read of `dburl/scheme.go`,
  2026-10-10). The generator writes a URL with a scheme and a host only.
- Driver of `usql` now: `uber/athenadriver` v1.1.15, registered in `usql` as
  `awsathena`. It uses the SDK of AWS and the SDK v1 of Go. Its `Version`
  statement is `SELECT node_version FROM system.runtime.nodes LIMIT 1`, and the
  service refused that statement on 2026-10-10 in both passes (recorded: "the
  node version").
- What `dbmeta` has: not read in this work.

## Requests

- Every call is `POST /` to `https://athena.<region>.amazonaws.com/`. The
  host of the signature was `athena.us-east-1.amazonaws.com` (recorded: "a
  wrong secret").
- The header `X-Amz-Target` names the operation, such as
  `AmazonAthena.StartQueryExecution`. The content type is
  `application/x-amz-json-1.1`. The recorder sent no other header than
  `X-Amz-Target`, `Content-Type`, `X-Amz-Date` and `Authorization` (recorded:
  every request).
- The signature is AWS Signature Version 4. The credential scope was
  `20261010/us-east-1/athena/aws4_request`, so the service name is `athena`. The
  signed headers were `content-type;host;x-amz-date;x-amz-target`. The server
  printed its canonical request in the error of a wrong secret (recorded: "a
  wrong secret"). That text shows the method, the path `/`, an empty query, the
  four headers and the hash of the body. A temporary credential needs the header
  `X-Amz-Security-Token`. That header is not measured here, because the login
  is an IAM user with a key pair, and the work had no call to AWS STS to get a
  session token. The source is the AWS documentation for SigV4. D169 holds the
  same fact for DynamoDB.
- A statement is two or more calls:
  1. `StartQueryExecution` with the members `QueryString`, `WorkGroup`,
     `ClientRequestToken`, `QueryExecutionContext` (with `Database`) and
     `ExecutionParameters`. It answers `{"QueryExecutionId": "<uuid>"}` at once,
     before the query runs (recorded: "a select").
  2. `GetQueryExecution` with `QueryExecutionId`, until the state ends. The
     states that the recording shows are `QUEUED`, `RUNNING`, `SUCCEEDED`,
     `FAILED` and `CANCELLED`. The poll at once after the start of a query that
     ran for 8.8 seconds answered `QUEUED`, with only `QueryQueueTimeInMillis`
     and `TotalExecutionTimeInMillis` (293 ms) in `Statistics`. In the same
     answer the object `QueryExecutionDetail` said `RUNNING`. The next two polls
     answered `RUNNING` and the poll 9 seconds after the start answered
     `SUCCEEDED` (recorded: "a long statement for the states: the execution",
     "the state of the long statement after 0s", "the state of the long
     statement after 3s", "the state of the long statement after 5s"). A second
     query, which the recorder wrote to run for about a minute, was `RUNNING` in
     all three polls. `StopQueryExecution` followed the third poll, 6 seconds
     after the start. The next poll answered `CANCELLED` with
     `StateChangeReason` "Query cancelled by user". The query had ended 6.1
     seconds after its start, so the stop took effect within about a second.
     `QueryExecutionDetail` spells the state `CANCELED` with one L, and
     `QueryExecution` spells it `CANCELLED`.
     A `CANCELLED` answer has no `AthenaError` (recorded: "a statement that
     runs about a minute", "the state of the minute statement after 5s", "stop
     the minute statement", "the minute statement after the stop"). The SDK
     names all five states (the SDK, `QueryExecutionState`). A
     `RUNNING` answer has no `CompletionDateTime`, no `DataScannedInBytes` and no
     `EngineExecutionTimeInMillis` at first, and its `Stats` object in
     `QueryExecutionDetail` is empty. The later `RUNNING` answer, after 3
     seconds, had `DataScannedInBytes` and `EngineExecutionTimeInMillis`. The
     time of the succeeded queries in `TotalExecutionTimeInMillis` was from 242
     to 8800 ms for `DML` (the longest is the query of 8.8 seconds), from 274 to
     4134 ms for `DDL` (the longest is `MSCK REPAIR TABLE`) and from 212 to 2326
     ms for `UTILITY` (recorded: every execution). The first Iceberg `UPDATE`
     took 1472 ms (recorded: "an update of the iceberg table").
  3. `GetQueryResults` with `QueryExecutionId`, and `MaxResults` and `NextToken`
     for the pages after the first.
- `ClientRequestToken` was 40 characters in the recording. The SDK says that
  the minimum is 32 characters (the SDK, `StartQueryExecutionInput`). The same
  token with the same query and the same parameters returned the same
  `QueryExecutionId` in both calls, with HTTP 200 and no other sign that the
  second call was a repeat (recorded: "the same token and the same query 1",
  "the same token and the same query 2"). The same token with another query is
  an error: HTTP 400, `IDEMPOTENT_PARAMETER_MISMATCH`, "Idempotent parameters
  do not match" (recorded: "a client request token that is used twice").
- `QueryExecutionContext` has `Database`, and the SDK says that it can also have
  `Catalog` (the SDK). The name of the catalog in an error was
  `awsdatacatalog` (recorded: "a missing table"). A request with `Catalog` set
  to `AwsDataCatalog` ran, and the execution echoed the name in lower case,
  `awsdatacatalog` (recorded: "a catalog in the context"). A request with a
  `Catalog` that does not exist, `dbimp_nosuch`, also ran and succeeded, and
  the execution echoed that name. The statement touched no table, as in the
  case of a missing database (recorded: "a missing catalog"). A table
  name with a catalog that does not exist answered like a missing right (see
  Statements, the federated query). The context with the catalog `dbimp-cw` and
  the database `/aws/lambda/dbimp-cw` found the table `all_log_streams`, and the
  execution echoed both names (recorded: "a federated query with the catalog in
  the context: the execution").
- `ResultConfiguration` is `OutputLocation`, an S3 URI. The workgroup of this
  work sets `EnforceWorkGroupConfiguration` to true and its own
  `OutputLocation` (recorded: "the workgroup"). A request that sent another
  `OutputLocation`, `s3://<bucket>/other/`, was not refused, and the server
  used the location of the workgroup, `results/<QueryExecutionId>.csv`
  (recorded: "another result configuration"). The server does not tell the
  client that it ignored the location. The location of a result was
  `s3://<bucket>/results/<QueryExecutionId>.csv` for a `SELECT`, `.txt` for a
  statement of DDL or `DESCRIBE`, and the id alone for an `INSERT`, an `UNLOAD`
  and a `CREATE TABLE AS SELECT` (recorded: "a select: the execution",
  "describe the table: the execution", "an insert into the external table:
  the execution", "unload: the execution").
- `ResultReuseConfiguration` appeared in each `GetQueryExecution` answer. A
  request with `ResultReuseByAgeConfiguration` of `Enabled` true and
  `MaxAgeInMinutes` 60 was accepted, and the answer echoed it. The recorder
  sent `SELECT id FROM dbimp_it_ext` twice with that member. The first run
  reported `ReusedPreviousResult` false and scanned 272 bytes. The second run
  had its own `QueryExecutionId` and reported `ReusedPreviousResult` true. It
  scanned 0 bytes, its `OutputLocation` was the location of the first run, and
  its 23 rows were the rows of the first run (recorded: "a result reuse: the
  execution", "a result reuse again: the execution", "a result reuse: the
  results", "a result reuse again: the results"). A query over a table was
  reused.
- The workgroup `dbimp` has `BytesScannedCutoffPerQuery` of 104857600 (100
  MiB), `EnforceWorkGroupConfiguration` true, and `PublishCloudWatchMetricsEnabled`
  false (recorded: "the workgroup").
- The query string is at most 262144 bytes of UTF-8, and it is not adjustable
  (the quotas page). A query of 262217 characters answered at the start with
  HTTP 400, `INVALID_INPUT`, "1 validation error detected: Value at
  'queryString' failed to satisfy constraint: Member must have length less than
  or equal to 262144" (recorded: "a long query text"). The text was ASCII, so
  the recording does not say whether the server counts bytes or characters.
- A request that names a workgroup that does not exist answered with
  `AccessDeniedException`, as a workgroup that the user cannot use did, with
  the same text (recorded: "a missing workgroup", "the primary workgroup").
- A database that does not exist did not fail a `SELECT 1` that touched no
  table (recorded: "a missing database"). A `CREATE EXTERNAL TABLE` in a
  database that does not exist, `dbimp_nosuch`, failed in the state `FAILED`
  with `AthenaError.ErrorType` 1500. The user has no right to `glue:GetDatabase`
  on that database, so the answer names the right and not the missing database.
  `StateChangeReason` named `glue:GetDatabase`, and `ErrorMessage` named
  `glue:CreateTable`. What Athena says for a missing database when the user has
  the right is not measured (recorded: "a table in a database that does not
  exist: the execution").

## The DSN

`dburl` writes `athena://host` today (read of `dburl/scheme.go`, 2026-10-10).
What a driver needs, as facts and with no choice:

- Region, such as `us-east-1`. It is in the host and in the credential scope.
- Endpoint host, `athena.<region>.amazonaws.com`. A VPC endpoint or a test
  server needs another host. Not measured.
- Workgroup. The recording used `dbimp`. Without `WorkGroup` the request goes to
  the workgroup `primary`, and the user of this work cannot use it (recorded:
  "the primary workgroup"). That denial is a fact of the policy and not of
  Athena.
- Database of the Glue Data Catalog, sent as `QueryExecutionContext.Database`.
- Catalog, if it is not `AwsDataCatalog`. The request accepts the member (see
  Requests). A table is found in another catalog, as the federated query shows (see Statements).
- Output location, an S3 URI. It is optional when the workgroup sets one. The
  SDK says that a workgroup that enforces its configuration overrides the
  request (the SDK, `StartQueryExecutionInput`).
- Access key id and secret access key of an IAM user, and a session token for a
  temporary credential. The environment, a profile file and a role are other
  sources in the SDK of AWS. This work used a key pair only.
- athenadriver takes one DSN string, `s3://<bucket>/?region=...`, and reads the
  profile of AWS and the environment too (athenadriver, `config.go`). Its keys
  are not copied here.

## Responses

- A response is one JSON body, not a stream. Its media type is
  `application/x-amz-json-1.1`. The recording shows no `Content-Encoding`, no
  redirect and no `Retry-After`. The headers of a response were `Date`,
  `Content-Type`, `Content-Length` and `X-Amzn-Requestid` (recorded: every
  response).
- A number of time in `GetQueryExecution` is the seconds since the epoch in
  floating point, such as `1.791609504406E9` (recorded: "setup: drop the
  external table: the execution").
- `GetQueryExecution` answers `QueryExecution` and, as well, a second object
  `QueryExecutionDetail` that holds a shorter copy of the same facts. The SDK
  does not name `QueryExecutionDetail` (the SDK). `QueryExecution` holds
  `Status` (`State`, `StateChangeReason`, `SubmissionDateTime`,
  `CompletionDateTime`), `StatementType`, `SubstatementType`, `Statistics`,
  `EngineVersion`, `ResultConfiguration`, `ResultReuseConfiguration`,
  `QueryExecutionContext`, `Query` and `WorkGroup`. A failed query adds
  `Status.AthenaError` (recorded: "a division by zero: the execution").
- `StatementType` is `DDL`, `DML` or `UTILITY`. `SubstatementType` was, for
  `DDL`, `CREATE_TABLE`, `CREATE_TABLE_AS_SELECT`, `CREATE_VIEW`, `DROP_TABLE`,
  `DROP_VIEW`, `MSCK_REPAIR` and `ALTER_TABLE_ADD_PARTITION`. For `DML` it was
  `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `MERGE`, `EXPLAIN`, `EXECUTE` and
  `UNLOAD`. For `UTILITY` it was `DESCRIBE_TABLE`, `SHOW_TABLES`,
  `SHOW_CREATE_TABLE`, `SHOW_PARTITIONS`, `SHOW_COLUMNS` and `PREPARE`
  (recorded: the execution of each statement).
- `Statistics` has `DataScannedInBytes`, `EngineExecutionTimeInMillis`,
  `QueryQueueTimeInMillis`, `TotalExecutionTimeInMillis`,
  `ServicePreProcessingTimeInMillis`, `ServiceProcessingTimeInMillis`,
  `ResultReuseInformation` and, for some queries, `QueryPlanningTimeInMillis`.
  An `UNLOAD` adds `DataManifestLocation`, the S3 location of a manifest
  (recorded: "unload: the execution"). `SELECT 1` scanned 0 bytes, and a
  `SELECT` of 21 rows from the external table scanned 244 bytes (recorded:
  "a select", "a select from the external table"). The most that any query
  scanned was 41934 bytes, for the count over the federated table. The next
  most was 1184 bytes, for the select of four identical rows from the typed
  table, and the select from the Iceberg table scanned 586 (recorded: "a
  federated query with the catalog in the context: the execution", "select
  typed rows", "the iceberg rows after").
- `GetQueryResults` answers `ResultSet`, `UpdateCount` and, for some
  statements, `Output`. `ResultSet` holds the same data in two forms:
  - `ResultSetMetadata.ColumnInfo` with `Rows`, where each row is
    `{"Data": [{"VarCharValue": "1"}]}`. This is the form that the SDK names.
  - `ColumnInfos` with `ResultRows`, where each row is `{"Data": ["1"]}` and a
    NULL is JSON `null`. The SDK does not name this form.
  Both forms were equal in all 93 recorded results, with the same
  `ColumnInfo` in `ColumnInfos` and in `ResultSetMetadata` (recorded: "every
  type in one row", and each other result). A driver must read one of them.
- Where the names and the order of the columns come from: `ColumnInfo` of the
  first page, in the order of the statement. It is present when the result has
  no rows (recorded: "columns of a result with no rows"). Two columns can have
  the same name (recorded: "columns that share a name"). A column with no name
  is `_col0`, `_col1` and so on (recorded: "a select with a comment"). An alias
  in quotes keeps its case (recorded: "a column in lower case", `Mixed`).
- A `ColumnInfo` has `CaseSensitive` (true for `varchar` and `char`, false for
  every other type), `CatalogName` (`hive`), `Label` (the same as `Name`),
  `Name`, `Nullable` (always `UNKNOWN`), `Precision`, `Scale`, `SchemaName`
  (empty), `TableName` (empty) and `Type` (recorded: "a select", and every
  result).
- The header row. For a statement with `StatementType` `DML` that returns
  columns (`SELECT`, `EXPLAIN`), the first row of the first page holds the
  names of the columns as text, and it is not data (recorded: "a select",
  "explain"). A `SELECT` that finds nothing has the header row and no more
  (recorded: "columns of a result with no rows"). A statement with
  `StatementType` `UTILITY` has no header row (recorded: "show tables",
  "show create table", "describe the table", "show columns", "show
  partitions"). `EXECUTE` is `DML` and has a header row (recorded: "execute
  the prepared statement"). `MSCK REPAIR TABLE` is `DDL` and gave a result
  with no columns, no rows and an empty `Output` (recorded: "msck repair
  table"). The header row counts against
  `MaxResults`: with `MaxResults` 1000 the first page held the header and 999
  rows (recorded: "a result of 2500 rows").
- A statement that changes rows gives a result with one column `rows` of the
  type `bigint`, no rows, and the count in `UpdateCount`. An `INSERT` of three
  rows gave `UpdateCount` 3, into the external table and into the Iceberg
  table. On the Iceberg table an `UPDATE` of two rows gave 2, a `DELETE` of
  one row gave 1, and a `MERGE` of one row gave 1 (recorded: "an insert into
  the external table", "an insert into the iceberg table", "an update of the
  iceberg table", "a delete from the iceberg table", "a merge into the iceberg
  table", and the same four with "rows" in item 11). The rows that the table
  held at the end matched the changes (recorded: "the iceberg rows after").
  A `CREATE TABLE AS SELECT` of one row gave `UpdateCount` 1 and the same
  `rows` column (recorded: "a create table as select with no location").
  `DESCRIBE`, `SHOW CREATE TABLE`, `EXPLAIN` and `MSCK REPAIR TABLE` answered
  with no `UpdateCount`, and the other statements answered 0.
- `DESCRIBE` has three columns in `ColumnInfo` and one value in each row. That
  value joins the three fields with tab characters and pads each field with
  spaces (recorded: "describe the table"). The answer has `Output` with the
  same text. `SHOW TABLES` has one column and one name in each row.
  `SHOW CREATE TABLE` has one column and one line of the DDL in each row
  (recorded: "show create table"). `SHOW COLUMNS` has one column `field` of
  the type `string`, and each name is padded with spaces to 20 characters
  (recorded: "show columns"). `SHOW PARTITIONS` has one column `partition`,
  with values such as `p=a` (recorded: "show partitions"). athenadriver has code for a row that has
  fewer values than columns (athenadriver, `rows.go`).
- Paging: `GetQueryResults` answers `NextToken` while pages remain. `MaxResults`
  from 1 to 1000. The answer to 5000 is HTTP 400, `INVALID_INPUT`, "MaxResults
  is more than maximum allowed length 1000" (recorded: "a page larger than the
  limit"). When a request leaves out `MaxResults`, the first page of a result
  of 2500 rows held 1000 rows, the header row and 999 data rows, and it had a
  `NextToken` (recorded: "the default page"). So the default size is the
  maximum. The SDK says that the paginator uses
  `MaxResults` as its limit (the SDK). A page after the first holds data only
  (recorded: "the second page"). The last page has no `NextToken` (recorded:
  "the third page", 501 rows). A token that is not valid is HTTP 400,
  `INVALID_INPUT`, "Malformed nextPageToken nope" (recorded: "a wrong token of
  a page"). How long a token lives is not measured.
- A result is in S3, and `GetQueryResults` reads it from there. The SDK says that
  the caller needs `s3:GetObject` on the location of the results (the SDK). The
  results of 2500 rows needed no other call (recorded: "a result of 2500 rows").
  A larger result is not measured. A `GetQueryResults` on a query that a
  `StopQueryExecution` followed, after the query had ended, returned the
  whole result (recorded: "the results of the stopped statement").

## Types

The type name is the member `Type` of `ColumnInfo`, in lower case. Each value is
a string in `VarCharValue`. A value that is NULL has no `VarCharValue`, and its
datum is `{}` (recorded: "the type of CAST(NULL AS integer)"). In the form
`ResultRows` it is JSON `null`. An empty string is `{"VarCharValue": ""}`, and
`""` in `ResultRows`. The two differ in a literal and in a table of text files
(see the rule below).

- NULL against the empty string. `SELECT '' AS e, CAST(NULL AS varchar) AS n`
  gave `{"VarCharValue": ""}` and `{}` (recorded: "an empty string and a null
  in a literal"). A Hive external table of text files held the rows
  `(4, '')` and `(5, NULL)`. The select gave `""` and `isnull` of `false` for
  the first, and no value and `isnull` of `true` for the second (recorded:
  "insert an empty string and a null", "select the empty string and the
  null"). So the driver can tell them apart, and a NULL is nil and an empty
  string is `""`. The type of a literal `''` is `varchar` with `Precision` 0.

Each of these was recorded with `SELECT <expression> AS v`, in the request
named "the type of ..." for item 3. The names of the requests are in
`requests.json`.

- `tinyint`, `smallint`, `integer` and `bigint` are decimal digits. The
  `Precision` is 3, 5, 10 and 19 and the `Scale` is 0. The largest `bigint`,
  `9223372036854775807`, arrived whole. The smallest value of each type is not
  measured.
- `float` is the type name for a `REAL`. The `Precision` is 17. `double` is
  also 17. A value is the text `1.5`. The values of `nan()`, `infinity()` and
  `-infinity()` are `NaN`, `Infinity` and `-Infinity`, with the type `double`.
  The text of a very large or small value, such as `1E-10`, is not measured.
- `decimal` has `Precision` and `Scale`. `decimal(38,2)` gave `12345678.91`. A
  decimal of 38 digits is not measured, because the literal of 36 digits that
  the request used was refused again (recorded: "the type of CAST(1234...
  AS decimal(38,0))", HTTP 400, "Queries of this type are not supported").
- `varchar` has `Precision` 2147483647 for a column with no length, and 1 for the
  literal `'x'`. A value is UTF-8 text (`héllo`). `char(4)` has `Precision` 4 and
  the value is padded with spaces to that length (`ab  `).
- `string` is the type of the columns of `SHOW` and `DESCRIBE`, with `Precision`
  0 (recorded: "describe the table"). A Hive column of the type `string` is a
  `varchar` in a `SELECT` (recorded: "select the empty string and the null").
- `boolean` is `true` or `false`.
- `date` is `2026-10-10`. The smallest and the largest, `0001-01-01` and
  `9999-12-31`, arrived as they are written (recorded: "the type of DATE
  '0001-01-01'", "the type of DATE '9999-12-31'").
- `time` has `Precision` 3 for a literal with milliseconds. A value is
  `12:34:56.123`.
- `timestamp` has `Precision` 3, and the value is `2026-10-10 12:34:56.123`,
  with a space and no zone. A value with no fraction has no fraction in the
  text: the parameter `TIMESTAMP '2026-10-10 01:02:03'` gave `2026-10-10
  01:02:03` with the same `Precision` 3 (recorded: "a timestamp parameter").
  A parser must accept both forms.
- `timestamp with time zone` is `2026-10-10 12:34:56.123 +07:00` for a literal
  that names an offset (recorded: "the type of TIMESTAMP '2026-10-10
  12:34:56.123 +07:00'"). A literal with a named zone gave the name and no
  offset: `2026-10-10 12:34:56 Europe/Paris`. `AT TIME ZONE 'Asia/Tokyo'` of a
  UTC value gave `2026-10-10 21:34:56 Asia/Tokyo` (recorded: "a timestamp with
  a named zone", "at time zone"). So the text ends with an offset or with a
  name, and a name has no offset. The `Precision` is 3 and the zero fraction is
  left out, as for `timestamp`. The text for a zone at offset zero, and for a
  column of this type in a table, is not measured.
- `time with time zone` has `Precision` 3, and the value is `12:34:56+07:00`,
  with no space before the offset and no fraction, because the literal had none
  (recorded: "time with time zone"). The text of a value with a fraction is not
  measured.
- `varbinary` has `Precision` 1073741824. The value is lower case hex in pairs
  that a space separates, `de ad be ef`. A Hive column of the type `binary`
  has the type `varbinary` and the same text (recorded: "select typed rows").
  So the server has no wire type named `binary`.
- `array` is `[1, 2, 3]`, `map` is `{a=1, b=2}` and `row` is `{a=1, b=x}`. None
  of them is JSON, and `ColumnInfo` names no type of the elements. A Hive
  column of the type `array<string>` gave `[p, q]`, with no quotes, a `map`
  of `map<string,int>` gave `{k=7}`, and a `struct<a:int,b:string>` has the
  type `row` and the text `{a=1, b=x}` (recorded: "select typed rows"). So a
  Hive `struct` has no wire type of its own. `DESCRIBE` names the Hive types
  `binary`, `struct<a:int,b:string>`, `map<string,int>` and `array<string>`
  (recorded: "describe the typed table"). The text has no escape. The array
  `ARRAY['a,b','c"d']` gave `[a,b, c"d]`, the map `MAP(ARRAY['k,1'],ARRAY['v"2'])`
  gave `{k,1=v"2}`, and the row of `'x,y'` and `'z"w'` gave `{a=x,y, b=z"w}`. A
  comma and a quote are written as they are, and the elements are joined with a
  comma and a space. So the text of the array above cannot be told from an
  array of three elements (recorded: "commas and quotes in an array, a map and a
  row: the results"). A NULL element, a space, a brace and a nested container
  in the text are not measured. Decided, D192 item 3: the driver does not parse it.
- `json` is the JSON text that was written, `{"a":1}`.
- `ipaddress` is `2001:db8::1`. `uuid` is the 36 character text.
- `interval day to second` is `1 00:00:00.000` and `interval year to month` is
  `1-0`.
- `geometry` is the well known text, `POINT (1 2)`. The recorder asked
  `ST_Point(1,2)`.
- `unknown` is the type of a bare `NULL`. Its value is absent (recorded: "a null
  parameter").
- `time with time zone`, `binary` and `struct` are in the lists of athenadriver
  (athenadriver, `rows.go`). The recording measured the first as a wire type,
  and the other two as the wire types `varbinary` and `row`.
- The only type name that the SDK page lists for `Type` is a string with no
  list of values (the SDK, `ColumnInfo`). The names above come from the
  recording.

The mapping of step 8a follows. The Go types are the types of the kinds of
[TYPES.md](TYPES.md). The row of each of `ARRAY`, `MAP` and `ROW` is decided,
D192 item 3, and the row of each interval and of
`IPADDRESS` follows the sibling Trino driver or the kind that D177 added (decided, D192 item 11). The
row of `TIME WITH TIME ZONE` uses the kind that D139 added, and the row of
`TIMESTAMP WITH TIME ZONE` follows the Trino driver for the zone that a text
names (decided, D192 item 7).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| TINYINT | integer | `int64` | `int64` | `TINYINT` | yes |
| SMALLINT | integer | `int64` | `int64` | `SMALLINT` | yes |
| INTEGER | integer | `int64` | `int64` | `INTEGER` | yes |
| BIGINT | integer | `int64` | `int64` | `BIGINT` | yes |
| FLOAT | float | `float64, with the infinities and NaN from their text` | `float64` | `FLOAT` | yes |
| DOUBLE | float | `float64, with the infinities and NaN from their text` | `float64` | `DOUBLE` | yes |
| DECIMAL | decimal | `*apd.Decimal, from the text` | `*apd.Decimal` | `DECIMAL` | yes |
| VARCHAR | string | `string` | `string` | `VARCHAR` | yes |
| CHAR | string | `string, with the padding of spaces that the server sends` | `string` | `CHAR` | yes |
| STRING | string | `string` | `string` | `STRING` | yes |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| TIME | time of day | `dbimp.LocalTime` | `dbimp.LocalTime` | `TIME` | yes |
| TIME WITH TIME ZONE | time of day with offset | `dbimp.OffsetTime, with no fraction when the text has none` | `dbimp.OffsetTime` | `TIME WITH TIME ZONE` | yes |
| TIMESTAMP | local timestamp | `dbimp.LocalDateTime, with no fraction when the text has none` | `dbimp.LocalDateTime` | `TIMESTAMP` | yes |
| TIMESTAMP WITH TIME ZONE | timestamp | `time.Time, in the zone that the text names, an offset or a name` | `time.Time` | `TIMESTAMP WITH TIME ZONE` | yes |
| VARBINARY | binary | `[]byte, from the hex pairs that spaces separate` | `[]uint8` | `VARBINARY` | yes |
| JSON | json | `the decoded JSON value, from the JSON text` | `interface {}` | `JSON` | yes |
| IPADDRESS | ip address | `netip.Addr` | `netip.Addr` | `IPADDRESS` | yes |
| UUID | uuid | `uuid.UUID` | `uuid.UUID` | `UUID` | yes |
| INTERVAL DAY TO SECOND | interval | `dbimp.Interval, with Days and Nanoseconds` | `dbimp.Interval` | `INTERVAL DAY TO SECOND` | yes |
| INTERVAL YEAR TO MONTH | interval | `dbimp.Interval, with Months` | `dbimp.Interval` | `INTERVAL YEAR TO MONTH` | yes |
| GEOMETRY | geometry | `string, the well known text of the server` | `string` | `GEOMETRY` | yes |
| ARRAY | other | `string, the text of the server` | `string` | `ARRAY` | yes |
| MAP | other | `string, the text of the server` | `string` | `MAP` | yes |
| ROW | other | `string, the text of the server` | `string` | `ROW` | yes |
| UNKNOWN | null | `nil` | `interface {}` | `UNKNOWN` | yes |
<!-- /dbimp:types -->

Rows that were doubtful, all decided in D192 (items 3, 7 and 10):

- `ARRAY`, `MAP` and `ROW`: the text is not JSON and the types of the elements
  are not in `ColumnInfo`. The text of an `array<string>` has no quotes, so
  `[p, q]` cannot say whether an element is a string, and an element that holds
  a comma cannot be told apart. The fifth pass settled that the text has no
  escape: `[a,b, c"d]` is the array of the two strings `a,b` and `c"d`. The kind `other` returns the text, which D135
  allows for a type that has no Go type. Both models agreed that parsing is
  unsafe. The alternative is a list or a map of strings that a parser reads
  from the text, and that is a decoded value that guesses its types.
- `INTERVAL DAY TO SECOND` and `INTERVAL YEAR TO MONTH` as `dbimp.Interval`
  follow the Trino driver. The alternative for the day to second type is
  `time.Duration`, which the kind duration names.
- `IPADDRESS` as `netip.Addr` follows the kind of D177. The Trino driver keeps it
  as the text.
- `GEOMETRY` as the text is the form that the Databend and Trino drivers use.
- `TIMESTAMP WITH TIME ZONE` has two forms of zone in the text, an offset and a
  name such as `Europe/Paris`. A name with no offset needs the zone database
  of the host (decided, D192 item 7).
- `TIME WITH TIME ZONE` as `dbimp.OffsetTime` follows D139. The text with a
  fraction and of the offset zero are not measured. Presto wrote the offset zero
  as `UTC` in the Trino driver ([TRINO.md](TRINO.md)), and Athena is not measured for it.
- `FLOAT`: the server names a `REAL` `float`, so `ColumnTypeDatabaseTypeName`
  gives `FLOAT` for it and for a Hive `float`.
- `BINARY` and `STRUCT` have no row, because the server reports a Hive
  `binary` as `varbinary` and a Hive `struct` as `row`.

## Parameters

- `StartQueryExecution` takes `ExecutionParameters`, a list of strings. Each `?`
  in the statement takes the next string, in order. Each string is the text of a
  SQL expression, and the server reads it: `42` was an integer, `'x'` was a
  `varchar`, `NULL` was an `unknown` with no value, and
  `TIMESTAMP '2026-10-10 01:02:03'` was a `timestamp` (recorded: "a parameter of
  the type integer", "a parameter that is a string literal", "a null
  parameter", "a timestamp parameter"). A parameter in a comparison worked
  (recorded: "a parameter in a comparison").
- The bare word `x`, with no quotes, gave the string `x` with the type
  `varchar(1)` (recorded: "a parameter that is a bare word"). Why is not
  measured.
- The server binds the values, but a value is SQL text that the driver writes,
  so the driver needs a writer of literals for each Go type. The rules of
  Trino for a literal are not measured here. athenadriver escapes a string with
  a backslash, which is the rule of MySQL (athenadriver, `connection.go`).
- Too few and too many parameters fail the query, not the request. The state is
  `FAILED` with `INVALID_PARAMETER_USAGE: line 1:1: Incorrect number of
  parameters: expected 2 but found 1` (recorded: "too few parameters", "too
  many parameters"). A value that does not fit fails too, with
  `INVALID_CAST_ARGUMENT` (recorded: "a parameter that does not fit").
- A parameter holds at most 1024 characters. A longer one fails the request with
  HTTP 400, `INVALID_INPUT` (live run of 2026-10-10, "Member must have length
  less than or equal to 1024"). So when an argument is longer than 1024 bytes,
  the driver writes every argument into the text of the statement, with the same
  literals, and sends no `ExecutionParameters`.
- Named parameters: none. A parameter in `INSERT` and in DDL: not measured.
  The most parameters: not measured.
- A prepared statement of the workgroup works in two ways. The call
  `CreatePreparedStatement` with `StatementName`, `WorkGroup` and
  `QueryStatement` answered HTTP 200 and `{}`, and `GetPreparedStatement`
  answered `PreparedStatement` with `LastModifiedTime`, `QueryStatement`,
  `StatementName` and `WorkGroupName` (recorded: "create a prepared
  statement", "get the prepared statement"). A query `PREPARE name FROM ...`
  also made one: its execution was `UTILITY` and `PREPARE`, and the
  teardown deleted it with `DeletePreparedStatement` (recorded: "prepare in a
  query", "teardown: delete the other prepared statement"). In the first pass
  the user lacked `athena:CreatePreparedStatement`, and the query failed.
  `PREPARE dbimp_p FROM SELECT ?` also ran, as `UTILITY` and `PREPARE`
  (recorded: "a prepared statement: the execution").
- `EXECUTE name USING 5, 'x'` ran as a query with no `ExecutionParameters`. The
  values are SQL text, as for a `?`. The result had a header row, and the types
  were `integer` and `varchar(1)` (recorded: "execute the prepared
  statement"). Too few values failed the query with `INVALID_PARAMETER_USAGE:
  line 1:1: Incorrect number of parameters: expected 2 but found 1` (recorded:
  "execute with too few values"). `DeletePreparedStatement` answered HTTP 200
  and `{}`. A `GetPreparedStatement` after it answered HTTP 400,
  `ResourceNotFoundException`, "No Prepared Statement with statement name
  dbimp_ps exists in WorkGroup dbimp" (recorded: "delete the prepared
  statement", "get the deleted prepared statement"). A second delete answered
  HTTP 400, `ResourceNotFoundException`, "PreparedStatement dbimp_ps was not
  found in WorkGroup dbimp" (recorded: "teardown: delete the prepared
  statement"). The text of the two messages differs. The quotas page says that
  a workgroup can hold 1000 prepared statements.
- The teardown deletes the statements that the queries made. It deleted
  `dbimp_p` and `dbimp_ps2`, each with HTTP 200 and `{}` (recorded: "teardown:
  delete the first prepared statement", "teardown: delete the other prepared
  statement"). It tried to delete `dbimp_ps` a second
  time and got the HTTP 400 that is named above.

## Transactions

- None. `START TRANSACTION` answered HTTP 400, `MALFORMED_QUERY`, "Queries of
  this type are not supported", before a query existed (recorded: "a start
  transaction"). The same message answered `USE` (recorded: "a use statement"),
  `SELECT node_version FROM system.runtime.nodes` (recorded: "the node
  version") and a decimal literal of 36 digits (recorded: "the type of
  CAST(1234... AS decimal(38,0))"). So the message does not name the cause.
- The parser lists `COMMIT`, `ROLLBACK` and `START` as words that a statement can
  begin with (recorded: "a syntax error"). That list is the list of Trino and
  not proof that Athena runs them.
- Each Iceberg statement is its own atomic commit in the table. That is a fact
  of the format and not measured here.
- athenadriver returns an error from `Begin` (athenadriver, `connection.go`).

## Errors

An error has one of six forms.

1. HTTP 400 `InvalidRequestException`. The body has `__type`, `AthenaErrorCode`,
   `ErrorCode` (the same value) and `Message`. It means that the request was
   refused and no query ran. The values of `AthenaErrorCode` recorded:
   - `MALFORMED_QUERY`, for a syntax error, for two statements, and for the
     message "Queries of this type are not supported" (recorded: "a syntax
     error", "two statements", "a start transaction", "an index"). The syntax
     error of `INSERT OVERWRITE INTO` was "line 1:8: mismatched input
     'OVERWRITE'. Expecting: 'INTO'" (recorded: "insert overwrite"). A
     `CREATE EXTERNAL TABLE` with a primary key, a unique column, a foreign
     key, a default value or `NOT NULL` gave "line 1:8: mismatched input
     'EXTERNAL'. Expecting: 'MATERIALIZED', 'MULTI', 'OR', 'PROTECTED', 'ROLE',
     'SCHEMA', 'TABLE', 'VIEW'" (recorded: "a primary key on a hive table",
     "a unique column", "a foreign key", "a default value", "a not null
     column"). That is the list of the Trino parser, not a message about the
     constraint, and it is the same text for all five.
   - `INVALID_INPUT`, for `MaxResults` above 1000, a token that is not valid,
     the `external_location` that the workgroup refuses, and a query string that
     is too long (recorded: "a page
     larger than the limit", "a wrong token of a page", "a create table as
     select", "a long query text").
   - `QUERY_EXECUTION_NOT_FOUND`, for an id that does not exist (recorded: "the
     results of an unknown execution").
   - `INVALID_QUERY_EXECUTION_STATE`, for `GetQueryResults` on a query that
     failed. The message is "Query did not finish successfully. Final query
     state: FAILED" (recorded: "an error after some rows: the results"). On a
     query that was still running the message is "Query has not yet finished.
     Current state: RUNNING" (recorded: "a federated query: the results").
   - `IDEMPOTENT_PARAMETER_MISMATCH`, for a token that was used with another
     query (recorded: "a client request token that is used twice").
   - `RESULT_NOT_FOUND`, for `GetQueryResults` on a query that was cancelled
     (see form 6).
2. HTTP 400 `AccessDeniedException`. The body has `__type` and `Message` with a
   capital M, and no `AthenaErrorCode` (recorded: "the primary workgroup", "a
   missing workgroup"). In the first pass the same form answered
   `ListDatabases` and `ListQueryExecutions`, because the user lacked the
   rights. In the second pass both worked.
3. HTTP 400 `InvalidSignatureException`. The body has `__type` and `message`
   with a small m. It holds the canonical request and the string to sign
   (recorded: "a wrong secret"). The status is 400 and not 403.
4. HTTP 200, and the query fails later. `GetQueryExecution` answers `State`
   `FAILED`, `StateChangeReason` as text and `AthenaError` with
   `ErrorCategory`, `ErrorMessage`, `ErrorType` and `Retryable`. The values that
   were recorded:

   | ErrorType | Text begins | Request |
   | --- | --- | --- |
   | 1001 | `DIVISION_BY_ZERO` | "a division by zero" |
   | 1006 | `COLUMN_NOT_FOUND` | "a missing column" |
   | 1100 | `INVALID_CAST_ARGUMENT`, `INVALID_PARAMETER_USAGE` | "a parameter that does not fit", "too few parameters", "execute with too few values" |
   | 1106 | `INVALID_FUNCTION_ARGUMENT` | "a statement that runs long" |
   | 1200 | `NOT_SUPPORTED` | "an update of the external table", "iceberg time travel by version" |
   | 1500 | the right `glue:GetDatabase` | "a table in a database that does not exist" |
   | 9999 | the right `athena:GetDataCatalog` | "a federated query on a catalog that does not exist" |
   | 1301 | `TABLE_NOT_FOUND` | "a missing table" |
   | 1303 | `FUNCTION_NOT_FOUND` | "version function" |

   The type 1500 is for the messages `Insufficient permissions to execute the
   query` and `You are not authorized`, which the missing rights of the user
   caused. The first pass had it. The second pass had the rights and had none.
   The fifth pass has one, for the missing right that is named in the table.
   The type 9999 is "Unknown error occurred." in `ErrorMessage`, with the text
   about the right in `StateChangeReason`.

   `ErrorCategory` was 2 (user) in every case except the type 9999, which had 3,
   and `Retryable` was false. The SDK says that 1 is system, 2 is user and 3 is
   other (the SDK, `AthenaError`).
   The `ErrorMessage` of the type 1106 is an empty string, and the text is only
   in `StateChangeReason` (recorded: "a statement that runs long: the
   execution"). A driver must read `StateChangeReason` and not only
   `AthenaError`. In the other cases both hold the same text.
   The message of a missing table names the catalog, the database and the table:
   `Table 'awsdatacatalog.dbimp_test.dbimp_it_nosuch' does not exist`.
5. HTTP 400 `ResourceNotFoundException`. The body has `__type` and `Message`
   with a capital M, and no `AthenaErrorCode`. It answered a prepared
   statement that does not exist (recorded: "get the deleted prepared
   statement", "teardown: delete the prepared statement").
6. A `CANCELLED` query has no error. `GetQueryExecution` answers the state and
   the text "Query cancelled by user". `GetQueryResults` of it is HTTP 400
   `InvalidRequestException` with `AthenaErrorCode` `RESULT_NOT_FOUND`,
   "Could not find results" (recorded: "the results of the stopped minute
   statement"). A `FAILED` query gave `INVALID_QUERY_EXECUTION_STATE` there.
- No query failed after some rows. The query that divides by zero at row 1500 of
  2500 failed as a whole, and `GetQueryResults` refused it (recorded: "an error
  after some rows"). So an error never comes after a row.
- A statement that did not reach the server was refused before any query. The
  wrong secret, a statement of two parts, and a missing workgroup are such
  cases. A reply that holds `__type` `InvalidSignatureException` or
  `AccessDeniedException` means that no query started, because no id came back.
- A `GetQueryExecution` that follows a refused start answered
  `QUERY_EXECUTION_NOT_FOUND` for the id (recorded: "two statements: the
  execution"). The recorder sent the text `{{id8}}` there, because the capture
  failed.
- Throttling: `TooManyRequestsException` when the active queries pass the quota,
  and `ThrottlingException` "Rate exceeded" when the calls per second pass the
  quota (the quotas page, and the SDK, `errors.go`). Not measured. The work did
  not reach a limit, and it ran no burst of calls, because a burst costs money
  and can throttle the account of Ken.
- The workgroup limit `BytesScannedCutoffPerQuery` stops a query that scans more.
  What the state and the message are is not measured. The most that any
  recorded query scanned was 41934 bytes, and a test of 100 MiB needs a table
  of that size, which the work did not create.
- `GetQueryRuntimeStatistics` answered `QueryRuntimeStatistics` with a
  `Timeline` of five times and no other member for a `SELECT`, the same numbers
  as its `Statistics` (recorded: "the statistics of a select", "the statistics
  of an execution"). `BatchGetQueryExecution` answered `QueryExecutions`, with
  the same objects as `GetQueryExecution`, and `UnprocessedQueryExecutionIds`,
  which was empty (recorded: "batch get of two selects", "batch get"). Both
  requests of item 10 used the ids of `SELECT` statements and answered HTTP 200.
  The id of a DDL statement was not sent, so what the two calls answer for it is
  not measured.

### The refused credential (D197)

`errors.Is(err, dbimp.ErrAuthentication)` is true when the `Type` of the `Error`
is `InvalidSignatureException` or `UnrecognizedClientException`. The server
answers HTTP 400 for both. The recorded case is `InvalidSignatureException` for
"a wrong secret", on the start of the query and on its execution.
`UnrecognizedClientException` is the form that AWS documents for an access key
that it does not know. No recording holds it, because the recorded run used a
known key, and the unit test builds it from that form.

The match is false for `AccessDeniedException`, which is a missing permission
(recorded: a workgroup that the user cannot use, and the list of data
catalogs). It is false for `InvalidRequestException`, for a query that ended in
the state FAILED or CANCELLED, and for every other type. `errors.Is` also finds
the match through a wrapped error and through `errors.Join`.

## Cancellation and timeouts

- `StopQueryExecution` answered HTTP 200 and `{}` in every call. On a query
  that was running, it ended the query. The query that the recorder wrote to
  run for about a minute was `RUNNING` 6 seconds after its start. The stop
  followed at once, and the next poll answered `CANCELLED`, "Query cancelled by
  user", with a completion 6.1 seconds after the start (recorded: "a statement
  that runs about a minute", "the state of the minute statement after 5s",
  "stop the minute statement", "the minute statement after the stop"). The
  answer to the call does not say whether the query was running. The state
  comes from the next `GetQueryExecution`. `GetQueryResults` of the cancelled
  query is HTTP 400, `RESULT_NOT_FOUND` (recorded: "the results of the stopped
  minute statement").
- On a query that had ended, the stop answered 200 and `{}`, and the state
  stayed as it was. One query had failed (recorded: "stop the statement", "the
  execution after the stop", "stop a statement that has ended"), and the other
  had succeeded (recorded: "stop the long statement", "the long statement after
  the stop"). The second ran for 8.8 seconds, and the poll after 5 seconds,
  which came 9 seconds after the start, had already seen `SUCCEEDED`. A `GetQueryResults` after that stop returned the whole result,
  2500000000 for the count (recorded: "the results of the stopped statement").
  The call is accepted for any query that exists.
- The first long statement of the first pass failed at once with
  `INVALID_FUNCTION_ARGUMENT: result of sequence function must not have more
  than 50000 entries` (recorded: "a statement that runs long"). The queries of
  the later passes use the function in a join, so each call stays under that
  limit.
- The answer of `StartQueryExecution` comes before the query runs, so the query
  runs on the server whether or not the client stays. A client that leaves
  stops nothing. That follows from the design, and the recording did not
  disconnect a client.
- athenadriver calls `StopQueryExecution` when the context ends during the poll,
  and it uses `context.Background` for that call (athenadriver,
  `connection.go`).
- Timeouts: the quotas page says that a DML query has a timeout that can rise to
  240 minutes, and it names a timeout for DDL. It gives no default number. A
  default of 30 minutes for DML was named in the brief for this work, from the
  AWS documentation, and it is not measured. A timeout in the request: none that
  the recorder sent.
- A time to wait for a result: the polling interval is the choice of the
  client. A call of `GetQueryExecution` took the time of a normal request.

## Statements

- One statement for each `StartQueryExecution`. Two statements answered HTTP
  400, `MALFORMED_QUERY`, "Only one sql statement is allowed. Got: SELECT 1;
  SELECT 2" (recorded: "two statements"). A trailing semicolon after one
  statement worked (recorded: "a select with a trailing semicolon"). A comment
  before and a comment after the statement worked (recorded: "a select with a
  comment").
- A statement that is not valid answered at the start, not at the poll
  (recorded: "a syntax error", with the list of words that can begin a
  statement).
- `SELECT`, `INSERT`, `UPDATE`, `DELETE` and `MERGE` ran. `INSERT` and `SELECT`
  worked on a Hive external table. `UPDATE` and `DELETE` on that table failed
  with `NOT_SUPPORTED: Modifying Hive table rows is only supported for
  transactional tables` (recorded: "an update of the external table", "a delete
  from the external table"). `INSERT`, `UPDATE`, `DELETE` and `MERGE` all
  worked on an Iceberg table, with rows, and `UpdateCount` gave the count (see
  Responses, and recorded: "an insert into the iceberg table", "an update of
  the iceberg table", "a delete from the iceberg table", "a merge into the
  iceberg table", "the iceberg rows"). The first pass failed the `INSERT`
  because the user lacked `glue:UpdateTable`, and the second pass had it.
- `DROP TABLE` of an external table leaves its data files in S3. The first
  `SELECT` from the new external table, which had a location that held files
  from earlier runs, returned six data rows in the second pass where the
  statement had inserted three, each row twice. In the fifth pass it returned
  21 rows: each of the ids 1, 2 and 3 five times, and the ids 4 and 5 three
  times each, which earlier runs had written (recorded: "a select from the
  external table"). The same leftover files repeated the rows of the typed
  table, four times, and of the table of empty strings and NULLs, four times
  each
  (recorded: "select typed rows", "select the empty string and the null"). A test that uses an external table
  must use a new location in S3 for each run.
- DDL: `CREATE EXTERNAL TABLE`, `CREATE TABLE` with `table_type` of `ICEBERG`
  and `DROP TABLE IF EXISTS` ran (recorded: "setup: create the external table",
  "setup: create the iceberg table", "setup: drop the external table"). A
  Hive table of Parquet files with columns of the types `binary`, `struct`,
  `map` and `array` also ran (recorded: "setup: create the typed table"), and
  a partitioned one (recorded: "setup: create the partitioned table").
- Constraints and indexes. A `CREATE EXTERNAL TABLE` with `PRIMARY KEY (id)`,
  with `id int UNIQUE`, with a `FOREIGN KEY ... REFERENCES`, with `id int
  DEFAULT 1` or with `id int NOT NULL` answered HTTP 400, `MALFORMED_QUERY`, at
  the start, with the same text each time (see Errors). The same statement
  with no constraint runs (recorded: "setup: create the external table"), so
  the constraint is the cause, but the text does not name it. An Iceberg
  `CREATE TABLE` with `PRIMARY KEY (id)` answered HTTP 400 too, "line 1:54:
  mismatched input 'LOCATION'. Expecting: 'COMMENT', 'WITH', <EOF>" (recorded:
  "a primary key on an iceberg table"). `CREATE INDEX` answered "Queries of this
  type are not supported" (recorded: "an index"). The cases of `NOT NULL`,
  `UNIQUE`, `DEFAULT` and a foreign key on an Iceberg table were not sent.
- Bucketing and partition projection. `CREATE EXTERNAL TABLE ... CLUSTERED BY
  (id) INTO 4 BUCKETS` ran as `DDL` with `CREATE_TABLE`. A table with
  `PARTITIONED BY (p string)` and the properties `projection.enabled`,
  `projection.p.type` of `enum`, `projection.p.values` and
  `storage.location.template` ran too (recorded: "a bucketed table", "a table
  with partition projection"). No `INSERT` or `SELECT` followed, so what a
  query does with the buckets or the projection is not measured.
- Time travel on the Iceberg table. `FOR TIMESTAMP AS OF (current_timestamp -
  interval '1' minute)` ran and scanned 264 bytes. `FOR VERSION AS OF 1` failed
  with `NOT_SUPPORTED: Unsupported type for table version: integer`, so the
  version is not an `integer` literal. `SELECT snapshot_id FROM
  "dbimp_it_ice$history"` ran and scanned 192 bytes. The recorder read none of
  the three results, so the rows are not measured, and a version with a
  `bigint` snapshot id was not sent (recorded: "iceberg time travel by
  timestamp", "iceberg time travel by version", "the history of an iceberg
  table").
- Federated queries. The recorder used a data catalog `dbimp-cw` of the type
  `LAMBDA` (recorded: "get the federated data catalog"), with one database,
  `/aws/lambda/dbimp-cw`, and one table, `all_log_streams`. The names of the
  catalog and of the database need quotes in a statement.
  - `SELECT count(*) AS c FROM "dbimp-cw"."/aws/lambda/dbimp-cw".all_log_streams`
    was accepted at the start. Its single poll answered `RUNNING`, 13.7 seconds
    after the start, and `GetQueryResults` answered HTTP 400,
    `INVALID_QUERY_EXECUTION_STATE`, "Query has not yet finished. Current
    state: RUNNING". The recording has no later poll, so the final state of this
    query is not measured. The cause of the delay is not measured. A cold start
    of the Lambda function is one guess (recorded: "a federated query", "a
    federated query: the execution", "a federated query: the results").
  - The same count with `Catalog` `dbimp-cw` and `Database` `/aws/lambda/dbimp-cw`
    in `QueryExecutionContext`, and `FROM all_log_streams`, succeeded in 5994 ms.
    It scanned 41934 bytes. The result is one column `c` of the type `bigint`,
    with the header row and the value 220 (recorded: "a federated query with the
    catalog in the context", "a federated query with the catalog in the context:
    the execution", "a federated query with the catalog in the context: the
    results"). The statement type is `DML`, as for a table of Glue.
  - `SHOW TABLES` with that context ran as `UTILITY` with `SHOW_TABLES`. The
    column is `tab_name`, and the 8 rows are 7 tables with names such as
    `2026/10/10/[$LATEST]<hex>`, one for each log stream, and `all_log_streams`
    (recorded: "show tables with the catalog in the context: the results").
  - `SHOW DATABASES IN "dbimp-cw"` answered HTTP 400, `MALFORMED_QUERY`, "line 1:6:
    mismatched input 'DATABASES'". The list of words that can follow `SHOW`
    names `SCHEMAS` and `CATALOGS`, and those were not sent (recorded: "show
    databases in the federated catalog"). `SHOW TABLES IN "dbimp-cw"."/aws/lambda/dbimp-cw"`
    answered HTTP 400, `MALFORMED_QUERY`, at the quoted catalog name (recorded:
    "show tables in the federated catalog"). `DESCRIBE` with the name of three
    parts answered HTTP 400, `MALFORMED_QUERY`, "no viable alternative at input
    'DESCRIBE "dbimp-cw"'" (recorded: "describe a federated table"). A `SELECT`
    takes the name of three parts (see the first item).
  - `SELECT table_name FROM "dbimp-cw".information_schema.tables LIMIT 5`
    succeeded and scanned 67 bytes. The rows were the tables of the
    `information_schema` of the catalog, `columns`, `tables`, `views`,
    `schemata` and `table_privileges`, with the type `varchar` (recorded:
    "information schema of the federated catalog: the results").
  - The calls of the Athena API for a catalog: `GetDataCatalog` answered
    `DataCatalog` with `Name`, `Description`, `Type` `LAMBDA` and `Parameters`
    (`catalog`, `metadata-function` and `record-function`, the last two the ARNs
    of the function). `ListDatabases` with `CatalogName` answered the one database,
    and `GetDatabase` answered `Database` with its `Name` only. `ListTableMetadata`
    answered `TableMetadataList` for the same 8 tables, with no `NextToken`,
    and `GetTableMetadata` answered `TableMetadata` for `all_log_streams`: the
    columns `time` of `bigint` and `message` of `varchar`, the partition key
    `log_stream` of `varchar`, and `TableType` `EXTERNAL`. `ListDataCatalogs`
    answered HTTP 400, `AccessDeniedException`, because the user lacks
    `athena:ListDataCatalogs` (recorded: "get the federated data catalog",
    "list the databases of the federated catalog", "get a database of the
    federated catalog", "list the tables of the federated catalog", "get a table
    of the federated catalog", "list the data catalogs").
  - A catalog that does not exist, `lambda:dbimp_nosuch`, answered like a missing
    right: the query was accepted at the start and then `FAILED` with
    `ErrorType` 9999 and `ErrorCategory` 3, "You are not authorized to perform:
    athena:GetDataCatalog on the resource". The answer is the same although the
    user now has the right for the catalog `dbimp-cw`. So a missing catalog
    cannot be told from a missing right (recorded: "a federated query on a
    catalog that does not exist: the execution").
- Data scanned is billed (see Principals). The recording scanned at most 41934
  bytes for a statement.

## Principals

- One login, an IAM user, and no ordinary user. The manifest of the third
  pass names `noOrdinaryUser`. The user ran queries in the workgroup `dbimp`,
  read and wrote the bucket, read the workgroups, and created and dropped tables in the Glue
  database `dbimp_test`.
- The user still had no right to start a query in the workgroup `primary`
  (recorded: "the primary workgroup"). The first pass found that the user had
  no right to call `ListDatabases`, `ListQueryExecutions`, `glue:GetDatabases`
  (which `INFORMATION_SCHEMA` needs), `glue:UpdateTable` (which an Iceberg
  `INSERT` needs) or `athena:CreatePreparedStatement`. The second pass added
  those rights, and all of those calls worked (recorded: "list the databases",
  "list executions", "information schema tables", "insert rows into the iceberg
  table", "create a prepared statement"). `ListWorkGroups` gave three
  workgroups, `dbimp`, `dbmeta` and `primary`, with the engine category `Presto`
  (recorded: "list the workgroups"). `ListDatabases` gave one database,
  `dbimp_test` (recorded: "list the databases"). `ListQueryExecutions` gave
  ids of five executions, a `NextToken` and no other member (recorded: "list
  executions").
- The user still lacked two rights in the fifth pass: `athena:ListDataCatalogs`
  and `glue:GetDatabase` on the database `dbimp_nosuch` (recorded: "list the
  data catalogs", "a table in a database that does not exist: the execution").
  `dbsetup` gave the user `athena:GetDataCatalog` and the right for the Lambda
  function, and the calls that use them worked (recorded: "get the federated
  data catalog", "a federated query with the catalog in the context").
- The version is not a query. `GetWorkGroup` gives
  `EngineVersion.EffectiveEngineVersion` of "Athena engine version 3" and
  `SelectedEngineVersion` of `AUTO` (recorded: "the engine version of the
  workgroup"). The user can read it. D181 gives a product that has such a
  source no `SELECT version()` of its own, and the driver sends no request for it
  (decided, D192 item 6).
- The statement that `usql` runs for the version is
  `SELECT node_version FROM system.runtime.nodes LIMIT 1`. The administrator,
  which is the only login of this work, gets HTTP 400 with the code
  `MALFORMED_QUERY` and the text "Queries of this type are not supported"
  (recorded: "the node version"). There is no ordinary user, so there is no
  second answer. The driver answers no version request, as D192 item 6 says, and
  the statement fails with that error. `SELECT version()` fails too, in the state
  `FAILED`, with the type 1303 `FUNCTION_NOT_FOUND` (recorded: "version
  function"). The test `TestIntegrationFeatures` runs both statements and
  expects each refusal.
- Cost, from the pricing page (fetched 2026-10-10): the page gives an example
  of `3 TB scanned is 3 * $5/TB = $15`. Its text of the 10 MB minimum names
  federated queries. It does not say whether DDL and cancelled queries are
  billed. It says that S3 charges include failed queries. Gemini and Qwen did not
  name the price. The cost of this recording was not measured. The workgroup
  stops a query at 100 MiB.

## Flavors

None. Athena is one service. The engine version is `AUTO`, which was version 3
(recorded: "the workgroup"). Athena engine version 2 and the Apache Spark
engine are not measured.

## Interfaces

`athena/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares, and the credentials that the driver signs each request with (D192). |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping sends GetWorkGroup, or ListWorkGroups when the DSN names no workgroup. Neither starts a query, so neither scans data or costs money. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because each query is its own request, so there is nothing to reset. |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, and the values that the driver writes as a literal of a type of their own: a decimal, a dbimp.Date, a dbimp.LocalTime, a dbimp.OffsetTime, a dbimp.LocalDateTime, a dbimp.Interval, a uuid.UUID, a netip.Addr, a list and a map (D192). |
| `driver.QueryerContext` | yes | The statement goes to StartQueryExecution with its arguments as ExecutionParameters, which the server binds to its ? (D192). |
| `driver.ExecerContext` | yes | Exec reads the result to its end with no decoding, and RowsAffected is UpdateCount, or an error that wraps dbimp.ErrNotSupported when the answer has none (D178). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. The driver does not use the prepared statements of Athena (D192 item 4). |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because the server refuses START TRANSACTION (D20 and D192). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, so a query has one result. The driver reads the pages of one result as one set of rows. |
| `driver.RowsColumnTypeScanType` | yes | ColumnInfo names the type of each column, and each type has one Go type (D135 and D192). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The type of the column in upper case, such as BIGINT. The server names a REAL float. |
| `driver.RowsColumnTypeLength` | yes | A char column has its length, and a varchar with no length, a string and a varbinary have the largest length. |
| `driver.RowsColumnTypeNullable` | yes | The member Nullable of ColumnInfo is always UNKNOWN, and every type can be NULL, so every column can. |
| `driver.RowsColumnTypePrecisionScale` | yes | A decimal column has its precision and its scale in ColumnInfo. |
<!-- /dbimp:interfaces -->

`Ping` sends `GetWorkGroup` for the workgroup of the DSN. When the DSN names
none, it sends `ListWorkGroups` with one entry. Neither call starts a query,
so neither scans data or costs money. A connection holds no state on the
server, because each query is its own request. The driver sends no request
to learn the version, as D192 item 6 says, and the server refuses the query
that `usql` runs for it.

## Faults

Faults of athenadriver, read from its source on 2026-10-10, which this driver
must not repeat:

- It finds the header row by comparing the first row with the names of the
  columns, and it drops the row when all the names match (athenadriver,
  `rows.go`). The header row is a fact of the statement type, not of the data.
- It gives an `int8`, an `int16`, an `int32` and a `float32` for the small
  types, and a `time.Time` for a date, a time and a timestamp (athenadriver,
  `rows.go`). D135 and D138 name other types.
- It has no case for the types `uuid` and `geometry`, and it fails the row
  with `unknown type` for them (athenadriver, `rows.go`). It returns the text
  of an array, a map and a row.
- It writes a Go `bool` as `1` or `0` and a `[]byte` as `_binary'...'`, which are
  the forms of MySQL, and it escapes a string with a backslash (athenadriver,
  `connection.go`).
- It sends no `ClientRequestToken` and so cannot repeat a start safely
  (athenadriver, `connection.go`).
- It checks the workgroup with `GetWorkGroup` before it starts a query, and it
  can create a workgroup (athenadriver, `connection.go`). Hard rule 2 and D7
  say that a package holds no configuration of that kind.
- It reads the query text for `pc:` commands and keeps the `Version` of `usql`
  as a query of Trino that the service refuses (recorded: "the node version").
- Its default `MaxResults` is the default of the service, so it reads 1000 rows
  at most for each call (athenadriver, `rows.go`).

Faults of the service:

- The message "Queries of this type are not supported" names no cause (see
  Transactions).
- A start that fails leaves no id, and a statement that is sent too early can
  name an id that does not exist.
- `GetQueryResults` of a query that did not finish is an error of the request
  and carries no reason. The reason is in `GetQueryExecution`.
- The form of the error differs: `Message` and `message` are two spellings.
- The type 1106 has an empty `AthenaError.ErrorMessage` and the text only in
  `StateChangeReason` (see Errors). The type 9999 has the text "Unknown error
  occurred." in `ErrorMessage`, and the cause only in `StateChangeReason`.
- A refused constraint in `CREATE EXTERNAL TABLE` gets the list of words of the
  Trino parser, which names no constraint (see Errors).
- The error of a query that the user has no right to run can name a different
  right in `ErrorMessage` and in `StateChangeReason` (recorded: "a table in a
  database that does not exist: the execution").
- The server ignores some members that it cannot use, and it tells the client
  nothing. A `ResultConfiguration.OutputLocation` that the workgroup overrides,
  and a `Catalog` that does not exist, did not fail the request or the query
  (recorded: "another result configuration", "a missing catalog").

## Second opinions

- Gemini (`gemini-3.1-pro-preview`, 2026-10-10) was asked the four questions of
  step 5a. It named `UPDATE`, `DELETE`, `MERGE` and `INSERT OVERWRITE` as Iceberg
  only, and `UNLOAD`, `MSCK REPAIR TABLE`, federated queries, partition
  projection, workgroups, result reuse, prepared statements and time travel as
  features of Athena. It said that there is no primary key, foreign key, index,
  unique constraint or default value. The fifth pass agrees: each was refused
  (recorded: "a primary key on a hive table", "a foreign key", "an index", "a
  unique column", "a default value"). The recording agrees for `UPDATE`,
  `DELETE` and `MERGE`, which a Hive table refused and an Iceberg table ran
  (recorded: "an update of the external table", "an update of the iceberg
  table"). It agrees that `UNLOAD`, `MSCK REPAIR TABLE`, partitions and prepared
  statements are statements of Athena (recorded: "unload", "msck repair table",
  "show partitions", "create a prepared statement"). It disagrees for `INSERT
  OVERWRITE`, which the parser refused on a Hive table, and the case of an
  Iceberg table was not tried (recorded: "insert overwrite"). Partition
  projection and Iceberg time travel by a timestamp ran (recorded: "a table
  with partition projection", "iceberg time travel by timestamp"). A federated
  query is not measured, because the user lacks a right (recorded: "a federated
  query: the execution"). It said that `varbinary` is hex with spaces, which is true (recorded:
  "the type of X'DEADBEEF'"). It said that `map` and `row` are JSON text, which
  is not true: `{a=1, b=2}` is not JSON (recorded: "the type of MAP(...)").
- Qwen (`qwen3-coder-plus`, 2026-10-10) was asked the same questions, shorter.
  It said that a `varbinary` is base64, that column names are always lower case
  and that timestamps are always UTC. Each is not true: the value is spaced hex,
  `Mixed` kept its case, and a timestamp with a zone kept its offset (recorded:
  "the type of X'DEADBEEF'", "a column in lower case", "the type of TIMESTAMP
  '2026-10-10 12:34:56.123 +07:00'"). It said that a default value is
  supported, which is not true: the server refused it (recorded: "a default
  value").
- Kimi K3, Qwen Max and DeepSeek Pro each timed out or closed the connection on
  every try, on 2026-10-10. They gave no answer.
- Gemini (2026-10-10) was asked what step 6 missed. Its leads and what the
  server said:
  - Stop a query in the state `QUEUED` or `RUNNING`, and expect `CANCELLED`.
    True for `RUNNING` (recorded: "stop the minute statement", "the minute
    statement after the stop"). A stop in the state `QUEUED` was not sent, but
    `QUEUED` was seen (recorded: "a long statement for the states: the
    execution").
  - Send the same token with the same query, and expect the same
    `QueryExecutionId`. True (recorded: "the same token and the same query 1",
    "the same token and the same query 2").
  - A NULL is a datum with no `VarCharValue`. True (recorded: "the type of
    CAST(NULL AS integer)").
  - A DDL statement and a query that finds nothing give a header row only. False
    for DDL, which gives no rows and no header, and true for a `SELECT` that
    finds nothing (recorded: "msck repair table", "a create table as select
    with no location", "columns of a result with no rows").
  - A syntax error succeeds at the start and fails at the poll. False. The
    server refused it at the start with HTTP 400 (recorded: "a syntax error").
  - Run `SELECT 1` as the check of a connection. True (recorded: "a select").
  - A column alias is in `ColumnInfo`. True (recorded: "a select").
  - A `?` with `ExecutionParameters`. True (recorded: "a parameter of the type
    integer").
  - Too few parameters fail the query. True (recorded: "too few parameters").
  - Set `QueryExecutionContext.Catalog`. The request accepts it (recorded: "a
    catalog in the context"). What it does for a table is not measured.
  - Set `ResultConfiguration.OutputLocation`. The request accepts it, and a
    workgroup that enforces its location ignores it (recorded: "another
    result configuration").
  - A local timeout must call `StopQueryExecution`. The call ends a running
    query, so a driver that calls it stops the work (recorded: "stop the minute
    statement").
  - A token of a page that is not valid is HTTP 400. True (recorded: "a wrong
    token of a page").
  - A runtime error fails at the poll. True (recorded: "a division by zero").
  - A query over the limit of bytes of the workgroup fails. Not measured. The
    limit is 100 MiB.
  - A query string of more than 256 KB fails at the start, with HTTP 400. True
    (recorded: "a long query text").
- Qwen (2026-10-10) was asked what step 6 missed. It named throttling with
  backoff, the states `QUEUED` and `CANCELLED`, NULL against an empty string,
  result reuse, `UNLOAD`, `CTAS`, views, time travel, `EXECUTE ... USING` and
  caching. It gave no request to send, so each is a topic and not a lead. The
  second pass settled NULL against an empty string (they differ), `CTAS` (works
  with no location), views (work) and `EXECUTE ... USING` (works). The
  fifth pass settled `CANCELLED` (seen) and time travel (works by a timestamp).
  The fifth pass settled result reuse (a reuse was seen for a query over a
  table) and `UNLOAD` (it succeeded with a new target). Throttling and `QUEUED`
  are not measured.
- Gemini (2026-10-10) reviewed the type mapping against TYPES.md. It said that
  `IPADDRESS` must be `netip.Addr`, which the table now says (D177). It said
  that `varbinary` needs a decoder for the spaces, that `json` must be decoded,
  that `NaN`, `Infinity` and `-Infinity` must be read from their text, and that
  the interval texts need a parser. It recommended the kind `other`, the server
  text, for `array`, `map` and `row`, because the text is ambiguous and
  `ColumnInfo` names no type of the elements. It said that a geometry as the
  text is fine.
- Qwen (2026-10-10) reviewed the same mapping. It said that a timestamp with no
  zone must be a `time.Time`, which is false by D138. It said that `array` is
  JSON, which is false for strings, because the text has no quotes (the
  second pass confirmed it: an `array<string>` gave `[p, q]`, recorded:
  "select typed rows"). The fifth pass showed that the text has no quotes and no
  escape even for an element with a comma or a quote (recorded: "commas and
  quotes in an array, a map and a row: the results"). It agreed on `other` for `map` and `row`. It wanted a `net.IP`, and the kind of
  D177 is `netip.Addr`.
- The Go clients were read on 2026-10-10: athenadriver v1.1.15 and the SDK
  v1.55.8 of AWS for Go. The v2 SDK for Athena is not in the cache. A client in
  another language was not read, so step 5a is not complete for a second client.
  The JDBC driver of Athena is not in the cache.

## Open questions

Ken decided the questions of step 9 on 2026-10-10, in D192. These earlier
questions are answered, each as "decided, D192":

1. The account and CI (R). Decided, D192 item 2: the integration tests run on a
   hosted account only where a person supplies it, as for Snowflake (D182), and
   they skip when the DSN variable is empty.
2. The version. Decided, D192 item 6: the driver gives no answer to a version
   request, as D181 says.
3. `ARRAY`, `MAP` and `ROW`. Decided, D192 item 3: the value is the text of the
   server, in a `string`, of the kind `other`, and the driver does not parse it.
4. The intervals, `IPADDRESS`, `GEOMETRY` and `TIME WITH TIME ZONE`. Decided,
   D192 item 10: the proposals stand as the type table writes them.
5. Zone names. Decided, D192 item 7: a `TIMESTAMP WITH TIME ZONE` with a named
   zone is a `time.Time` in that zone, from `time.LoadLocation`, and the driver
   imports `time/tzdata`.
6. The header row. Decided, D192 item 10: the driver skips the header row by the
   statement type.
7. The literals for `ExecutionParameters`. Decided, D192 item 4: the driver
   binds with `ExecutionParameters`, keeps each `?` in the text, writes each
   value as an escaped SQL literal, and does not use a prepared statement. So
   the parser of the root package (D34) is not needed.
8. Polling. Decided, D192 item 8: the driver polls `GetQueryExecution` from
   100 ms and backs off to 1 s.
9. `ClientRequestToken`. Decided, D192 item 10: the driver sends a new token for
   each statement.
10. Cancel. Decided, D192 items 5, 8 and 10: the driver calls
    `StopQueryExecution` when the context ends, a `CANCELLED` query is an error
    that names the state and its reason, also when the driver did not stop it,
    and the rows of a query store the context (hard rule 4).
11. The federated pass. Decided, D192 item 9, and done: the fifth pass ran with
    the catalog and the Lambda connector of `dbsetup`.

D192 leaves these open. Ken decides:

1. The rules for the escaped literal of D192 item 4. The package doubles each
   quote in a string and keeps a backslash as it is. It writes a time with the
   fewest digits of fraction, and it sends a value finer than a millisecond as it
   is, so the server decides (`athena/params.go`). The rules of Trino are not
   measured, so Ken has not decided them.
2. Billing. A driver that opens a result of a large table can scan terabytes.
   Whether the driver sets a limit, or leaves it to the workgroup, is not
   measured.
3. Data in S3 for the tests. `DROP TABLE` of an external table leaves its files,
   and `UNLOAD` refuses a directory that exists. The tests use a new prefix in S3
   for each run, under `dbimp-it/<run>/`, but a clean up of S3 is not Athena, and
   the driver cannot do it. Whether a tool removes the old prefixes is open.
4. A repeat of a start on a broken connection, with the same token, is not
   measured.
5. The long argument rule. The service refuses an `ExecutionParameters` member of
   more than 1024 characters (live run of 2026-10-10). When one argument is longer
   than 1024 bytes, the driver writes all arguments into the text of the
   statement (see Parameters). D192 item 4 says that the driver binds with
   `ExecutionParameters`, so this rule is a choice of the driver that Ken has not
   decided. The reason is that a caller can hit the limit with one long string.
6. A row with fewer values than the columns, such as a row of `DESCRIBE`, has NULL
   for the values that it lacks. Ken has not decided this.
7. `WithTimeout` fails with `dbimp.ErrNotSupported`, because only a workgroup sets
   a timeout on the server. The context bounds the wait. Ken has not decided this.
8. The tests name the federated catalog in the variable
   `ATHENA_FEDERATED_CATALOG`, and the database `/aws/lambda/<catalog>`.

Decided, D192 item 12: the region is the label after `athena` or `athena-fips` in
the host, a DSN with no key and no secret makes `Connect` fail with
`ErrNoCredentials`, `GetQueryResults` runs for every statement, DDL too, and the
driver sends `StopQueryExecution` after a failed poll.

### The leads of the fifth live pass

The second pass ended with 12 leads for a third pass. The later passes answered
them as follows, in the order of the old list.

1. A query that runs for a minute, stopped after a few seconds. Settled. The
   state was `CANCELLED`, with "Query cancelled by user" (recorded: "stop the
   minute statement", "the minute statement after the stop"). The query was
   `RUNNING` in the three polls before the stop.
2. The state `QUEUED`. Settled. The first poll of the long statement answered
   `QUEUED` (recorded: "a long statement for the states: the execution"). A stop
   in that state was not sent.
3. A reuse with `ReusedPreviousResult` true. Settled. A query over a table, run
   twice, reported false and then true (recorded: "a result reuse: the
   execution", "a result reuse again: the execution").
4. A successful `UNLOAD` with a new directory. Settled. It succeeded, with
   `UpdateCount` 1 (recorded: "unload", "unload: the execution", "unload: the
   results").
5. `GetQueryRuntimeStatistics` and `BatchGetQueryExecution` with a real id.
   Settled for the id of a `SELECT` (recorded: "the statistics of an
   execution", "batch get"). The id of a DDL statement was not sent.
6. A `Catalog` that does not exist, with a table name. Not settled. A federated
   name that does not exist answered like a missing right (recorded: "a
   federated query on a catalog that does not exist: the execution").
7. The text of an array, a map and a row for a string that holds a comma or a
   quote. Settled. The text has no escape (recorded: "commas and quotes in an
   array, a map and a row: the results"). A space, a brace and a NULL element
   were not sent.
8. A `time with time zone` and a `timestamp with time zone` with a fraction and at
   offset zero. Not settled. No request was sent.
9. The smallest value of each integer type, `1E-10` and a decimal of 38 digits.
   Not settled. No request was sent.
10. A parameter in `INSERT` and in DDL, and the most parameters. Not settled. No
    request was sent.
11. The temporary credential, the burst, the limit of 100 MiB, and the timeouts
    of a query. Not settled. The login and the budget stay the same.
12. A query string of 262144 bytes of two byte characters. Not settled. No
    request was sent.

The other leads that the later passes answered, each with the request:

- A primary key, a unique column, a foreign key, a default value and a `NOT
  NULL` column on a Hive external table: each was refused at the start, with
  the same `MALFORMED_QUERY` text (recorded: "a primary key on a hive table",
  "a unique column", "a foreign key", "a default value", "a not null column").
  A primary key on an Iceberg table was refused too (recorded: "a primary key
  on an iceberg table").
- An index: `CREATE INDEX` was refused with "Queries of this type are not
  supported" (recorded: "an index").
- Bucketing: `CLUSTERED BY ... INTO 4 BUCKETS` ran (recorded: "a bucketed
  table").
- Partition projection: the table with the properties ran (recorded: "a table
  with partition projection").
- A federated query: it runs with the catalog in the context, and the calls for
  the catalog work except `ListDataCatalogs` (recorded: "a federated query with
  the catalog in the context: the results", "get the federated data catalog").
- Iceberg time travel by a timestamp ran. By the version `1` it failed on the
  type of the version. The `$history` table ran (recorded: "iceberg time travel
  by timestamp", "iceberg time travel by version", "the history of an iceberg
  table").
- A table in a database that does not exist: failed on the right `glue:GetDatabase`
  (recorded: "a table in a database that does not exist: the execution").

The leads that no pass has settled, each with a new request:

1. `GetQueryRuntimeStatistics` and `BatchGetQueryExecution` with the id of a DDL
   statement.
2. The `UNLOAD` of many rows, and the files that it writes.
3. A `Catalog` that does not exist, with a table name in the statement.
4. The text of an array, a map and a row for a string that holds a space, a brace
   or a NULL element, and for a nested container.
5. A `time with time zone` and a `timestamp with time zone` with a fraction and
   at offset zero, and a column of each type in a table.
6. The smallest value of each integer type, `1E-10` and a decimal of 38 digits.
7. A parameter in `INSERT` and in DDL, and the most parameters.
8. The temporary credential, the burst, the limit of 100 MiB, and the
   timeouts of a query.
9. A query string of 262144 bytes of two byte characters, to learn whether the
   server counts bytes or characters.
10. `FOR VERSION AS OF` with a `bigint` snapshot id, and the rows of the `$history`
    table and of the time travel queries.
11. An `INSERT` and a `SELECT` on the bucketed table and on the table with
    partition projection.
12. `NOT NULL`, `UNIQUE`, `DEFAULT` and a foreign key on an Iceberg table.
13. A stop of a query in the state `QUEUED`.
14. The final state of the first federated query, which was still `RUNNING` at its
    only poll, `SHOW SCHEMAS` in the federated catalog, and the rows of
    `all_log_streams`.

## Integration tests

The integration tests of the driver read `ATHENA_DSN` and skip when it is empty
(hard rule 9). The variable holds the DSN of D192 item 10, with the access key
as the user and the secret key as the password:

    ATHENA_DSN='athena://KEY:SECRET@athena.us-east-1.amazonaws.com/DATABASE?workgroup=WG&output=s3://BUCKET/results/'

The key `token` holds the session token of a temporary credential. The variable
`ATHENA_FEDERATED_CATALOG` is optional. It names a data catalog of the type
`LAMBDA`, whose database is `/aws/lambda/<catalog>`, and the test of the
federated query skips when it is empty. `dbrun` does not start Athena, and the
workflow has no job for it and no secret (D192 item 2). A person runs the tests
on an account with the login that `dbsetup` made:

    go test -race -count=1 -run Integration -v ./athena/...

The login is one IAM user, so each test runs as that user only, and the manifest
has no ordinary user. The user needs these rights:

- `athena:StartQueryExecution`, `GetQueryExecution`, `GetQueryResults`,
  `StopQueryExecution`, `BatchGetQueryExecution`, `ListQueryExecutions`,
  `GetWorkGroup`, `ListWorkGroups`, `CreatePreparedStatement`,
  `GetPreparedStatement` and `DeletePreparedStatement`, in the workgroup.
- The rights of Glue to make, change and drop tables and partitions in the
  database of the DSN, and `glue:UpdateTable` for an Iceberg table.
- The rights of S3 to read, write and list the bucket of the output location, and
  `s3:DeleteObject` for the Iceberg tables.
- For the federated query only: `athena:GetDataCatalog` and the right to invoke
  the Lambda function of the catalog.

The tests make their tables in the database of the DSN, with the name of the run
for a prefix (`dbimp_it_` and eight characters), and their data in the bucket of
the output location, under `dbimp-it/<run>/`. `TestMain` looks for a table or a
view of the run that a test left, drops it and fails. `DROP TABLE` of an external
table leaves its files in S3, and the driver cannot delete them, because S3 is
not Athena. So each run has its own prefix, and a person removes `dbimp-it/` from
the bucket from time to time.

The tests were written on 2026-10-10 with no account, and they have not run. The
tests and what each one holds:

- `TestIntegrationPing`, `TestIntegrationWrongSecret` and
  `TestIntegrationSelect`: the call of the ping, the error of a wrong secret key,
  which holds neither key, and a `SELECT` with its header row.
- `TestIntegrationContextDeadline`: a deadline that ends while a query runs. The
  driver stops the query, and the test reads its state with
  `BatchGetQueryExecution` until it is `CANCELLED`, within a limit of time.
- `TestIntegrationCRUD`, `TestIntegrationSchema` and `TestIntegrationFeatures`:
  the entries of `features.json`, with one subtest for each entry. The entries
  that the survey marks no send the operation and expect the refusal of the
  server.
- `TestIntegrationRoundTrip`: every type that `features.json` marks yes, with
  `dbimptest.RoundTrip`, as a bound argument and as a literal, and the two types
  that it marks no.

A table of Athena holds few types, and only an Iceberg table takes `UPDATE` and
`DELETE`. So each type has one of two homes. A type that Iceberg holds is a column
of that type in an Iceberg table. A type that only Hive holds, such as `TINYINT`,
`CHAR`, `ARRAY`, `MAP` and `ROW`, is a column in a Hive table of Parquet files,
which takes `INSERT` and `SELECT` only, so its round trip skips the update and
the delete. A type that no table holds, which is `TIME`, `TIME WITH TIME ZONE`,
`TIMESTAMP WITH TIME ZONE`, `JSON`, `IPADDRESS`, `UUID`, both intervals and
`GEOMETRY`, is a column of text. The statement writes the cast of the value, and
the select casts it back, so the column that the driver reads has the type. The
type `UNKNOWN` is a bare `NULL` that a select reads from a row that holds `NULL`.
The type `STRING` is the type of the columns of `DESCRIBE` and `SHOW`, and a
Hive column of the type `string` is a `varchar` in a select, so its round trip
reads a `varchar`, and `TestIntegrationFeatures` holds the type `STRING` in the
result of `DESCRIBE`. The two entries that are no, `BINARY` and `STRUCT`, read a
Hive column of each type and expect the wire types `VARBINARY` and `ROW`.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was
written on 2026-10-10 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of Athena from the sections above.

### The server

| | Couchbase | Athena |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /`, with the operation in `X-Amz-Target`. A statement is `StartQueryExecution`, then `GetQueryExecution` until the state ends, then `GetQueryResults` (Requests) |
| Database | The key `query_context` of the body | `QueryExecutionContext.Database`, and `Catalog` for a data catalog (Requests) |
| Language | SQL++, which is close to SQL | The SQL of Trino, and Hive DDL. One statement for each request (Statements) |
| DDL | In SQL++ | Hive DDL, and Iceberg tables with `table_type`. A key, a unique column, a default value and an index are refused (Statements) |
| Parameters | `?`, `$1` and `$name` | `?`, with `ExecutionParameters` as a list of strings, each the text of an SQL expression (Parameters) |
| Framing | One body for the whole result, which does not page | One JSON object for each page of at most 1000 rows. A page after the first needs `NextToken` and is another request (Responses) |
| Columns | `signature`, before the first row | `ColumnInfos` of the first page, before the first row, with the type, the precision and the scale of each column. The first row of a `SELECT` is a header row (Responses) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement (Responses) |
| Errors | Can come with HTTP 200, after some rows | A request that the server refuses is HTTP 400. A query that fails is HTTP 200 and the state `FAILED` of the poll, so an error never comes after a row (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, and every value is a string or null, with the type name of the column. A container is text that is not JSON (Types) |
| Cancel | The server stops a query when the client leaves | A query runs on when the client leaves, and `StopQueryExecution` stops it (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None. `START TRANSACTION` is refused (Transactions) |
| Authentication | Basic, or `creds` in the body | AWS Signature Version 4, with an access key and a secret key, and a session token for a temporary credential (Requests) |
| Default port | 8093, or 18093 with TLS | 443, with TLS always |

The differences that a caller sees:

- A statement is three requests or more, and it takes at least a few hundred
  milliseconds, where Couchbase sends one. The driver polls from 100 ms and backs
  off to one second (D192 item 8).
- The result is in pages of 1000 rows, and the driver reads each page one row at
  a time and the next page only when the caller asks for it. An error cannot come
  after a row, but a request for a page can fail, and the error then wraps
  `dbimp.ErrIncomplete` (D192 item 5).
- The first row of a `SELECT` is a header row, and the driver drops it by the
  type of the statement (D192 item 10).
- The server runs a query on when the client leaves, so the driver stops the
  query when the context ends (D192 item 10).
- An `ARRAY`, a `MAP` and a `ROW` are the text of the server in a `string`, and the
  driver does not parse it (D192 item 3).
- A transaction has no form (D192 item 10).

### The driver

| | `couchbase` | `athena` |
| --- | --- | --- |
| Size, without tests, on 2026-10-10 | About 1300 lines in 8 files | About 2200 lines in 9 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `Region`, `User`, `Password` (the secret key), `Token`, `Database`, `WorkGroup`, `Output` and `Catalog`. The DSN has the keys `workgroup`, `output`, `token` and `catalog`, and the region comes from the host (D192) |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, `WithWorkGroup`, `WithOutput` and `WithCatalog`, through `WithOptions` or an argument (D109). `WithParameter` sets any key of `StartQueryExecution`. `WithTimeout` and `WithReadonly(true)` fail with `dbimp.ErrNotSupported` |
| Arguments | Sent to the server as `args` and `$name` | Written as SQL literals in `ExecutionParameters`, from the Go type of each argument, and the server binds each to a `?`. A named argument fails with `dbimp.ErrArguments` (`athena/params.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | A reader of its own, which reads one page token by token, then the next page (`athena/rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | The same, and `ColumnTypeLength` for a char, a varchar, a string and a varbinary, and `ColumnTypePrecisionScale` for a decimal |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column, as the type table says: `int64`, `float64`, `*apd.Decimal`, `string`, `bool`, `[]byte`, `dbimp.Date`, `dbimp.LocalTime`, `dbimp.OffsetTime`, `dbimp.LocalDateTime`, `time.Time`, `dbimp.Interval`, `netip.Addr`, `uuid.UUID` and the decoded JSON value (D135 and D192) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from `UpdateCount`, or `dbimp.ErrNotSupported` when the answer has none. `LastInsertId` always gives `dbimp.ErrNotSupported` (D178 item 14) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D192 item 10) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The poll stops when the context ends, and the driver sends `StopQueryExecution` with a limit of 5 seconds. The rows keep the context for the pages after the first (D192 items 5 and 8, and item 12 for the stop after a failed poll) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Type, Code, State, ErrorType, Category, Message, QueryID}`, which unwraps to `*dbimp.StatusError` for a request, and the sentinels `ErrCanceled` and `ErrNoCredentials` |
| Authentication | Basic | AWS Signature Version 4 from `dbimp.SignV4`, with the service `athena`. The driver follows no redirect, so the signature goes to the host of the DSN only, and it reads no credential from the environment (D7 and D192) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, the two sentinels, `Config`, `ParseDSN` and `NewConnector` |

The differences that a caller sees:

- A value keeps its type, a date, a time, a decimal, an interval, an address and a
  UUID too, where Couchbase gives JSON shapes (D135 and D192).
- `RowsAffected` gives an error for a statement with no `UpdateCount`, such as
  `DESCRIBE`, and the count for a statement that changes rows (D178 item 14).
- A row of `DESCRIBE` has one value, in the first column, and NULL in the others,
  because the server sends one value for three columns (Responses).
- `WithParameter` replaces a key of `StartQueryExecution`, as in Couchbase.
- `WithTimeout` fails with `dbimp.ErrNotSupported`. No decision explains it, and
  it is open question 7.
- An argument of more than 1024 bytes moves every argument into the text of the
  statement. No decision explains it, and it is open question 5.
- `GetQueryResults` runs for every statement, DDL too (decided, D192 item 12).
- A row of `DESCRIBE` has NULL for the values that it lacks. No decision explains
  it, and it is open question 6.
- A DSN needs a host with the label `athena` and then the region. A DSN with no
  access key opens no connection, and the error is `ErrNoCredentials` (decided,
  D192 item 12).
- A `TIMESTAMP WITH TIME ZONE` with a named zone is in that zone, from the zone
  database that the package `time/tzdata` holds (D192 item 7).
