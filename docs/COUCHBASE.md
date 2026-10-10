# Couchbase

This file holds what is known about the Couchbase query service, for the
first driver, `github.com/xo/dbimp/couchbase` (D23 and D26 in
[decisions/](decisions/README.md)). W5 in [BACKLOG.md](BACKLOG.md) is the work, and it
follows [DRIVER.md](DRIVER.md). The headings are the template of that file.

Each fact says how it was measured. "Recorded" means that an exchange under
`testdata/couchbase/` holds it, for 7.2.9, 7.6.12 and 8.0.3, as the
administrator and as the ordinary user, recorded on 2026-09-27 from the
script `testdata/couchbase/requests.json` with `dbimptest/cmd/record`. A fact
that only one release or one principal shows names it. "Measured with curl"
means that this session sent the request by hand on 8.0.3 on 2026-09-27, and
no file holds it. "Tested" means that an integration test in
`couchbase/integration_test.go` holds it, and passed on 7.2.9, 7.6.12 and
8.0.3 as both principals on 2026-09-27. A fact marked "not measured" is a
lead, not a fact.

## Summary

- The product is Couchbase Server, through its query service and SQL++,
  which Couchbase also calls N1QL.
- `dbrun` names three releases in the Tested tier: `couchbase-7.2.9`,
  `couchbase-7.6.12` and `couchbase-8.0.3`. At dbmeta commit `2b93710`,
  7.6.12 was in the Nightly tier (read from `dbrun list --json` on
  2026-09-27). dbmeta D104, in commit `323cd36`, moved it to the Tested tier
  (read on 2026-09-28). Each is the Enterprise image
  `docker.io/library/couchbase`, which is free for development (D31).
- It meets R, H and S, and it is P1 and the first driver (D23).
- The scheme in `dburl` had the `Driver` name `n1ql`, the alias `couchbase`,
  and the dialect `n1ql`. The move of step 16 made `couchbase` the `Driver`
  name and the dialect, with the aliases `n1ql` and `n1`, and the
  `GoPackage` `github.com/xo/dbimp/couchbase` (D30). dburl `v0.33.0`
  releases it.
- The survey of step 5a is `testdata/couchbase/features.json`. It holds the
  operations, the features and the types, each settled against 8.0.3, and
  each one names the integration test that holds it (step 14a). They were
  asked of
  Gemini, DeepSeek, `github.com/couchbase/gocb/v2` v2.12.5 and the Python
  SDK on 2026-09-27. The script `testdata/couchbase/requests.json` records
  each one on the three releases, as both principals.
- `usql` used `github.com/couchbase/go_n1ql` at
  `v0.0.0-20220303011133-0ed4bf93e31d`, in `usql/drivers/couchbase`. The
  driver here replaces it (D23). usql commit `8407785` made the move, and no
  release of `usql` holds it yet (read on 2026-09-28).

## Requests

- A query is `POST /query/service` with `Content-Type: application/json`, and
  the statement in `statement` (recorded). `GET /query/service?statement=...`
  works too (recorded).
- `"args": [...]` holds the positional parameters, and `"$name": value` holds
  a named parameter (recorded).
- Basic authentication works (recorded). The `creds` parameter in the body
  works in place of it (measured with curl on 8.0.3). Authentication with a
  certificate and TLS on port 18093 are not measured, because `dbrun`
  publishes only port 8093.
- `readonly: true` refuses a write with HTTP 403 and code 1000 (recorded).
- `metrics: false` leaves the `metrics` object out of the response
  (recorded).
- `query_context`, such as `default:dbmeta._default`, names the bucket and
  the scope of a collection that the statement names alone (recorded).
- A prepared statement runs by name, with `"prepared": "<name>"` and no
  `statement`, or with `EXECUTE <name>` (recorded). A request that holds both
  `statement` and `prepared` is refused with code 1060.
