# Amazon DynamoDB

This document holds what is known about Amazon DynamoDB, for its driver
`dynamodb` (W30 and D162). ScyllaDB Alternator speaks the API of DynamoDB,
and D162 names it a flavor. The headings are the template of
[DRIVER.md](DRIVER.md).

Steps 5a to 7 measured DynamoDB Local 3.2.0 and 3.3.1 as its one key, and
Alternator 2025.1 and 2026.3 as `cassandra` and as `dbmeta_user`, on
2026-10-01 and 2026-10-02. A fact marked "recorded" is in
`testdata/dynamodb/`, and the name in quotes after it is the name of its
request in `requests.json` there. A fact marked "measured" names how it was
measured. The two releases of DynamoDB Local gave the same answers. The two
releases of Alternator gave the same answers, except where a line says
otherwise. Each section starts with the measured facts. The sources, each
read on 2026-10-01, are these:

- "godynamo" is `github.com/btnguyen2k/godynamo` v1.3.0 of 2024-05-02, the
  driver that `usql` uses, read from the cache of Go modules.
- "boto3" is `boto3/dynamodb/types.py` of the Python SDK of AWS, at the tag
  1.43.106, where the file last changed in commit `e2d0e64`.
- "The dbmeta entries" are `container/dynamodb.go` and
  `container/alternator.go` in `dbmeta`, and dbmeta D118.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, each
  asked on 2026-10-01.
- "The AWS documents" are the public documents of DynamoDB, as the brief of
  this work named them. Nobody here read them again.

## Summary

