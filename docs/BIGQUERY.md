# Google BigQuery

This file holds what is known about Google BigQuery over its REST API, for a
possible driver `bigquery` (W35 and D184). The headings are the template of
[DRIVER.md](DRIVER.md). A fact is "recorded", with the name of its request in
quotes, "measured", with the date, or "not measured", with its source.

Step 6 recorded the same script three times on 2026-10-10: on two releases of
the community emulator, and on the real service that Google hosts. The
recordings are under `testdata/bigquery/`, and `testdata/bigquery/requests.json`
is the script. The script in the repository is the hosted variant.

- Hosted service. The release name in the manifest is `bigquery`, and the files
  are `bigquery-NNN`. The recorder sent 452 requests in 5 minutes and 25 seconds,
  as one login: a service account that has the scope
  `https://www.googleapis.com/auth/bigquery`. The project id is replaced by
  `dbimp-project` in every file, and the dataset is `dbimp_test`, in the
  multi-region `US`. The account can run queries, and it can create tables, views,
  routines, clones, datasets (`CREATE SCHEMA` and `datasets.insert`), and it can
  drop a dataset that it made with `DROP SCHEMA ... CASCADE` (recorded:
  bigquery-226, bigquery-359, bigquery-394, bigquery-397, bigquery-398 and
  bigquery-446). It reads `INFORMATION_SCHEMA.JOBS_BY_PROJECT` (recorded:
  bigquery-399) and it writes and reads the bucket `gs://dbimp-project-scratch`
  (recorded: bigquery-405 and bigquery-407). It cannot read
  `INFORMATION_SCHEMA.SCHEMATA` and `JOBS` (recorded: bigquery-059, bigquery-064
  and bigquery-396), and it cannot delete a snapshot (recorded: bigquery-450). The
  service account was given the rights to create datasets, to read all jobs and to
  write the bucket before the latest pass. A table that a request made expires
  after seven days (recorded: bigquery-062, where `TABLE_OPTIONS` shows
  `expiration_timestamp`).
- Emulator. The release names are `bigquery-0.7.2` and `bigquery-0.8.1`, and the
  files are `bigquery-0.7.2-NNN` and `bigquery-0.8.1-NNN`. The emulator checks no
  credential, so it is one login. The emulator makes the project `dbmeta` and the
  dataset `dbmeta`. The emulator needs a new container for each run, because a
  job id and a dataset that an earlier run made stay in it. The recordings of the
  emulator hold the script of `dbmeta` and not the script of the repository.
- A citation with a number, such as bigquery-076, names a hosted file, and the
  number is the number of the request in the run. A citation with only the name
  of a request, such as "a statement", names the emulator recordings of both
  releases, and the text says "emulator" where a reader can doubt it. Where the
  hosted service answered the same request, the text gives the hosted file too.