- The options that `gocb` and the Python SDK send are all taken: `profile`,
  `max_parallelism`, `scan_cap`, `scan_wait`, `pipeline_batch`,
  `pipeline_cap`, `use_fts`, `preserve_expiry` and `query_context`
  (recorded). `use_replica` is refused on 7.2.9 with code 1065, and taken on
  7.6.12 and 8.0.3 (recorded). `scan_consistency: at_plus` needs a
  `scan_vector`, which only a client of the data service has, and is
  refused with code 1050 without one (recorded).
- `client_context_id` names a request. The response then holds
  `clientContextID` after `requestID` (measured with curl on 8.0.3).

## The DSN

- `v0.1.0` took the DSN `couchbase://::/`, which `net/url` reads as the host
  `:`, and `FormatDSN` of it wrote a URL that does not parse. The fuzz test
  of the SurrealDB DSN found it, and `dbimp.ParseURL` now refuses a host
  with a colon that is not an IPv6 address.
- The driver takes a URL whose scheme is `couchbase`, such as
  `couchbase://user:pass@127.0.0.1:8093`, and no other scheme (D35). `dburl`
  turns each alias, such as `n1ql`, into that URL.
- The key `tls` says whether the driver speaks HTTPS or HTTP, and it is
  `false` by default (D38). The default port is 8093, or 18093 with
  `tls=true`. It is never a second scheme such as `couchbases`, because the
  driver registers one name (D28).
- The other keys are `query_context`, `scan_consistency` and `timeout`
  (D38), `durability_level` (D43) and `txtimeout` (D46).
- Each of these keys is also an option, for one statement or for
  `BeginTx` (D40). `WithDatabase` sets `query_context`, as
  `WithQueryContext` does, because the query service has no database, and
  every driver has `WithDatabase` (D109).
- `dbrun dsn --json couchbase-<release>` prints two forms: `dsn`, which is
  `http://Administrator:...@127.0.0.1:<port>` for `go_n1ql`, and `url`, which
  is `couchbase://Administrator:...@127.0.0.1:<port>/` (read on 2026-09-27).
  The `url` field has the form of D35. Both fields name the administrator.
  The URL of the ordinary user is in the field `principals` (see
  Principals).
- `go_n1ql` first treats a DSN as the address of a cluster manager, on port
  8091. A cluster in a container answers with addresses inside the
  container, which a client outside it cannot reach (the `n1ql` session).
  This driver keeps no form of DSN from `go_n1ql` (D27). It talks to the
  host of the URL only, and does not find the other nodes of a cluster
  (D38).

## Responses

- The fields of a response arrive in this order: `requestID`, then
  `clientContextID` if the request named one, `signature`, `results`,
  `errors`, `status`, `metrics` (recorded). The signature arrives before the
  first row, so the driver knows the columns before it reads a row (D18).
- 7.6.12 and 8.0.3 send the signature and every result object in the order
  of the projection. 7.2.9 sends both in the order of the names (recorded).
  Ken accepted the order of the names on 7.2 for `xo/n1ql`, rather than a
  second request for each query (n1ql D33). This driver keeps the order of
  the names on 7.2 too (D39).
- For `SELECT RAW`, the signature is a string such as `"json"`, not an
  object, and each result is a bare value (recorded). The result has one
  column.
- `SELECT *` has the signature `{"*":"*"}`, and each result is an object with
  one member, named for the keyspace, that holds the whole document
  (recorded).
- The response to a DML statement has `"signature": null`, an empty
  `results`, and a count in `metrics.mutationCount` (recorded).
- Other statements with `"signature": null` return rows (tested).
  `CREATE INDEX` returns one object, `{"id": ..., "name": ..., "state":
  "online"}`, and `INFER` returns one array. So the driver reads the rows of
  a null signature as it reads `SELECT *` when the first row is an object,
  and as it reads `SELECT RAW` when it is not.
- `BEGIN WORK` has the signature `"json"`, and its one row is an object with
  the `txid` (tested). A person who types it sees one column that holds the
  object.
- The integration tests name the columns of a statement in the order of
  their names, so that every release returns the same order.
