# Azure Cosmos DB

This file holds what is known about Azure Cosmos DB over its REST API, for a
possible driver `cosmos` (W36 and D184). The headings are the template of
[DRIVER.md](DRIVER.md). A fact is "recorded", with the name of its request in
quotes, "measured", with the date, or "not measured", with its source.

Step 6 ran twice on 2026-10-10, with one script, `requests.json`. The script
makes the database `dbimp_it` with the containers `kv`, `bulk`, `hier`, `uk`
and `nopolicy`, and it seeds `bulk` with 2,500 documents through 25 batches. It
drops the database at its end.

- The first run was on the emulator. The release is `cosmos-EN20260907`, which
  `dbrun` starts from the image `mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator`
  with the tag `vnext-EN20260907` (read of `dbmeta/container/cosmos.go`,
  2026-10-10).
- The second run was on a hosted account of Azure Cosmos DB that Ken set up. It
  is the API for NoSQL on the free tier, in the region East US, with 400 request
  units a second. The release name is `cosmos`. The run created the database
  with the header `X-Ms-Offer-Throughput: 400`, so its containers share that
  throughput (recorded: "setup: create the database"). It waited 1 to 2 seconds
  between the batches that seed `bulk`.

The files under `testdata/cosmos/` are the hosted run, named `cosmos-001-...`
to `cosmos-217-...`. The files of the emulator run, named
`cosmos-EN20260907-...`, are not in that folder (read of the folder,
2026-10-10), although `manifest.json` still lists them. A request name in
quotes is the `name` in `requests.json`, and it names a file of the hosted run
unless the text says "emulator". Every fact that the text marks "emulator" was
recorded in the first run, and no file in the folder shows it now.

The recorder signs each request with the master key, and it replaces the key
with `REDACTED` in every file. No file holds the key, and this file names no
key and no name of the account.