- The other sources are the REST reference of the API v2 on
  `docs.cloud.google.com`, the document of the service account flow of Google,
  and the discovery document. The hosted service gave its own discovery
  document at the revision `20260922` (recorded: bigquery-236, "the discovery
  document"). The emulator serves the revision `20250928`.

The files are of the latest hosted pass. An earlier pass wrote the project id
`dbimp-project` with a hyphen and no backticks, which GoogleSQL refuses in many
statements, and its slow queries ran from the cache. The latest pass writes the
name in backticks, runs heavy queries with the cache off, sends a request with no
token and requests with a token that is wrong, and has the widened rights. Four
facts of the latest pass matter when a reader compares the numbers:

- The dataset was clean at the start, so the pass records the creation of a
  clone, a function, a table function and a procedure (recorded: bigquery-299,
  bigquery-301, bigquery-303 and bigquery-304). Two snapshots of earlier passes
  stay: `CREATE SNAPSHOT TABLE` answered HTTP 409 `duplicate` (recorded:
  bigquery-300), and `INFORMATION_SCHEMA` lists them as `SNAPSHOT` (recorded:
  bigquery-060). So the creation of a snapshot is not recorded in this pass.
- The first request for a wildcard table has a second backtick by mistake and
  answered `Invalid empty identifier` (recorded: bigquery-315). A later request
  for two like tables is right (recorded: bigquery-393).
- The teardown drops what it can. It cannot drop the snapshots `dbimp_it_sn` and
  `dbimp_it_sn_r2`, because the account lacks `bigquery.tables.deleteSnapshot`
  (recorded: bigquery-438 and bigquery-450). It never drops the dataset
  `dbimp_it_ds2` that `datasets.insert` made, and it never drops the routines
  `dbimp_it_tf` and `dbimp_it_proc`. These objects stay, and a person with the
  right must drop the snapshots.
- The table `dbimp_it_kv` comes from the request with a foreign key (recorded:
  bigquery-289) and has only the column `id`. So the request that made the same
  table with `tables.insert` answered HTTP 409, and the row with a column `s`
  that was streamed into it answered `no such field: s` (recorded: bigquery-360
  and bigquery-361). The later streaming insert uses a table of its own
  (recorded: bigquery-417 and bigquery-418).

## Summary

- Product: Google BigQuery, a hosted data warehouse. Name in `dbrun`:
  `bigquery-0.7.2` and `bigquery-0.8.1`, which are the emulator, and nothing for
  the service. The kind is `container` and the tier is `staged` (read of
  `dbmeta/container/bigquery.go`, 2026-10-10). The image is
  `ghcr.io/goccy/bigquery-emulator`, a community project under the MIT licence
  that Google does not maintain. `dbrun` starts it with the project `dbmeta` and
  the dataset `dbmeta`. The user in the URL of `dbrun` is `admin`, and nothing
  checks it. `dbmeta` has no model that reads it, so no CI job runs it yet.
- R: the emulator meets R and the service does not. `dbrun` starts both
  releases, and a driver can run against them with no credential. The service
  needs a Google Cloud project, a service account with a key, and a dataset that
  the account can write (D184). CI cannot test the service without a secret.
  This is the one condition of R that the product does not meet. The emulator
  answers many requests in another way than the service, as the section Hosted
  and emulator shows, so a test on the emulator cannot stand for the service.
- H: `POST /bigquery/v2/projects/{project}/queries` is HTTPS and JSON, and the
  service answered every statement that the account can run (recorded:
  bigquery-017, "a statement"). It needs no binary encoding. A result is JSON
  text. A result of 3000 rows arrived whole in one answer of 364,592 bytes
  (recorded: bigquery-164, "a result of 3000 rows with no maxResults"). With
  `maxResults` of 500 the service cut the first page at 500 rows and sent a
  `pageToken`, and `jobs.getQueryResults` read the next pages (recorded:
  bigquery-163 to bigquery-171, "a result of 3000 rows with maxResults of 500"
  and "page 2 of the job"). The Arrow, the Storage Read API and gRPC are not
  needed to read a result.
- S: GoogleSQL. The service answered `SELECT`, `INSERT`, `UPDATE`, `DELETE`,
  `MERGE`, `TRUNCATE TABLE`, `CREATE TABLE`, `ALTER TABLE` and scripts
  (recorded: bigquery-032, bigquery-025, bigquery-026, bigquery-028,
  bigquery-366, bigquery-030, bigquery-024, bigquery-308 and bigquery-238). The
  service has a legacy dialect, and the default of `useLegacySql` is `true`
  (recorded: bigquery-236, "the discovery document"). So a driver must send
  `"useLegacySql":false` in every request. Whether the service reads a request
  with no `useLegacySql` as legacy SQL is not shown: the request that sends no
  member answered as it would for either dialect (recorded: bigquery-022, "a
  request with no useLegacySql").
- Whether it can be a driver: yes, over HTTPS and JSON, with no Arrow. No
  condition of "When it cannot be a driver" failed on the service. The columns
  come in `schema.fields` of the same JSON object as the rows, and the member
  `schema` comes before `rows` (recorded: bigquery-017 and bigquery-163, where
  the order of the members is `kind`, `schema`, `jobReference`, `totalRows`,
  `pageToken` when there is one, `rows`, and then the totals and the times). A
  result has no cut: `getQueryResults` reads every page by `pageToken`, and the
  last page has no `pageToken` (recorded: bigquery-171, "page 6 of the job"). An
  error of a query comes before any row, because the service runs the whole
  statement before it answers (recorded: bigquery-187, "a division by zero in a
  row after the first"). The facts that make it costly are the login (a signed
  JWT and a token exchange), the form of a `TIMESTAMP` that loses precision by
  default, and the rule that a script answers only the result of its last
  statement.
- What the hosted service showed that the emulator did not, in one line each
  (the section Hosted and emulator holds the evidence):
  - It answers `numDmlAffectedRows` and `dmlStats` after a DML statement.
  - It answers `jobComplete: false` for a query that runs longer than
    `timeoutMs`, and `getQueryResults` answers `jobComplete: false` until the job
    ends.
  - It uses real reasons: `invalidQuery`, `notFound`, `accessDenied`, `duplicate`,
    `invalid`, `required`, `parseError`, `badRequest`, `forbidden`,
    `bytesBilledLimitExceeded` and `stopped`. It never answered `jobInternalError`.
  - It has sessions that carry a transaction across requests, and it rolls back
    in a script and in a session.
  - It cancels a running job, and it deduplicates an `INSERT` that repeats a
    `requestId`.
  - It cuts a page at `maxResults` and gives a `pageToken`.
  - It writes `NaN`, `Infinity` and `-Infinity` as text, a `TIMESTAMP` as a
    double with an exponent, and refuses a NULL element in an array.
  - It has time travel, a materialized view, a clone, a snapshot, routines, a
    foreign key, a wildcard table, datasets that a statement creates and drops,
    a search index, an external table over a bucket, `EXPORT DATA` and `LOAD
    DATA`.
  - It answers HTTP 401 for a request with no token and for a token that is wrong.
- Scheme in `dburl`: `Name` is `bigquery`, the alias is `bq`, the generator is
  `GenSchemeHost("bigquery")`, and the `Dialect` is `bigquery`. `GoPackage` is
  `github.com/xo/dbimp/bigquery`. The generator rewrites the scheme and
  returns an error when the URL names no host, because the host is the project
  (read of `dburl/scheme.go` and `dburl/dsn.go`, 2026-10-10).
- Driver of `usql` now: `gorm.io/driver/bigquery` v1.2.1, which uses
  `cloud.google.com/go/bigquery` v1.83.0. The driver registers no hook, so
  `usql` has no `Version` statement for it (read of
  `usql/drivers/bigquery/bigquery.go` and `usql/docs/BACKLOG.md` W50,
  2026-10-10). `usql` lists the cost of the driver as 4.2, with the Google Cloud
  SDK and Arrow v15 as the cause (read of `usql/docs/BACKLOG.md`,
  2026-10-10).
- What `dbmeta` has: the container entry only, with the releases 0.7.2 and
  0.8.1. It has no model (`dbmeta` D194, as `usql/docs/BACKLOG.md` W50 reports).

## Hosted and emulator

The hosted service differs from the emulator in many places. Each fact below is
recorded on the hosted service, with the number of its file. The line that
follows it says what the emulator did. A claim of an earlier version of this
file that the hosted service contradicts is corrected in the section that holds
it, and the emulator fact stays there with the word "emulator".

- Polling. A query that runs longer than `timeoutMs` answered HTTP 200 with
  `jobComplete: false`. The answer has `kind`, `jobReference` (with `jobId` and
  `location`), `jobComplete`, `queryId`, `jobCreationReason`, `location`,
  `creationTime`, `startTime` and `statementType`, and it has no `schema`, no
  `totalRows` and no `rows` (recorded: bigquery-215 with `timeoutMs` of 1, and
  bigquery-216 with `timeoutMs` of 100 and `maxResults` of 1). The query was a
  count over `GENERATE_ARRAY(1, 1000000)` crossed with `GENERATE_ARRAY(1, 30000)`
  with `useQueryCache` of `false`. `getQueryResults` of that job answered HTTP 200
  and `jobComplete: false`, with `kind`, `etag`, `jobReference` and `jobComplete`
  only (recorded: bigquery-217). The run polled once. How long such a job runs
  and the poll that ends with `jobComplete: true` are not recorded, and the jobs
  of bigquery-215 and bigquery-216 were not cancelled. A request with `timeoutMs`
  of 1 and a query that the cache answers still returned `jobComplete: true`
  (recorded: bigquery-205). Emulator: `jobComplete` was always `true`.
- Page caps. With `maxResults` of 500 in `jobs.query` the service sent 500 rows,
  `totalRows` of `"3000"` and a `pageToken` (recorded: bigquery-163). Without
  `maxResults` it sent all 3000 rows in one answer (recorded: bigquery-164). With
  `maxResults` of 0 it sent a `pageToken` and no rows (recorded: bigquery-165).
  The page token is an opaque text of about 600 characters. The default page
  cap of 10 MB is not reached by 3000 short rows, so it is not measured. Emulator:
  it ignores `maxResults` of `jobs.query`.
- Job reads of a `jobs.query` job. `jobs.get` and `getQueryResults` accept the
  job id of a `jobs.query` answer, and `getQueryResults` pages it with `pageToken`
  (recorded: bigquery-034, bigquery-035 and bigquery-166 to bigquery-171).
  `startIndex` also works (recorded: bigquery-172) and gives no rows past the end
  (recorded: bigquery-174). A page token that is not valid answers HTTP 400 with
  the reason `invalid` and the message `Invalid paging token:abc` (recorded:
  bigquery-173). A job in the wrong location answers HTTP 404 `notFound` (recorded:
  bigquery-351, "the results of a job with a location that is wrong"). Emulator
  0.7.2: the job is not known. Emulator 0.8.1: it is known, and the page token is
  an offset.
- DML counts. A DML answer has `numDmlAffectedRows` as a string and `dmlStats`
  with `insertedRowCount`, `updatedRowCount` or `deletedRowCount`, and
  `dmlMode` (recorded: bigquery-025, bigquery-026, bigquery-028, bigquery-162 with
  `"3000"`, and bigquery-366). Emulator: it sends neither.
- DML schema. A statement with no rows, such as `CREATE TABLE`, `INSERT` and
  `UPDATE`, answers the schema of the table that it touched, with no `mode`, and
  no `rows` and no `totalRows` (recorded: bigquery-024 and bigquery-025). `DROP
  TABLE` and `ALTER TABLE` answer no schema (recorded: bigquery-050 and
  bigquery-308). Emulator: `"schema":{}`, `"rows":[]` and `"totalRows":"0"`.
  The member `statementType` names the kind of statement: `SELECT`, `INSERT`,
  `UPDATE`, `DELETE`, `MERGE`, `CREATE_TABLE`, `CREATE_TABLE_AS_SELECT`,
  `DROP_TABLE`, `ALTER_TABLE`, `CREATE_VIEW`, `CREATE_MATERIALIZED_VIEW`,
  `DROP_VIEW`, `DROP_MATERIALIZED_VIEW`, `DROP_FUNCTION`, `BEGIN_TRANSACTION`,
  `ROLLBACK_TRANSACTION` and `SCRIPT`.
- Error reasons. The service uses `invalidQuery` with the location `q` for an
  error in the text of a query (recorded: bigquery-181 and bigquery-186), and
  `notFound` for a table (recorded: bigquery-182). A query on a dataset that does
  not exist answers HTTP 403 `accessDenied`, because the account cannot tell
  that it does not exist (recorded: bigquery-183). The service never answered
  `jobInternalError`. The Errors section holds the list. Emulator: HTTP 400 and
  `jobInternalError` for every error of a query.
- Sessions. `createSession: true` answers `sessionInfo.sessionId` (recorded:
  bigquery-276 and bigquery-383). A `connectionProperties` entry with the key
  `session_id` and the id joins the session, and every later answer of that
  session echoes `sessionInfo` (recorded: bigquery-384 to bigquery-388). An id
  that is not a session answers HTTP 400, the reason `invalid` and `Invalid input
  session id.` (recorded: bigquery-277). `CALL BQ.ABORT_SESSION()` ends the
  session (recorded: bigquery-390 and bigquery-416). Emulator: it answers no `sessionInfo`.
- Transactions. In a script, `BEGIN TRANSACTION; INSERT ...; ROLLBACK
  TRANSACTION` answered HTTP 200 and the row was not in the table afterwards
  (recorded: bigquery-266 and bigquery-267). In a session across requests, `BEGIN
  TRANSACTION`, an `INSERT`, a `SELECT` that saw the row, `ROLLBACK TRANSACTION`
  and a `SELECT` that saw no row all worked, and a `SELECT` outside the session
  saw no row either (recorded: bigquery-384 to bigquery-389). A second session
  with `BEGIN TRANSACTION`, an `INSERT` and `COMMIT TRANSACTION` kept the row, and
  a `SELECT` outside the session saw it (recorded: bigquery-411 to bigquery-415).
  The statement types are `BEGIN_TRANSACTION`, `ROLLBACK_TRANSACTION` and
  `COMMIT_TRANSACTION` (recorded: bigquery-384, bigquery-387 and bigquery-414). A script with an error inside the transaction answered HTTP 400
  and left no row (recorded: bigquery-268 and bigquery-269). A `BEGIN ... EXCEPTION
  WHEN ERROR THEN ROLLBACK` block rolled back (recorded: bigquery-270 and
  bigquery-271). `BEGIN TRANSACTION`, `COMMIT TRANSACTION` and `ROLLBACK
  TRANSACTION` as separate requests with no session answer HTTP 400 `Transaction
  control statements are supported only in scripts or sessions` (recorded:
  bigquery-272 to bigquery-274). Emulator: it refuses `ROLLBACK` and it answers
  200 for a lone `BEGIN`.
- Cancel. `jobs.cancel` of a running job answered HTTP 200 with the job still
  `RUNNING` (recorded: bigquery-210). A `jobs.get` right after that
  still showed `state` `RUNNING` (recorded: bigquery-211). The `getQueryResults`
  of the job answered HTTP 499 with the status `CANCELLED`, the reason `stopped`
  and `Job execution was cancelled: User requested cancellation` (recorded:
  bigquery-212). So the cancel takes effect a little after its answer, and the
  state that `jobs.get` shows can lag. `jobs.cancel` of a job that had
  ended answered HTTP 200 (recorded: bigquery-213). A cancel of a job that does
  not exist answers HTTP 403 `accessDenied` with `Permission bigquery.jobs.update
  denied on job ... (or it may not exist)` (recorded: bigquery-214). Emulator:
  HTTP 404, and it reports every job as `DONE`.
- `jobs.insert` of a statement that fails answers HTTP 200 with `status.errorResult`
  and `state` `DONE` (recorded: bigquery-336). `jobs.get` of that job keeps the
  `errorResult` (recorded: bigquery-337). `getQueryResults` of it answers HTTP 400
  with the same error (recorded: bigquery-338). A `jobs.insert` answers `state`
  `RUNNING` for a running job (recorded: bigquery-209, bigquery-339 and
  bigquery-340), so the service has real asynchronous jobs. Emulator: the later
  `jobs.get` showed no error.
- `dryRun: true` answers the schema, `cacheHit` `true`, and a `jobReference` with
  no `jobId` (recorded: bigquery-020). Emulator: it ignored the member.
- `requestId`. An `INSERT` that repeats a `requestId` with an identical body ran
  once. The first request inserted the row, the second answered HTTP 200 with the
  same counts and `jobComplete: false`, and a count of the rows with that id was
  1 (recorded: bigquery-380, bigquery-381 and bigquery-382). So the service
  deduplicates a statement that changes data. The second answer has
  `numDmlAffectedRows` and an `endTime` and still says `jobComplete: false`, so a
  driver must not loop on that member alone for a DML answer. A read repeats under
  another job id, because a read can ignore the token (recorded: bigquery-018 and
  bigquery-019, and bigquery-236 for the rule). Emulator: it ran the statement
  again.
- `maximumBytesBilled` of 1 on a scan of a table with `useQueryCache` of `false`
  answered HTTP 400, the reason `bytesBilledLimitExceeded` and `Query exceeded limit
  for bytes billed: 1. 10485760 or higher required.` (recorded: bigquery-204). So
  the service enforces the limit and bills at least 10 MB for a scan. Emulator: it
  ignored the member.
- NaN, the infinities and the `TIMESTAMP`. The service writes them as text:
  `"NaN"`, `"Infinity"` and `"-Infinity"` (recorded: bigquery-076, bigquery-077
  and bigquery-078), and a `TIMESTAMP` as a double with an exponent, such as
  `"1.704164645123456E9"` (recorded: bigquery-070). `useInt64Timestamp` gives
  the exact microseconds, such as `"253402300799999999"` for the year 9999
  (recorded: bigquery-379). `timestampOutputFormat` of `ISO8601_STRING` gives
  `"2024-01-02T03:04:05.123456Z"` (recorded: bigquery-342). Emulator: `null`,
  `"+Inf"` and seconds with six fixed digits.
- `INT64` overflow answers HTTP 400, `invalidQuery` and `Integer Overflow`
  (recorded: bigquery-074 and bigquery-188). Emulator: it wrapped.
- Time travel, materialized views, clones, snapshots, routines and foreign keys
  exist (recorded: bigquery-316, bigquery-296, bigquery-299, bigquery-060,
  bigquery-301, bigquery-303, bigquery-304 and bigquery-289). A `CALL` of a
  procedure ran and answered its result (recorded: bigquery-355). On the service, `PRIMARY KEY ... NOT ENFORCED`, `DEFAULT`,
  `PARTITION BY` and `CLUSTER BY` work (recorded: bigquery-288 and bigquery-291).
  `UNIQUE` is not a constraint (recorded: bigquery-290). `ALTER TABLE DROP
  COLUMN` works (recorded: bigquery-311). `CREATE SEARCH INDEX` on a text column
  worked, and `SEARCH(doc, 'alpha')` found the row (recorded: bigquery-402 and
  bigquery-404). `CREATE VECTOR INDEX` on an `ARRAY<FLOAT64>` column was refused
  because the table has 2 rows and the `IVF` type needs at least 5000: `Total
  rows 2 is smaller than min allowed 5000` (recorded: bigquery-403). So the
  service knows the vector index, and a working one is not recorded. Emulator: it refused time travel, the
  materialized view, the clone and the snapshot, and its `DROP COLUMN` failed.
- Datasets. `CREATE SCHEMA` made a dataset, a table went into it, and `DROP
  SCHEMA ... CASCADE` dropped it (recorded: bigquery-394, bigquery-395 and
  bigquery-398). `datasets.insert` made a dataset (recorded: bigquery-359 and
  bigquery-397). A `DROP SCHEMA` of a dataset that does not exist answers HTTP 403
  `accessDenied` and `Permission bigquery.datasets.delete denied ... (or it may not
  exist)` (recorded: bigquery-441 and bigquery-447). `GRANT` on a dataset that does
  not exist answers HTTP 403 (recorded: bigquery-357). The list of datasets shows
  the new datasets (recorded: bigquery-227 and bigquery-378). `SELECT` from
  `INFORMATION_SCHEMA.SCHEMATA` is refused for the account, with and without the
  region (recorded: bigquery-059 and bigquery-396), so the way to list datasets is
  `datasets.list`. `JOBS_BY_PROJECT` with the region `region-us` is readable
  (recorded: bigquery-399), and `JOBS` of the project is refused (recorded:
  bigquery-064). Emulator: it refused `DROP SCHEMA` and `datasets.insert` made a
  dataset.
- Cloud Storage. `EXPORT DATA` to the bucket answered HTTP 200 with the
  `statementType` `EXPORT_DATA` (recorded: bigquery-405). `LOAD DATA` from the
  exported files answered `LOAD_DATA` and loaded rows (recorded: bigquery-407).
  The load read 1 row of 2, because the export wrote no header and the load
  skipped the first line (recorded: bigquery-408). `CREATE EXTERNAL TABLE` over the
  files worked, and a `SELECT` from it answered the same row (recorded:
  bigquery-409 and bigquery-410). A missing bucket answers HTTP 404 `Not found:
  Files` (recorded: bigquery-313, bigquery-314 and bigquery-305). Emulator:
  `EXPORT DATA` failed or exported nothing, and `LOAD DATA` loaded nothing.
- Two part names. `CREATE TABLE dataset.table` works on the service (recorded:
  bigquery-200). Emulator: it refused the name.
- Script variables do not leak. After a script that declared `dbimp_v`, the same
  name worked as a column name and as an alias (recorded: bigquery-243 and
  bigquery-244). `IF`, `WHILE` and a temporary table work (recorded: bigquery-251
  to bigquery-253). Emulator: a variable replaced the name in later requests, and
  a `WHILE` loop failed.
- A script answers the result of its last statement, and the `statementType`
  is `SCRIPT` (recorded: bigquery-238, "two selects in one request"). A script
  that ends with a statement that has no rows answers no schema and no rows
  (recorded: bigquery-247 and bigquery-261). Emulator: the same rule.
- A script with no transaction whose second statement fails answers HTTP 400,
  and the row that the first statement inserted stays (recorded: bigquery-248 and
  bigquery-249). Emulator: the row was not kept.
- `UPDATE` and `DELETE` with no `WHERE` clause are refused with `UPDATE must
  have a WHERE clause at [1:1]` (recorded: bigquery-027 and bigquery-368).
- `INFORMATION_SCHEMA` has `TABLES`, `COLUMNS`, `TABLE_OPTIONS`, `VIEWS`,
  `ROUTINES`, `KEY_COLUMN_USAGE` and `COLUMN_FIELD_PATHS` at the level of a
  dataset (recorded: bigquery-060 to bigquery-063, bigquery-065, bigquery-066 and
  bigquery-358). `TABLES` shows the `table_type` `BASE TABLE`, `CLONE` and
  `SNAPSHOT` (recorded: bigquery-060). A read of the level of the project,
  `SCHEMATA` and `JOBS`, answers HTTP 403 for lack of the right (recorded:
  bigquery-059 and bigquery-064). A read of `INFORMATION_SCHEMA` bills at least
  10 MB (recorded: bigquery-060, where `totalBytesProcessed` is `"10485760"`).
  Emulator: `SCHEMATA`, `TABLES`, `COLUMNS` and `TABLE_OPTIONS` only.
- Rates. The 418 requests ran in 4 minutes and 44 seconds, and none answered HTTP
  429 or the reason `rateLimitExceeded` (recorded: bigquery-001 to bigquery-452,
  by the `Date` headers). The limits of the service are not met by this load.
- Login. A request with no `Authorization` header answered HTTP 401, the status
  `UNAUTHENTICATED`, the reason `required`, `Login Required.` with the location
  `Authorization`, and an `ErrorInfo` with the reason `CREDENTIALS_MISSING`
  (recorded: bigquery-219). A request whose token had `-wrong` added to its end
  answered HTTP 200 (recorded: bigquery-220), so the service did not refuse a good
  token with a suffix. A token that is wholly wrong answered HTTP 401, the status
  `UNAUTHENTICATED`, the reason `authError`, `Invalid Credentials` with the
  location `Authorization`, and `Request had invalid authentication credentials.
  Expected OAuth 2 access token, login cookie or other valid authentication
  credential.` on a query and on a read of the datasets (recorded: bigquery-420
  and bigquery-421). An API key that is wrong,
  sent with the real token, answered HTTP 400 `badRequest` and `API key not valid.
  Please pass a valid API key.` (recorded: bigquery-221). So the service checks a
  key before it reads the token. A key that is right is not measured. Emulator: it
  took every request.
- Version. The service has no statement and no endpoint for a version.
  `SELECT @@version` answered HTTP 400 `Unrecognized name: @@version` (recorded:
  bigquery-229). The only revision is the one of the discovery document
  (recorded: bigquery-236).
- Compression. A request with `Accept-Encoding: gzip` got a gzip answer, with
  `Content-Encoding: gzip` (recorded: bigquery-038 and bigquery-280). A request
  with `Content-Encoding: gzip` and a body that is not gzip answered HTTP 400,
  the reason `badRequest` and `Invalid http request` (recorded: bigquery-364). A
  request with a gzip body is not recorded on the service. Emulator: it never
  compressed an answer, and it read a gzip body.
- Session facts. `SESSION_USER()` answered the email of the service account
  (recorded: bigquery-231). `@@project_id` answered the project, `@@dataset_id`
  answered NULL and `@@time_zone` answered `UTC` (recorded: bigquery-230).
- Streaming. `tabledata.insertAll` of two rows into a table of its own answered
  HTTP 200 with no `insertErrors` (recorded: bigquery-418). A row whose field does
  not exist answered HTTP 200 with `insertErrors`, the index, the reason `invalid`,
  the location of the field and `no such field: s.` (recorded: bigquery-361). So a
  caller must read `insertErrors` and not the HTTP code.
- Children of a script. `jobs.list` with `parentJobId` of a job that is not a
  script answered an empty list (recorded: bigquery-345 and bigquery-419). The job
  of the request was a plain `SELECT` (recorded: bigquery-262), so the children of
  a real script are not recorded.

The answers that this run did not show are in Open questions.

## Requests

- A statement is `POST /bigquery/v2/projects/{project}/queries` with a JSON body
  (recorded: bigquery-017, "a statement"). The member `query` is the text. The
  service names the operation `jobs.query` (source: the REST reference).
- The hosted service took these members and used them: `query`, `useLegacySql`,
  `parameterMode`, `queryParameters`, `defaultDataset`, `maxResults`, `dryRun`,
  `useQueryCache`, `createSession`, `connectionProperties` and
  `formatOptions.useInt64Timestamp` (recorded: bigquery-018, bigquery-020,
  bigquery-163, bigquery-176, bigquery-276, bigquery-277, bigquery-371 and
  bigquery-072). It took `location` and answered it in `jobReference`, and every
  answer had `location` `US` (recorded: bigquery-018). It acted on `timeoutMs`,
  `requestId` and `maximumBytesBilled` (recorded: bigquery-215, bigquery-381 and
  bigquery-204), and it kept `labels` on the job (recorded: bigquery-373). It took
  `jobCreationMode` and still made a job (recorded: bigquery-334 and
  bigquery-353). It refused `reservation` of `none` with HTTP 400 and the reason
  `invalid`, because the project does not enable the override (recorded:
  bigquery-333). It took a negative `timeoutMs` as HTTP 400 `Value out of range`
  (recorded: bigquery-206). It took a member that does not exist, such as
  `nosuchmember`, with no error (recorded: bigquery-039). The values
  of `formatOptions.timestampOutputFormat` are `FLOAT64`, `INT64` and
  `ISO8601_STRING` (recorded: bigquery-236), and all three worked (recorded:
  bigquery-343, bigquery-344 and bigquery-342).
- The emulator took these members and used them: `query`, `parameterMode`,
  `queryParameters`, `defaultDataset` and `formatOptions.useInt64Timestamp`
  (recorded: "a statement with a default dataset" and "a select with a default
  dataset", "the rows of every type with integer timestamps", and the parameter
  requests of item 4). It took `location` and answered it in `jobReference` on
  0.8.1 only. It took these members and ignored them: `useLegacySql`,
  `useQueryCache`, `timeoutMs`, `maxResults`, `requestId`, `labels`,
  `maximumBytesBilled`, `dryRun`, `createSession`, `connectionProperties`,
  `jobCreationMode`, `reservation` and `preserveNulls` (recorded: "a statement
  with every optional member", "a dry run", "a request with the legacy dialect",
  "a select with a label", "a select with a reservation", "a select with
  jobCreationMode"). It also took a member that does not exist (recorded: "a
  request with a member that does not exist").
- The members of the request of the real service are `query`, `maxResults`,
  `defaultDataset`, `timeoutMs`, `dryRun`, `useQueryCache`, `useLegacySql`,
  `parameterMode`, `queryParameters`, `location`, `formatOptions`,
  `connectionProperties`, `labels`, `maximumBytesBilled`, `requestId`,
  `createSession`, `jobCreationMode`, `jobTimeoutMs`, `reservation`, `maxSlots`,
  `continuous`, `writeIncrementalResults`, `preserveNulls`,
  `queryResultsFormat`, `secureContext`, `arrowSerializationOptions` and
  `destinationEncryptionConfiguration` (recorded: bigquery-236, "the discovery
  document"). The defaults are 10,000 for `timeoutMs`, `true` for
  `useQueryCache`, `true` for `useLegacySql`, and no maximum row count for
  `maxResults`, with a limit of 10 MB for a response (recorded: bigquery-236).
  `requestId` is case sensitive and has at most 36 ASCII characters (recorded:
  bigquery-236). The service says that the member `queryResultsFormat` with the
  value `ARROW` is not yet available (recorded: bigquery-236).
- The header that the service needs is `Authorization`. The service read a body
  with no `Content-Type` as JSON (recorded: bigquery-036,
  "a request with no content type"). `X-Goog-User-Project` names the project that
  pays and holds the quota. A request that named a project which the account cannot
  use answered HTTP 403 `forbidden` with a message that names the role
  `roles/serviceusage.serviceUsageConsumer` (recorded: bigquery-222). The
  `User-Agent` and `X-Goog-Api-Client` that a client sends changed nothing
  (recorded: bigquery-281). An `Expect` header changed nothing (recorded:
  bigquery-285). `PUT` and `DELETE` on the path of the queries answered HTTP 404
  with the reason `notFound` and the message `Request couldn't be served.`
  (recorded: bigquery-037 and bigquery-196). A path that does not exist answered
  HTTP 404 with an HTML page (recorded: bigquery-195). `OPTIONS` answered HTTP 200
  (recorded: bigquery-283), and `HEAD` answered HTTP 404 with no body (recorded:
  bigquery-284). Emulator: it read a body with no `Content-Type`, and it
  answered `PUT`, an unknown path and `HEAD` with HTTP 500.
- The other endpoints that the service answered, with the real names:
  - `GET /bigquery/v2/projects/{project}/queries/{jobId}` is
    `jobs.getQueryResults` (recorded: bigquery-035 and bigquery-166). Its query
    keys are `maxResults`, `pageToken`, `startIndex`, `timeoutMs`, `location` and
    `formatOptions.useInt64Timestamp` (recorded: bigquery-166 to bigquery-177), and
    `formatOptions.timestampOutputFormat` (recorded: bigquery-236).
  - `POST /bigquery/v2/projects/{project}/jobs` is `jobs.insert`. It answers when
    the job exists, and the job can be `RUNNING` (recorded: bigquery-339). A job
    id that is used again answers HTTP 409 and the reason `duplicate` (recorded:
    bigquery-041). Emulator: it runs the query before it answers.
  - `GET /bigquery/v2/projects/{project}/jobs/{jobId}` is `jobs.get`, `POST
    .../jobs/{jobId}/cancel` is `jobs.cancel`, and `GET .../jobs` is `jobs.list`
    (recorded: bigquery-034, bigquery-210 and bigquery-043). `jobs.list` takes
    `stateFilter`, `maxResults` and `parentJobId`, and gives `nextPageToken`
    (recorded: bigquery-043, bigquery-345 and bigquery-346).
  - `datasets.list`, `datasets.get`, `tables.list`, `tables.get`,
    `tables.insert`, `tables.delete`, `tabledata.list` and `tabledata.insertAll`
    answered (recorded: bigquery-054, bigquery-055, bigquery-051, bigquery-052,
    bigquery-360, bigquery-363, bigquery-058 and bigquery-361, where `tables.insert`
    answered HTTP 409 because the table existed). `datasets.insert`
    made a dataset (recorded: bigquery-359 and bigquery-397). `tabledata.list`
    gives the rows in the order of the storage and not the order of the insert
    (recorded: bigquery-178), and it takes `selectedFields` and `startIndex`
    (recorded: bigquery-348 and bigquery-349). `tables.list` takes `maxResults` and
    gives `nextPageToken` (recorded: bigquery-180). `GET /bigquery/v2/projects`
    lists the project (recorded: bigquery-057). `GET .../serviceAccount` answers the
    email of the service account of the encryption of the project, which is not the
    login (recorded: bigquery-237).
- The host of the real service is `bigquery.googleapis.com` (recorded:
  bigquery-034, in `selfLink`). The emulator writes `0.0.0.0:9050` in the member
  `selfLink` of a job, whatever port `dbrun` publishes.
- Authentication: the hosted service took the real token. A request with no
  token answered HTTP 401 (recorded: bigquery-219). A request whose token had
  `-wrong` added at its end answered HTTP 200 (recorded: bigquery-220), so the service
  seems to read only the start of a token. A token that is wholly wrong
  answered HTTP 401 on a query and on a read (recorded: bigquery-420 and
  bigquery-421). A key that is wrong answered HTTP 400 (recorded: bigquery-221). Emulator: it took every request with no
  credential, with a Bearer token that is wrong, and with an API key that is
  wrong. See The DSN for the login.
- `Content-Encoding`: the hosted service answers a gzip body of the answer when
  the request says `Accept-Encoding: gzip` (recorded: bigquery-038), and it
  refuses a request with `Content-Encoding: gzip` and a body that is not gzip
  (recorded: bigquery-364). A gzip request body that is right is not recorded.
  Emulator: it read a gzip body (measured, 2026-10-10, with `curl` on 0.8.1) and it
  answered HTTP 400 `failed to decode gzip content: gzip: invalid header` when the
  body was not gzip (recorded: "lead: a request with a gzip content encoding and a
  body that is not gzip"). It did not compress a response.
- A redirect did not occur in any recording. The recorder does not follow one.

## The DSN

The driver reads this DSN (D189, D27 and D35):

    bigquery://<project>/<dataset>?credential_file=/path/key.json

- The scheme is `bigquery`, and the driver registers that one name and no alias.
  The host is the id of the project, and it is a part of every request path. A host
  with a port, or with a colon that is not an IPv6 address, is refused.
- The path holds the dataset of each statement, which the driver sends as
  `defaultDataset`, so that a statement can name a table with no dataset. A path of
  two parts is `/<location>/<dataset>`, which is the form of `dburl`. Both parts are
  optional, and a path of three parts is refused.
- The user of the URL is ignored, because `dbrun` writes `admin` for the emulator.
  A password is refused, because the secret is the path of a key file in a key of
  the query, and a URL never holds the text of the key (D189 item 2, which is the
  alternative to D94 for a secret that is a JSON file).
- The driver refuses a key that is not in the table below, and a key that appears
  twice. The query holds no other key.

| Key | Default | Meaning |
| --- | --- | --- |
| `credential_file` | none | The path of the key file of a service account. The driver reads it when it makes the connector |
| `disable_auth` | `false` | `true` sends no `Authorization` header, for an emulator. It cannot go with `credential_file` |
| `endpoint` | `https://bigquery.googleapis.com` | The address of the service, `http` or `https` with a host and nothing else, such as `http://127.0.0.1:9050` |
| `location` | none | The location of the job, such as `US`, `EU` or `asia-southeast1`. The path can name it too, and not both |
| `scopes` | `https://www.googleapis.com/auth/bigquery` | The scopes of the token, separated by a comma or a space |
| `timeout` | none | The time that the service gives a job, as `jobTimeoutMs`. A duration with a unit, such as `60s` |
| `max_results` | none | The most rows in a page of a result, as `maxResults`. None leaves the size to the service, which cuts a page at 10 MB |

A caller can pass a ready access token in `Config.AccessToken` of a connector. A
DSN cannot hold it, and `FormatDSN` leaves it out. A connector with no key file, no
access token and no `disable_auth` fails the first statement with `ErrNoCredential`.

Examples, for the service and for the emulator that `dbrun` starts:

    bigquery://my-project/my_dataset?credential_file=/home/me/key.json
    bigquery://my-project/EU/my_dataset?credential_file=/home/me/key.json&timeout=5m
    bigquery://admin@dbmeta/dbmeta?endpoint=http%3A%2F%2F127.0.0.1%3A9050&disable_auth=true

The facts that the DSN rests on follow.

- `dburl` writes `bigquery://<project>/<dataset>?<options>`, and `dbrun` prints
  `bigquery://admin@dbmeta/dbmeta?endpoint=http%3A%2F%2F127.0.0.1%3A<port>&disable_auth=true`
  for the emulator (read of `dbmeta/container/bigquery.go`, 2026-10-10). The host
  is the project. The path is the dataset, with an optional location before it,
  as `/<location>/<dataset>`. The query keys that the `usql` driver reads are
  `endpoint`, `disable_auth`, `scopes`, `credential_file` and `credential_json`
  (a base64 text of the key file), as `gorm.io/driver/bigquery` v1.2.1 reads them
  (read of `driver/driver.go`, 2026-10-10). A driver here needs the key
  `endpoint` for the emulator and for any other address, and the service answers
  at `https://bigquery.googleapis.com` when `endpoint` is absent.
- The project is a part of every request path. A request for a project that the
  account cannot use answered HTTP 403 `accessDenied` with `User does not have
  bigquery.jobs.create permission in project nosuch` (recorded: bigquery-194).
  Reads of the datasets and of a table of another project answered HTTP 404
  `notFound` (recorded: bigquery-224 and bigquery-225). A project named `other`
  answered HTTP 400 `badRequest` with `Cannot parse  as CloudRegion.` (recorded:
  bigquery-223). The dataset goes in `defaultDataset` of the request, as
  `projectId` and `datasetId`, and it makes an unqualified table name work
  (recorded: bigquery-369 to bigquery-371). Emulator: a request for another
  project answered HTTP 404.
- The location goes in `location` of the request. The service answered
  `location` `US` in every job reference of this run (recorded: bigquery-017).
  A read of a job with the wrong location answered HTTP 404 (recorded:
  bigquery-351), so a driver must send the location of the job on `jobs.get`,
  `jobs.cancel` and `getQueryResults` for a job that is not in `US` or `EU`
  (recorded: bigquery-236, in the text of those methods).
- The endpoint is the address of the service. An emulator is `http://` with a
  port, and the service is `https://bigquery.googleapis.com`.
- The login of the service is a service account. Its key file has these members
  that the Go library of Google reads: `type`, `client_email`, `private_key_id`,
  `private_key`, `token_uri` and `project_id` (read of `golang.org/x/oauth2`
  v0.37.0, `google/google.go`, 2026-10-10). A request with no token answers HTTP 401 (recorded: bigquery-219). The login of
  this run followed the flow below (measured, 2026-10-10, with a small program of the run that signs the
  JWT and posts it, and that keeps only the token). The request and the answer
  of the token endpoint are not recorded, because the recorder takes a finished
  token. The program built the token with the claims below, got an access token,
  and the service accepted it as `Authorization: Bearer <access_token>` in all 400
  requests of the run. The flow is (source: the document of the service account
  flow of Google, 2026-10-10):
  - The client builds a JWT with the header `alg` of `RS256` and `typ` of `JWT`,
    and the claims `iss` (the email of the service account), `scope` (the scopes,
    separated by spaces), `aud` (the `token_uri` of the key, which is
    `https://oauth2.googleapis.com/token`), `iat` and `exp`. The `exp` is at most
    one hour after `iat`, and the run used 55 minutes.
  - The client signs the first two parts with the private key by RSA SHA-256
    (PKCS 1 version 1.5). The key is a PKCS 8 PEM text of an RSA key.
  - The client sends `POST https://oauth2.googleapis.com/token` with the content
    type `application/x-www-form-urlencoded` and the fields `grant_type`, which
    is `urn:ietf:params:oauth:grant-type:jwt-bearer`, and `assertion`, which is
    the signed JWT.
  - The answer is JSON with `access_token`, `token_type` of `Bearer`, and
    `expires_in`, which is 3600 in the document. The run did not keep the answer,
    so `expires_in` is not measured here.
  - The client sends `Authorization: Bearer <access_token>` and signs a new JWT
    before the token ends. A run of 4 minutes and 44 seconds never met the end.
- The scope that the run used is `https://www.googleapis.com/auth/bigquery`, and
  it was enough for queries, `jobs`, `tables`, `tabledata` and `datasets`
  (recorded: bigquery-017, bigquery-034, bigquery-052, bigquery-054 and
  bigquery-384). The scopes that the discovery document names are
  `https://www.googleapis.com/auth/bigquery`,
  `https://www.googleapis.com/auth/bigquery.insertdata`,
  `https://www.googleapis.com/auth/cloud-platform`,
  `https://www.googleapis.com/auth/cloud-platform.read-only` and three scopes of
  Cloud Storage, `devstorage.full_control`, `devstorage.read_only` and
  `devstorage.read_write` (recorded: bigquery-236). Whether the narrower scopes
  `bigquery.insertdata` or `cloud-platform.read-only` can run a query is not
  measured.
- A token that a person already has, such as the output of `gcloud auth
  print-access-token`, goes in the same header. The run used a token of that kind
  after the exchange. Other sources of a credential, such as the metadata server
  of Google Cloud, are not measured.
- An API key goes in the query key `key`. A key that is wrong answered HTTP 400
  `badRequest` even with a right token (recorded: bigquery-221), so a driver must
  not send one by accident. What a right key does is not measured. BigQuery needs
  a token for a private dataset, so a key alone is not a login (source: the
  documentation of Google, not measured).
- Decided, D189: the secret is a path to a key file, in a key of the DSN query such
  as `credential_file`. The driver reads the file, signs a JWT with RS256 for the
  token endpoint that the file names, and exchanges it. The key text never sits
  in a URL. A caller can also pass a ready access token through a connector. This
  is the alternative to D94 for a secret that is a JSON file. A key file is about 2
  KB, and the private key of a service account is a PEM text.
- The cost of the login in the driver: an RSA signature and one extra HTTPS
  request, both in the standard library (`crypto/rsa`, `crypto/x509`,
  `encoding/pem` and `net/http`). No dependency is needed. The driver must cache
  the token and renew it. The signing program of the run has about 90 lines.

## Responses

- A finished query answers HTTP 200 with one JSON object. The hosted service
  wrote the members in this order: `kind` (`bigquery#queryResponse`), `schema`,
  `jobReference`, `totalRows`, `rows`, `totalBytesProcessed`, `jobComplete`,
  `cacheHit`, `queryId`, `jobCreationReason`, `totalBytesBilled`, `totalSlotMs`,
  `location`, `creationTime`, `startTime`, `endTime`, `pageRowCount` and
  `statementType` (recorded: bigquery-017). A DML answer has `numDmlAffectedRows`
  and `dmlStats` in place of `totalRows` and `rows` (recorded: bigquery-025). A
  `getQueryResults` answer has `kind` (`bigquery#getQueryResultsResponse`),
  `etag`, `schema`, `jobReference`, `totalRows`, `pageToken` when there is one,
  `rows`, `totalBytesProcessed`, `jobComplete` and `cacheHit` (recorded:
  bigquery-166). The answer of a first page has `pageToken` after `totalRows` and
  before `rows` (recorded: bigquery-163). The `jobReference` has `projectId`,
  `jobId` and `location`. A dry run has no `jobId` (recorded: bigquery-020).
  The answer of a statement is pretty printed, with two spaces of indent, so a
  result of 3000 rows is 364,592 bytes (recorded: bigquery-164). Emulator: the
  order was `jobReference`, `schema`, `rows`, `totalRows` and `jobComplete`, and
  it wrote no `kind`, no `cacheHit` and no `totalBytesProcessed`.
- The order of the members is what the service did in this run. The documents do
  not promise it, and Gemini said that real clients never rely on it. A driver
  that reads the answer one token at a time and needs `schema` before `rows`
  must hold the rows, or read the schema from `getQueryResults`, when `rows` comes
  first. The driver refuses such an answer with an error (see Open questions).
- A query that is not done answers HTTP 200 with `jobComplete` `false`, the
  `jobReference`, `queryId`, `jobCreationReason`, `location`, `creationTime`,
  `startTime` and `statementType`, and no `schema`, no `totalRows` and no `rows`
  (recorded: bigquery-215 and bigquery-216). `getQueryResults` answers `kind`,
  `etag`, `jobReference` and `jobComplete` `false` (recorded: bigquery-217). A
  deduplicated DML answer has `jobComplete` `false` together with
  `numDmlAffectedRows` and an `endTime` (recorded: bigquery-381). So `jobComplete` alone
  is not a safe signal to stop or to continue for a DML answer. Whether a poll
  reaches `jobComplete` `true` is not recorded.
- The names of the columns and their order come from `schema.fields`. Each field
  has `name`, `type` and `mode`, and a field of the type `RECORD` has `fields`
  (recorded: bigquery-017 and bigquery-070). A field of the type `RANGE` has
  `rangeElementType`, such as `{"type":"DATE"}` (recorded: bigquery-070). The
  schema comes also for a result with no rows (recorded: bigquery-045, "columns
  of a result with no rows"). Two columns can share a name, and the service
  renames the second one `a_1` (recorded: bigquery-046, "columns that share a
  name"). A column with no name is named `f0_`, `f1_` and so on (recorded:
  bigquery-047, "columns with no name"). Emulator: the second column kept its name,
  and a column with no name was `$col1`. The field does not name a precision or a
  length. The field `type` is the legacy name: `INTEGER`, `FLOAT`, `BOOLEAN` and
  `RECORD`, and never `INT64`, `FLOAT64`, `BOOL` or `STRUCT` (recorded:
  bigquery-070).
- A statement with no result, such as `DROP TABLE` and `ALTER TABLE`, answers no
  `schema`, no `rows` and no `totalRows` (recorded: bigquery-050 and
  bigquery-308). A statement that changes a table, such as `CREATE TABLE`, `INSERT`,
  `UPDATE`, `DELETE`, `MERGE` and `TRUNCATE TABLE`, answers the `schema` of the
  table, with no `mode`, and no `rows` (recorded: bigquery-024, bigquery-025,
  bigquery-026, bigquery-028, bigquery-366 and bigquery-030). The count of rows
  that a DML statement changed is `numDmlAffectedRows`, as a string, and
  `dmlStats` has the three counts of `insertedRowCount`, `updatedRowCount` and
  `deletedRowCount`, `dmlMode` and sometimes `fineGrainedDmlUnusedReason`
  (recorded: bigquery-025, bigquery-026 and bigquery-366). The `getQueryResults`
  of a DML job answers `numDmlAffectedRows` too (recorded: bigquery-035).
  Emulator: it answered `"schema":{}`, `"rows":[]` and `"totalRows":"0"`, and no
  count.
- A row is `{"f":[{"v":<value>},...]}`, in the order of the columns. Each `v` is
  JSON text, or `null` for a NULL. A number is a string, such as `"1"`. A column
  of mode `REPEATED` has `v` as a list of `{"v":...}`. A `RECORD` has `v` as
  `{"f":[{"v":...},...]}` (recorded: bigquery-070, bigquery-095 and bigquery-097,
  "an array of structs" and "a nested struct").
- The answer is one JSON object, so the driver can read the members one token at
  a time (D25). It can stop at the last row. The service holds the result: it
  sent 3000 rows in one object when `maxResults` was not set (recorded:
  bigquery-164).
- Paging: `jobs.getQueryResults` takes `maxResults`, `pageToken` and
  `startIndex`. The `pageToken` of the service is an opaque text of about 600
  characters in the alphabet of base 32, such as the one in bigquery-163. A
  driver must send it back as it is, in the query key `pageToken`. The service
  answered `totalRows` on every page, and it left out `pageToken` on the last
  page (recorded: bigquery-166, bigquery-167 and bigquery-171, "the first page of
  the job", "page 2 of the job" and "page 6 of the job"). A page token that is not
  valid answered HTTP 400 and the reason `invalid` (recorded: bigquery-173). A
  `startIndex` of 2900 gave the last 100 rows with no `pageToken`, and a
  `startIndex` past the end gave no `rows` (recorded: bigquery-172 and
  bigquery-174). A job that does not exist answered HTTP 404 `notFound`
  (recorded: bigquery-175). `maxResults` of 1,000,000 was taken (recorded:
  bigquery-350). `tabledata.list` takes the same keys and gave a `pageToken` and
  `totalRows` (recorded: bigquery-178 and bigquery-179). Emulator: the
  `pageToken` was the offset of the next row, as text, such as `"500"`, a token
  that is not valid was read as 0, and `tabledata.list` ignored `selectedFields`
  and `startIndex`.
- The `maxResults` of `jobs.query` cuts the first page (recorded: bigquery-163).
  The service cuts a page at 10 MB too (recorded: bigquery-236). A driver must
  then read the next pages with `getQueryResults`, with the `jobId` of the first
  answer, and with its `location`. The limit on the size of the whole result is
  not measured. The error reason `responseTooLarge` has the HTTP code 403 in the
  table of errors of Google (source: the document of error messages,
  2026-10-10). Emulator: it ignored `maxResults` in both releases.
- A `jobs.query` job can be read again by `jobs.get` and by `getQueryResults`
  (recorded: bigquery-034, bigquery-035 and bigquery-166). Emulator 0.7.2: both
  answered HTTP 404 for the job of `jobs.query` and knew the job of `jobs.insert`
  only (recorded: "the job of the update", "the first page of the job").
- A result of a cached read has `cacheHit` `true` and the same rows (recorded:
  bigquery-036 and bigquery-164). A result has no fixed order of rows when the
  statement has no `ORDER BY` (recorded: bigquery-178 for `tabledata.list` and
  bigquery-326 for a recursive query, which are not in the order of their
  numbers).
- Compression: see Requests. A redirect did not occur.

## Types

The type name is the `type` member of the field. Each value is text. A hosted
fact comes first and the emulator fact follows it.

- `INTEGER` is a decimal text of an `INT64`, such as `"9223372036854775807"` and
  `"-9223372036854775808"` (recorded: bigquery-073, "the largest and smallest
  INT64"). The range is 64 bits. An overflow answers HTTP 400, `invalidQuery` and
  `Integer Overflow` (recorded: bigquery-074 and bigquery-188). Emulator: it
  wrapped the overflow to the other end.
- `FLOAT` is the text of a double in the form of Java, such as `"1.5"`,
  `"1.0E308"`, `"4.9E-324"`, `"-0.0"` and `"0.30000000000000004"` (recorded:
  bigquery-075, "FLOAT64 values"). The text has an exponent for a large or a small
  value. `NaN` is `"NaN"`, and the infinities are `"Infinity"` and `"-Infinity"`
  (recorded: bigquery-076, "FLOAT64 infinity and NaN"). The casts of `'NaN'` and
  of `'inf'` work (recorded: bigquery-077 and bigquery-078). A `NaN` is not
  `null`, so a driver can tell it from a NULL. Emulator: `NaN` was `null` and an
  infinity was `"+Inf"`, and the casts failed.
- `NUMERIC` has 38 digits and a scale of 9, such as
  `"99999999999999999999999999999.999999999"` for the largest value (recorded:
  bigquery-079, "NUMERIC limits"). `BIGNUMERIC` has 76 digits before the point
  and a scale of 38, such as
  `"578960446186580977117854925043439539266.34992332820282019728792003956564819967"`
  for the largest value (recorded: bigquery-080, "BIGNUMERIC limits"). Both are
  exact decimal text, with no trailing zeros (recorded: bigquery-070, where
  `1.5` stays `"1.5"`).
- `BOOLEAN` is `"true"` or `"false"` as a string (recorded: bigquery-081, "BOOL
  values"). A JSON boolean never arrives. DeepSeek said that a `BOOL` is a JSON
  boolean and a `FLOAT64` is a JSON number, and the service showed that both are
  strings.
- `STRING` is UTF-8 text, and it keeps an empty string, a newline and a string of
  1000 characters (recorded: bigquery-082, "STRING values"). The service
  writes a non-ASCII character as UTF-8. The limit on a string is not measured.
- `BYTES` is base64 text, such as `"YWJj"` for `abc`, `"AP8="` for `00 ff`, and
  `""` for an empty value (recorded: bigquery-083, "BYTES values").
- `DATE` is `"2024-01-02"`, from `"0001-01-01"` to `"9999-12-31"` (recorded:
  bigquery-084, "DATE limits").
- `TIME` is `"12:34:56.789000"`, with six digits of fraction when the value has a
  fraction, and `"00:00:00"` when it has none, from `"00:00:00"` to
  `"23:59:59.999999"` (recorded: bigquery-085 and bigquery-070, "TIME values").
  Emulator: it wrote `"12:34:56.789"`.
- `DATETIME` is `"2024-01-02T03:04:05.123456"`, with a `T` between the date and
  the time, from `"0001-01-01T00:00:00"` to `"9999-12-31T23:59:59.999999"`, with
  no zone and with no fraction when the value has none (recorded: bigquery-086,
  "DATETIME values"). Gemini and DeepSeek said that a space stands in the place
  of the `T`, and both the service and the emulator showed the `T`.
- `TIMESTAMP` by default is a double in the form of Java, in seconds since the
  epoch, such as `"1.704164645123456E9"`, `"1.7041646455E9"`, `"0.0"`,
  `"-0.5"` and `"-6.21355968E10"` for `0001-01-01` (recorded: bigquery-070,
  bigquery-087, bigquery-089 and bigquery-232). The text is the shortest text
  that a double reads back, so the service loses precision for a large value:
  `9999-12-31 23:59:59.999999` arrived as `"2.534023008E11"`, which has lost the
  microseconds (recorded: bigquery-087, "TIMESTAMP values"). A driver cannot get
  them back from that text. A zone in the input changes the instant and is not in
  the output (recorded: bigquery-088). With `formatOptions.useInt64Timestamp` set
  to `true`, or with `timestampOutputFormat` of `INT64`, the text is the
  microseconds since the epoch, such as `"1704164645123456"`, and it is exact
  (recorded: bigquery-072, bigquery-344 and bigquery-379). Both `useInt64Timestamp` and
  `timestampOutputFormat` can be sent together (recorded: bigquery-344). The
  value `FLOAT64` gives the default form (recorded: bigquery-343). The value
  `ISO8601_STRING` gives the text `"2024-01-02T03:04:05.123456Z"`, with a `Z` and
  six digits of fraction (recorded: bigquery-342). The integer form keeps the last
  microsecond of the year 9999: `"253402300799999999"` (recorded: bigquery-379).
  Whether the ISO form keeps it is not recorded. A `TIMESTAMP` can have a
  precision of 12 digits, the member `timestampPrecision` of a parameter type
  (recorded: bigquery-236). Whether the service sends a picosecond value over REST
  is not measured. Emulator: seconds with six fixed digits, such as
  `"1704164645.123456"`, and it ignored `timestampOutputFormat`. Gemini was right
  that the default has an exponent.
- `JSON` is a string that holds JSON text, such as `"{\"a\":1}"` (recorded:
  bigquery-090, "JSON values"). The JSON value `null` arrives as the text
  `"null"`, and a SQL NULL arrives as `null`, so the two can be told apart. A
  number of 20 digits stayed exact in the text, as `"12345678901234567890"`.
- `GEOGRAPHY` is WKT text with no space after the name, such as `"POINT(1 2)"`,
  `"LINESTRING(0 0, 1 1)"` and `"POLYGON((0 0, 1 0, 1 1, 0 0))"` (recorded:
  bigquery-091, "GEOGRAPHY values"). Emulator: `"POINT (1 2)"`.
- `INTERVAL` is the text `<years>-<months> <days> <hours>:<minutes>:<seconds>`,
  such as `"0-0 1 0:0:0"`, `"1-2 3 4:5:6.789"`, `"-0-5 0 0:0:0"` and
  `"10000-0 0 0:0:0"` (recorded: bigquery-092, "INTERVAL values"). The field has
  the type `INTERVAL`, which the list of the discovery document does not name
  (recorded: bigquery-070 and bigquery-236). A sign on the years and months part
  covers both, and the days and the time can have their own signs (source: the
  Go client `ParseInterval` in `cloud.google.com/go/bigquery`, read 2026-10-10,
  not measured on the service).
- `ARRAY` is a field of mode `REPEATED` with the element type in `type`. An empty
  array and a NULL array both arrive as `[]` (recorded: bigquery-070, where the
  NULL array of the second row is `{"v":[]}`, and bigquery-144, where an empty
  array parameter is `{"v":[]}`). The service refuses a NULL element in the result
  of a query with HTTP 400, `invalidQuery` and `Array cannot have a null element;
  error in writing field withnull` (recorded: bigquery-093, "ARRAY values").
  Gemini was right. Emulator: it allowed a NULL element as `{"v":null}`. An array
  of arrays is not a type of GoogleSQL, and a `STRUCT` that holds an array works
  (recorded: bigquery-094).
- `RECORD` is a `STRUCT`, and the field has `fields` with the names and the types
  of its members. The value is `{"f":[{"v":..},..]}` in the order of the fields.
  A NULL struct arrives as `null`, and a struct of NULL members arrives as
  `{"f":[{"v":null}]}` (recorded: bigquery-096, "STRUCT values"). A struct in an
  array and a nested struct work (recorded: bigquery-095 and bigquery-097).
  GoogleSQL allows two members with the same name and members with no name
  (source: Gemini, not measured).
- `RANGE` is the text `"[2024-01-01, 2024-02-01)"`, with `UNBOUNDED` for an open
  end, such as `"[2024-01-01, UNBOUNDED)"` (recorded: bigquery-098, "RANGE
  values"). A `RANGE` of `TIMESTAMP` has the bounds in the float form with six
  fixed digits, such as `"[1704067200.000000, 1704153600.000000)"` (recorded:
  bigquery-098). The field has the `type` `RANGE` and the member
  `rangeElementType`, so a driver can learn the element type from the schema
  (recorded: bigquery-070). Emulator: the field had no `type`.
- A NULL is JSON `null` in every type (recorded: bigquery-099, "a NULL of each
  type", and bigquery-070, "a row of NULL in every type"). A NULL that has no
  type, as `SELECT NULL`, has the type `INTEGER` (recorded: bigquery-100, "a bare
  NULL").
- The types that the discovery document lists for a field are `STRING`, `BYTES`,
  `INTEGER` (or `INT64`), `FLOAT` (or `FLOAT64`), `BOOLEAN` (or `BOOL`),
  `TIMESTAMP`, `DATE`, `TIME`, `DATETIME`, `GEOGRAPHY`, `NUMERIC`, `BIGNUMERIC`,
  `JSON`, `RECORD` (or `STRUCT`) and `RANGE` (recorded: bigquery-236). A type of
  a parameter can also be `INT64`, `FLOAT64`, `BOOL`, `STRUCT` and `ARRAY`.
- The values of a column that `tables.get` describes use the same names (recorded:
  bigquery-071, "the schema of the table of every type").

The mapping is in the table below. The table was written by hand for step 8a.
Step 10 will generate it from the code.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| INTEGER | integer | `int64` | `int64` | `INTEGER` | yes |
| FLOAT | float | `float64` | `float64` | `FLOAT` | yes |
| NUMERIC | decimal | `*apd.Decimal` | `*apd.Decimal` | `NUMERIC` | yes |
| BIGNUMERIC | decimal | `*apd.Decimal` | `*apd.Decimal` | `BIGNUMERIC` | yes |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| STRING | string | `string` | `string` | `STRING` | yes |
| BYTES | binary | `[]byte` | `[]uint8` | `BYTES` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| TIME | time of day | `dbimp.LocalTime` | `dbimp.LocalTime` | `TIME` | yes |
| DATETIME | local timestamp | `dbimp.LocalDateTime` | `dbimp.LocalDateTime` | `DATETIME` | yes |
| TIMESTAMP | timestamp | `time.Time` | `time.Time` | `TIMESTAMP` | yes |
| JSON | json | `the decoded JSON value` | `interface {}` | `JSON` | yes |
| GEOGRAPHY | geometry | `string` | `string` | `GEOGRAPHY` | yes |
| INTERVAL | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL` | yes |
| ARRAY | array | `[]any` | `[]interface {}` | `ARRAY` | no |
| RECORD | tuple | `map[string]any` | `map[string]interface {}` | `RECORD` | yes |
| RANGE | range | `string` | `string` | `RANGE` | yes |
<!-- /dbimp:types -->

Notes on the mapping. D189 settled the notes that name it:

- `FLOAT` needs a reader that accepts `NaN`, `Infinity` and `-Infinity`, and the
  exponent form of Java such as `1.0E308`. `strconv.ParseFloat` reads all of
  them.
- `NUMERIC` has scale 9 at most and `BIGNUMERIC` has scale 38 at most, so both
  fit an `*apd.Decimal` with no loss. Neither fits an `int64`, so the kind
  decimal gives `*apd.Decimal` for every value, as D33 says.
- `TIMESTAMP` has microseconds, and the Go type has nanoseconds. The default text
  is a double with an exponent, and it loses the microseconds of a large value
  before it leaves the service. A driver that wants every digit must send
  `formatOptions.useInt64Timestamp` as `true` in each request and in each page
  request, and it must read the text as an integer of microseconds. The text of
  the default form must never go through a `float64` in the driver, because a
  value of 16 digits loses its last digit there. A column of 12 digits of
  precision, if the service sends one, loses its last three digits.
- `TIME` and the bounds of a `RANGE` of `TIMESTAMP` come in the text forms of this
  section, and neither follows `useInt64Timestamp` in the recordings of this run.
  Whether the bounds of a range follow it is not measured.
- `GEOGRAPHY` arrives as WKT. The kind geometry leaves the Go type to the
  decision of the driver, and the table gives `string`, as Databend does
  (`TYPES.md`).
- `RECORD` has the kind tuple in the table because a struct can have two members
  with one name and members with no name. Decided, D189: the Go value is a
  `map[string]any`, as the table shows. The names are in the schema, and the driver has no
  column type method that returns them.
- `RANGE` has no kind of its own for the bounds. The kind range gives the text
  of the server. The schema gives the element type, so a later driver can parse
  it.
- An `ARRAY` is never NULL. The service writes a NULL array as an empty array
  (recorded: bigquery-070). A NULL element cannot arrive, because the service
  refuses it (recorded: bigquery-093), so a driver need not handle one.
- `JSON` arrives as text, and the value `null` of JSON arrives as `"null"`, which
  differs from a NULL on the wire. Decided, D189 item 7 and D195 item 1: a JSON
  column is a decoded Go value, and a JSON null and a SQL NULL are both `nil`.
  A caller who must tell them apart selects `TO_JSON_STRING`, which gives the
  text `null`.
- Decided, D189: a `STRUCT` column is a `map[string]any`. A struct whose members
  repeat a name or have none makes the driver return an error that names the
  column. The default of that error is the choice of the decision, and Ken can
  change it. The note on the tuple above gives the reason that the names can
  repeat.
- Decided, D189: the driver asks for timestamps as `ISO8601_STRING`. The recording
  shows the form with a `Z` and six digits (recorded: bigquery-342), and the
  year 9999 is not recorded in that form, so the integration test must check it.
  The integer form is exact (recorded: bigquery-379) and is the fallback if the
  ISO form loses digits.

## Parameters

The server binds parameters. The request carries `queryParameters`, a list of
`{ "name", "parameterType", "parameterValue" }`, and `parameterMode`, which is
`NAMED` or `POSITIONAL` (source: the REST reference of `jobs.query`). A driver
here does not need the parser for placeholders (D34). The mode is the choice of
the driver. The hosted service checks it, as the next facts show.

- A parameter type is `{"type":"INT64"}`. A parameter of `ARRAY` has `arrayType`,
  a `STRUCT` has `structTypes` with a `name` and a `type` for each member, and a
  `RANGE` has `rangeElementType` (recorded: bigquery-236). A value is
  `{"value":"5"}`, an array is `{"arrayValues":[{"value":".."}]}`, a struct is
  `{"structValues":{"a":{"value":".."}}}` and a range is
  `{"rangeValue":{"start":{..},"end":{..}}}`. Every scalar value is text.
- `@name` and `?` both work. A named parameter has `name` in the entry, and the
  statement writes `@name`. A positional parameter has no name, and the
  statement writes `?`. The mode must agree with the entries: a positional entry
  in the mode `NAMED` answered HTTP 400, the reason `invalid` and `POSITIONAL
  query parameters cannot be used in NAMED parameter mode`, and the reverse
  answered the same form (recorded: bigquery-152 and bigquery-153). With no mode
  both worked (recorded: bigquery-131 and bigquery-132). The service allows only
  one form in a statement (source: the documentation of BigQuery, not
  measured).
- Every scalar type came back with its own type, whether named or positional:
  `INT64`, `FLOAT64`, `NUMERIC`, `BIGNUMERIC`, `BOOL`, `STRING`, `BYTES`, `DATE`,
  `TIME`, `DATETIME`, `TIMESTAMP`, `JSON`, `GEOGRAPHY` and `INTERVAL` (recorded:
  bigquery-101 to bigquery-114 for the named form, and bigquery-115 to
  bigquery-128 for the positional form). A `TIMESTAMP` parameter of
  `2024-01-02 03:04:05.123456+00:00` came back as `"1.704164645123456E9"`
  (recorded: bigquery-111). The type name `INTEGER` is also a valid parameter
  type (recorded: bigquery-139). Emulator: most positional parameters failed or
  came back as another type, and it read `?` as an `INT64`.
- Arrays, structs and ranges: a named and a positional array came back as a
  `REPEATED` `INTEGER` list, an empty array came back as `[]`, a named and a
  positional struct came back as a `RECORD`, and a named range came back as a
  `RANGE` (recorded: bigquery-142, bigquery-143, bigquery-144, bigquery-146,
  bigquery-147 and bigquery-148). An array in `IN UNNEST(@p)` worked (recorded:
  bigquery-145). Emulator 0.8.1: a named array came back as strings, and a
  positional array failed. Emulator 0.7.2: every array, struct and range was
  `null`.
- NULL: a parameter with a type and an empty `parameterValue` came back as NULL
  of that type (recorded: bigquery-133 to bigquery-135). The Go client of Google
  sends a NULL as an entry with no `value` (read of `cloud.google.com/go/bigquery`
  `params.go`, 2026-10-10). Emulator: NULL came back with the type `INTEGER`.
- `FLOAT64` `NaN` and `Infinity` are sent as the text `"NaN"` and `"Infinity"`, and
  they came back as the same text (recorded: bigquery-136 and bigquery-137). An
  `INT64` of 9223372036854775807 came back exact (recorded: bigquery-138).
- The service tests the entries. A type that does not exist, such as `INT65`,
  answered HTTP 400 and the reason `invalid` (recorded: bigquery-140). A value
  that does not fit its type, such as `notanumber` for an `INT64`, answered
  HTTP 400 and `Unparseable query parameter` (recorded: bigquery-141). A
  statement that names a parameter that the request does not hold answered HTTP
  400 and `invalidQuery` with `Query parameter 'a' not found` (recorded:
  bigquery-151). Too few positional parameters answered HTTP 400 and `Query
  parameter number 2 is not defined (1 provided)` (recorded: bigquery-150). Too
  many positional parameters were taken, and the extra one was ignored
  (recorded: bigquery-149). Emulator: it took a type that does not exist, a
  value that is not a number and a name that the statement does not use.
- A parameter cannot stand for a name of a table or a column (source: the
  documentation of BigQuery, not measured). A `LIMIT` took a named parameter
  (recorded: bigquery-158). A parameter worked in a `WHERE` clause and in an
  `INSERT` (recorded: bigquery-154 to bigquery-157, bigquery-374 and
  bigquery-375).
- The Go client of Google sends a Go `int` as `INT64`, a `float64` as `FLOAT64`, a
  `[]byte` as `BYTES`, a `time.Time` as `TIMESTAMP`, a `*big.Rat` as `NUMERIC`,
  and the civil types as `DATE`, `TIME` and `DATETIME` (read of
  `cloud.google.com/go/bigquery` `params.go`, 2026-10-10). It sends the text of a
  timestamp in the form `2006-01-02 15:04:05.999999-07:00`.
- A value that the driver sends must carry its own type, because the service
  cannot infer it from the text (source: the REST reference). The parameters of
  a `database/sql` call have only a Go type, so the driver must map each Go type
  to a parameter type, as the Go client does.

## Transactions

- The service has transactions in a script: `BEGIN TRANSACTION`, `COMMIT
  TRANSACTION` and `ROLLBACK TRANSACTION` in one request, and in a session that
  `createSession` starts and `connectionProperties` with the key `session_id`
  names on the next requests (recorded: bigquery-236, in the text of
  `createSession`).
- A script that begins a transaction, inserts a row and commits keeps the row
  (recorded: bigquery-265 and bigquery-267). A script that rolls back leaves no
  row (recorded: bigquery-266 and bigquery-267). A script with a failing
  statement inside the transaction answers HTTP 400 and leaves no row (recorded:
  bigquery-268 and bigquery-269). A block `BEGIN ... EXCEPTION WHEN ERROR THEN
  ROLLBACK TRANSACTION; END` rolls back when the statement fails (recorded:
  bigquery-270 and bigquery-271). `BEGIN` and `COMMIT` with no word `TRANSACTION`
  work (recorded: bigquery-275 and bigquery-279). Emulator: it refused `ROLLBACK
  TRANSACTION` with HTTP 400 and `Statement not supported: RollbackStatement`
  (recorded: "a transaction that rolls back"), and the row of a failing script
  stayed.
- `BEGIN TRANSACTION`, `COMMIT TRANSACTION` and `ROLLBACK TRANSACTION` alone in a
  request, with no session, answered HTTP 400 and the reason `invalidQuery` with
  `Transaction control statements are supported only in scripts or sessions`
  (recorded: bigquery-272 to bigquery-274). So a driver that sends `BEGIN` as one
  request on a plain connection fails. Emulator: it answered HTTP 200 and did
  nothing.
- A session carries a transaction across requests. The run did this in six
  requests with the id from the first answer in `connectionProperties`:
  `createSession` (recorded: bigquery-383), `BEGIN TRANSACTION` (bigquery-384), an
  `INSERT` (bigquery-385), a `SELECT` that saw the row (bigquery-386), `ROLLBACK
  TRANSACTION` (bigquery-387) and a `SELECT` that saw no row (bigquery-388). A
  `SELECT` with no session saw no row (recorded: bigquery-389). `CALL
  BQ.ABORT_SESSION()` ended the session (recorded: bigquery-390). Every answer in
  the session carries `sessionInfo.sessionId`, and the statement types are
  `BEGIN_TRANSACTION`, `ROLLBACK_TRANSACTION` and `COMMIT_TRANSACTION`. A request
  with a `session_id`
  that does not exist answered HTTP 400 and `Invalid input session id.`
  (recorded: bigquery-277). The run kept the id with the capture rule of the
  recorder, which a driver does with a variable. A `COMMIT` in a second session kept the row
  and a `SELECT` outside the session saw it (recorded: bigquery-411 to
  bigquery-415). The service ends an idle session by itself (source: the documentation of
  BigQuery, not measured).
- The isolation of a transaction, the effect of a session on the cache and the
  lifetime of a session are not measured. The documentation says that a multi
  statement transaction lasts for one script or one session (source: Gemini, not
  measured). The Go driver of `usql` returns a transaction whose `Commit` and
  `Rollback` do nothing (see Faults).
- Decided, D189: `BeginTx` returns the error of D20 for the first release. A
  transaction in one script request still works, because the driver sends the text
  as is. A session across requests works on the service, as the recordings show,
  and a later release can add it.

## Errors

- The error body is `{"error":{"code","message","errors":[{"message","domain",
  "reason","location","locationType"}],"status"}}` (recorded: bigquery-013,
  bigquery-181 and bigquery-182). `domain` is `global` and `status` is a name such
  as `INVALID_ARGUMENT`, `NOT_FOUND`, `PERMISSION_DENIED` or `ALREADY_EXISTS`.
  `location` and `locationType` are in an error of the text of the query only, as
  `q` and `parameter`. Emulator: the shape was the same minus `status`, and the
  member `debugInfo` was in each error.
- The reasons that the hosted service answered, with the HTTP code (recorded):
  - `invalidQuery`, 400: an error in the text of a query or in its values. A
    syntax error, a column that does not exist, a type that does not match, a
    division by zero, a call of `ERROR` and an overflow (recorded: bigquery-181,
    bigquery-184, bigquery-185, bigquery-186, bigquery-189 and bigquery-188), a
    parameter that the statement does not find (recorded: bigquery-151), an
    `INSERT` of a wrong type or with the wrong number of values (recorded:
    bigquery-197 and bigquery-198), a `NULL` element in an array (recorded:
    bigquery-093) and a failed `ASSERT` (recorded: bigquery-356).
  - `required`, 401, with `UNAUTHENTICATED`: a request with no `Authorization`
    header (recorded: bigquery-219).
  - `authError`, 401, with `UNAUTHENTICATED`: a token that is wholly wrong
    (recorded: bigquery-420 and bigquery-421).
  - `bytesBilledLimitExceeded`, 400: a scan that bills more than
    `maximumBytesBilled` (recorded: bigquery-204).
  - `stopped`, 499, with `CANCELLED`: the `getQueryResults` of a job that was
    cancelled. The same reason is in `status.errorResult` of `jobs.get` with HTTP
    200 (recorded: bigquery-212 and bigquery-211).
  - `notFound`, 404: a table that a query names and that does not exist, a
    table of the tables API, a dataset, a job and a project in the datasets API
    (recorded: bigquery-182, bigquery-053, bigquery-056, bigquery-175 and
    bigquery-224), and an external file (recorded: bigquery-305). A path that does
    not exist answered an HTML page (recorded: bigquery-195).
  - `accessDenied`, 403, with `PERMISSION_DENIED`: the account lacks a right, and
    the message names it, such as `bigquery.datasets.get`, `bigquery.jobs.create`,
    `bigquery.jobs.update`, `bigquery.datasets.create`, `bigquery.datasets.delete`
    and `bigquery.tables.deleteSnapshot` (recorded: bigquery-059, bigquery-194,
    bigquery-214, bigquery-359, bigquery-441 and bigquery-450). A query on a
    dataset that does not exist answered this reason and the message `or perhaps
    it does not exist` (recorded: bigquery-183). A driver cannot tell a missing
    dataset from a missing right.
  - `duplicate`, 409, with `ALREADY_EXISTS`: a table, a clone, a snapshot, a
    routine or a job id that exists (recorded: bigquery-199, bigquery-041,
    bigquery-299, bigquery-300, bigquery-301 and bigquery-303). A `CREATE
    PROCEDURE` of a procedure that exists answered `invalidQuery` with `Already
    Exists: Routine` and HTTP 400, so the reason does not tell a duplicate there
    (recorded: bigquery-304).
  - `invalid`, 400: a value of the request that is not valid, such as a type of a
    parameter, a mode that does not match, a negative `timeoutMs`, a page token
    that is not valid, a session id that is not valid, a reservation that is not
    enabled and a drop of the wrong kind of table (recorded: bigquery-140,
    bigquery-152, bigquery-206, bigquery-173, bigquery-277, bigquery-333 and
    bigquery-438).
  - `required`, 400: no `query` in the body, or an empty body (recorded:
    bigquery-190 and bigquery-192). `parseError`, 400: a body that is not JSON
    (recorded: bigquery-191). `badRequest`, 400: a key that is not valid, a
    project that cannot be parsed and a gzip body that is not gzip (recorded:
    bigquery-221, bigquery-223 and bigquery-364). `forbidden`, 403: a quota
    project that the account cannot use (recorded: bigquery-222).
  - The reason `jobInternalError` never came, and no 5xx answer came.
  Emulator: every error of a query was HTTP 400 and `jobInternalError`, a missing
  table was 400, and an unknown path was HTTP 500 `internalError`.
- The other reasons of the table of Google, with their HTTP codes (source: the
  document of error messages, 2026-10-10): `rateLimitExceeded` 403 and 429,
  `backendError` 500 502 503 and 504, `internalError` 500, `jobRateLimitExceeded`
  400, `jobBackendError` 400, `quotaExceeded` 403, `stopped` 200,
  `responseTooLarge` 403, `billingNotEnabled` 403 and `jobInternalError` 400. The
  document says to retry `rateLimitExceeded`, `backendError`, `internalError`,
  `jobRateLimitExceeded`, `jobBackendError` and `jobInternalError`, with backoff.
  Not measured, because no run of 4 minutes and 44 seconds meets them. The Go
  client of Google retries `backendError`, `rateLimitExceeded`,
  `jobRateLimitExceeded` and `internalError` (read of `bigquery.go`,
  `jobRetryReasons`, 2026-10-10).
- The message of an error in the text of a query has a position, such as `at
  [1:8]` (recorded: bigquery-184). A driver can show the message as it is.
- A body that is not JSON answered HTTP 400 and `parseError` with the message of
  the parser, which has a caret under the position (recorded: bigquery-191). An
  empty query and a body with no `query` answered HTTP 400 and `required`
  (recorded: bigquery-190 and bigquery-192). A body with `query` that is a number
  was read as the text `5` and answered a syntax error (recorded: bigquery-193).
- An error with HTTP 200: a job that `jobs.insert` makes with a statement that
  fails answers HTTP 200 with the job, and `status.errorResult` and
  `status.errors` hold the error (recorded: bigquery-336). A later `jobs.get`
  shows the same `errorResult` (recorded: bigquery-337), and `getQueryResults`
  answers the HTTP 400 error (recorded: bigquery-338). The `jobs.query` answer
  never had HTTP 200 with an error.
- An error after some rows: none came. An error comes before any row, because the
  service runs the whole statement before it answers (recorded: bigquery-187,
  where the error is in the row 1500 of 3000 and no row came). A result that was
  complete cannot fail on a later page, because the service reads the pages from
  the stored result. A job that `jobs.query` left running (`jobComplete: false`)
  can fail at `getQueryResults`, as a cancelled job does with HTTP 499 and the
  reason `stopped` (recorded: bigquery-212), and as a job of `jobs.insert` that
  failed does with HTTP 400 (recorded: bigquery-338).
- A script whose second statement fails after an `INSERT`, with no transaction,
  answered HTTP 400, and the inserted row stayed (recorded: bigquery-248 and
  bigquery-249). Emulator: the row was not kept.
- A create of a table with a name of two parts, `dataset.table`, worked on the
  service (recorded: bigquery-200). Emulator: it answered HTTP 400 `unexpected
  table name path`, and on 0.8.1 the failed statement still left a table of that
  name in `INFORMATION_SCHEMA` (measured, 2026-10-10, by hand, and not kept in a
  recording).
- `DROP TABLE IF EXISTS` for a table that does not exist answered HTTP 200 (recorded:
  bigquery-050). `DROP TABLE` of a table that does not exist answered HTTP 404
  `notFound` (recorded: bigquery-203). `CREATE OR REPLACE TABLE` worked for a
  table that exists (recorded: bigquery-287). `DROP TABLE` of a snapshot answered
  HTTP 400 and `invalid` with `Cannot drop ... which has type SNAPSHOT. A table
  was expected.` (recorded: bigquery-438). `DROP SNAPSHOT TABLE` answered HTTP
  403 `accessDenied` for the account (recorded: bigquery-450). Emulator: `CREATE OR REPLACE TABLE`
  answered `duplicate`, and `DROP TABLE IF EXISTS` failed on 0.7.2.
- A limit on the rate of requests: none came in 418 requests over 4 minutes and
  44 seconds (recorded: bigquery-001 to bigquery-452). The service answers HTTP
  403 or 429 with the reason `rateLimitExceeded` (source: the document of error
  messages).
- An error that means that the request did not reach the server: a failure to
  connect. HTTP 5xx with a reason of `backendError` or `internalError` can mean
  that the server did not run the job, and the service document says that the
  client can retry it (source: the document of error messages). A request with a
  `requestId` is safe to repeat for a statement that changes data: the second
  `INSERT` did not insert again (recorded: bigquery-380 to bigquery-382).

## Cancellation and timeouts

- A job that runs longer than `timeoutMs` leaves `jobs.query` with `jobComplete:
  false`. The run started a heavy count with `useQueryCache` of `false`, and the
  service answered after the timeout with `jobComplete` `false` and the
  `jobReference` (recorded: bigquery-215 with `timeoutMs` of 1, and bigquery-216
  with `timeoutMs` of 100). `getQueryResults` of that job answered `jobComplete`
  `false` again at once (recorded: bigquery-217). A negative `timeoutMs` answered
  HTTP 400 and `Value out of range` (recorded: bigquery-206). A cheap or cached
  query ignores `timeoutMs` and answers `jobComplete: true` (recorded:
  bigquery-205). The documents say that the service waits up to `timeoutMs`,
  which is 10 seconds by default, and that `getQueryResults` returns after about
  200 seconds at most, even if the query is not done (recorded: bigquery-236, in
  the text of `timeoutMs`). The poll that ends with `jobComplete` `true` is not
  recorded. When `jobComplete` is `false`, `totalRows` is not available (recorded:
  bigquery-236, in the text of `jobComplete`). Emulator: `jobComplete` was always
  `true`.
- `jobs.insert` of the same heavy query answered `state` `RUNNING`, and `jobs.get`
  answered `RUNNING` (recorded: bigquery-209 and bigquery-208). `jobs.cancel`
  answered HTTP 200 with a `jobCancelResponse` whose job was still `RUNNING`
  (recorded: bigquery-210). The next `jobs.get` still answered `RUNNING` (recorded: bigquery-211), and
  `getQueryResults` of the job answered HTTP 499 with the status `CANCELLED`, the
  reason `stopped` and `Job execution was cancelled: User requested cancellation`
  (recorded: bigquery-212). So a cancel is
  asynchronous: the answer comes first, and the job stops a little later. The
  service says the same, and that a cancelled job can still cost money (source:
  the REST reference of `jobs.cancel`, 2026-10-10).
- `jobs.cancel` of a job that had ended answered HTTP 200 (recorded:
  bigquery-213). A cancel of a job that does not exist answered HTTP 403
  `accessDenied` and `Permission bigquery.jobs.update denied on job ... (or it may
  not exist)` (recorded: bigquery-214). Emulator: it answered HTTP 200 with `DONE`
  for a job that was running, it never stopped the query, it always reported
  `DONE`, even four seconds into a query (recorded: "the slow job while it
  runs"), and it answered HTTP 404 for a job that does not exist.
- The jobs of bigquery-215 and bigquery-216 were not cancelled, so they ran on
  the service and cost money. The jobs of a script need a teardown that cancels
  them.
- What the service does when the client leaves is not measured. The request "a
  slow query that the client gives up on" left no file in this pass, because the
  recorder records no answer for a request that the client abandons. A query of
  BigQuery keeps running after the client leaves, and its job can be read with
  `jobs.get` (source: Gemini and the REST reference, not measured). The next
  requests answered normally (recorded: bigquery-376 to bigquery-378). The job
  reference of the first answer is what a driver needs to cancel. Emulator: when
  the client left, the query stopped (measured, 2026-10-10, with `dbrun logs`,
  `failed to scan rows: sqlite3: interrupted`), and on 0.7.2 the server then
  answered HTTP 500 `sql: connection is already closed` to every later request in
  the first run of the script, when the next request came at once. A second run,
  with a wait of three seconds, did not show it, so the fault is not proven and no
  recording keeps it. The next requests answered normally on 0.8.1 (recorded: "a
  query after the client gave up", "a second query after the client gave up").
- `maximumBytesBilled` of 1 on a scan with the cache off failed the query with
  the reason `bytesBilledLimitExceeded` (recorded: bigquery-204). `jobTimeoutMs`
  and `maxSlots` are members of the request (recorded: bigquery-236), and no
  request of the run sent them. Emulator: it ignored `maximumBytesBilled`.

## Statements

- Several statements in one request: the service ran every statement of the
  script and answered the result of the last one, with the `statementType`
  `SCRIPT` (recorded: bigquery-238, bigquery-239, bigquery-246 and bigquery-247,
  "two selects in one request", "two selects with a trailing semicolon", "an
  insert and a select" and "a select and an insert"). A script whose last
  statement has no rows answered no schema and no rows (recorded: bigquery-247 and
  bigquery-261). A script with a syntax error in its first statement answered HTTP
  400 and ran no statement (recorded: bigquery-250). A script with a statement that fails after
  one that changed data kept the change when it had no transaction (recorded:
  bigquery-248 and bigquery-249). The `numChildJobs` of a script and its child
  jobs are not recorded: the request that lists the children named a job that was
  not a script (recorded: bigquery-345). Emulator: the same rules, except that
  the failed script kept no row.
- A trailing semicolon and a semicolon in a string or in a comment worked
  (recorded: bigquery-240, bigquery-256 and bigquery-257). An empty statement
  between two semicolons answered HTTP 400 `Unexpected ";"` (recorded:
  bigquery-259). A statement of only a comment answered HTTP 400 (recorded:
  bigquery-260). A block comment, a `--` comment and a `#` comment worked
  (recorded: bigquery-254, bigquery-255 and bigquery-258). Emulator: the empty
  statement worked.
- `DECLARE` and `SET` worked inside one script (recorded: bigquery-241 and
  bigquery-242). A variable of an earlier script did not stay: the name worked as a
  column name and as a table alias in later requests (recorded: bigquery-243 and
  bigquery-244). Emulator: a variable stayed, and its value replaced the name in
  later requests, so `SELECT 1 AS dbimp_v` failed with `Unexpected integer
  literal "6"` (recorded: "the name of a variable of an earlier script used as a
  column name"). The first run of this work met the fault with the name `x`, and
  every later query that used `x` failed until the container was removed
  (measured, 2026-10-10). The service has no such fault.
- `IF` and `WHILE` ran and sent their rows (recorded: bigquery-251 and
  bigquery-252). A temporary table worked inside a script (recorded:
  bigquery-253). A temporary function worked (recorded: bigquery-302). `EXECUTE
  IMMEDIATE` worked (recorded: bigquery-354). `@@row_count` answered the count of
  the last statement (recorded: bigquery-341). `ASSERT 1 = 2 AS '...'` failed with
  HTTP 400 and the text of the assertion (recorded: bigquery-356).
- `WITH`, `WITH RECURSIVE`, window functions, `QUALIFY`, `SELECT * EXCEPT`,
  `SELECT * REPLACE`, `UNNEST`, `TABLESAMPLE`, `SAFE.` and `SAFE_` forms and the
  pipe syntax worked (recorded: bigquery-325, bigquery-326, bigquery-324,
  bigquery-317, bigquery-318, bigquery-319, bigquery-320, bigquery-323,
  bigquery-321 and bigquery-322). A wildcard table over two like tables
  answered HTTP 200 and the count of their rows, which is 0 (recorded:
  bigquery-393). The first request for a wildcard table had a second backtick by
  mistake (recorded: bigquery-315). `FOR SYSTEM_TIME AS OF` worked (recorded:
  bigquery-316). Emulator: `FOR SYSTEM_TIME AS OF` failed.
- `UPDATE` and `DELETE` with no `WHERE` clause are refused with HTTP 400 and the
  message `UPDATE must have a WHERE clause at [1:1]` (recorded: bigquery-027 and
  bigquery-368). `MERGE` worked with a table and with a query as the source
  (recorded: bigquery-029 and bigquery-366). `TRUNCATE TABLE` worked (recorded:
  bigquery-030). Emulator: `MERGE` with a query as the source failed.
- `EXPORT DATA` to the bucket answered `EXPORT_DATA`, and `LOAD DATA` from the
  exported files answered `LOAD_DATA` and loaded rows (recorded: bigquery-405 and
  bigquery-407). The load read 1 row of 2, because the export wrote no header and
  the load skipped the first line (recorded: bigquery-408). With a bucket that does
  not exist they answered HTTP 404 and `Not found: Files
  gs://nosuch/x000000000000` and `Not found: URI gs://nosuch` (recorded:
  bigquery-313 and bigquery-314). Emulator: `EXPORT DATA` failed for lack of
  credentials on 0.8.1 and succeeded on 0.7.2 with the rows of the query and no
  export, and `LOAD DATA` answered HTTP 200 and loaded nothing.
- DDL that worked on the service: `CREATE TABLE`, `CREATE TABLE ... AS SELECT`,
  `CREATE OR REPLACE TABLE`, `PRIMARY KEY ... NOT ENFORCED`, `DEFAULT`,
  `PARTITION BY`, `CLUSTER BY`, `CREATE VIEW`, `CREATE MATERIALIZED VIEW`,
  `CREATE TABLE ... CLONE`, `CREATE SNAPSHOT TABLE`, `ALTER TABLE ADD COLUMN`,
  `RENAME TO` and `DROP COLUMN` (recorded: bigquery-024, bigquery-286,
  bigquery-287, bigquery-288, bigquery-291, bigquery-294, bigquery-296,
  bigquery-308, bigquery-309 and bigquery-311), and `CREATE TABLE` with a `FOREIGN
  KEY ... NOT ENFORCED` (recorded: bigquery-289). The clone, the function, the table
  function and the procedure were created in this pass (recorded: bigquery-299,
  bigquery-301, bigquery-303 and bigquery-304). The snapshot exists:
  `CREATE SNAPSHOT TABLE` answered HTTP 409 `duplicate` for a snapshot of an
  earlier pass, and `INFORMATION_SCHEMA` lists two as `SNAPSHOT` (recorded:
  bigquery-300 and bigquery-060). A `CALL` of the procedure ran and answered `1`
  (recorded: bigquery-355). A view that the script made had no rows, because its
  table had none (recorded: bigquery-295). `UNIQUE` is not a constraint of
  BigQuery: the service refused it with a syntax error (recorded: bigquery-290).
  `CREATE SEARCH INDEX` on a text column worked and `SEARCH()` found the row
  (recorded: bigquery-402 and bigquery-404). `CREATE VECTOR INDEX` was refused
  because the table has fewer than 5000 rows (recorded: bigquery-403). `CREATE
  SCHEMA`, `DROP SCHEMA ... CASCADE` and `datasets.insert` worked (recorded:
  bigquery-394, bigquery-398 and bigquery-397), and a `GRANT` on a dataset that
  does not exist answered HTTP 403 (recorded: bigquery-357). `CREATE EXTERNAL
  TABLE` over the exported files worked and a `SELECT` from it answered the row
  (recorded: bigquery-409 and bigquery-410).
- `INFORMATION_SCHEMA`: `<dataset>.INFORMATION_SCHEMA.TABLES`, `COLUMNS`,
  `TABLE_OPTIONS`, `VIEWS`, `ROUTINES`, `KEY_COLUMN_USAGE` and
  `COLUMN_FIELD_PATHS` answered (recorded: bigquery-060 to bigquery-063,
  bigquery-065, bigquery-066 and bigquery-358), with the `table_type` `BASE TABLE`, `CLONE` and `SNAPSHOT`, the
  keys of the constraints (`pk$`) in `KEY_COLUMN_USAGE` (recorded: bigquery-066),
  and the type names of GoogleSQL in `data_type`, such as `INT64` (recorded:
  bigquery-060 and bigquery-061). The level of the project, `SCHEMATA` and `JOBS`,
  answered HTTP 403 (recorded: bigquery-059 and bigquery-064), and `SCHEMATA` of
  the region answered HTTP 403 too (recorded: bigquery-396). `JOBS_BY_PROJECT` of
  the region `region-us` answered the jobs (recorded: bigquery-399). Emulator:
  `SCHEMATA` answered the dataset, and `VIEWS`, `ROUTINES`, `JOBS` and
  `KEY_COLUMN_USAGE` did not exist.
- The REST API lists tables and datasets. `tables.list` answered
  `{"tables":[...]}` with `nextPageToken` when `maxResults` cut it, and
  `datasets.list` answered a list of `datasetReference` (recorded: bigquery-051,
  bigquery-054 and bigquery-180). `tables.get` answered the schema and `numRows`
  (recorded: bigquery-052). The list of datasets did not grow after the jobs of
  `jobs.insert` (recorded: bigquery-227). The result of such a job goes to a
  hidden dataset whose name starts with an underscore (recorded: bigquery-373, in
  `destinationTable`). Emulator: it kept a visible dataset for each job that
  `jobs.insert` made.
- `tabledata.insertAll` answered HTTP 200 with `insertErrors` for a row with an
  unknown field (recorded: bigquery-361). The rows that the streaming insert of
  bigquery-418 wrote are not read back by a query in this pass. `tables.insert` answered HTTP 409 for a table that
  existed (recorded: bigquery-360), and `tables.delete` answered HTTP 204 (recorded: bigquery-363).

## Principals

- The hosted run has one principal, the service account (recorded: bigquery-231,
  where `SESSION_USER()` answers its email). The text of `noOrdinaryUser` in the
  manifest says that no ordinary user exists for this work, and the hosted
  entries carry the principal `administrator`. No hosted request ran as a second
  user, so no ordinary user is recorded.
- The rights of the account show in the refusals and the successes. It can run a
  query (`bigquery.jobs.create`), read and write the tables of the dataset, create
  routines, clones and datasets, and drop a dataset that it made (recorded:
  bigquery-394, bigquery-397, bigquery-398 and bigquery-446). It reads
  `JOBS_BY_PROJECT` (recorded: bigquery-399), and it writes and reads the bucket
  (recorded: bigquery-405 and bigquery-407). It cannot read `SCHEMATA` of the
  project or of the region (`bigquery.datasets.get` at the dataset level, recorded:
  bigquery-059 and bigquery-396), it cannot read `JOBS` of the project (recorded:
  bigquery-064), it cannot update a job that it did not make or that does not
  exist (`bigquery.jobs.update`, recorded: bigquery-214), it cannot use another
  project (`bigquery.jobs.create` in that project, recorded: bigquery-194), and it
  cannot delete a snapshot (`bigquery.tables.deleteSnapshot`, recorded:
  bigquery-450). A drop of a dataset that does not exist answers HTTP 403 with
  `bigquery.datasets.delete` (recorded: bigquery-441 and bigquery-447), so a
  refusal does not tell a missing dataset from a missing right. The roles of the
  account are not recorded. The right that would settle the snapshots is
  `bigquery.tables.deleteSnapshot`.
- The version: the service has no statement for it. `SELECT @@version` failed with
  `Unrecognized name: @@version` (recorded: bigquery-229). The discovery document
  names the revision `20260922` of the REST API on the service (recorded:
  bigquery-236), and the emulator serves `20250928`. The service has no version
  that a client reads. `usql` registers no `Version` statement for BigQuery (read
  of `usql/drivers/bigquery/bigquery.go`, 2026-10-10). Emulator: `SELECT
  SESSION_USER()` answered `dummy`.
- The version for `usql` (step 16). `usql` runs no statement for the version of
  BigQuery, because its driver registers no `Version` function. So no statement ran
  as the administrator or as the ordinary user, and the one login has no ordinary
  user. The nearest statement that a person can type, `SELECT @@version`, is
  refused for the one login (recorded: bigquery-229). The driver answers no
  `SELECT version()` of its own, because D181 gives that answer only to a product
  that has no way to read the release, and this driver has no endpoint that
  carries a release either. So the statement goes to the service and fails there.
  `TestReplayNoVersion` holds that the driver sends it.
- `GET /bigquery/v2/projects/{project}/serviceAccount` answered the email of the
  service account of the encryption of the project (recorded: bigquery-237).
  `GET /` answered HTTP 404 `notFound` (recorded: bigquery-235). Emulator: `{}` and
  HTTP 500.
- A ping that costs little: `GET /bigquery/v2/projects/{project}/datasets/{dataset}`
  answered the dataset (recorded: bigquery-234). The `usql` driver does the same
  (read of `driver/connection.go` in `gorm.io/driver/bigquery` v1.2.1, `Ping`,
  2026-10-10). The ping needs the right `bigquery.datasets.get` on the dataset.
- The only test of the identity that the emulator makes is the project: a path
  with a project other than `dbmeta` answered HTTP 404 and `project <name> is not
  found`. A header `X-Goog-User-Project` and a query key `key` changed nothing
  there (recorded: "a request for a project that is not this one", "a request with
  a header of the project of a quota", "a request with an API key that is wrong").

## Flavors

BigQuery has one interface, and no other product speaks it in a way that this work
found. The hosted service and the two releases of the community emulator differ,
as the section Hosted and emulator shows. The two releases of the emulator differ
in these places (recorded: every request that is in both releases):

- 0.7.2 does not know the job id of a `jobs.query` answer in `jobs.get` and in
  `getQueryResults`. 0.8.1 knows it.
- 0.7.2 answers every array, struct and range parameter as `null`. 0.8.1 answers a
  named array as a list of strings and a named struct as JSON text, and fails on a
  positional one.
- 0.7.2 does not echo `location` in `jobReference`. 0.8.1 does.
- 0.7.2 fails `DROP TABLE IF EXISTS` for a table that does not exist. 0.8.1 does
  not.
- 0.7.2 answers `EXPORT DATA` with the rows of the query. 0.8.1 fails it for lack of
  credentials.
- 0.7.2 writes `totalBytesBilled` and `totalBytesProcessed` in the statistics of
  `jobs.list`. 0.8.1 does not.
- The message of a failed `MERGE` with a query as the source differs.

The driver cannot tell the releases apart from a version statement, because the
emulator has none (see Open questions).

## Interfaces

`TestTables` makes the table below from the types of the driver, and it fails when
the document holds another table. Run it with `DBIMP_UPDATE=1` to write the table.

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares, and the access token that the driver gets by signing a request with the key file of the DSN (D189). |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1 as a dry run, which checks the token, the project and the right to make a job, and which the service does not bill (D189). |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because each request is its own job with no session (D189). |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, and the values that the driver binds with a type of their own: a decimal, a dbimp.Date, a dbimp.LocalTime, a dbimp.LocalDateTime, a dbimp.Interval, and a list and a map that fail with dbimp.ErrArguments (D189). |
| `driver.QueryerContext` | yes | The statement goes to POST /queries with its arguments as typed parameters, which the server binds (D189). |
| `driver.ExecerContext` | yes | Exec reads the head of the answer and no row, and RowsAffected is numDmlAffectedRows, or an error that wraps dbimp.ErrNotSupported when the answer has no count (D178). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because the service refuses BEGIN TRANSACTION alone and the first release has no session across requests (D20 and D189). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement or one script, and the answer holds the result of its last statement, so it has one result. The driver reads the pages of one result as one set of rows (D189). |
| `driver.RowsColumnTypeScanType` | yes | The schema names the type of each field, and each type has one Go type (D135 and D189). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The type of the field in upper case, such as INTEGER, and ARRAY for a field of the mode REPEATED. |
| `driver.RowsColumnTypeLength` | no | The schema names no length for a field (recorded: bigquery-070). |
| `driver.RowsColumnTypeNullable` | yes | The mode of the field: an ARRAY is never NULL, and a REQUIRED field is not NULL. |
| `driver.RowsColumnTypePrecisionScale` | no | The schema names no precision and no scale for a field (recorded: bigquery-070). |
<!-- /dbimp:interfaces -->

## Faults

The faults of `gorm.io/driver/bigquery` v1.2.1 and of the Go client that it uses,
which this driver must not repeat (read of the source in the module cache,
2026-10-10):

- `Begin` returns a transaction whose `Commit` and `Rollback` return `nil` and do
  nothing. A caller believes that its statements are atomic.
- `QueryContext` calls `query.Read(context.Background())`, so the context of the
  caller does not stop the query. `Rows.Close` does nothing and does not release
  the iterator. The connection keeps a `context.Background()` that it made at
  `Open`.
- `RowsAffected` returns `RowIterator.TotalRows`, which is the count of the rows of
  a result and not the count of the rows that a DML statement changed.
  `LastInsertId` returns an error.
- `Statement.NumInput` returns 0.
- `Conn.Query` returns `nil, nil` when `Prepare` fails. The branch cannot happen
  today.
- A struct or an array in a row goes through a special statement
  `adaptor.RerouteQuery` and a type `bigQueryReroutedColumn`, which a plain
  `database/sql` caller cannot scan.
- `Close` returns `driver.ErrBadConn` when a flag `bad` is set. No code sets it.
- `Open` takes the configuration from the DSN and from the file
  `credential_file` and the key `credential_json`, which holds the key as base64
  text in the URL. A key in a URL leaks in logs.
- The Go client switches to the Storage Read API when a result has a `pageToken` and
  the client has the Storage client enabled. That path needs Arrow, which is the
  cost that `usql` lists (read of `query.go`, `Read`).
- The Go client sets `useInt64Timestamp` to `true`, `useLegacySql` to `false` and
  a random `requestId` for every query, and it retries the reasons `backendError`,
  `rateLimitExceeded`, `jobRateLimitExceeded` and `internalError` with a backoff
  of 1 second that doubles to 32 seconds (read of `query.go`, `probeFastPath`, and
  `bigquery.go`, `runWithRetryExplicit`).
- `usql` has no `Version` statement for BigQuery, and no hook (W50).
- The emulator, as a test double, has the faults that the sections above list. The
  main ones are the ignored members of the request, the error reason
  `jobInternalError` for every query error, the positional parameters, the leak of
  a script variable, and the lost state of `DROP`.

## Integration tests

The integration tests of the driver read `BIGQUERY_DSN` and skip when it is empty
(hard rule 9). The variable holds the DSN of D189. For the service it names the
project, the dataset that holds the tables of the tests, and the key file:

    BIGQUERY_DSN='bigquery://PROJECT/DATASET?credential_file=/path/key.json'

For the emulator that `dbrun` starts, it is the `url` that `dbrun` prints:

    (cd ../dbmeta/test && go run ./cmd/dbrun start bigquery-0.8.1)
    export BIGQUERY_DSN=$(cd ../dbmeta/test && go run ./cmd/dbrun dsn --json bigquery-0.8.1 | jq -r '.[0].url')
    go test -race -count=1 -run Integration -v ./bigquery/...

Two tests need a bucket of Cloud Storage that the account can read and write, and
they skip when `BIGQUERY_BUCKET` holds no name. The name has no `gs://`.

The service account needs the rights that the first section lists: run queries,
make and drop tables, routines, clones and datasets, read `JOBS_BY_PROJECT`, and
read and write the bucket. It cannot drop a snapshot, so the test of a snapshot
logs the refusal of the drop, and a person with `bigquery.tables.deleteSnapshot`
drops the snapshot. `TestMain` drops each table, view and clone of the run that a
test left, and it fails if it could not drop one.

The login is one principal, a service account, and the emulator has one login with
no password, so each test runs as that principal only, and the manifest has no
ordinary user. A test that needs the service and not the emulator skips on the
emulator with the reason from the section Hosted and emulator. The emulator
supports only the release `bigquery-0.8.1`, and the driver does not support
`bigquery-0.7.2` (D189 item 4), so CI must not run the integration tests on that
release (open question 2).

The tests were written on 2026-10-10 with no project and no emulator. On
2026-10-11 the main session ran them: the hosted suite passes, and the emulator
suite passes on `bigquery-0.8.1` with skips that name the difference of the
emulator. The test of a snapshot leaves a snapshot that the account cannot drop,
because it lacks `bigquery.tables.deleteSnapshot` and cannot set the expiry of a
snapshot. The snapshot expires after 7 days. The tests and what each one holds:

- `TestIntegrationConnect`: the login, `Ping`, the principal, the project, the
  refusal of `SELECT @@version`, and the options `WithDatabase` and
  `WithMaxResults`.
- `TestIntegrationErrors`: an error before any row for a syntax error, a table that
  does not exist, an overflow, a division by zero halfway, an array with a NULL
  and a failed assertion, with the reasons of the service, a wrong token with the
  reason `authError`, and a dataset that does not exist with the reason
  `accessDenied`.
- `TestIntegrationTransactions`: `BeginTx` and the refusal of `BEGIN TRANSACTION`
  alone.
- `TestIntegrationContext`: a deadline that ends while a heavy job runs, the cancel
  of the job, and the reason `stopped` that `JOBS_BY_PROJECT` then shows.
- `TestIntegrationParameters`: each Go type that the driver binds, as a named
  parameter and as a positional one, a NULL cast in the statement, and the
  refusal of a list and of a mix.
- `TestIntegrationTimestampForm`: the two ends of the range of a `TIMESTAMP` in the
  ISO form, which is the check that D189 item 5 asks for.
- `TestIntegrationCRUD`, `TestIntegrationSchema` and `TestIntegrationFeatures`: the
  entries of `features.json`, in the order that the file names them. The requests
  that the driver never sends go by hand with the token of the connector:
  `jobs.insert`, `jobs.get`, `jobs.cancel`, `tabledata.insertAll`, and a body that
  is gzip.
- `TestIntegrationRoundTrip`: every type that `features.json` marks yes, with
  `dbimptest.RoundTrip`, as a bound argument and as a literal. Each statement names
  its arguments, so that it can use a value more than once. A JSON document, a
  STRUCT, an ARRAY, a RANGE and a GEOGRAPHY go in as text that the statement
  parses, because they have no Go type to bind. A NULL has no type, so the
  statement casts each value. The emulator runs five types, and skips the others
  with a reason.

## Second opinions

- Gemini (`gemini-3.1-pro-preview`, 2026-10-10) was asked the four questions of
  step 5a. It named the statements of CRUD, the schema features, the features and the
  types that `features.json` holds. It said that `UPDATE` and `DELETE` need a
  `WHERE` clause, which the emulator confirmed (recorded: "an update with no where
  clause"). It said that a `PRIMARY KEY` and a `FOREIGN KEY` are `NOT ENFORCED`,
  which the emulator showed for the key (recorded: "a create table with a primary
  key") and refused for the foreign key (recorded: "a create table with a foreign
  key"). It said that a `FLOAT64`, a `BOOL` and an `INT64` are strings, and that a
  `DATETIME` is `YYYY-MM-DD HH:MM:SS.mmmmmm` with a space. The server showed that
  all three are strings and that the `DATETIME` has a `T`.
- DeepSeek (`deepseek-v4-flash`, 2026-10-10) was asked the same four questions as
  the second model. It gave the same operations, and it listed `GRANT`, `REVOKE`,
  `CALL` and `EXECUTE IMMEDIATE`. It said that a `FLOAT64` is a JSON number and a
  `BOOL` is a JSON boolean, and that a `DATETIME` has a space, which the server
  showed to be wrong. It said that `STRING` and `BYTES` are at most 16 MB, which is
  not measured. `deepseek-v4-pro`, `kimi-k3`, `kimi-k2.7-code` and `qwen-max` timed
  out on every try on 2026-10-10, and `qwen` timed out too, so they gave no answer.
- The Go client `cloud.google.com/go/bigquery` v1.83.0 and the driver
  `gorm.io/driver/bigquery` v1.2.1 were read on 2026-10-10 in the module cache. The
  Python client and the JDBC driver were not read, so the survey has no client in
  another language. The REST reference and the discovery document stand in for it.
- Gemini (2026-10-10) was asked what step 6 did not find on the emulator. The
  hosted run came later, and the sections above say what the service showed. Its
  leads, and what the emulator said:
  - A query that takes longer than `timeoutMs` answers `jobComplete: false`, and the
    client must poll. Not measured, because the emulator never answers it.
  - A DML statement gives `numDmlAffectedRows` and `dmlStats`. The emulator gives
    neither (recorded: "an update and its answer", "lead: the insert job with its
    statistics").
  - The reason of an error decides if the driver retries. The emulator gives
    `jobInternalError` for every error, and the service uses that reason for a
    retry (see Errors).
  - A failed job has `errorResult` with no rows. True for a job of `jobs.insert`
    (recorded: "lead: a failing job inserted with jobs.insert"). The later `jobs.get`
    did not keep it.
  - A session answers `sessionInfo.sessionId`, and `connectionProperties` with
    `session_id` joins it. The emulator answered neither (recorded: "a request that
    creates a session").
  - A script has child jobs, listed by `jobs.list` with `parentJobId`. The emulator
    answered the list with every job and no children (recorded: "lead: the child
    jobs of a job", "the job of a script").
  - A page is cut at 10 MB, and the driver must use the `pageToken` and not the
    count of rows. The REST reference says the 10 MB. The emulator cuts nothing
    (recorded: "a result of 3000 rows with maxResults of 500").
  - A `TIMESTAMP` arrives in the form of an exponent by default, and as microseconds
    with `useInt64Timestamp`. The emulator sent `"1704164645.123456"` and the
    microseconds (recorded: "TIMESTAMP values", "the rows of every type with
    integer timestamps"). The exponent is not measured.
  - `NaN`, `Infinity` and `-Infinity` arrive as strings. The emulator sent `null`
    and `+Inf` (recorded: "FLOAT64 infinity and NaN").
  - `formatOptions` has `timestampOutputFormat`. True in the discovery document, and
    the emulator ignored it (recorded: "lead: timestamps in the ISO 8601 form").
    Gemini did not name the value `ISO8601_STRING`, and the document names it.
  - A job of a region other than `US` and `EU` needs `location` on `jobs.get`,
    `jobs.cancel` and `getQueryResults`. The documents say so, and the emulator
    ignored a wrong location (recorded: "lead: the results of a job with a
    location that is wrong").
  - `jobCreationMode` `JOB_CREATION_OPTIONAL` can leave out the job. The emulator
    ignored it and always made a job (recorded: "lead: a request with a job
    creation mode of required").
  - `Content-Encoding: gzip` on a request. The emulator reads it (recorded: "lead: a
    request with a gzip content encoding and a body that is not gzip").
  - A limit on the rate answers HTTP 429. Not measured.
  - A login by a JWT exchanged at `oauth2.googleapis.com/token`, with the scopes.
    The document of Google says so (see The DSN), and the emulator has no login.
  - Gemini said that BigQuery forbids a NULL inside an array. The emulator allowed
    it (recorded: "ARRAY values"), so the rule is not measured.
- Gemini (2026-10-10) reviewed the type mapping of step 8a against `TYPES.md`. It
  found every kind and every Go type right. It said that the reader of `FLOAT` must
  parse `+Inf`, `-Inf` and `NaN` as text, which the notes of the Types section say.
  It said that a `TIMESTAMP` with a precision of 12 loses its last three digits in a
  `time.Time`, which the notes say too. It said that the reader of `JSON` must keep
  a large number exact, which the Go type `json` of D25 does with the decoder of
  `encoding/json/v2`. It said that a `STRUCT` must be a tuple because a struct can
  have two members with one name and members with no name, and that a driver must
  put `nil` in the tuple for a member that is `{"v":null}`. It said that the text of
  an `INTERVAL` needs a careful parser. The second review is in the next item.
- Gemini Flash (`gemini-3.8-flash`, 2026-10-10, a conversation of its own) reviewed the
  mapping as the second model. It is a model of the same vendor, because the models
  of other vendors timed out (`deepseek-v4-flash`, `deepseek-v4-pro`, `qwen`,
  `qwen-max`, `kimi-k3` and `kimi-k2.7-code`, each tried at least once). It said:
  - `JSON` must not go through a `float64`, or a large number loses digits. The Go
    type of the kind json decodes with the decoder of D25, which keeps a number
    exact when its type is a decimal or an integer. It suggested a `string` or a
    `[]byte` for a JSON column, which is a different kind. Decided, D189: a decoded Go value.
  - A `TIMESTAMP` of float seconds must not be read through a `float64`, because a
    value of 16 digits can lose its last digit. The driver must split the text at
    the point, or send `useInt64Timestamp`. This is true of the text that the
    emulator sent, such as `"1704164645.123456"`.
  - An `INTERVAL` has microsecond precision, and each of its three parts can carry
    its own sign, such as `-1-2 +3 -4:0:0` (source: Gemini, not measured).
  - A NULL array and an empty array look alike. It noted that BigQuery returns an
    empty array for a NULL array.
  - A `STRUCT` as `[]any` drops the field names. It keeps the order, the duplicate
    names and the unnamed members. A caller reads the names from the schema, so a
    custom type that holds both is a choice. Decided, D189: a `map[string]any`.
  - A NULL cannot be inside an array of a query result (the same claim as the
    first review, not measured on the service, and refuted on the emulator).
  - It found no kind wrong.
- Gemini (`gemini-3.1-pro-preview`, 2026-10-10) read the section Hosted and emulator
  and said what real clients do that this work missed. It answered after one
  request. Its points, and what this work found:
  - The client must poll `getQueryResults` while `jobComplete` is `false`. True.
    The run recorded the first answer and one poll (recorded: bigquery-215 and
    bigquery-217), and not a poll that ends (see Cancellation and timeouts).
  - A script has child jobs, and a client reads the result of each statement with
    `jobs.list` and `parentJobId`, and then `getQueryResults` of each child. The
    answer of `jobs.query` holds the last result only (recorded: bigquery-238). Not
    measured: the request that lists children named a job that was not a script
    (recorded: bigquery-345).
  - A client must not rely on the order of the members of the JSON answer. The
    Responses section says so now.
  - `jobInternalError` and the 5xx reasons exist, and no 400 request of this run
    hit them. A client retries them with backoff. The Errors section lists them.
  - A client sends the `location` of the first `jobReference` on every later call.
    The DSN section says so.
  - A client pages with `pageToken`, and `startIndex` is not for a large result.
    The driver here must use `pageToken`.
  - A NULL cannot be an element of an array, and a NULL array is `[]`. Both
    agree with the recordings (recorded: bigquery-093 and bigquery-070).
- A second model of another vendor did not answer. Qwen Max (`qwen-max`) timed out
  twice on 2026-10-10, and DeepSeek Pro (`deepseek-v4-pro`) answered an empty text
  once, and then timed out twice. So the review of the section Hosted and emulator
  has one model, and Ken must add a second one when a vendor answers. The JDBC
  and Python clients were not read.


## Open questions

Ken decided the step 9 questions on 2026-10-10 in D189, and the questions that
the live run raised on 2026-10-11 in D194 and D195. The decided answers come first,
and what is still open follows.

Decided:

1. The timestamp form at the year 9999. D189 item 5 chooses `ISO8601_STRING`. The
   hosted integration test `TestIntegrationTimestampForm` passed on 2026-10-11 at
   `0001-01-01` and at `9999-12-31 23:59:59.999999`, so the ISO form loses no
   digit, and the driver keeps it. It does not switch to `useInt64Timestamp`.
2. The emulator. D189 item 4 supports `bigquery-0.8.1` and the service, and not
   `bigquery-0.7.2`.
3. A struct whose members repeat a name or have none. D189 item 7: the driver
   returns an error that names the column.
4. A JSON null and a SQL NULL. D195 item 1 amends D189 item 7: both are `nil`.
5. Rule 4 of `AGENTS.md`. D194 item 1: the rows of a BigQuery query keep the
   context of the statement, because the request for each next page needs it.
6. `rows` that come before `schema`. The driver reads the answer one token at a
   time and refuses such an answer with `dbimp.ErrInvalidValue`
   (`TestRowsRefuse`). The service sent `schema` first in every recording.

Live results of 2026-10-11, which the main session got with the tests of this
driver:

- The hosted integration suite passes.
- The emulator suite passes on `bigquery-0.8.1`, with skips that name the
  difference of the emulator.
- One test leaves an object behind: the test of a snapshot makes a snapshot that
  the account cannot drop, because it lacks `bigquery.tables.deleteSnapshot` and
  it cannot set the expiry of a snapshot. The snapshot expires after 7 days.
  `TestMain` reports it and a person with the right drops it.

Still open:

1. The first wait of a statement. The driver sends `timeoutMs` of 3000, so that
   the service answers `jobComplete` `false` after three seconds and the driver
   then polls the job. The value is the choice of the package and it is not
   measured. A job has no id until the first answer arrives, so a context that
   ends in those three seconds cannot cancel the job, and the job runs on at the
   service. A request through `jobs.insert` with an id that the driver makes would
   close the gap, and it is a change of D189 item 8, which names `jobs.query`.
2. CI and the emulator. The job `releases` runs every driver under `testdata/` on
   each release that `dbrun list` names for it, so it would run the integration
   tests on `bigquery-0.7.2` too, which D189 item 4 does not support. Hosted runs
   need a secret that CI does not hold. Ken decides whether the tests skip on
   `bigquery-0.7.2`, or the workflow leaves that release out.
3. The mode of the parameters. D189 item 8 says named parameters bound by the
   server. The driver sends the mode `NAMED` when every argument has a name, as
   `sql.Named("p", 5)` and `@p`, and the mode `POSITIONAL` when none has a name,
   as `?`. It refuses a mix with `dbimp.ErrArguments`, because the service refuses
   it too (recorded: bigquery-152 and bigquery-153).
4. The types that a parameter cannot have. A NULL has no Go type, so the driver
   sends it as a `STRING` with no value, which only a `STRING` column or a `CAST`
   takes. The driver binds no `ARRAY` and no `STRUCT`, and it binds no `JSON`,
   `GEOGRAPHY`, `RANGE` or `BIGNUMERIC` of its own: text goes as a `STRING`, and a
   decimal that does not fit a `NUMERIC` goes as a `BIGNUMERIC`. A caller parses
   JSON text in the statement. A typed NULL and the two structured types can come
   in a later release.
5. A type that the driver does not know, and a field with no type, as the emulator
   writes for a `RANGE`, read as the text of the service, with the Go type
   `string`.
6. A value of finer than a microsecond. BigQuery has microseconds, so the driver
   refuses to send a `dbimp.LocalTime`, a `dbimp.LocalDateTime`, a `time.Time`
   and a `dbimp.Interval` with nanoseconds, with `dbimp.ErrInvalidValue`, and it
   never rounds one. A `time.Time` goes as UTC.
7. The keys and members that D189 does not name. D189 names `credential_file`. The
   DSN also has `endpoint`, `disable_auth`, `scopes`, `location`, `timeout` and
   `max_results`, because the emulator needs an address and no login, and each
   option of D109 needs a key that it can change. `Config.AccessToken` holds a
   ready token. The token endpoint of the key file must be `https`, or `http` on
   the machine of the caller, so that a signed request never crosses the network
   in the clear.
8. `Exec` reads the head of the answer and no row, and it leaves the rest of a
   result at the service, so `Exec` of a `SELECT` costs one request, and
   `RowsAffected` of it is an error. `Ping` runs `SELECT 1` as a dry run, which
   the service does not bill.
9. `WithParameter` sets a member of the request, such as `labels`,
   `maximumBytesBilled`, `requestId`, `useQueryCache`, `dryRun` and
   `createSession`. It refuses the members that change how the driver binds or
   reads a value: `query`, `queryParameters`, `parameterMode`, `useLegacySql`,
   `formatOptions` and `queryResultsFormat`. A session that `createSession` makes
   is not carried to the next statement (D189 item 3).
10. The driver answers no `SELECT version()`. BigQuery has no endpoint or header
    that carries a release, so D181 has nothing to read, and the statement goes to
    the service, which refuses it (recorded: bigquery-229). Ken decides if the
    driver answers with the revision of the REST API instead.
11. What the hosted run still did not show, and what it needs:
    - A script with several statements, listed with `parentJobId`. The children of
      a real script and `numChildJobs` are not recorded. D189 item 6 leaves the
      child jobs for a later release.
    - A gzip request body that is right, which the recorder cannot send. The
      integration test sends one.
    - A request with a valid API key.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was
written on 2026-10-10 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of BigQuery from the sections above.

### The server

| | Couchbase | BigQuery |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /bigquery/v2/projects/{project}/queries` with `query`, `useLegacySql`, `queryParameters`, `parameterMode`, `defaultDataset`, `location`, `timeoutMs`, `jobTimeoutMs`, `maxResults` and `formatOptions` (Requests) |
| Database | The key `query_context` of the body | The project is a part of the path, and the dataset is `defaultDataset` in the body (The DSN) |
| Language | SQL++, which is close to SQL | GoogleSQL. The service has a legacy dialect that is the default, so the driver sends `useLegacySql` as `false` every time. A request can hold a script, and the answer holds the rows of the last statement (Statements) |
| DDL | In SQL++ | In SQL. A key is declared and not enforced, `UNIQUE` is refused, and a dataset, a routine, a clone and a snapshot have statements (Statements) |
| Parameters | `?`, `$1` and `$name` | `?` or `@name`, with a typed entry in `queryParameters` for each, and the mode `NAMED` or `POSITIONAL`. The service refuses a mix (Parameters) |
| Framing | One body for the whole result, which does not page | One JSON object for a page. The service cuts a page at 10 MB or at `maxResults`, and the next page is `GET /queries/{jobId}` with the `pageToken` (Responses) |
| Columns | `signature`, before the first row | `schema.fields`, before `rows` in the order that the service wrote. Each field has a legacy type name and a mode (Responses) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement. A second column of one name is renamed `a_1`, and a column with no name is `f0_` (Responses) |
| Errors | Can come with HTTP 200, after some rows | Before any row, with HTTP 400, 401, 403 or 404 and a JSON object with a reason such as `invalidQuery`. A later page can fail (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, and every scalar is a string or null. The text of a `TIMESTAMP` is a double by default, and the driver asks for the ISO form (Types) |
| Cancel | The server stops a query when the client leaves | A job runs on when the client leaves, and `POST /jobs/{jobId}/cancel` stops it a little after it answers (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | In a script of one request, or in a session that `createSession` makes. `BEGIN TRANSACTION` alone is refused (Transactions) |
| Authentication | Basic | A service account. The client signs a JWT with RS256, exchanges it at the token endpoint of its key file, and sends the access token as a Bearer token (The DSN) |
| Default port | 8093, or 18093 with TLS | 443, with TLS always. The emulator uses 9050 over `http` |

The differences that a caller sees:

- A job that runs longer than the first wait answers `jobComplete` `false`. The
  driver then polls the job, so a long statement costs a request each interval, and
  it cancels the job when the context ends (D189 item 8 and open question 1).
- A result can have several pages. The driver reads the first from the answer and
  each later one only when the caller has read the rows before it, and an error in
  a later page wraps `dbimp.ErrIncomplete` (D21, D107 and D189).
- The secret of the DSN is the path of a key file, and the driver sends a token that
  it gets by signing, never the key (D189 item 2).
- A script gives the rows of its last statement, and a transaction has no form
  across requests (D189 items 3 and 6).
- A timestamp has microseconds, so the driver refuses a value that has nanoseconds
  when it sends one (open question 6).
- The service has no version that a client reads, so `SELECT version()` is a
  statement of the service and fails (D181 and open question 10).

### The driver

| | `couchbase` | `bigquery` |
| --- | --- | --- |
| Size, without tests, on 2026-10-10 | About 1300 lines in 8 files | About 2500 lines in 10 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Project`, `Dataset`, `Location`, `Endpoint`, `CredentialFile`, `AccessToken`, `DisableAuth`, `Scopes`, `Timeout` and `MaxResults`. The DSN has the keys `credential_file`, `disable_auth`, `endpoint`, `location`, `scopes`, `timeout` and `max_results` (D189) |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, `WithLocation` and `WithMaxResults`, through `WithOptions` or an argument (D109). `WithParameter` sets a member of the request and refuses six. `WithReadonly(true)` fails with `dbimp.ErrNotSupported` |
| Arguments | Sent to the server as `args` and `$name` | Typed parameters, from the Go type of each argument: `INT64`, `FLOAT64`, `NUMERIC`, `BIGNUMERIC`, `BOOL`, `STRING`, `BYTES`, `DATE`, `TIME`, `DATETIME`, `TIMESTAMP` and `INTERVAL`. A NULL is a `STRING` with no value. A list and a map fail with `dbimp.ErrArguments` (`bigquery/params.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | A reader of its own, which reads the members of the answer, one row for each call, then the members after the rows, and then the next page (`bigquery/rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName`, `ColumnTypeScanType` and `ColumnTypeNullable` from the schema. The schema has no length, precision or scale, so the driver has no method for them |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the field, as the type table says: `int64`, `float64`, `*apd.Decimal`, `bool`, `string`, `[]byte`, `dbimp.Date`, `dbimp.LocalTime`, `dbimp.LocalDateTime`, `time.Time`, the decoded JSON value, `dbimp.Interval`, `[]any` and `map[string]any` (D135 and D189) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from `numDmlAffectedRows`, or `dbimp.ErrNotSupported` when the answer has none. `LastInsertId` always gives `dbimp.ErrNotSupported` (D178 item 14) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D189 item 3) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | When the context ends while the job runs, the driver sends `jobs.cancel` with the location of the job and a limit of 5 seconds. A result that the service finished needs no cancel (D189 item 8) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Reason, Status, Message, Location, JobID}`, which unwraps to `*dbimp.StatusError`, and the sentinels `ErrCut`, `ErrCanceled` and `ErrNoCredential` |
| Authentication | Basic | A JWT of the service account, signed in the package with RS256, exchanged for an access token that the driver keeps and renews five minutes before its end. The driver follows no redirect, so the token goes to the host of the endpoint only (D189 items 2 and 8) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, the three sentinels, `Config`, `ParseDSN` and `NewConnector` |

The differences that a caller sees:

- A value keeps its type, a date, a time, a decimal and an interval too, where
  Couchbase gives JSON shapes (D135 and D189).
- A JSON column is the decoded value, and a JSON null and a SQL NULL are both `nil`
  (D189 item 7, amended by D195 item 1).
- A STRUCT is a `map[string]any`, and a struct whose members repeat a name or have
  none fails with an error that names the column (D189 item 7).
- `RowsAffected` gives an error for DDL and for a `SELECT`, and the count for a
  statement that changes rows (D178 item 14).
- The driver sends one request to start a statement, one for each poll, and one for
  each later page, where Couchbase sends one (D189 item 8).
- `WithParameter` sets a member of the request and refuses the members that change
  how the driver binds or reads a value, where it replaces any key of the body in
  Couchbase (open question 9).
- A NULL argument is a `STRING`, so a statement casts it, and the driver binds no
  list and no map (open question 4).
- The DSN has no password. It names a key file, and the emulator needs `endpoint`
  and `disable_auth` (D189 item 2 and open question 7).