- `status` is `success`, `fatal`, `stopped` or `errors` (recorded). An
  `INSERT` of a key that exists ends with `errors` and code 12009. A query
  stopped by a cancel ends with `stopped` and no error.
- A result is one response, and it does not page. A result of 20000 rows
  arrived whole in one body (recorded).
- 7.6.12 and 8.0.3 limit the result of `ARRAY_RANGE` to 20 MiB, and refuse a
  larger one with code 5037 and HTTP 200, before any row. 7.2.9 has no such
  limit, and sent 20000000 rows, 188 MB, for the same statement. That
  recording was deleted for its size, and the script no longer sends it.
- The server does not compress a response, even when the request asks for
  gzip (recorded).

## Types

- The signature names the kind of each column: `number`, `string`,
  `boolean`, `null`, `missing`, `array`, `object`, or `json` when the kind is
  not known (recorded). A field read from a document is `json`, and a
  literal has its kind. A column of the kind `json` scans into `any`, its
  database type is `JSON`, and it holds any value of the table below. So the
  row of bytes, with the scan type `string`, holds for a literal, and a
  field of a document that holds base64 has the kind `json`.
- The number 9007199254740993 and the largest int64, 9223372036854775807,
  arrive exact (recorded). The driver converts the text of the token (D19).
- The server holds a larger integer as a float64 itself: it sends
  123456789012345678901234567890 as `123456789012345680000000000000`
  (recorded). The driver cannot recover digits that the server did not send.
- The server writes a float64 that is a whole number as its digits, with no
  exponent. `-1.5e300` arrives as `-15` and 299 zeros, so the driver returns
  it as a `*apd.Decimal`, because it does not fit an int64 (tested). A
  fraction such as `1.5e-300` arrives as a float64 (tested). D39 holds the
  correction.
- `0.1` arrives as `0.1` (recorded).
- A field that is MISSING is absent from the result object, and the
  signature still names it. `SELECT d.x, d.y, d.z` over the document
  `{"x":1,"y":null}` returned `{"x":1,"y":null}`, with no `z` (recorded).
- `TYPE()` of a value returns its kind. `t.v IS MISSING` is true for a
  field that a document lacks (recorded).
- A field of a document has no declared type. Couchbase keeps no schema, so
  a field holds whatever JSON value was written to it, and JSON has no type
  for bytes. Bytes are stored as a base64 string, which is how json/v2
  encodes a Go `[]byte` and how the driver sends one (D40). `{"v":
  "3q2+7w=="}` as a literal, and `$1` bound to `"3q2+7w=="`, both store a
  string, and both read back as `3q2+7w==` with the kind `string`, and
  `BASE64_DECODE` of each equals the four bytes `DEADBEEF` (recorded on
  8.0.3, in `couchbase-8.0.3-178-post--query-service.json` to `-180-`).
- The kind `binary` exists only for a value inside a statement, such as the
  result of `BASE64_DECODE`, and for a whole document written through the
  data service. The server writes such a value into JSON as the text
  `"<binary (1 b)>"`, the number of its bytes and not the bytes (recorded on
  each release, in `couchbase-8.0.3-173-post--query-service.json` and the
  same request of 7.2.9 and 7.6.12). The engine of the query service does
  this in its own code: `binaryValue.MarshalJSON` and `WriteJSON` in
  `value/binary.go` of `github.com/couchbase/query` write `"<binary (%d b)>"`
  with the length (read on 2026-09-27).
- So a statement must not turn bytes into a binary value (recorded on
  8.0.3):
  - `BASE64_ENCODE` of a binary value encodes the text of the placeholder:
    the four bytes `DEADBEEF` came back as `IjxiaW5hcnkgKDQgYik+Ig==`, which
    is `"<binary (4 b)>"`.
  - A binary value written to a field is stored as the text of the
    placeholder, and reads back with the kind `string`.
  - A whole binary document cannot be written through the query service. The
    server refuses it with code 12030, "UPSERT of binary document is not
    supported". A binary document written through the data service is not
    measured, because `dbrun` does not publish its port.