What the emulator is. The header `Server` of every answer is `PGSQL`, and the
header `X-Ms-Gatewayversion` is `2.0.0-unknown` (emulator, "the account"). The
errors of the emulator name `PostgresError`, so it seems to keep its documents in
PostgreSQL. It is not the service. What the hosted account is. The header
`Server` is `Microsoft-HTTPAPI/2.0`, `X-Ms-Gatewayversion` is `version=2.14.0`
and `X-Ms-Serviceversion` is `version=2.14.0.0` (recorded: "read a document
that does not exist"). Where the two differ, this file writes both and says
which is which.

## Summary

- Product: Azure Cosmos DB, with the API for NoSQL, which was called the SQL
  API. Name in `dbrun`: `cosmos`. The kind is `container`, the tier is `staged`
  and the release is `cosmos-EN20260907` (measured, `dbrun dsn --json
  cosmos-EN20260907`, 2026-10-10). The emulator starts in 8 seconds. The hosted
  account is not a release of `dbrun`.
- R: yes. The hosted account needs the endpoint and the key of the account, and
  both are Ken's. `dbrun` starts the emulator, and it publishes one port. The
  emulator speaks HTTPS only, with a certificate that no authority signed
  (measured, `curl`, 2026-10-10: a request that verifies the certificate fails
  with "self-signed certificate in certificate chain", and a plain HTTP request
  gets an empty reply). A client must accept that certificate for the emulator,
  as the flag `-insecure` of the recorder does. `dbrun dsn --json` prints no
  `api` address for this entry. The recorder takes the key from the field `user`
  of the principal.
- H: yes, on both. Every operation is an HTTPS request with a JSON body, and every
  answer is JSON (recorded: "the account", "a query that selects every
  document"). It needs no binary encoding.
- S: yes, for reading. The language is a SQL dialect that has `SELECT`, `FROM`,
  `WHERE`, `JOIN` inside a document, `GROUP BY`, `ORDER BY`, `TOP`,
  `OFFSET LIMIT`, `DISTINCT`, aggregates and subqueries. The hosted gateway
  answered a join, `IN`, `LIKE`, a subquery and an array function (recorded: "a
  join inside a document", "in and like", "a subquery", "an array function"). It
  planned the others and did not run them across partitions (see The cross
  partition query). The language has no `INSERT`, `UPDATE` or `DELETE`, and
  both servers answer each with HTTP 400 (recorded: "a statement of INSERT", "a
  statement of UPDATE", "a statement of DELETE"). A write is a REST request on a
  document, so a statement that writes needs a language that the driver
  defines. The priority is Ken's to set (TARGETS.md has the row, W36).
- Whether it can be a driver: yes, and no condition of "When it cannot be a
  driver" holds. The columns are not known before the first row, but D18
  supplies them (see Responses). A result is a list that arrives in pages by the
  header `X-Ms-Continuation`, so it can be read one page at a time (recorded:
  "the first page with the default size"). Nothing cuts a result short. No
  package that Ken refused is needed. The condition that the emulator hid is now
  measured. The hosted gateway refuses an aggregate, `TOP`, `ORDER BY`,
  `OFFSET LIMIT` and `DISTINCT` over a container that the query does not
  limit to one partition key or to one range, and it answers HTTP 400 with
  substatus 1004 (recorded: "the count of the large container", "top with a small
  page", "an ordered query with a page of 2", "offset and limit past the end",
  "distinct value"). It refuses a `GROUP BY` and an aggregate with no `VALUE` with
  another HTTP 400 (recorded: "a group by", "an aggregate of every kind"). So a
  driver that sends a statement as it is fails on these. The driver either runs
  that part of the query itself, or sends only a query that names one partition
  key, or refuses the rest. That is a question for Ken. The facts are under
  The cross partition query.
- What the emulator did that the hosted account does not, and the reverse:
  - Signature. The emulator did not look at it. A request with no signature
    answered HTTP 200 (emulator, "a request that the recorder sends with no
    signature"). A key of the wrong bytes and a date 20 minutes away from the
    clock also answered HTTP 200 (measured, a script, 2026-10-10, not recorded).
    The hosted account answered HTTP 401 to a request with no `Authorization`
    header (recorded: "a request that the recorder sends with no signature"). A
    wrong key and a date out of the window are not measured on the hosted
    account, because the recorder cannot build them.
  - Partition key ranges. Both had one range, `0`, from `""` to `FF` (recorded:
    "the partition key ranges", "the partition key ranges of the large
    container"). So neither shows a fan out. A split of a partition is not
    measured.
  - Order of the attributes in a `SELECT`. The emulator sorted them by length and
    then by name. The hosted account keeps the order of the statement (recorded:
    "a projection in the order of the statement").
  - The plan. The emulator planned no `ORDER BY`, `GROUP BY` or `TOP`. The hosted
    account plans all of them (recorded: "the query plan of a group by and an
    order by", "lead: the query plan of an ordered query", "lead: the query plan
    of distinct and top").
  - Cross partition queries. The emulator answered them whole. The hosted
    gateway refuses them (see above).
  - Server side code. The emulator ran no user defined function and no stored
    procedure. The hosted account ran both (recorded: "a query that calls a user
    defined function", "a call of the stored procedure"). It created a trigger
    (recorded: "a trigger"). A write that fires the trigger is not measured.
  - HTTP 304. The emulator answered HTTP 200 to `If-None-Match`. The hosted
    account answered HTTP 304 (recorded: "lead: read with the etag that the
    document has").
  - HTTP 429. The emulator cannot give it. The hosted account gave none: 25
    batches of 100 documents cost 704.76 request units each on a container with
    400 request units a second, with 1 to 2 seconds between them, and none was
    refused (recorded: "setup: seed the container bulk, batch 25"). Not measured.
  - Page sizes. See Responses.
  - The size of a request unit. The emulator charged `1` for every answer. The
    hosted account gave real charges, such as 1 for a point read and 2.26 for a
    query of one document (recorded: "a read with the etag of the document", "a
    query that selects every document").
  - Hierarchical keys. The emulator took a container with two paths. The hosted
    account refused it at the version `2018-12-31` of the API (recorded: "setup:
    create the container hier").
  - Consistency. The emulator took `Strong`. The hosted account is `Session` and
    answered HTTP 400 (recorded: "lead: strong consistency and a session
    token").
  - The error text and the status of a body that is not JSON. See Errors.
- Scheme in `dburl`: `Name` is `cosmos`, the alias is `cm`, the generator is
  `GenSchemeHost("cosmos")`, the `Dialect` is `cosmos`, and `GoPackage` is
  already `github.com/xo/dbimp/cosmos` (read of `dburl/scheme.go` at commit
  `fffaa03`, 2026-10-10). The generator rewrites only the scheme, and it needs
  a host. The test of `dburl` writes `cosmos://key@host/db`, with the key as the
  user name (read of `dburl/dburl_test.go`, 2026-10-10).
- Driver of `usql` now: `btnguyen2k/gocosmos` v1.1.0, released on 2024-02-13
  (read of `usql/go.mod`, `usql/drivers/cosmos/cosmos.go` and the source in the
  module cache, 2026-10-10). The file of `usql` registers the driver with an
  empty `drivers.Driver{}`, so `usql` has no `Version` statement for Cosmos DB
  and no hook that strips a semicolon in that file. `gocosmos` has its own SQL
  for writes (see Statements).
- What `dbmeta` has: a staged container entry and no model. Ken decided that
  `dbmeta` builds no model for a hosted service other than Redshift and
  Snowflake (dbmeta D194). The comment of the entry says that Microsoft marks
  the feed of databases and the feed of collections as not yet done in the
  emulator.

## Requests

- Every request goes to `https://<host>:<port>` with `X-Ms-Date`, `X-Ms-Version`
  and `Authorization`. The recorder sends `X-Ms-Version: 2018-12-31`. Both servers
  accepted it, except for the container with two partition key paths on the
  hosted account (recorded: "setup: create the container hier"). The endpoint of
  the hosted account has the form `https://<account>.documents.azure.com:443/`
  (recorded: "the account", the member `writableLocations`).
- The account is `GET /` (recorded: "the account"). The body names the
  `id`, the `writableLocations` and the `readableLocations`, the member
  `userConsistencyPolicy` (`Session` on the hosted account) and the
  `queryEngineConfiguration` as JSON text, such as `maxJoinsPerSqlQuery` 10 and
  `maxSqlQueryInputLength` 524288.
- A database is `POST /dbs` with `{"id": ...}`, and `GET /dbs` lists them
  (recorded: "setup: create the database", "the list of databases"). The header
  `X-Ms-Offer-Throughput: 400` on the create gives the database shared
  throughput. The comment in `dbmeta` says that the emulator does not do these
  feeds. The emulator answered both (emulator, "the list of databases", "the
  list of containers"), and so did the hosted account (recorded: "the list of
  databases", "the list of containers").
- A container is `POST /dbs/{db}/colls` with `id` and `partitionKey`, which has
  `paths` and `kind` (recorded: "setup: create the container kv"). A body with no
  `partitionKey` is HTTP 400 (recorded: "a container with no partition key"). On
  the hosted account the text is "Shared throughput collection should have a
  partition key", because the database has shared throughput. A name with `/` is
  HTTP 400 (recorded: "a container with a name that is not allowed"). A container
  with `kind` `MultiHash` and two paths is HTTP 400 on the hosted account at
  `X-Ms-Version: 2018-12-31`, with the text "The 'kind' value 'MultiHash'
  specified in the partition key definition is invalid. Please choose 'Hash'
  partition type" (recorded: "setup: create the container hier"). A newer version
  of the API was not tried. Lead for the next run.
- A document is `POST /dbs/{db}/colls/{c}/docs` with the header
  `X-Ms-Documentdb-Partitionkey`, whose value is a JSON array, such as
  `["a"]` (recorded: "a document is created"). The document must have an `id`
  (recorded: "a document with no id"). A header that does not match the
  attribute is HTTP 400 (recorded: "a document with the wrong partition key").
  A create with no header is HTTP 400 on the hosted account, with substatus 1001
  and the text "The partition key supplied in x-ms-partitionkey header has fewer
  components than defined in the the collection" (recorded: "a document with no
  partition key header"). The emulator took the key from the document (emulator).
  A read with no header is HTTP 400 on both (recorded: "read a document with no
  partition key").
- A read is `GET .../docs/{id}`, a replace is `PUT`, a delete is `DELETE` and
  returns HTTP 204, and a patch is `PATCH` with `{"operations": [...]}`
  (recorded: "read a document", "replace a document", "delete a document",
  "patch a document"). An upsert is `POST` with `X-Ms-Documentdb-Is-Upsert:
  True`. It answers HTTP 201 for a new document and HTTP 200 for an existing
  one (recorded: "an upsert of a new document", "an upsert of an existing
  document"). The charges on the hosted account were 1 for a read, 6.67 for a
  create and for a delete, 10.67 for an upsert of an existing document and
  11.05 for a replace.
- A query is `POST .../docs` with the content type `application/query+json`,
  and the body `{"query": ..., "parameters": [...]}` (recorded: "a query that
  selects every document"). The header `X-Ms-Documentdb-Isquery: True` is what
  the documentation names. A request without it and with the content type of a
  query was taken as a query on the hosted account, because the answer was about
  the missing header of cross partition (recorded: "a query without the header
  of a query"). The same body with the content type `application/json` is HTTP
  400, because the gateway reads it as a document and wants the partition key
  header (recorded: "a query that has the content type of a document"). A body
  of plain text with the content type `application/sql` was taken as a query on
  the hosted account: it got the refusal of substatus 1004 with a plan (recorded:
  "lead: a statement as plain text"). The emulator answered HTTP 403 (emulator).
- The header `X-Ms-Documentdb-Query-Enablecrosspartition: True` lets a query
  span partitions. A query with neither that header nor the header of a
  partition key is HTTP 400 on the hosted account, with the text "Cross
  partition query is required but disabled" (recorded: "a query for several
  partitions with no header of cross partition"). The emulator answered it
  (emulator).
- A query for one partition key sends `X-Ms-Documentdb-Partitionkey` (recorded:
  "a query for one partition key").
- The partition key ranges are `GET .../pkranges`. Both containers had one
  range, `0`, from `""` to `FF`, with `status` `online` and `throughputFraction`
  1 (recorded: "the partition key ranges", "the partition key ranges of the
  large container").
- The query plan is the same query with `X-Ms-Cosmos-Is-Query-Plan-Request:
  True` and the header `X-Ms-Cosmos-Supported-Query-Features`. See The cross
  partition query. The emulator answered with empty lists for `ORDER BY` and
  `GROUP BY` (emulator).
- Other paths answer HTTP 400 with the text "Request url is invalid" on the
  hosted account (recorded: "a path that does not exist", "a method that does not
  exist", "a view"). The emulator wrote `{"error":"Invalid path or method"}`
  (emulator).
- `OPTIONS /` answers HTTP 403 with `{"code":"Forbidden"}` on the hosted account
  (recorded: "a request for the options of the server"). The emulator answered
  HTTP 200 with no body (emulator).
- Stored procedures, triggers and user defined functions are created with
  `POST .../sprocs`, `.../triggers` and `.../udfs`, and the hosted account
  answered HTTP 201 for each (recorded: "a stored procedure", "a trigger", "a
  user defined function"). A stored procedure runs with `POST .../sprocs/{id}`
  and a body `[]`. The answer was HTTP 200 with the body `1` (recorded: "a call
  of the stored procedure"). A query ran the function: `SELECT udf.plus1(1) AS v`
  gave `[{"v": 2}]` (recorded: "a query that calls a user defined function"). The
  master key did all of this. A trigger fires only when a write names it, and no
  recording does that.
- Authentication is the master key. The signature is the Base64 text of
  HMAC-SHA256 over `"{verb}\n{resourceType}\n{resourceLink}\n{date}\n\n"`, with
  the verb, the type and the date in lower case, and the key decoded from
  Base64. The header is the URL encoding of
  `type=master&ver=1.0&sig=<signature>` (not measured as a form, source:
  "Access Control on Azure Cosmos DB Resources"). The recorder uses exactly this
  form, and the hosted account accepted every request that carried it. A request
  with no `Authorization` header is HTTP 401 with the text "Required Header
  authorization is missing" (recorded: "a request that the recorder sends with no
  signature"). The date header carries the clock of the recorder, so a date out
  of the window was not sent.

### The cross partition query

Every fact here is from the hosted account. The container `bulk` held 2,500
documents. It had one partition key range, `0`, from `""` to `FF` (recorded:
"the partition key ranges of the large container"). The container `kv` had the
same one range (recorded: "the partition key ranges"). So no recording shows a
fan out over two ranges.

What the gateway refuses. A query with
`X-Ms-Documentdb-Query-Enablecrosspartition: True` and no range header is HTTP
400 with the header `X-Ms-Substatus: 1004` and the text "The provided cross
partition query can not be directly served by the gateway", when it holds one of
these:

- an aggregate in the form `SELECT VALUE COUNT(1)` (recorded: "the count of the
  large container").
- `TOP`, also with a parameter (recorded: "top with a small page", "a parameter
  in TOP").
- `ORDER BY`, with one key or two (recorded: "an ordered query with a page of
  2", "order by two attributes").
- `OFFSET LIMIT`, also with parameters (recorded: "offset and limit past the
  end", "a parameter in OFFSET and LIMIT").
- `DISTINCT` (recorded: "distinct value", "lead: distinct with a small page").

An aggregate that has no `VALUE` is a different HTTP 400, with the text "Cross
partition query only supports 'VALUE <AggregateFunc>' for aggregates" and no
plan (recorded: "an aggregate with no alias", "an aggregate of every kind"). A
`GROUP BY` with an aggregate has the same text (recorded: "a group by", "a group
by on the partition key"). A plain `SELECT c.id, c.n FROM c` is answered, in
pages (recorded: "the first page with the default size"). A join inside a
document, `IN`, `LIKE`, a subquery and an array function are answered too
(recorded: "a join inside a document", "in and like", "a subquery", "an array
function").

The plan is in the refusal. The body of the HTTP 400 with substatus 1004 has the
member `additionalErrorInfo`, which is JSON text. It holds the plan that the plan
request returns (recorded: "the next query after an abandoned query", "lead: a
statement that expects no continuation", "an ordered query with a page of 2").
So a client that sends the query and reads the refusal gets the plan without a
second request. The header `X-Ms-Documentdb-Query-Iscontinuationexpected:
False` did not change the refusal.

The plan request. It is `POST .../docs` with the content type
`application/query+json`, the header `X-Ms-Cosmos-Is-Query-Plan-Request: True`
and the header `X-Ms-Cosmos-Supported-Query-Features`, and the same body as the
query. The recorder sent this value for the features: `Aggregate,
CompositeAggregate, Distinct, GroupBy, MultipleOrderBy, MultipleAggregates,
OffsetAndLimit, OrderBy, Top, NonValueAggregate, DCount, NonStreamingOrderBy`
(recorded: "the query plan of an aggregate"). The answer is HTTP 200. Without
the header of features, the plan request of an aggregate is HTTP 400 with the
text "Query contains 1 or more unsupported features ... Query contained
Aggregate, which the calling client does not support" (recorded: "the query
plan with no list of features"). A shorter list of features was not measured.

The members of the answer. `partitionedQueryExecutionInfoVersion` is 2. The member
`queryInfo` has `distinctType` (`None` or `Unordered`), `top`, `offset`,
`limit`, `orderBy` (a list of `Ascending` or `Descending`),
`orderByExpressions` (a list of text, such as `c.n`), `groupByExpressions`,
`groupByAliases`, `aggregates` (a list, such as `["Count"]`),
`groupByAliasToAggregateType`, `rewrittenQuery`, `hasSelectValue`, `dCountInfo`
and `hasNonStreamingOrderBy`. The member `queryRanges` is a list of `min`,
`max`, `isMinInclusive` and `isMaxInclusive`, and here it was one range, `""`
to `FF`. The member `hybridSearchQueryInfo` was null (recorded: "the query plan
of an aggregate", "the query plan of a group by and an order by", "lead: the
query plan of an ordered query", "lead: the query plan of distinct and top",
"the query plan of a projection").

What the plan held for each form:

- `SELECT VALUE COUNT(1)`: `aggregates` `["Count"]` and `rewrittenQuery`
  `SELECT VALUE [{"item": COUNT(1)}] FROM c`.
- `ORDER BY c.n`: `orderBy` `["Ascending"]`, `orderByExpressions` `["c.n"]`, and a
  `rewrittenQuery` that selects `c._rid`, the keys as `orderByItems` and the
  columns as `payload`, with the text `{documentdb-formattableorderbyquery-filter}`
  in a `WHERE`. A second key gives two entries in both lists (recorded: "order
  by two attributes").
- `TOP 3`: `top` 3 and an empty `rewrittenQuery`. `TOP 2` with `ORDER BY` gave
  `top` 2, the order by lists and an order by `rewrittenQuery` that keeps `TOP 2`
  (recorded: "top and order by").
- `OFFSET 1 LIMIT 2` with `ORDER BY`: `offset` 1 and `limit` 2, and an order by
  `rewrittenQuery` (recorded: "offset and limit"). A parameter in `TOP` or in
  `OFFSET` and `LIMIT` gave the number in the plan, so the gateway binds it.
- `DISTINCT`: `distinctType` `Unordered`, and an empty `rewrittenQuery`.
- `GROUP BY c.grp` with `COUNT(1)`: `groupByExpressions` `["c.grp"]`,
  `groupByAliases` `["grp","n"]`, `groupByAliasToAggregateType` with `n` as
  `Count`, and a `rewrittenQuery` with `groupByItems` and `payload` (recorded:
  "the query plan of a group by and an order by"). The plan request took a
  `GROUP BY` with an aggregate that has no `VALUE`. A bare aggregate with no
  `VALUE` and no `GROUP BY` was not sent to the plan request.
- A plain projection: empty lists and an empty `rewrittenQuery` (recorded: "the
  query plan of a projection").

One query for a range. The header `X-Ms-Documentdb-Partitionkeyrangeid: 0` with
`X-Ms-Documentdb-Query-Enablecrosspartition: True` made the gateway answer the
aggregate that it refused without it. The answer was HTTP 200 with `[2500]`, and
it repeats the header of the range (recorded: "lead: a query on one range with
the range header"). That recording sent the original query, and not the
`rewrittenQuery`. A query with `X-Ms-Documentdb-Partitionkey: ["p1"]` and the
same aggregate was HTTP 200 with `[500]` (recorded: "a query for one partition
key"). The answers of a range with `SUM`, `MIN`, `MAX`, `AVG`, `ORDER BY`, `TOP`,
`OFFSET LIMIT`, `DISTINCT` or `GROUP BY` were not recorded. Neither was a query
for one partition key with `ORDER BY`, `TOP`, `OFFSET LIMIT`, `DISTINCT` or
`GROUP BY`. The `rewrittenQuery` was never sent.

The list of ranges is `GET .../pkranges`. Each range has `id`, `minInclusive`,
`maxExclusive`, `ridPrefix`, `throughputFraction`, `status` and `parents`. The
`id` is what the header of the range takes.

The pages. The header `X-Ms-Continuation` came back as JSON text, first as an
object with `token` and `range`, then as a list of such objects (recorded: "the
first page with the default size", "the second page"). A driver must treat it
as opaque text. The last page had none (recorded: "the third page"). A
continuation with a range header was not measured.

What a driver must do, as far as the recordings go:

1. Send the query. If the answer is HTTP 200, read the pages.
2. If the answer is HTTP 400 with substatus 1004, read `additionalErrorInfo`,
   or send the plan request.
3. Read `queryRanges` and `pkranges`, and find the ids of the ranges that the
   plan names. With one range, the id is `0`.
4. Send the query once for each range, with the header of the range.
5. Merge the answers by the plan. What the recordings prove for each form:
   - `COUNT`: add the counts. Proved for one range, so the add itself was not
     run (recorded: "lead: a query on one range with the range header").
   - `SUM`, `MIN`, `MAX` and `AVG`: not measured. `AVG` needs a sum and a count
     from each range, and the `rewrittenQuery` of an aggregate hides them in
     `{"item": ...}`. Not measured.
   - `TOP`: the plan names the number. The merge is not measured.
   - `OFFSET LIMIT`: the plan names both numbers. The merge is not measured.
   - `ORDER BY`: the plan names the keys and their directions, and the placeholder
     of the filter. The merge is not measured.
   - `DISTINCT`: the plan says `Unordered`. The merge is not measured.
   - `GROUP BY`: the plan names the keys and the aggregate of each alias. The
     merge is not measured.
   - A bare aggregate with no `VALUE`: the gateway refuses it without a plan.
     Whether the plan request takes it is not measured.

A driver that runs a query only for one partition key, or for one range, avoids
the merge. The recordings prove that only for `COUNT`.

## The DSN

- Facts of the URL that `dburl` writes: the scheme is `cosmos`, the host is
  required, and the generator does not change the rest (read of
  `dburl/dsn.go`, 2026-10-10). The URL of `dbrun` for the emulator is
  `cosmos://<key, percent encoded>@127.0.0.1:<port>/?InsecureSkipVerify=true`,
  which holds the key as the user name (measured, `dbrun dsn --json`,
  2026-10-10). D94 says that the secret is the password of the URL.
- What a client needs to reach the hosted account (recorded: every request of the
  hosted run):
  - The endpoint of the account, `https://<account>.documents.azure.com:443/`
    (recorded: "the account").
  - The master key of the account, which is 64 bytes in Base64 text. Microsoft
    names a resource token and a third type, `aad`, for Microsoft Entra ID (not
    measured, source: "Access Control on Azure Cosmos DB Resources"). Neither
    was tried.
  - The name of the database and the name of the container, because the path
    of a document request holds both and a statement does not. The SQL has
    `FROM c` and no container name (recorded: "a query that selects every
    document"). The emulator ignored the container name in `FROM`: `FROM nope c`
    read the container of the path (emulator, a script, 2026-10-10, not
    recorded). The hosted account was not asked.
  - The value of the partition key for a point read, a replace and a delete. A
    request with no value is HTTP 400 on the hosted account (recorded: "read a
    document with no partition key").
- The emulator needs a client that accepts its certificate (measured,
  2026-10-10). The hosted account needs none.
- Examples are the URL of `dbrun` above and the `dsn` field of `dbrun`,
  `AccountEndpoint=https://127.0.0.1:<port>/;AccountKey=<key>;InsecureSkipVerify=true`,
  which is the form of `gocosmos`.

## Responses

- The framing is one JSON object. A query answers
  `{"_rid": "...", "Documents": [...], "_count": n}` (recorded: "a query that
  selects every document"). The emulator also wrote `"_attachments":
  "attachments/"` (emulator). A list of databases uses the key `Databases` and a
  list of containers uses `DocumentCollections` (recorded: "the list of
  databases", "the list of containers"). The documents come as an array, so a
  client can read them one token at a time. A batch answers a JSON array
  (recorded: "a batch that succeeds").
- Columns. There is no metadata of columns, no type and no order, before or
  after the first row. The only names are the keys of the documents.
  - `SELECT *` returns the whole document with the system attributes `_rid`,
    `_self`, `_etag`, `_attachments` and `_ts` (recorded: "select star of one
    document"). A projection returns no system attribute (recorded: "a
    projection in the order of the statement").
  - `SELECT c.a, c.b` returns an object with the keys `a` and `b`. A key whose
    attribute is missing is left out, and a key whose value is `null` is kept
    (recorded: "a projection with a missing attribute and a null").
  - `SELECT VALUE expr` returns the bare value, so there is no key (recorded:
    "select value of an attribute", "select value of an array", "select value
    of an object"). A query that returns no row has an empty list, and a
    `SELECT VALUE` of a missing attribute also has an empty list (recorded: "a
    query that returns no rows", "select value of a missing attribute").
  - An expression with no alias is named `$1`, `$2` and so on (recorded: "an
    expression with no alias"). An aggregate with no alias and no `VALUE` is
    refused by the hosted gateway (recorded: "an aggregate with no alias").
  - Two columns with one alias are HTTP 400 with the code `SC2022` (recorded:
    "two columns with one alias").
  - A document read by `GET` keeps the order of its attributes (recorded: "the
    stored order of a document"). The hosted account keeps the order of the
    statement in a projection. The projection `c.zeta, c.alpha, c.mid, c.b,
    c.abcdef, c.nu` came back as `zeta`, `alpha`, `mid`, `b`, `abcdef`, `nu`
    (recorded: "a projection in the order of the statement"). A projection
    `c.alpha, c.nu, c.nope, c.zeta` came back as `alpha`, `nu`, `zeta`, in the
    order of the statement with the missing key left out (recorded: "a
    projection with a missing attribute and a null"). `SELECT *` keeps the
    stored order (recorded: "select star of one document"). The emulator sorted
    the keys by length and then by name (emulator). The ordering of `gocosmos` is
    no help, because it sorts the column names itself.
  - Ways that a driver can know the columns, as facts: the keys of the first
    document (it misses a key that the first row lacks), the union of the keys
    of all rows (it needs the whole result), the text of the statement (it
    needs a parser of the SELECT list), and `SELECT VALUE [a, b]`, which
    returns an array in the order that the statement gives (recorded: "select
    value of an array", "lead: select value of an array with a missing
    attribute"). The last form drops a missing member on the emulator
    (emulator). The hosted account gave `[2, null]` for it, so a missing member
    came back as `null` and the array kept its length (recorded: "lead: select
    value of an array with a missing attribute").
- Paging. The page size is the header `X-Ms-Max-Item-Count`. The hosted account
  answered 1000 items when the header was absent, and the emulator answered 100
  (recorded: "the first page with the default size"; emulator). The next page
  needs the header `X-Ms-Continuation` with the value that the last answer gave.
  That value is JSON text: an object with `token` and `range` for the first
  continuation, and a list of such objects for the second (recorded: "the first
  page with the default size", "the second page"). The emulator wrote text such
  as `-RID:~prwTAOZU-SklDgAAAAAAAA==#RT:1#TRC:100` (emulator). The last page has
  no such header (recorded: "the third page"). The documentation says the size
  is from 1 to 1000 with a default of 100 (not measured, source: Microsoft, and
  the hosted account contradicts both numbers).
  - 1000 gave pages of 1000, 1000 and 500 over 2,500 rows (recorded: "a page of
    1000", "the second page of 1000", "the third page of 1000"). So did the
    default.
  - 5000 and -1 gave all 2,500 rows in one answer (recorded: "a page of 5000",
    "a page of minus one"). The charge was 34.72 request units.
  - 0 gave a page of 1000 (recorded: "a page of zero"). A value that is not a
    number is HTTP 400 on the hosted account, with the text "The input PageSize
    abc is invalid. Ensure to pass a valid page size which must be a positive
    integer or -1 for a dynamic page size" (recorded: "a page that is not a
    number"). The emulator gave the default of 100 (emulator).
  - `TOP`, `ORDER BY` and an aggregate with a small page were refused by the
    hosted gateway, so the page sizes of those are not measured there (see The
    cross partition query). The emulator gave pages of 2 and 2 for `TOP 5`, and
    all 2,500 rows for `ORDER BY` with a page of 2 (emulator).
  - A continuation that the server did not give is HTTP 400 on the hosted account,
    with the text "Invalid Continuation Token" (recorded: "a continuation that
    is not valid"). It was HTTP 500 on the emulator (emulator).
  - `X-Ms-Documentdb-Query-Iscontinuationexpected: False` changed nothing, on the
    emulator and on the hosted account (recorded: "lead: a statement that expects
    no continuation").
  - A query that spans all partitions with the header of the range
    `X-Ms-Documentdb-Partitionkeyrangeid: 0` answered the aggregate that the
    gateway refuses without it (recorded: "lead: a query on one range with the
    range header").
- No cap on the size of a result was met at 2,500 rows. The header
  `X-Ms-Resource-Quota` names `documentSize=10240` (KB) on both servers
  (recorded: "a query that selects every document"). A document of 3 MiB was HTTP
  413 on both, with the text "Request size is too large" on the hosted account
  (recorded: "a document that is too large"). The limit of a request is not
  measured.
- Compression. A request with `Accept-Encoding: gzip` got a body with no
  `Content-Encoding` on both (recorded: "a request that accepts gzip"). A request
  body that is compressed was not measured, because the recorder sends text.
- Redirects. No answer was a redirect.
- Every answer has `X-Ms-Request-Charge`, `X-Ms-Activity-Id`, and
  `X-Ms-Session-Token`. A query has `X-Ms-Item-Count` (recorded: "a query that
  selects every document"). The charge is a real number of request units on the
  hosted account, such as 2.26 for a query of one document and 17.16 for a page
  of 1000 rows with two columns. A batch of two creates cost 12.57 and each
  operation gave its own `requestCharge` (recorded: "a batch that succeeds"). The
  emulator gave `1` for every answer and `0` for each operation of a batch
  (emulator).
- `X-Ms-Documentdb-Populatequerymetrics: True` adds the header
  `X-Ms-Documentdb-Query-Metrics` (recorded: "query metrics"). It is a list of
  `name=value` pairs, such as `totalExecutionTimeInMs=0.28`.
- A read of the change feed with `A-Im: Incremental feed` and the range header
  returns documents with the extra attribute `_lsn` and a header `Etag`, which
  was `"43"` (recorded: "the change feed").

## Types

Cosmos DB stores JSON. A column has no declared type, and the value of a column
can change its type from one row to the next.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| string | string | `string` | `interface {}` | `` | yes |
| number | number | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64` | `interface {}` | `` | yes |
| boolean | boolean | `bool` | `interface {}` | `` | yes |
| null | null | `nil` | `interface {}` | `` | yes |
| array | array | `[]any` | `interface {}` | `` | yes |
| object | map | `map[string]any` | `interface {}` | `` | yes |
| undefined | null | `nil` | `interface {}` | `` | yes |
<!-- /dbimp:types -->

This table is the proposal of step 8a. Ken has not reviewed it. Step 10
generates it from the code.

- A number is JSON text. The measurements on the hosted account (recorded: "numbers
  beyond 2^53 and the numbers that JSON loses", "read the numbers back", "select
  the numbers", "lead: numbers written in the statement"):
  - `9007199254740993` and `-9223372036854775808` came back exactly, by `GET`
    and by a query.
  - `18446744073709551615` came back exactly by `GET`, and as
    `1.8446744073709552e+19` in a query.
  - `18446744073709551616` and `123456789012345678901234567890` were stored as
    `1.8446744073709552e+019` and `1.2345678901234568e+029`, so the server
    already lost them. The answer of a `GET` and of a create writes an exponent
    with three digits, such as `e+019`. A query writes `e+19`. Both are valid
    JSON. Whether `encoding/json/v2` reads `e+019` was not run.
  - `0.1000000000000000055511151231257827` came back as `0.1`, `1.0` as `1`,
    `-0.0` as `0`, `1e-400` as `0`, `5e-324` as `4.94065645841247e-324`, and
    `12345678901234567890.123` as `1.2345678901234567e+019` (by `GET`) and
    `1.2345678901234567e+19` (by a query). The emulator wrote `5e-324` and
    `12345678901234567000` (emulator).
  - `1e400` and a `NaN` in a document are HTTP 400 on the hosted account, with the
    text "The request payload is invalid" (recorded: "a number that cannot be
    represented", "NaN in a document"). They were HTTP 403 on the emulator
    (emulator). A query that divides by zero is HTTP 400 with the code `4001`
    (recorded: "a division by zero").
  - Arithmetic in a statement: `9007199254740993 + 1` is `9007199254740994`,
    `0.1 + 0.2` is `0.30000000000000004` and `1/3` is `0.33333333333333331`
    (recorded: "arithmetic beyond 2^53"). The literal `12345678901234567890` in
    a statement came back as `1.2345678901234567e+19` (recorded: "lead: numbers
    written in the statement").
  - The range of the service is a double precision number for a value that a
    query computes, which has 53 bits of integer. A stored integer of 64 bits
    came back exactly. The models said the same (not measured, source: models,
    see Second opinions).
- A string is UTF-8. A NUL in a string was kept (recorded: "a string with a
  NUL"). A lone surrogate is accepted on the hosted account and stored as the
  replacement character U+FFFD, so it does not read back (recorded: "a lone
  surrogate", "the NUL and the surrogate read back"). It was HTTP 403 on the
  emulator (emulator).
- A boolean and a `null` are JSON values (recorded: "the document of every JSON
  type"). `IS_NULL` and `IS_DEFINED` tell them apart from a missing value
  (recorded: "the types of values").
- Missing. A missing attribute is left out of a projection, and a `SELECT VALUE`
  of it gives no row. A missing member of `SELECT VALUE [..]` is `null` on the
  hosted account (recorded: "lead: select value of an array with a missing
  attribute"). A parameter that has no value is not defined (recorded: "a
  parameter with no value"). The driver gets `nil` for a key that is not in the
  document only if it chose a list of columns, so D18 decides it (see Open
  questions).
- Empty. An empty string, an empty array and an empty object are kept
  (recorded: "the document of every JSON type").
- Dates, UUIDs, binary values and decimals have no type. They are JSON strings,
  such as `2026-10-10T12:00:00Z`, `123e4567-e89b-12d3-a456-426614174000`,
  `AAEC/w==` and `12345678901234567890.123456789`. The server compares a date
  text as text (recorded: "dates as text"). A driver cannot tell a date from
  another string.
- Geospatial types are GeoJSON objects in a document. They were not measured.
  The policy of a container names `geospatialConfig` `Geography` on both servers
  (recorded: "one container").

## Parameters

- The body has `"parameters": [{"name": "@p", "value": v}]` (recorded: "a
  parameter that is a string"). The name starts with `@`. A name with no `@` is
  HTTP 400 on the hosted account, with the text "Parameter names should be in the
  format of symbol '@' followed by a valid identifier" (recorded: "a parameter
  with no at sign"). It was HTTP 500 on the emulator (emulator).
- The server binds by name. The order of the list does not matter (recorded:
  "parameters in another order"). A positional placeholder does not exist.
- A value is any JSON value. A string, an integer, a float, `true`, `null`, an
  array and an object each came back as they went (recorded: "a parameter that is
  a string" to "a parameter that is a date as text"). `9007199254740993` kept its
  value, `18446744073709551615` became `1.8446744073709552e+19` and `1e300`
  was kept (recorded: "a parameter that is an integer beyond 2^53", "a
  parameter that is an integer of 64 bits unsigned", "a parameter that is a
  huge number").
- A parameter with the value `null` is defined and `IS_NULL` is true. A parameter
  with no `value` member is not defined, and a parameter that the list does not
  hold is not defined either. Neither gave an error. The first gave an object with
  only the key `d` (`false`), and the second an empty object (recorded: "a
  parameter that is null", "a parameter with no value", "a parameter that is not
  passed"). The documentation says an unspecified parameter is `undefined` (not
  measured, source: Microsoft).
- A name that the list gives twice is HTTP 400 on the hosted account, with the text
  "Specified duplicate parameter name" (recorded: "a parameter named twice"). It
  was HTTP 500 on the emulator (emulator). A name that the statement does not use
  is accepted (recorded: "a parameter that the statement does not use").
- A parameter works in a predicate, in the id, in `ARRAY_CONTAINS` and as the
  name of an attribute `c[@a]` (recorded: "a parameter in a predicate", "a
  parameter as the id", "a parameter in IN", "a parameter as a name of an
  attribute"). A parameter in `TOP`, and in `OFFSET` and `LIMIT`, is bound by
  the server, because the plan holds the number, and the hosted gateway refuses
  the query for the reason in The cross partition query (recorded: "a parameter
  in TOP", "a parameter in OFFSET and LIMIT"). `IN (@a, @b)` was not measured.
  A list passes as one array parameter.
- The server binds parameters, so the driver needs no escaper (D34) for a query.

## Transactions

- A transactional batch is `POST .../docs` with `X-Ms-Cosmos-Is-Batch-Request:
  True`, `X-Ms-Cosmos-Batch-Atomic: True`, the header of the partition key and
  a JSON array of operations. An operation has `operationType`, `id` or
  `resourceBody` (recorded: "a batch that succeeds"). The types that were
  measured are `Create`, `Read`, `Upsert`, `Replace`, `Patch` and `Delete`
  (recorded: "a batch of every operation"). Each answer has `statusCode`,
  `requestCharge`, `eTag` and `resourceBody`.
- All documents of a batch have one partition key. A document of another key
  is a refusal inside HTTP 207, with the status 400 for the operation and the
  substatus 1001 on the answer (recorded: "a batch with a document of another
  partition").
- A failed batch is HTTP 207 with one entry for each operation. The operation
  that failed has its own status, such as 409, and the others are 424
  (recorded: "a batch that fails in its second operation"). Nothing was written:
  the documents of that key were still `b1` and `b2` (recorded: "the documents
  of the failed batch"). The emulator did the same (emulator).
- A batch of 101 operations is HTTP 400 on both, with the text "Batch request
  has more operations than what is supported" on the hosted account, and the
  limit is 100 (recorded: "a batch of 101 operations"). A batch of 100
  creates cost 704.76 request units on the hosted account (recorded: "setup: seed
  the container bulk, batch 25").
- The header of the brief, `X-Ms-Cosmos-Batch`, is not a header of either
  server. The hosted account read the array as a document and answered HTTP 400
  with "One of the specified inputs is invalid" (recorded: "a batch with the
  header of the brief"). The path `/docs/$batch` is HTTP 405 with the code
  `MethodNotAllowed`, and `/_apis/batch`, which two models named, is HTTP 400 with
  "Request url is invalid" (recorded: "a batch on the path of a batch", "lead: a
  batch on the path of the apis").
- With `X-Ms-Cosmos-Batch-Atomic: False` and `X-Ms-Cosmos-Batch-Continue-On-Error:
  True` the hosted account wrote the operations that were valid and returned 409
  for the one that failed, in HTTP 207 (recorded: "a batch that is not atomic").
  This is not a transaction.
- A batch gives no `BEGIN`, no read inside a transaction and no `COMMIT`. A
  transaction that spans calls does not exist. The real service has the same
  shape (not measured, source: models, see Second opinions).
- No other kind of transaction was measured. A stored procedure runs inside one
  partition on the real service (not measured, source: Microsoft). The hosted
  account ran a stored procedure that only returns `1` (recorded: "a call of the
  stored procedure"). A procedure that writes was not run.

## Errors

- A body of an error has `code` and `message`. On the hosted account the message
  is text that holds a JSON object and a line `ActivityId: ...`, such as
  `Message: {"Errors":["Resource Not Found. ..."]}\r\nActivityId: ...` (recorded:
  "setup: drop the database", "a database that does not exist"). For a missing
  document the message is "Entity with the specified id does not exist in the
  system", followed by a diagnostic text that holds `systemHistory` (recorded:
  "read a document that does not exist"). The same `code` and `message` form is
  used for HTTP 404, 409, 412 and 400 of a document (recorded: "a document that
  already exists", "replace with a wrong etag", "a document with no id"). The
  emulator wrote `{"code":"NotFound", "message":"Resource Not Found..."}`
  (emulator).
- An error of a query has `code` and `message` too, and the message holds the
  JSON text `{"errors": [{"severity": "Error", "location": {"start": 7, "end":
  11}, "code": "SC1001", "message": "..."}]}` on the hosted account, with HTTP
  400 (recorded: "a syntax error", "an unknown function", "a trailing
  semicolon", "an empty statement"). A driver must read the text inside the
  message to get the code and the location. The emulator wrote `errors` as a
  member of the body (emulator). The code of a division by zero is the number
  `4001`, has no location and has the substatus 4001 (recorded: "a division by
  zero").
- The header `X-Ms-Substatus` of an HTTP 400 on the hosted account was 1001 for a
  partition key that has the wrong number of parts (recorded: "a document with
  no partition key header", "read a document with no partition key", "a
  document with the wrong partition key") and 1004 for a cross partition query
  that the gateway cannot serve (see The cross partition query).
- A missing container in a query is HTTP 404 with the message `Message:
  {"Errors":["Resource Not Found..."]}`, and so is a missing database (recorded:
  "a container that does not exist", "a database that does not exist"). The
  emulator told them apart by the form of the body (emulator).
- A body that is not JSON is HTTP 400 on the hosted account, with "The request
  payload is invalid. Ensure to provide a valid request payload" (recorded: "a
  body that is not JSON"). A number that JSON cannot hold is the same (recorded:
  "a number that cannot be represented"). Both were HTTP 403 on the emulator,
  with "Failed to parse Json request" (emulator).
- Status codes recorded on the hosted account: 200, 201, 204, 207, 304, 400,
  401, 403, 404, 405, 409, 412, 413. Not seen: 410, 429, 500, 503.
  - 304 is a read with `If-None-Match` and the etag that the document has. The
    answer has the header `Etag` and no body (recorded: "lead: read with the etag
    that the document has").
  - 401 is a request with no `Authorization` header (recorded: "a request that
    the recorder sends with no signature").
  - 403 was two answers on the hosted account. One is `OPTIONS /` (recorded: "a
    request for the options of the server"). The other is a replace of the
    container `uk` with an indexing policy that omits the unique key policy, with
    the text "The unique index cannot be modified" (recorded: "replace the
    indexing policy"). So 403 was not a refusal for a lack of a privilege.
  - 405 is `POST` on `/docs/$batch` (recorded: "a batch on the path of a batch").
  - 409 is a document that exists, a database or a container that exists, and
    a unique key that is broken (recorded: "a document that already exists", "a
    database that already exists", "a container that already exists", "a
    document that breaks the unique key"). A broken unique key has the same
    text as a document that exists.
  - 412 is a wrong `If-Match` (recorded: "replace with a wrong etag"). The
    same header with the etag that the document has gave HTTP 200 (recorded:
    "lead: replace with the etag that the document has").
  - 413 is a document of 3 MiB (recorded: "a document that is too large").
  - The emulator gave HTTP 500 for a continuation that is not valid, a parameter
    with no `@` and a parameter named twice, with the text "Database query failed:
    PostgresError" (emulator). The hosted account gave HTTP 400 for each.
- No answer had HTTP 200 and an error in the body. A batch has HTTP 200 or 207
  with a status for each operation (recorded: "a batch that fails in its second
  operation").
- An error after some rows was not produced. Every error of a query came before
  the first row. Whether the real service can send an error with the second page
  is not measured.
- Not recorded:
  - HTTP 401 for a wrong signature, and HTTP 403 for an expired one. The recorder
    cannot build them (not measured, source: Microsoft, "Querying Azure Cosmos DB
    resources using the REST API").
  - HTTP 429, with the header `x-ms-retry-after-ms` and the substatus. No answer
    of the hosted run had a header with `retry` in its name. A script that sends
    more than 400 request units a second for a longer time is the lead (not
    measured, source: models, see Second opinions).
  - HTTP 410 for a split of a partition (not measured, source: model).
- Which errors mean that the request did not reach the server. A refusal of
  HTTP 429 does not run the request, so a driver can send it again after the
  wait (not measured, source: models). `driver.ErrBadConn` fits a connection
  that broke before the request went out. No run showed such a case.

## Cancellation and timeouts

- A client that gives up after 1 millisecond on a query was recorded in the
  script, and the abandoned request is not a file, because the client never got an
  answer. The next query in the script was `SELECT VALUE COUNT(1)`, and the
  hosted gateway refused it with substatus 1004 (recorded: "the next query after
  an abandoned query"). So the hosted run does not show what the server did with
  the abandoned query. The emulator answered the next query (emulator). Lead: send
  a next query that the gateway serves.
- The REST API has no request that cancels a query on the server. This is not
  measured for the real service. The headers that the documentation lists have
  none (source: Microsoft, "Querying Azure Cosmos DB resources using the REST
  API").
- No header of a request sets a timeout for a query, and the list of the
  documentation of Microsoft names none (not measured, source: Microsoft). The
  `queryEngineConfiguration` of the account names `maxQueryRequestTimeoutFraction`
  0.9 (recorded: "the account"). No run showed a timeout of a query.
- The continuation header is stateless on the server. Microsoft says that a
  query can resume at any time with it (not measured, source: Microsoft).
  So a client that stops reads pages loses nothing on the server.
- On the real service, a request with the signature date out of the window is
  HTTP 403 (not measured, source: Microsoft). The recorder replaces the date
  with the clock, so no recording sends a date out of the window.
## Statements

The hosted account gave the same answer as the emulator for every item of
this section, with the same request names, except where the text says
otherwise.

- A statement of the SQL dialect is one `SELECT`. A trailing semicolon is HTTP
  400 with the code `SC1010` and the text "Syntax error, invalid token ';'"
  (recorded: "a trailing semicolon"). This confirms what the `usql` session
  reported, on both servers. A semicolon between two statements is the same error, so one request
  runs one statement (recorded: "two statements in one request").
- A comment that starts with `--` works, and it runs to the end of the line
  (recorded: "a line comment", "a line comment and a new line"). A comment of the
  form `/* */` is HTTP 400 (recorded: "a block comment"). A comment that ends
  the statement can hide a closing character, so a driver that strips a
  semicolon must also read the comments.
- Case does not matter for the keywords (recorded: "a statement in lower
  case"). New lines and tabs are white space (recorded: "a statement with new
  lines and tabs").
- An empty statement is HTTP 400 with the code `SC1002` (recorded: "an empty
  statement").
- The SQL cannot write. `INSERT`, `UPDATE` and `DELETE` are syntax errors
  (recorded: "a statement of INSERT", "a statement of UPDATE", "a statement of
  DELETE"). A write is `POST`, `PUT`, `PATCH` or `DELETE` on a document, and a
  batch (see Requests). There is no DDL in the SQL: a database and a container
  are resources too (see Requests).
- `gocosmos` gives `database/sql` its own statements, none of which the server
  knows. It parses `CREATE DATABASE`, `CREATE COLLECTION`, `DROP`, `LIST
  DATABASES`, `INSERT INTO db.coll (...) VALUES (...) WITH pk=/id`, `UPSERT`,
  `UPDATE db.coll SET ... WHERE id=... AND pk=...` and `DELETE`, and a `SELECT`
  with `WITH db=... WITH table=...` and `CROSS PARTITION` (read of
  `SQL.md` and `stmt.go` in `gocosmos` v1.1.0, 2026-10-10).
- The statement names no container, so the driver must learn it from the DSN or
  from a clause of its own.

## Principals

- The recordings are of the master key of the account, on both servers (recorded in
  `manifest.json`, the field `noOrdinaryUser`, for the emulator). The hosted
  account has other keys and other kinds of token, and no recording used one, so
  a principal with fewer rights is not measured. The recorder cannot build a
  request with a wrong key.
- Microsoft names resource tokens for a user and a permission, and
  `GET /dbs/{db}/users` lists users (not measured, source:
  Microsoft, "Access Control on Azure Cosmos DB Resources"). Both servers listed
  none (recorded: "the list of users").
- `GET /offers` listed the throughput of each database, 400 request units a
  second for each of two databases (recorded: "the list of offers"). The master
  key read it.
- The version. The account has no field for a version. The only version facts
  are the headers `X-Ms-Gatewayversion` and `X-Ms-Serviceversion`, and the
  version of the API that the client names in `X-Ms-Version` (recorded: "the
  version of the account"). No statement returns a version, and `usql` has none
  for this driver.

## Flavors

- Two servers were measured: the vNext emulator and the hosted service. The older
  emulator, which needs a Windows host, is left out by `dbmeta` (read of
  `container/cosmos.go`).
- Cosmos DB has other APIs for MongoDB, Cassandra, Gremlin and Table. They are
  other protocols, and they are out of this file.
- The driver can tell the emulator from the service by the header `Server`, which
  is `PGSQL` on the emulator (emulator, "the account") and `Microsoft-HTTPAPI/2.0`
  on the hosted account (recorded: "the account"). The header
  `X-Ms-Gatewayversion` is `2.0.0-unknown` on the emulator and `version=2.14.0`
  on the hosted account.
- The differences that matter to a driver are in the Summary. A driver that
  works against the emulator only is not enough: the emulator answers a cross
  partition aggregate and the hosted gateway refuses it.

## Interfaces

Not written yet. Step 10 writes the table from the code.

## Faults

The faults of `btnguyen2k/gocosmos` v1.1.0, which this driver must not repeat
(read of the source in the module cache, 2026-10-10):

- It does not pass the context. `StmtSelect.QueryContext` has the comment
  `TODO: pass ctx to REST API client` and calls `context.Background` in `Query`
  (`stmt_document.go`).
- It holds the whole result in memory. The comment of
  `QueryDocumentsCrossPartition` says "intermediate results are kept in memory,
  and all matched rows are returned" (`restclient.go`).
- It sorts the column names with `sort.Strings`, so the columns have the order of
  the alphabet and not the order of the statement (`stmt.go`, `init`).
- It removes every key that starts with `_`, so a column named `_x` and the
  system attributes disappear from a `SELECT *` (`restclient.go`,
  `RemoveSystemAttrs`).
- It takes the columns from the union of the keys of every row, and it types a
  column from the first row that has the key (`stmt.go`, `init`). A `SELECT
  VALUE` or an aggregate with no alias becomes the column `$1` (`stmt.go`).
- Its type name of a column is `NUMBER` for every float, and it knows no
  integer, so a number is a `float64` (`utils.go`, `goTypeToCosmosDbType`).
  It reads the answer with `encoding/json` into `interface{}` values
  (`restclient.go`), so a number is a `float64` and an integer beyond 2^53
  loses its value. This is a reading of the code and was not run.
- It does the query plan handshake and the fan out over the partition key
  ranges itself, and merges the results (`restclient.go`). That is the work
  that the real service asks of a client, so a driver here needs it too.
- It needs one more request to learn the partition key of a container, unless
  the statement says `WITH pk=`.
- `usql` has no `Version` statement for it and no hook in its own file. The
  `usql` session reported that an old hook of `usql` stripped a trailing
  semicolon.

## Second opinions

The survey of step 5a asked these on 2026-10-10. Each answer is a lead, not a
fact.

- Gemini (`gemini-3.1-pro-preview`) listed the verbs for a document, said that the
  SQL is read-only, named hierarchical keys, unique keys, TTL, the change feed,
  stored procedures, triggers and functions as existing, and view, default
  value and foreign key as missing. It said that a transactional batch is
  `POST` to `/$batch`. The server refused that path (recorded: "a batch on the
  path of a batch"). It said that `SELECT c.missing` returns `{}` and
  `SELECT VALUE c.missing` is left out, and that 64-bit integers are kept exactly
  by the service. The emulator agrees on the first (recorded: "a projection
  with a missing attribute and a null", "select value of a missing attribute")
  and not on the integers above 2^53 in a query (recorded: "select the
  numbers"). Two of its answers were cut by its limit, so it gave no more.
- Qwen (`qwen3.7-plus`) gave the same list for the verbs and the schema. It said
  that a batch goes to `/_apis/batch`, which the server refused (recorded: "lead:
  a batch on the path of the apis"). It said that a driver for `database/sql`
  infers the columns from the keys of the first document. Kimi K3, Qwen Max and
  DeepSeek Pro were asked first. Each answered with a timeout or with an
  empty body, twice, so the survey used Qwen. The work then used DeepSeek Flash
  for the review of the mapping.
- The leads of step 7. Gemini was asked what step 6 did not find. Its answer was
  cut by its limit after the first lead, which says that a driver can learn the
  column order only from a parser of the statement, from the first row, or from
  `SELECT VALUE [..]`. Qwen timed out on the same question. The leads that the
  work tested, with what the emulator said (the next list gives the hosted
  answers):
  - `X-Ms-Cosmos-Batch` is the header of a batch: wrong (recorded: "a batch with
    the header of the brief").
  - `/$batch` and `/_apis/batch` are the paths of a batch: wrong (recorded: "a
    batch on the path of a batch", "lead: a batch on the path of the apis").
  - `If-None-Match` with the etag gives HTTP 304: the emulator gave HTTP 200
    (recorded: "lead: read with the etag that the document has").
  - `If-Match` with the etag lets a replace through: right (recorded: "lead:
    replace with the etag that the document has").
  - `X-Ms-Documentdb-Query-Iscontinuationexpected: False` changes an aggregate:
    nothing changed (recorded: "lead: a statement that expects no continuation").
  - The header of the range sends a query to one range: the same answer on the
    emulator (recorded: "lead: a query on one range with the range header").
  - `X-Ms-Consistency-Level: Strong` with a session token is accepted (recorded:
    "lead: strong consistency and a session token").
  - A plain text statement with the content type `application/sql`: HTTP 403
    (recorded: "lead: a statement as plain text").
  - The query plan names `orderBy` for an `ORDER BY`: the emulator gave an empty
    list (recorded: "lead: the query plan of an ordered query").
  - A date in the header that is out of the window gives HTTP 403: the recorder
    replaces the date with the date of the clock, so the lead stayed not
    measured. A script that sent a date 20 minutes away got HTTP 200 (measured,
    2026-10-10, not recorded).
  - A wrong key gives HTTP 401: a script that signed with a wrong key got HTTP
    200 (measured, 2026-10-10, not recorded). A request with no signature got
    HTTP 200 (recorded: "a request that the recorder sends with no signature").
  - HTTP 429 with `x-ms-retry-after-ms`: not producible on the emulator. Not
    measured.
  - A compressed request body: not measured, because the recorder sends text.
- The same leads on the hosted account (each is "recorded"):
  - `X-Ms-Cosmos-Batch` is the header of a batch: wrong ("a batch with the header
    of the brief", HTTP 400).
  - `/$batch` and `/_apis/batch` are the paths of a batch: wrong ("a batch on the
    path of a batch", HTTP 405, and "lead: a batch on the path of the apis", HTTP
    400).
  - `If-None-Match` with the etag gives HTTP 304: right ("lead: read with the
    etag that the document has").
  - `If-Match` with the etag lets a replace through: right ("lead: replace with
    the etag that the document has").
  - `X-Ms-Documentdb-Query-Iscontinuationexpected: False` changes an aggregate:
    nothing changed ("lead: a statement that expects no continuation").
  - The header of the range sends a query to one range: right, and it also lets the
    gateway answer an aggregate that it refuses without it ("lead: a query on one
    range with the range header").
  - `X-Ms-Consistency-Level: Strong` with a session token is accepted: wrong for
    this account, which is `Session` ("lead: strong consistency and a session
    token", HTTP 400). A level that is not stronger than the account was not
    sent.
  - A plain text statement with the content type `application/sql`: the hosted
    gateway took it as a query ("lead: a statement as plain text").
  - The query plan names `orderBy` for an `ORDER BY`: right ("lead: the query plan
    of an ordered query").
  - A date in the header that is out of the window gives HTTP 403, and a wrong key
    gives HTTP 401: not measured. The recorder cannot build the request.
  - HTTP 429 with `x-ms-retry-after-ms`: not seen (see Errors).
  - A compressed request body: not measured, because the recorder sends text.
- The mapping of the types, reviewed on 2026-10-10.
  - Gemini said that no row is wrong. It said to decode an integer as `int64`,
    a fraction as `float64` and an integer that is too large as `*apd.Decimal`,
    to keep a date as a string, and to map an object to `map`. The server
    showed that an integer above 2^64 is already a float when the server
    returns it, so the `*apd.Decimal` branch needs a number that the server
    kept exactly, such as `18446744073709551615` by `GET`.
  - DeepSeek Flash said that `undefined` must not become `nil`, because only an
    explicit JSON `null` is `nil`, and that a date stays a string. The server
    shows that `undefined` is a missing key and never a value, so the row for
    `undefined` is a question for Ken (D18). Qwen timed out twice on the
    mapping.

The plan handshake, asked on 2026-10-10 about the section The cross partition
query. Each answer is a lead, not a fact. Nothing below was run.

- Gemini (`gemini-3.1-pro-preview`) read a short form of the section. Its first
  two tries ended with "Connection closed", and its third was cut by its limit
  after one point. The fourth gave the points below. It marked each "certain".
  - The SDKs send the `rewrittenQuery` to each range, and not the original
    query, so that the answer has the shape that the merge expects, such as
    `[{"item": 2500}]`. The section recorded the original query for `COUNT` only.
  - The .NET and Java SDKs build the plan on the client with a native library, and
    they skip the HTTP 400.
  - The placeholder `{documentdb-formattableorderbyquery-filter}` is replaced with
    `true` for the first page. For a later page it is replaced with a filter on
    the `orderByItems` of the last row, such as `c.n >= <value>`.
  - An `ORDER BY` merge keeps a queue with the first row of each range and sorts
    by `orderByItems`.
  - The continuation token that the SDK gives the caller holds the token of each
    range and the `ORDER BY` state, so it is not the header of one request.
  - `TOP` and `OFFSET LIMIT` are counted on the client over the merged stream.
    `DISTINCT` keeps a hash of each row. `GROUP BY` keeps an accumulator for each
    group. `AVG` is a `SUM` and a `COUNT` that the `rewrittenQuery` makes, and the
    client divides. `COUNT(DISTINCT)` uses `dCountInfo`.
  - A split of a partition gives HTTP 410 with substatus 1002. The SDK reads
    `pkranges` again and goes on with the child ranges and the token of the
    parent.
  - The SDKs read ranges in parallel up to a limit and read ahead.
  - A header `x-ms-documentdb-query-iscontinuation` on a resumed request. Gemini
    was sure, and the source of that claim is not known to this file.
  - Newer SDKs add features, such as `VectorSearch`, to the header of the
    features.
- A second model was asked in a separate conversation (Qwen Max, then DeepSeek
  Pro, then Qwen Max again, DeepSeek Pro again). Every try ended with "Connection
  closed" or with a timeout, so there is no second opinion. This is a gap.
  Nothing from the models above changes a fact of this file.

## Open questions

These wait for Ken. The facts are above, and the proposals are in the report of
step 6.

1. How much of the cross partition query does the driver run itself? The hosted
   gateway refuses an aggregate, `TOP`, `ORDER BY`, `OFFSET LIMIT`, `DISTINCT`
   and `GROUP BY` over a container, unless the query names one partition key or
   one range (recorded, see The cross partition query). The choices are:
   - Send the statement as it is, and return the refusal. A plain `SELECT` and
     any query that names one partition key still work. The recordings prove the
     partition key only for `COUNT`.
   - Send the plan request, then send the query once for each range with the
     header of the range, and merge. The account of the run had one range, so the
     recordings show no merge. It needs a container with more than one range to
     test, which the free tier does not give. Ken decides whether a test that
     costs money is worth it.
   - Merge only some forms, such as `COUNT`, and refuse the rest.
2. The SQL of Cosmos DB cannot write. Which language does the driver define for
   an insert, an update and a delete: the statements of `gocosmos`, JSON
   documents as statements, or none (a read-only driver)?
3. How does the driver know the columns, with no metadata? The keys of the first
   row, the text of the `SELECT` list, or `SELECT VALUE`? The hosted account
   keeps the order of the statement in a projection, so the text of the list
   gives a true order.
4. Does a missing attribute (`undefined`) map to `nil`, as D18 reads, or does
   it stay out of the row?
5. How does the DSN name the container, and where does the key go: the user
   (as `dburl` writes it) or the password (as D94 says)? The endpoint of a hosted
   account is a host with the port 443, and an emulator is a host with a port
   and a certificate that no authority signed.
6. Does the driver accept a trailing semicolon, or strip it (W36)? Both servers
   refuse it.
7. The mapping of the types of step 8a, with `undefined` and with a number
   that has no type on the wire. The hosted account writes `e+019` for a large
   exponent, and a lone surrogate does not read back.
8. Which server does the integration test of the driver use? The emulator differs
   from the hosted account in the plan, in the refusal of a cross partition
   query, in the order of the keys, in the page size, in HTTP 304 and in the
   signature. A test on the emulator alone can pass where the hosted account
   fails. A test on the hosted account needs the key of Ken.

Leads that no recording settled, for a later run on the hosted account:

- A newer `X-Ms-Version` for a container with two partition key paths.
- Replace the indexing policy with the unique key policy in the body, to see
  the replace itself.
- A write that fires the trigger, and a stored procedure that writes.
- `SUM`, `MIN`, `MAX` and `AVG` with `VALUE`, and `ORDER BY`, `TOP`, `OFFSET
  LIMIT`, `DISTINCT` and `GROUP BY`, each with the header of the range and with
  the header of one partition key, and with the `rewrittenQuery`.
- A bare aggregate with no `VALUE` sent to the plan request.
- A shorter list in the header of the features.
- A burst above 400 request units a second, for HTTP 429.
- A request with a wrong key and one with a date out of the window, which needs
  a recorder that can build them.
- A next query that the gateway serves, after an abandoned query.
- A second partition key range, and a split.