- DynamoDB is a store of items, which AWS runs as a service. An item is a
  map of attributes. Each table has a key of one or two attributes, and any
  other attribute can differ from one item to the next (recorded: "select
  every attribute"). AWS publishes DynamoDB Local, which runs on one
  machine, and `dbrun` runs it (the dbmeta entries).
- A statement is PartiQL, a dialect like SQL, which `ExecuteStatement`
  takes (recorded: "a statement").
- `dbrun` starts `dynamodb-3.2.0` and `dynamodb-3.3.1`, which are DynamoDB
  Local, and `alternator-2025.1` and `alternator-2026.3`, which are
  ScyllaDB with Alternator on. Each is in the Staged tier with the cadence
  `tested` (the dbmeta entries).
- R holds for DynamoDB Local and for Alternator (measured with `dbrun start`
  on 2026-10-01). The cloud service fails R, and DynamoDB Local stands in
  for it.
- H holds for both. Each answers `POST /` with JSON (recorded: "list the
  tables").
- S holds for DynamoDB Local, which runs PartiQL (recorded: "crud: select").
  S fails for Alternator. It refuses `ExecuteStatement`,
  `BatchExecuteStatement` and `ExecuteTransaction` with
  `UnknownOperationException` on both releases (recorded: "a statement", "a
  batch of statements" and "a transaction of writes"). TARGETS.md names this
  from the documents of Alternator, and the server agrees. See the open
  questions.
- [TARGETS.md](TARGETS.md) names DynamoDB as likely P2, because `usql`
  already has a driver (D24). D162 places it fifteenth in the order.
- `dburl` has the scheme `godynamo`, with the aliases `dy`, `dyn`, `dynamo`
  and `dynamodb`, the generator `GenDynamo`, the `GoPackage`
  `github.com/btnguyen2k/godynamo` and the deployment `DeploymentHosted`
  (`dburl/scheme.go`, read on 2026-10-01). So `dburl` maps `dynamodb:` to
  the scheme `godynamo` now.
- `usql` has `drivers/dynamodb`, which imports godynamo v1.3.0 and
  registers `godynamo` with no `Version`. So a driver here can replace it
  (D24).
- `dbmeta` has no model for DynamoDB. dbmeta D66 says that the emulator of
  DynamoDB has no SQL catalog.

## Requests

These facts were recorded on each release:

- Each call is `POST /` with the header `X-Amz-Target`, such as
  `DynamoDB_20120810.ExecuteStatement`, and a JSON body. The answer has the
  content type `application/x-amz-json-1.0` (recorded: "a statement").
- `ExecuteStatement` takes `Statement`, and can take `Parameters`,
  `NextToken`, `Limit`, `ConsistentRead`, `ReturnConsumedCapacity` and
  `ReturnValuesOnConditionCheckFailure` (recorded under items 3, 4 and 5).
  DynamoDB Local ignores a member that it does not know, such as `Bogus`,
  `Segment` and `ExpressionAttributeValues` (recorded: "an unknown member of
  the body" and "lead: segments of a scan").
- `BatchExecuteStatement` takes up to 25 `Statements`, and
  `ExecuteTransaction` takes up to 100 `TransactStatements`, each with its
  own `Parameters` (recorded: "a batch of statements", "setup: fill the table
  of pages, batch 1" and "a transaction of 101 statements").
- DynamoDB Local took the content type `application/x-amz-json-1.1` too
  (recorded: "the content type of JSON 1.1").
- A request with no `X-Amz-Target` fails with HTTP 500 and `InternalFailure`
  on DynamoDB Local, and with HTTP 400 and `Unsupported operation ` on
  Alternator (recorded: "no target").
- Each request is signed with AWS Signature Version 4, in the scope of the
  region `us-east-1` and the service `dynamodb`. The access key is the user
  of the URL of `dbrun`, and the secret key is its password. Alternator
  checks the signature, and DynamoDB Local does not (Principals).
- The tables, the indexes and the time to live are made through the API,
  with `CreateTable`, `DescribeTable`, `DeleteTable` and `UpdateTimeToLive`,
  on each flavor (recorded: "schema: a table with a sort key and indexes"
  and "schema: a time to live"). PartiQL has no DDL (Statements).
- Alternator answers the API of items: `GetItem`, `PutItem`, `Query` and
  `Scan` (recorded: "every type through the API", "crud: put an item
  through the API", "crud: query through the API" and "a scan through the
  API").

These facts come from the sources, and are not measured:

- The signature is AWS Signature Version 4, with the scope
  `<date>/<region>/dynamodb/aws4_request` (the AWS documents). The command
  `dbimptest/cmd/record` signs each request so, and both flavors took it.
- The service of AWS has an endpoint for each region, such as
  `https://dynamodb.us-east-1.amazonaws.com` (the AWS documents and
  godynamo, which takes the region and an optional `Endpoint`).

## The DSN

- Step 9 decides the URL (D27 and D35). Nothing here is measured.
- `dburl` writes, for the scheme `godynamo`, the string
  `Region=<host>;AkId=<user>;Secret_Key=<password>`, with each key of the
  query added as `;Key=value`. So the host of the URL is the region, and the
  endpoint is the key `Endpoint` of the query (`GenDynamo` in
  `dburl/dsn.go`).
- godynamo reads `Region`, `AkId`, `Secret_Key` or `SecretKey`, `Endpoint`
  and `TimeoutMs`, with a default of 10000 for `TimeoutMs`. It reads
  `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and
  `AWS_DYNAMODB_ENDPOINT` from the environment when a key is missing
  (godynamo, `driver.go`).
- The dbmeta entries give each principal as
  `dynamodb://<key>:<secret>@us-east-1/?Endpoint=http%3A%2F%2F127.0.0.1%3A<port>`.
  The secret of Alternator is a hash of SHA-512 crypt, such as
  `$6$dbmetadbmeta$Tw/8k...`, so its `/` is escaped in the URL (measured
  with `dbrun dsn --json` on 2026-10-01).

## Responses

These facts were recorded on each release of DynamoDB Local:

- A statement answers one JSON object, `{"Items": [...]}`, with
  `NextToken` when more follows. The whole page arrives in one body, with
  `Content-Length` (recorded: "a statement").
- An item is a map from the name of each attribute to its value. Each value
  is an object with one member, whose name is the type, such as
  `{"S": "x"}` (recorded: "every type").
- The order of the members of an item is not the order of the statement,
  and not the order of the insert. `SELECT s, nul, "absent", nint, pk` gave
  `nint`, `s`, `pk` and `nul` (recorded: "a projection"). DynamoDB Local
  gave the members of one item in an order that no rule here explains, and
  Alternator gave the key first and then the other attributes in the order
  of their names (recorded: "every type" and "every type through the API").
- An attribute that an item lacks is not in the item. A projection on an
  item that lacks every named attribute gives `{}` (recorded: "a projection
  on a row that lacks each attribute"). So the server sends no column list,
  and no member names a column that no item has.
- A nested path arrives under its last part. `SELECT m.k[1], l[0], m.z`
  gave `k[1]`, `l[0]` and `z` (recorded: "a projection of nested paths").
  Two paths with the same last part fail with `Duplicate identifiers in
  select clause: z`, and so do two equal names (recorded: "two nested paths
  with one last name" and "two columns with one name").
- An alias, `COUNT(*)`, a function in the projection, a bag in the
  projection and a parameter in the projection fail (recorded: "an alias",
  "a count", "a function in the projection", "lead: a bag in the
  projection" and "lead: a parameter in the projection").
- `"m.z"` in double quotes names a top level attribute whose name holds a
  dot (recorded: "a quoted name with a dot").
- `DescribeTable` names the key in `KeySchema`, in the order `HASH` then
  `RANGE`, and the type of each key attribute in `AttributeDefinitions`.
  It names no other attribute (recorded: "describe a table"). Alternator
  gave the same `KeySchema`, with the `AttributeDefinitions` in another
  order.
- A page ends at about 1 MB of items read, whatever the projection. A table
  of 300 items of 4000 bytes each gave 262 items of `SELECT id` and a
  `NextToken`, and the next page gave 38 and no `NextToken` (recorded: "a
  result larger than one page" and "the next page").
- `Limit` counts the items read, and not the items that match. `Limit` 2
  gave 2 items and a `NextToken`, and `WHERE id > 290` with `Limit` 10 gave
  no items and a `NextToken` (recorded: "a page with a limit" and "a limit
  with a filter"). `Limit` 0 fails (recorded: "a limit of zero").
- `LIMIT` in the statement fails with `Unsupported clause: LIMIT` (recorded:
  "LIMIT in the statement").
- A `NextToken` of another statement fails with `The provided starting key
  is invalid`, and a token that is not valid fails with `Invalid NextToken`
  (recorded: "the next page of another statement" and "a next token that is
  not valid").
- `ORDER BY` needs a `WHERE` on the key (recorded: "crud: order by with no
  key" and "crud: order by the key of a hash table").
- Neither flavor sends gzip to a request with `Accept-Encoding: gzip`
  (recorded: "a gzip answer" and "a gzip answer through the API"). DynamoDB
  Local sends `X-Amz-Crc32` with each answer, and Alternator does not.
- No answer was a redirect (recorded).

These facts were recorded on Alternator:

- `Scan` answers `Count`, `ScannedCount`, `Items` and `LastEvaluatedKey`
  when more follows. `Limit` 1 gave one item and a `LastEvaluatedKey`
  (recorded: "a scan page through the API").

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table,
and it waits for the review of Ken. Step 10 will write it from the code.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| NULL | null | `nil` | `interface {}` | `` | yes |
| BOOL | boolean | `bool` | `interface {}` | `` | yes |
| N | decimal | `*apd.Decimal, from the text of the number` | `interface {}` | `` | yes |
| S | string | `string` | `interface {}` | `` | yes |
| B | binary | `[]byte, from base64` | `interface {}` | `` | yes |
| L | array | `[]any, of the Go types of its elements` | `interface {}` | `` | yes |
| M | map | `map[string]any, of the Go types of its values` | `interface {}` | `` | yes |
| SS | set | `[]any, of string` | `interface {}` | `` | yes |
| NS | set | `[]any, of *apd.Decimal` | `interface {}` | `` | yes |
| BS | set | `[]any, of []byte` | `interface {}` | `` | yes |
<!-- /dbimp:types -->

A column has no type in DynamoDB. Each value names its own type, and two
items can hold two types in one attribute (recorded: "a type that a value
can change"). So the scan type of each row is `interface {}`, and the
database type is empty, as for SurrealDB. A value that is missing is not a
type, and step 9 decides whether it is nil, as NULL is (D18).

These facts were recorded on each release, from three items of every type
(recorded: "every type", "empty values and the range of numbers" and "a row
with the key only"):

- `S` is a JSON string. It can be empty, except in a key (recorded: "an
  empty key").
- `N` is a JSON string that holds a decimal number. It keeps 38 digits,
  such as `12345678901234567890123456789012345678` and
  `0.12345678901234567890123456789012345678`. A number of 39 digits fails
  with `DynamoDB only supports precision up to 38 digits` (recorded: "a
  number with 39 digits").
- The range of `N` is 1E-130 to 9.9999999999999999999999999999999999999E+125,
  with the same range below zero. 1E126 fails with `Number overflow`, and
  1E-131 with `Number underflow` (recorded: "a number above the range" and
  "a number below the range").
- The server writes each number with no exponent. 1E-130 came back as `0.`
  and 129 zeros and a `1`, and 9.9999999999999999999999999999999999999E+125
  as 38 nines and 88 zeros. `0.10` came back as `0.1`, `-0` as `0`, and
  `1.50` and `15E-1` as `1.5` (recorded: "empty values and the range of
  numbers", "read the parameters" and "lead: read the number with an
  exponent"). Alternator wrote the same text (recorded: "empty values
  through the API").
- A parameter `{"N": "abc"}` fails with `A value provided cannot be
  converted into a number` (recorded: "a number that is not a number").
- `B` is a string in base64. It can be empty. A parameter that is not
  base64 fails with HTTP 500 and `InternalFailure` on DynamoDB Local
  (recorded: "a binary that is not base64").
- `BOOL` is a JSON boolean. `NULL` is `{"NULL": true}`, and
  `{"NULL": false}` fails with `Null attribute value types must have the
  value of true` (recorded: "a NULL that is false").
- `M` is an object of values, and `L` is an array of values. Each can be
  empty and can nest the other (recorded: "every type" and "empty values and
  the range of numbers").
- `SS`, `NS` and `BS` are arrays of strings, of numbers as strings, and of
  base64. An empty set fails with `Empty bags are not supported`, a value
  twice with `Input collection contains duplicates`, and two types with
  `DynamoDB only supports either numbers or strings in bags` (recorded: "an
  empty set", "a set with one value twice" and "a set of two types").
  DynamoDB Local sorted each set, and Alternator kept the order of the
  insert (recorded: "every type" and "every type through the API").
- PartiQL writes a set as `<<'a', 'b'>>`, a map as `{'k': 1}` and a list as
  `[1, 'a']`, and a literal of each type kept its type, with `1.50` as
  `1.5` (recorded: "literals of each type" and "read the literals").
- A parameter of each type gave back the same type (recorded: "a parameter
  of each type" and "read the parameters").
- An update can give an attribute another type, such as a string where a
  number was (recorded: "a type that a value can change").
- There is no date, time, UUID or decimal type of its own. `DATE
  '2026-10-01'` fails with `Unsupported data type: UNKNOWN`, `CAST` fails,
  and `utcnow()` is not a function (recorded: "lead: a date literal", "lead:
  a cast" and "a date function").

These facts come from the sources, and are not measured:

- boto3 reads an `N` as a `Decimal` in a context of 38 digits, with the
  exponents -128 to 126, and traps a number that it would round. It reads
  `B` as its own type `Binary`, and a set as a Python `set` (boto3).
- godynamo reads each value with `attributevalue.Unmarshal` of the Go SDK
  into an `interface{}`, which gives a `float64` for `N`, and ignores the
  error (godynamo, `stmt.go`).
- An item is at most 400 KB, and a map or a list nests to 32 levels (Gemini
  and DeepSeek).

## Parameters

These facts were recorded on each release of DynamoDB Local:

- A parameter is `?`, and `Parameters` is an array of typed values, such as
  `[{"S": "t1"}, {"N": "42"}]`, in the order of the `?` (recorded:
  "positional parameters").
- Too few values fail with `Number of parameters in request and statement
  don't match.` Too many values are accepted with no error (recorded: "too
  few parameters" and "too many parameters").
- `:p` fails, and `ExpressionAttributeValues` is ignored (recorded: "a named
  parameter" and "lead: expression attribute values").
- `$1` is no parameter. `WHERE pk = $1` ran with no error and gave no items
  (recorded: "a numbered parameter").
- A `?` in a string is text (recorded: "a question mark in a string").
- A value that is not typed, such as `"t1"`, fails with HTTP 500 and
  `InternalFailure` (recorded: "a parameter that is not typed").
- A value of the wrong type for the key fails with `Key attribute's data
  type should match its data type in table's schema` (recorded: "a parameter
  of the wrong type for the key").
- A parameter cannot name a table (recorded: "a parameter as a table
  name").
- A number of 38 digits binds with every digit (recorded: "a number
  parameter with 38 digits").
- The API of items on Alternator takes named values in
  `ExpressionAttributeValues`, such as `:p` (recorded: "parameters through
  the API").

## Transactions

These facts were recorded on each release of DynamoDB Local:

- `ExecuteTransaction` runs its statements at once, and all of them or none.
  A transaction whose second statement updates a missing item fails with
  `TransactionCanceledException`, with `CancellationReasons` for each
  statement, and the first insert is not kept (recorded: "a transaction
  that fails" and "read after the transactions").
- A transaction holds reads only or writes only. A read and a write fail
  with `ExecuteTransaction API does not support both read and write
  operations in the same request.` (recorded: "a transaction of a read and
  a write").
- A transaction of reads gives one `Item` for each statement, and `{}` for a
  key that has no item (recorded: "a transaction of reads"). A read must
  name the whole key (recorded: "a read that is not by key in a
  transaction").
- `EXISTS(SELECT ... WHERE <key> AND <condition>)` is a condition in a
  transaction of writes, and the transaction fails when it is false
  (recorded: "a condition in a transaction" and "a condition that fails in
  a transaction").
- Two writes to one item fail, and so do 101 statements and `RETURNING`
  (recorded: "two writes to one row in a transaction", "a transaction of 101
  statements" and "a transaction with RETURNING").
- `ClientRequestToken` makes a transaction run once. The same token with the
  same statements answers success again, and with other statements fails
  with `IdempotentParameterMismatchException` (recorded: "a transaction with
  a token", "the same token again" and "the same token with another
  statement"). A second recording on one server, within minutes, sent the
  token of the first run, and the server answered success and did not run
  the update (measured on 2026-10-01). So each recording ran on a new
  server.
- No statement opens a transaction across requests. `BEGIN TRANSACTION`
  fails (recorded: "lead: begin a transaction in a statement").
- `BatchExecuteStatement` is not a transaction. Each statement has its own
  result or `Error`, with HTTP 200 (Errors).
- Alternator refuses `ExecuteTransaction` and `TransactWriteItems` (recorded:
  "a transaction of writes" and "a transaction through the API").

These facts come from the sources, and are not measured:

- godynamo keeps the statements of a transaction on the client, and sends
  them in one `ExecuteTransaction` at `Commit`. A query in a transaction is
  not supported (godynamo, `conn.go` and `stmt_document.go`).

## Errors

These facts were recorded on each release:

- An error answers HTTP 400 with `{"__type": ..., "Message": ...}` on
  DynamoDB Local, and `{"__type": ..., "message": ...}` on Alternator. The
  type names a namespace and a name, such as
  `com.amazon.coral.validate#ValidationException` and
  `com.amazonaws.dynamodb.v20120810#ResourceNotFoundException` (recorded:
  "a syntax error" and "an unknown table").
- A syntax error gives `Statement wasn't well formed, can't be processed:`
  and the statement (recorded: "a syntax error").
- A write that a condition stops gives `ConditionalCheckFailedException`.
  With `ReturnValuesOnConditionCheckFailure` set to `ALL_OLD`, the error
  holds the `Item` (recorded: "crud: a conditional update that fails" and
  "crud: return the values when a condition fails").
- An insert of a key that exists gives `DuplicateItem` (recorded: "crud:
  insert a key that exists").
- A body that is not JSON, and a body with no `Statement`, give HTTP 500 and
  `InternalFailure` on DynamoDB Local, and HTTP 400 and `Unsupported
  operation ExecuteStatement` on Alternator (recorded: "a body that is not
  JSON" and "a statement that is missing").
- An unknown operation gives `UnknownOperationException` (recorded: "an
  unknown operation").
- `BatchExecuteStatement` answers HTTP 200 when one statement fails, with an
  `Error` of `Code` and `Message` in place of its result (recorded: "an
  error in a batch" and "an error and a success in a batch").
- An error after some rows is an error on a later page. A `NextToken` of
  another statement failed after the first page (recorded: "the next page of
  another statement"). One page never holds rows and an error.
- No recorded answer had HTTP 429, on either flavor.

These facts come from the sources, and are not measured:

- The service of AWS limits the rate with
  `ProvisionedThroughputExceededException` and `ThrottlingException`, and
  the SDKs of AWS retry them (Gemini and DeepSeek).

## Cancellation and timeouts

- A client that gave up after 1 ms on a scan of 300 items left the server
  well. The next statement answered (recorded: "a statement after the
  client left").
- No operation cancels a statement. `CancelStatement` gives
  `UnknownOperationException` on each flavor (recorded: "lead: cancel a
  statement").
- Each request is one page, and a page ends at about 1 MB (Responses). So a
  request does not run for long.

## Statements

These facts were recorded on each release of DynamoDB Local:

- A text holds one statement. Two statements fail as a syntax error
  (recorded: "two statements in one text").
- Comments `--` and `/* */` are allowed, and so is a semicolon at the end
  (recorded: "comments" and "a statement that ends with a semicolon").
- An empty statement fails (recorded: "an empty statement").
- Keywords can be lower case, and a name can be in double quotes (recorded:
  "a lower case statement" and "a table name in quotes").
- Some words are reserved. `WHERE pad = 'none'` fails as a syntax error, and
  `WHERE "pad" = 'none'` runs (recorded: "crud: a reserved word as a name"
  and "a limit with a filter that matches nothing").
- `SELECT` reads one table or one index, as `"table"."index"` (recorded:
  "schema: read through the global index" and "crud: a join"). A `SELECT`
  with no `WHERE` on the key reads the whole table (recorded: "select every
  attribute").
- `INSERT INTO t VALUE {...}` writes one item. `INSERT` of a key that exists
  fails, and so does an insert of two items (recorded: "crud: insert", "crud:
  insert a key that exists" and "crud: insert of several rows").
- `UPDATE` and `DELETE` must name the whole key in the `WHERE` (recorded:
  "crud: update with no full key" and "crud: delete with no full key").
- `UPDATE` of a missing item fails with `ConditionalCheckFailedException`,
  and `DELETE` of a missing item succeeds (recorded: "crud: update a row
  that does not exist" and "crud: delete a row that does not exist").
- `UPDATE` and `DELETE` give no count of items. Each answers
  `{"Items": []}`. `RETURNING ALL OLD *`, `ALL NEW *`, `MODIFIED OLD *` and
  `MODIFIED NEW *` give the item, and an `INSERT` takes no `RETURNING`
  (recorded: "crud: update", "crud: update returning the old row", "crud:
  update returning what changed" and "crud: insert returning").
- `ReturnConsumedCapacity` gives no `ConsumedCapacity` on DynamoDB Local
  (recorded: "crud: the consumed capacity" and "lead: the capacity of an
  update").
- `SET`, `REMOVE`, `set_add`, `list_append`, `attribute_exists`,
  `begins_with`, `contains` and `IS MISSING` work (recorded under item 3).
- `UPSERT`, `REPLACE`, `GROUP BY`, `CREATE TABLE`, `CREATE INDEX`, `CREATE
  VIEW` and `ALTER TABLE` fail (recorded: "crud: upsert", "crud: replace",
  "crud: a group by" and the requests of `schema:`).

## Principals

- DynamoDB Local checks no key. It answered a wrong secret with the list of
  tables (recorded: "a wrong secret"). It refuses a request with no
  signature with `MissingAuthenticationToken` (measured with `curl` on
  2026-10-01). So it has no ordinary user, and the manifest says why.
- Alternator checks the signature. A wrong secret fails with
  `UnrecognizedClientException`, with the message `wrong signature` on
  2026.3 and `The security token included in the request is invalid.` on
  2025.1 (recorded: "a wrong secret").
- On Alternator, `dbmeta_user` reads every table, and is refused a write
  with `AccessDeniedException`, `MODIFY access on table ... is denied`, and
  a table with `CREATE access on ALL KEYSPACES is denied` (recorded: "a read
  as each user", "a write as each user" and "a table as each user").
- Neither flavor gives a version (Flavors). `usql` sends `SELECT version();`
  to a driver with no `Version`, and DynamoDB Local refuses it as a syntax
  error (recorded: "the version statement of usql").
- On the first start of `alternator-2025.1`, `dbrun start` stopped with exit
  status 1, and a second `dbrun start` said that it was up. Alternator then
  answered `User not found: dbmeta_user`, so `Init` had not run. `dbrun
  remove` and `dbrun start` fixed it (measured on 2026-10-01).

## Flavors

- Alternator runs no PartiQL. It refuses `ExecuteStatement`,
  `BatchExecuteStatement`, `ExecuteTransaction` and `TransactWriteItems`
  (Summary and Transactions). It answers `CreateTable`, `DescribeTable`,
  `ListTables`, `GetItem`, `PutItem`, `Query`, `Scan`, `UpdateTimeToLive`
  and `DescribeEndpoints` (recorded).
- DynamoDB Local refuses `DescribeEndpoints`, and Alternator answers it
  with its own address. Alternator refuses `DescribeLimits`, and DynamoDB
  Local answers it (recorded: "the endpoints" and "the limits").
- A signed `GET /` gives HTTP 500 and `InternalFailure` on DynamoDB Local,
  and `healthy: 127.0.0.1:<port>` as text on Alternator. `GET /localnodes`
  gives the nodes of Alternator, and `GET /shell/` and `GET /_readiness`
  give HTTP 404 on Alternator and HTTP 500 on DynamoDB Local (recorded: "the
  health", "lead: the nodes of Alternator", "lead: the shell of DynamoDB
  Local" and "lead: the readiness of Alternator").
- DynamoDB Local sends `Server: Jetty(12.1.11)`, `X-Amzn-Requestid` and
  `X-Amz-Crc32`. Alternator sends none of them (recorded).
- The `TableArn` of `DescribeTable` starts with
  `arn:aws:dynamodb:ddblocal:` on DynamoDB Local and with
  `arn:scylla:alternator:` on Alternator (recorded: "describe a table").
- An error names its text `Message` on DynamoDB Local and `message` on
  Alternator (Errors).
- Neither flavor gives the version of its release (recorded under item 9).
- The service of AWS was not measured, because it fails R.

## Interfaces

Step 10 writes this table from the code.

## Faults

These are faults of godynamo that a driver here does not repeat (godynamo,
read on 2026-10-01):

- With `SELECT *`, or with a projection that its expression does not
  match, the columns are the names of every attribute of every item of the
  result, sorted (`stmt.go`, `init`). Hard rule 3 and D18 forbid both.
- The type of each column comes from the first item that has it, and
  `attributevalue.Unmarshal` ignores its error (`stmt.go`).
- An `N` becomes a `float64`, which loses the digits after the
  fifteenth (godynamo, through the Go SDK). D19 forbids it.
- A `SELECT` reads every page into memory before the first row
  (`executeSelectContext` in `conn.go`). D25 forbids it.
- A method with no context makes one with `context.Background` and a
  goroutine that sleeps for the timeout (`newContext` in `conn.go`). Hard
  rule 4 forbids it.
- It takes `LIMIT` out of the statement with a regular expression and pages
  to that count, and adds `RETURNING ALL OLD *` to each `UPDATE` and
  `DELETE` (`stmt_document.go`). So it changes the statement of the
  caller.
- It has a language of its own for DDL, such as `CREATE TABLE ... WITH
  pk=...`, matched by regular expressions (`stmt.go` and `stmt_table.go`).
- Its DSN is not a URL, and it reads keys from the environment when the DSN
  lacks them (`driver.go`). D27, D35 and hard rule 2 forbid both.
- `RegisterAWSConfig` sets a package variable that every connection reads
  (`driver.go`). Hard rule 2 forbids it.
- `CheckNamedValue` accepts any value (`conn.go`).
- It imports the SDK of AWS, which D13 does not allow here.
- A `ConditionalCheckFailedException` of an `UPDATE` or a `DELETE` becomes
  success with no error (`stmt_document.go`).

## Second opinions

Step 7 asked Gemini and DeepSeek on 2026-10-01 what step 6 did not find,
and tested each lead on the server.

- Parameters. Both said that `?` is the only form. Gemini proposed
  `ExpressionAttributeValues` in the body of `ExecuteStatement`, and
  DeepSeek `:name` and `$1`. The server ignores the first, refuses `:name`,
  and reads `$1` as no parameter.
- Transactions. Both said that no transaction stays open across requests.
  `BEGIN TRANSACTION` fails. DeepSeek named `ClientRequestToken`, which
  works.
- Paging. Both said that `NextToken` is the only way. Gemini proposed
  `Segment` and `TotalSegments`, which `ExecuteStatement` ignores. The page
  of 1 MB holds on DynamoDB Local.
- Types. Both said that there is no date type, and that `N` is the decimal.
  A `DATE` literal and `CAST` fail. A number with an exponent comes back
  with none.
- Cancel. Both said that no operation cancels a statement.
  `CancelStatement` is unknown on each flavor.
- Flavors. Gemini proposed the `Server` header, `GET /shell`, `GET /` and
  `DescribeLimits`, and said that Alternator names itself in `Server`. It
  does not. DeepSeek proposed `DescribeEndpoints` and the fields of
  `DescribeTable`, which differ.
- A count of changed items. Both proposed `RETURNING`, which gives the item,
  and Gemini the consumed capacity, which DynamoDB Local does not send.
- PartiQL on Alternator. Both said that Alternator has none, and the server
  agrees. Gemini said that CQL on port 9042 reads the same tables. `dbrun`
  publishes one port, 8000, so this was not measured.
- In step 5a, Gemini said that `UPDATE` of a missing item creates it, and
  DeepSeek that it fails. It fails. DeepSeek said that `INSERT` takes
  `RETURNING`, and it does not.

Step 8a asked both models on 2026-10-02 to review the mapping of the types
against [TYPES.md](TYPES.md) and D135, in separate conversations:

- Both chose the kind decimal for `N`, and not the kind number, because the
  server names `N` as an exact decimal of 38 digits, and an `int64` or a
  `float64` loses digits or range. The server agrees: it kept 38 digits and
  the range of 1E-130 to 9.99E+125 (Types).
- Both kept `[]any` for a set, because [TYPES.md](TYPES.md) names it.
  DeepSeek said that `[]string`, `[]*apd.Decimal` and `[][]byte` read more
  naturally in Go, and that `[]any` loses that a set has no duplicates.
- Both agreed with the scan type `interface {}` for every column, because a
  column has no type. The server agrees: one attribute held an `N` and then
  an `S` (recorded: "a type that a value can change").
- Both said that NULL and a missing attribute become one nil unless the
  driver keeps them apart, which step 9 decides (D18).
- Both said that `*apd.Decimal`, `[]any` and `map[string]any` are not types
  of `driver.Value`, so a caller scans them into `*any` or into a type that
  scans them. Gemini said that the database type of a column is empty,
  which the table says.

## Open questions

Each one waits for Ken.

1. Alternator fails S, because it runs no PartiQL. D162 names it a flavor
   of this driver. A driver that sends PartiQL serves DynamoDB Local and
   the service of AWS only. Is Alternator dropped from this driver, or does
   the driver speak the API of items as a second language? The second
   language is JSON that a person types, such as a `Scan`, and no SQL.
2. The row of DynamoDB in [TARGETS.md](TARGETS.md) does not yet name the
   result of R, H and S, which this document holds.
3. The scheme of `dburl` is `godynamo`, with `dynamodb` as an alias, and
   the host of its URL is the region. D28 and D30 say that this driver
   registers `dynamodb`, and D27 says that the URL is standard. The host
   and the endpoint are a choice of step 9.
4. A value has its own type, and a column has none. Step 8a maps `N` to
   `*apd.Decimal`. The kind number would give an `int64` for an integer
   that fits. Ken reviews this in step 8a.
5. Step 9 decides whether a missing attribute and NULL are one value (D18),
   where the columns come from for `SELECT *`, and whether `RowsAffected`
   is known.
6. The command `dbimptest/cmd/record` gained AWS Signature Version 4
   signing for these recordings, and [DESIGN.md](DESIGN.md) does not
   describe it yet.