- Nothing in a response marks a string as base64. The signature names the
  kind `json` or `string`, so the driver cannot tell bytes from text. So a
  string that scans into a `*[]byte` is decoded as base64, and copied as it
  is when it is not valid base64 (D44).
- There is no decimal type. `TYPE(1.5)` is `number`, and `0.1 + 0.2` is
  `0.30000000000000004` (recorded).
- Each kind makes the round trip of step 14a on each release, as a bound
  argument and as a literal, in a document of the collection
  `dbmeta.dbimp.types` (recorded), and in the collection `types` of the
  scope that the integration tests make (tested).
- SQL++ has no type for a time, a binary value or a UUID. A time is a string
  in RFC 3339, `BASE64_ENCODE` returns a string, and a UUID is a string
  (recorded). The models agreed on this.

The type table comes from the code, in step 10.

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| missing | null | `nil` | `interface {}` | `MISSING` | yes |
| null | null | `nil` | `interface {}` | `NULL` | yes |
| boolean | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| number | number | `int64, float64, or *apd.Decimal for an integer too large for int64` | `interface {}` | `NUMBER` | yes |
| string | string | `string` | `string` | `STRING` | yes |
| array | array | `[]any` | `[]interface {}` | `ARRAY` | yes |
| object | map | `map[string]any` | `map[string]interface {}` | `OBJECT` | yes |
| bytes as a base64 string | binary | `[]byte, decoded from base64 (D44)` | `string` | `STRING` | yes |
<!-- /dbimp:types -->

A value whose JSON kind is not the kind that the signature names for its
column is an error, so that its Go type is always the scan type (D136). No
recording holds one: each of the 1,092 values of `testdata/couchbase/`
has the kind of its signature, or the signature says `json`.

## Parameters

- The server binds positional and named parameters (recorded). A string with
  a quote and a double quote, and a nested object, arrive unchanged.
- `?` and `$1` are both positional, and both take the first argument
  (recorded).
- The server ignores an argument that no placeholder names, positional or
  named (recorded).
- A placeholder with no argument fails with code 5010 and HTTP 200, after
  the signature and before any row (recorded).
- So the driver sends each argument to the server, and never uses the parser
  of D34 for them.
- `WithParameter(name, value)` sets any parameter of the request by its
  name, as the raw options of the SDKs do. The tests use it for
  `client_context_id`, `metrics`, `profile`, `max_parallelism`, `scan_cap`,
  `pipeline_batch`, `use_replica`, `use_fts`, `preserve_expiry` and
  `tximplicit`, which the driver has no option of its own for (tested).

## Transactions

- `BEGIN WORK` returns a row with a `txid`, for the administrator and for the
  ordinary user (recorded).
- With the default durability, `COMMIT WORK` fails on every release with
  code 17007, "Durability requirements are impossible to achieve", because
  the one node of the container cannot meet the default durability of
  `majority`. `tximplicit: true` fails the same way, with code 17020
  (recorded).
- With `durability_level: "none"` on `BEGIN WORK`, the transaction works on
  every release (recorded):
  - A write in it is not seen outside it.
  - `SAVEPOINT s1`, a write, and `ROLLBACK WORK TO SAVEPOINT s1` discard the
    write after the savepoint.
  - `COMMIT WORK` makes the rest seen.
  - `ROLLBACK WORK` discards all of it.
- A statement with the `txid` of a transaction that ended, or that passed
  its `txtimeout`, is refused with HTTP 500 and code 17004 or 17010
  (recorded).
- `tximplicit: true` with `durability_level: "none"` runs one statement in a
  transaction of its own (recorded).
- A transaction that nobody ends applies none of its writes. A write in it
  did not block a write to the same key from outside it, and the value from
  outside stayed after the transaction reached its `txtimeout` of 3 seconds.
  `ROLLBACK WORK` after that timeout is refused with code 17010 (measured
  with curl on 8.0.3).
