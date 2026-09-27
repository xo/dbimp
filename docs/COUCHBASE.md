# Couchbase

This file holds what is known about the Couchbase query service, for the
first driver, `github.com/xo/dbimp/couchbase` (D23 and D26 in
[PLAN.md](PLAN.md)). W5 in [BACKLOG.md](BACKLOG.md) is
the work.

The `n1ql` session measured these facts on 2026-09-27. It sent requests with
`curl` to Couchbase Server Enterprise 7.2.9, 7.6.12 and 8.0.3, which `dbrun`
started from dbmeta commit `3be3293`. `dbrun` publishes only port 8093, which
is the query service. A fact marked "not measured" is a lead, not a fact.

## Requests

- A query is `POST /query/service` with `Content-Type: application/json`, on
  all three releases.
- `"args": [...]` holds the positional parameters. `"$name": value` holds a
  named parameter. A string with a double quote, and a nested object, both
  arrive at the server unchanged. The driver never writes an argument into
  the text of the statement.
- Basic authentication works on the query service. The password is percent
  encoded in the user information of the URL. The older `creds` parameter and
  authentication with a certificate are not measured.

## The DSN

- The driver takes a URL whose scheme is `couchbase`, such as
  `couchbase://user:pass@127.0.0.1:8093`, and no other scheme (D35). `dburl`
  turns each alias, such as `n1ql`, into that URL.
- Whether the driver speaks HTTPS or HTTP is a key of the query, which step 9
  of [DRIVER.md](DRIVER.md) decides. It is never a second scheme such as
  `couchbases`, because the driver registers one name (D28).
- The `dsn` field of `dbrun dsn --json couchbase-<release>` is
  `http://user:pass@127.0.0.1:<port>` today, which is the query service
  itself, in the form that `go_n1ql` reads. The integration tests need a
  `couchbase://` URL, which W5 asks the `dbmeta` session for.
- The `n1ql` scheme in `dburl` defaults to `http://localhost:8093/`. The
  driver registers `couchbase`, and at the move `dburl` makes `couchbase` the
  name of the scheme (D30).
- `go_n1ql` first treats a DSN as the address of a cluster manager, on port
  8091. A cluster in a container answers with addresses inside the container,
  which a client outside it cannot reach. `dbmeta` points straight at the
  query service. This driver keeps no form of DSN from `go_n1ql` (D27).
  Whether it discovers the nodes of a cluster at all was n1ql open question
  Q12, and it is now a decision of step 9 for this driver.

## Responses

- The fields of a response arrive in this order: `requestID`, `signature`,
  `results`, `errors`, `status`, `metrics`. The signature arrives before the
  first row, so the driver knows the columns before it reads a row (D18).
- 7.6.12 and 8.0.3 send the signature and every result object in the order of
  the projection. 7.2.9 sends both in the order of the names. On 7.2.9,
  `PREPARE` returns a plan that lists the names in the order of the
  projection, but its signature is still sorted. Ken accepted the order of the
  names on 7.2, rather than a second request for each query (n1ql D33).
- A field that is MISSING is absent from the result object, and the signature
  still names it. `SELECT k.name, k.namespace, k.bucket, k.scope FROM
  system:keyspaces` returned `{"name":"dbmeta","namespace":"default"}` for a
  bucket. Ken decided that NULL and MISSING both scan as nil (n1ql D24).
- For `SELECT RAW`, the signature is a string such as `"number"` or `"json"`,
  not an object, and each result is a bare value. The result has one column.
- The number 9007199254740993 arrives exact in the text of the response. A
  decode through float64 changes it, so the driver converts the text of the
  token (D19).
- The response to a DML statement has `"signature": null`, no `results`, and
  a count in `metrics.mutationCount`. The `dbmeta` session measured this on
  2026-09-27.