- The timeout of a transaction is 15 seconds for a request that sets no
  `txtimeout`, and a setting of the node can lower it. The default
  durability is `majority`. Every statement of a transaction goes to the
  same query node. These three facts come from the documentation of
  Couchbase, read on 2026-09-27: "Configure Queries" and "SQL++ Support for
  Couchbase Transactions". The driver talks to one host (D38), so the last
  one holds by itself. D45 relies on the first.
- A transaction holds state on the server, so a `database/sql` transaction
  maps onto one connection that carries the txid (D20). The first release
  supports it (D41). The DSN key `durability_level` and the option
  `WithDurability` set the durability, because the default cannot commit on a node alone (D43).

## Errors

- 8.0 reserves the word `roles`, and 7.6 does not, so an unquoted `u.roles`
  is a syntax error on 8.0 alone. `system:dictionary` fails with code 5001, a
  panic of the server, on 7.6.12 and 8.0.3, even after
  `UPDATE STATISTICS`. The `dbmeta` session measured both through this driver
  on 2026-09-27, which reports each one as an error.
- The driver returns a `*ResponseError` for a response that failed. It holds
  the HTTP status, the status of the body, and each `Error` with its code,
  and `errors.As` finds each `Error` in it.
- A syntax error is HTTP 400 with code 3000, the line and the column, and no
  signature and no results (recorded).
- An error can arrive after rows. The `ABORT` statement returned HTTP 200,
  the results `[0,1,2]`, then code 5011 and `"status": "fatal"` (recorded).
  The driver returns that error from `Rows.Next`, which a caller of
  `database/sql` reads from `Rows.Err`, and never reports the result as
  complete (D21).
- A wrong password is HTTP 401 with code 2120 on 7.6.12 and 8.0.3
  (recorded). 7.2.9 accepted a wrong password for `SELECT 1 AS a`, and ran it
  (recorded). Gemini said that 7.2 checks credentials only for a statement
  that reads a keyspace. Whether 7.2.9 refuses a wrong password for a
  statement that reads a keyspace is not measured.
- A statement that needs a role that the user lacks is HTTP 401 with code
  13014 and the missing role (recorded, for the ordinary user on
  `system:user_info`).
- A request with no statement is HTTP 400 with code 1050 (recorded).

The refused credential (D197): `errors.Is(err, dbimp.ErrAuthentication)` is
true when the server refused the credential.

- An `Error` with the code 2120 matches. It is the "wrong password" of
  7.6.12 and 8.0.3 (item 32).
- An `Error` with the code 13014 does not match, although it is also HTTP 401.
  It means that the user lacks a role (item 209 and the other refusals of the
  ordinary user).
- The code 1000 for a write in `readonly` mode (HTTP 403) and the code 3000
  for a syntax error do not match.
- 7.2.9 accepted a wrong password for `SELECT 1 AS a`, so it sends no error
  to match.
- A prepared statement that the server does not know gives code 4040 on
  7.2.9 and 8.0.3, and `PREPARE` with a name that exists gives 4060 (the
  `n1ql` session).
- An index is updated after a write, not with it. On 7.6.12, a `SELECT`
  straight after an `UPSERT` returned no rows (the `dbmeta` session). A test
  that writes and then reads sends `scan_consistency: request_plus`, or reads
  by `USE KEYS`.
- The server makes a scope, a collection or an index a moment after the
  statement returns. On 7.2.9, `CREATE COLLECTION` straight after
  `CREATE SCOPE` failed with code 12021, "Scope not found" (tested). A
  `CREATE INDEX` that failed can still make the index, and the next try gets
  code 4300, "already exists" (tested). `DROP SCOPE` also ends a moment after
  it returns. So `TestMain` tries each statement again for a while.
- After a start, the data service can be cold. On 7.2.9, the first read by
  key, 3.6 seconds after a write, returned no rows (recorded, as the
  administrator). The same read a moment later, as the ordinary user, found
  the document.

## Cancellation and timeouts

- The server stops a query when the client disconnects. A query that runs
  for 8 seconds was running 2 seconds after it started. The client left at 2
  seconds, and 1 second later `GET /admin/active_requests` listed nothing
  (recorded, on each release). D36 relies on this.
- `timeout` in the body makes the server stop the query, with code 1080,
  `"retry": true`, and HTTP 200 (recorded).
- `DELETE FROM system:active_requests WHERE clientContextID = "<id>"` stops
  the query that carries that id. The query ends with `"status": "stopped"`
  and no error (recorded, for both principals).
- `DELETE /admin/active_requests/<requestID>` stops the query, and yet
  answers HTTP 500 with code 1130, "is not a http request" (measured with
  curl on 8.0.3). The same call with a `client_context_id` answers the same
  500, and does not stop the query (measured with curl on 8.0.3). A call
  with an id that does not exist answers the same 500 (recorded).

## Statements

- One request holds one statement. Two statements separated by `;` are
  HTTP 400 with code 3000 (recorded). The driver never splits a statement
  (D8), so it sends two as the caller wrote them, and the server refuses
  them.
- A comment with `/* */` or `--`, and a `?` or a `$1` inside one, does not
  affect the statement (recorded).

## Principals

The `dbmeta` session added an ordinary user to its Couchbase entry on
2026-09-27 (D31, and dbmeta D96). It measured these facts on 7.2.9, 7.6.12
and 8.0.3:

- The user is `dbmeta_user`, and its password is the password of
  `Administrator`. `dbmeta` holds the name in the constant
  `container.CouchbaseUser`. This repository never imports `dbmeta` (D29),
  so a test takes the name from its DSN.
- Its roles are `query_select`, `query_insert`, `query_update` and
  `query_delete` on the bucket `dbmeta`, and `query_system_catalog`.
- It can `UPSERT`, `SELECT` and `DELETE` in `dbmeta`, and read
  `system:keyspaces`.
- It is refused `system:user_info` and `CREATE INDEX`, with HTTP 401 and
  code 13014 (recorded), and the creation of a bucket through REST, with
  HTTP 403 (not recorded here). The text of the refusal differs by release. On 8.0 it names `user_admin_local`, and on 7.x it says "accessing
  user information".
- `Init` makes a primary index on `dbmeta`. Without one, a `SELECT` over the
  bucket is refused on every release. 7.2 has no sequential scan, and on 8.0
  the refusal asks for `query_use_sequential_scans`, a role that the user
  does not hold.
- The ordinary user reads the version with `SELECT RAW ds_version()`, the
  statement that `usql` runs (recorded). Both principals read
  `7.2.9-9230-enterprise`, `7.6.12-8946-enterprise` and
  `8.0.3-5933-enterprise` through this driver (tested, by
  `TestIntegrationVersion`, which step 16 asks for).
- The ordinary user can begin a transaction, and can stop its own query with
  `DELETE FROM system:active_requests` (recorded).
- The ordinary user is refused, with HTTP 401 and code 13014, an index, a
  vector index, a sequence and the use of one, an inline or a JavaScript
  function, and `CURL()` (recorded). It can run the CRUD of the survey on a
  collection that the administrator made, in the bucket `dbmeta`.
- From dbmeta D102, `dbrun dsn --json` prints the URL of the ordinary user,
  in the field `principals`, with the role `user`. The CI workflow reads it
  from there (read at dbmeta commit `eb35c7c` on 2026-09-27).

## Flavors

Ken decided on 2026-09-27 that Couchbase Analytics is part of this target,
and not a target of its own. By his reading, it is the same as the query
service. The models describe it as `POST /analytics/service` on port 8095,
with the same fields in the request and in the response, no transactions,
and a cancel that takes the `client_context_id`. `dbrun` does not publish
port 8095, so none of that is measured. D42 leaves it to a later work item,
because `dbrun` does not publish its port.