- An index is updated after a write, not with it. On 7.6.12, a `SELECT`
  straight after an `UPSERT` returned no rows, and a `DELETE` by `META().id`
  found nothing to delete. A test that writes and then reads sends
  `scan_consistency=request_plus`, or reads by `USE KEYS`. The `dbmeta`
  session measured this on 2026-09-27.

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
- It is refused `system:user_info`, `CREATE INDEX`, and the creation of a
  bucket through REST, with HTTP 403. The text of the refusal differs by
  release. On 8.0 it names `user_admin_local`, and on 7.x it says "accessing
  user information".
- `Init` makes a primary index on `dbmeta`. Without one, a `SELECT` over the
  bucket is refused on every release. 7.2 has no sequential scan, and on 8.0
  the refusal asks for `query_use_sequential_scans`, a role that the user does
  not hold.
- After a stop and a start, the query service answers before the bucket is
  warm. On 7.2.9 an `INSERT` failed in that window with "DML Error, possible
  causes include concurrent modification". `Init` now waits until
  `SELECT RAW COUNT(*) FROM dbmeta` succeeds.

`dbrun dsn --json` prints one DSN for each server, with the credentials of
`Administrator`. It prints none for the ordinary user. W5 holds that
question.

## Errors

- A syntax error is HTTP 400, with no signature and no results. `errors`
  holds code 3000, with the line and the column.
- An error can arrive after rows. `SELECT RAW CASE WHEN a < 3 THEN a ELSE
  ABORT("boom") END FROM ARRAY_RANGE(0, 5) AS a` returned HTTP 200, the
  results `[0,1,2]`, then `"errors": [{"code": 5011, ...}]` and
  `"status": "fatal"`. The driver must return that error from `Rows.Next` and
  `Rows.Close`, and never report the result as complete.
- A prepared statement that the server does not know gives code 4040 on 7.2.9
  and 8.0.3. `PREPARE` with a name that exists gives 4060. Code 4050 was not
  seen. Prepare again and retry only on 4040.

## Cancellation

`DELETE /admin/active_requests/<id>` exists, and returns HTTP 500 for an id
that the server does not know. `GET /admin/active_requests` returns `[]`.
Whether the id is the `requestID` or the `client_context_id` is not measured.

## Couchbase Analytics

Ken decided on 2026-09-27 that Couchbase Analytics is part of this target,
and not a target of its own. By his reading, it is the same as the query
service. The reviewers of the targets described it as SQL++ on port 8095, in
the Enterprise image that `dbmeta` already runs. Nothing about it is
measured. Step 6 of [DRIVER.md](DRIVER.md) measures it with the query
service, and "Flavors" in that file applies if the two differ.

## Faults in go_n1ql that the new driver must not repeat

The `n1ql` session and `dbmeta` found these:

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

The `Version` function of the Couchbase driver in `usql` calls
`strconv.Unquote` on the result of `SELECT RAW ds_version()`. That call
depends on the JSON text fault, and it must go when `usql` moves to this
driver.

## The design of xo/n1ql

Ken approved a design for the rewrite of `xo/n1ql`. It is in
`xo/n1ql/docs/PLAN.md`, in its decisions 4 to 10, 13, 17 to 20, 24, 26 to
29 and 33. These parts of it fit this repository:

- The standard library only, and not `gocb/v2`.
- `ctx` everywhere, and `driver.RowsColumnScanner` from Go 1.27.
- No `driver.ErrBadConn` for a fault in a query, and no request sent again
  after it can have reached the server.
- `RowsAffected` comes from `metrics.mutationCount`, and `LastInsertId`
  returns an error.
- Options for one query come as typed arguments or from `WithOptions(ctx)`.
  The DSN comes first, then the context, then the argument, as in cql D23.
- Transactions come after the first release (D20).

The `n1ql` session stopped its rewrite on 2026-09-27, when Ken decided that
this driver replaces it. Its `docs/PLAN.md` keeps its decisions as a record,
and its D31 points at this repository.