## Interfaces

The interface table comes from the code, in step 10.

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT RAW 1, because /admin/ping needs no credentials, so it would not check them. |
| `driver.SessionResetter` | yes | ResetSession rolls back a transaction left open (D41). |
| `driver.Validator` | yes | A connection holds no state on the server outside a transaction. |
| `driver.NamedValueChecker` | yes | An argument is any value that json/v2 encodes, and an Option is taken out (D40). |
| `driver.QueryerContext` | yes | The query service binds each argument itself. |
| `driver.ExecerContext` | yes | A write returns metrics.mutationCount as its rows affected. |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, bound each time. |
| `driver.ConnBeginTx` | yes | A transaction of the query service, with a txid (D41). |
| `driver.RowsColumnScanner` | yes | A value is decoded as it is scanned, and a byte slice gets base64 decoded (D44). |
| `driver.RowsNextResultSet` | no | A request holds one statement, so a response has one result. |
| `driver.RowsColumnTypeScanType` | yes | From the kind that the signature names. |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The kind that the signature names, in upper case. |
| `driver.RowsColumnTypeLength` | no | A document has no schema, so no column has a length. |
| `driver.RowsColumnTypeNullable` | yes | Every column can be NULL or MISSING, because a document has no schema. |
| `driver.RowsColumnTypePrecisionScale` | no | A number is a JSON number, with no precision or scale. |
<!-- /dbimp:interfaces -->

## Faults

The `n1ql` session and `dbmeta` found these faults in `go_n1ql`, which this
driver must not repeat:

- It writes each argument into the text of the statement with no escaping,
  and it rewrites a `?` inside a literal.
- It keeps its configuration in package variables, and it turns off TLS
  verification on a shared transport for the whole process (D7).
- It does not close the body of a response.
- It dereferences nil when `results` is absent.
- Its rows are a goroutine and a channel, with a data race on `closed` and a
  goroutine that leaks.
- Its retry loop removes a node by index during a race, and sends a failed
  POST again to another node, so a write can run twice (D8).
- It calls `rand.Seed` on every request.
- It makes type assertions, and ignores whether each one succeeded.
- `decodeSignature` prints to standard output.
- A prepared query retries on any error.
- `LastInsertId` returns 0 and no error.
- It takes no context anywhere.
- Against 8.0.3 it returns the columns in the order of the names (D8). It
  returns each value as JSON text with its quotes, and a row of one column as
  the whole object, such as `{"v": "8.0.3-..."}`.

The `Version` function of the Couchbase driver in `usql` called
`strconv.Unquote` on the result of `SELECT RAW ds_version()`. That call
depended on the JSON text fault, and usql commit `8407785` removed it when
`usql` moved to this driver.

The `n1ql` session stopped its rewrite of `xo/n1ql` on 2026-09-27, when Ken
decided that this driver replaces it. Its `docs/PLAN.md` keeps its decisions
as a record, and these parts of them fit this driver: the standard library
only, `ctx` everywhere, `driver.RowsColumnScanner`, no request sent again
after it can have reached the server, `RowsAffected` from
`metrics.mutationCount`, an error from `LastInsertId`, and options from the
DSN, then the context, then an argument.

## Second opinions

DeepSeek and Gemini were asked on 2026-09-27 about what step 6 did not
measure. Each lead, and what the server said on 8.0.3:

- A `txid` joins a statement to a transaction, and `ROLLBACK WORK` ends it.
  Both models said so, and the server agreed (measured with curl).
- `tximplicit: true` runs one statement in a transaction. The server took it
  (measured with curl).
- `readonly: true` refuses a write. The server agreed, with HTTP 403 and code
  1000 (recorded). Gemini said HTTP 400 and code 1160, which was wrong.
- `metrics: false` leaves the metrics out. The server agreed (recorded).
- `query_context` names the bucket and the scope. The server agreed
  (recorded).
- The `creds` parameter authenticates. The server agreed (measured with
  curl).
- A `DELETE` of an active request takes the `requestID`. The server stopped
  the query, but answered HTTP 500, which neither model said (measured with
  curl).
- Gemini said that a `DELETE` with a `client_context_id` answers HTTP 500
  with code 1130, and that the SQL `DELETE FROM system:active_requests`
  cancels by the context id. Both were right (recorded and measured with
  curl).
- DeepSeek said that `format: jsonl` makes the server send one object for
  each line. The server refused it with code 1030, "Unknown format value",
  so that lead was wrong (measured with curl).
- Both said that a `warnings` array can appear. A hint for an index that does
  not exist produced none, so it is not measured.
- Gemini said that 7.2 checks credentials only for a statement that reads a
  keyspace. The recording agrees for `SELECT 1`, and the rest is not
  measured.
- Both said that the nodes of a cluster are in
  `GET /pools/default/nodeServices` on port 8091. `dbrun` does not publish
  port 8091, so it is not measured.
- Both said that Couchbase Analytics is on port 8095. It is not measured.

The survey of step 5a asked the same two models four more questions on
2026-09-27. The recordings settled where they disagreed:

- DeepSeek said that sequences exist, and Gemini said that they do not.
  `CREATE SEQUENCE` and `NEXTVAL FOR` work on 7.6.12 and 8.0.3, and are a
  syntax error on 7.2.9 (recorded).
- DeepSeek said that `PIVOT` exists from 7.6. It is a syntax error on every
  release (recorded).
- DeepSeek said that `LATERAL` exists from 7.6, which is right (recorded).
- DeepSeek said that a `DECIMAL` type exists, which is wrong (recorded).
- Both said that foreign keys, unique constraints, views and defaults do not
  exist, which is right. `CREATE TABLE`, `CREATE UNIQUE INDEX` and
  `CREATE VIEW` are syntax errors (recorded).
- `WITH RECURSIVE`, JavaScript functions, `VECTOR_DISTANCE` and
  `use_replica` fail on 7.2.9, and work on 7.6.12 and 8.0.3 (recorded).
- A vector index needs vectors to train on. On 8.0.3 it built over 16
  documents, and a query ordered by `APPROX_VECTOR_DISTANCE` used it
  (recorded). 7.2.9 and 7.6.12 were not asked, and it is not measured
  there.
- `SEARCH()` returns no rows, because the cluster of `dbrun` runs no Search
  service (recorded). A search index is not measured.
- `CURL()` exists and is refused by the default configuration, with code
  5010 for the administrator and 13014 for the ordinary user (recorded).
- Asked what the server sends for a binary value, Gemini said a base64
  string, with a confidence of 95 percent, and said that the engine encodes
  it with `base64.StdEncoding` in `MarshalJSON`. That is wrong: the source of
  the engine and the recordings show the placeholder. DeepSeek said the
  placeholder, which is right, and said that `BASE64_ENCODE` recovers the
  bytes, which is wrong (recorded).
- `INFER` failed on 7.2.9 with code 7014, "No documents found", over
  documents that the same run had written, and worked on 7.6.12 and 8.0.3
  (recorded). The integration tests later found that `INFER` samples the
  documents at random, on every release. It can find none for a moment after
  a write, and for about ten seconds after a collection of the bucket is
  dropped. On 7.2.9 it can find none in a collection that holds many deleted
  documents. `INFER` works on each release when it is tried again, over a
  collection that holds no deleted documents (tested). `INFER` of an
  expression, such as `INFER [{"a": 1}]`, is a syntax error on 7.2.9, and
  works on 7.6.12 and 8.0.3 (measured with curl).

## Open questions

Ken decided the questions of step 9 on 2026-09-27, in D38 to D42 of
[decisions/](decisions/README.md). The durability of a transaction is D43: a
key of the DSN and an option for one transaction, which the tests set to
`none`. D44 decides how a string scans into a byte slice, D45 that a
transaction keeps the context of `BeginTx`, and D46 the key `txtimeout`.
