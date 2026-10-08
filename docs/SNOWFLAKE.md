# Snowflake

This file holds what is known about Snowflake over its SQL REST API, for a
possible driver `snowflake` (W34 and D182). The headings are the template of
[DRIVER.md](DRIVER.md). A fact is "measured", with the date, or "not measured",
with its source.

The measurement is blocked. On 2026-10-08 the session needed the private key
of the account, and `dbrun dsn --json snowflake` prints the key masked. The
flag `--reveal` prints it, and the permission system of the session refused
to run it. So no request has reached the account, and every fact below about
the server is "not measured". Only step 3 and the model half of step 5a are
done. See "Open questions".

## Summary

- Product: Snowflake, a hosted data warehouse. Release: not measured. The
  service has no release that a person picks.
- Name in `dbrun`: `snowflake`. Measured on 2026-10-08 with `dbrun list all`:
  the kind is `hosted`, the tier is `verified`, and the dialect is
  `snowflake`. `dbrun dsn --json snowflake` prints a URL with the scheme
  `snowflake`, the user, the account as host, the database as path, and the
  query keys `authenticator` (value `SNOWFLAKE_JWT`), `privateKey`, `role` and
  `warehouse`. Each secret is masked in that output. The role and the
  warehouse are named `DBMETA_ROLE` and `DBMETA_WH`.
- R: a hosted service has no container. `dbrun` cannot start or stop it. It
  lists the entry only while a person has put a connection string where
  `dbrun` reads it (dbmeta D117). The other targets start from an image that
  any machine can pull. Here CI needs a secret that holds a private key. The
  account is a trial that ends after 30 days or when its credit of 400
  dollars is used (D182). A test in CI would need a new account after that.
  This is the cost and the secret that R cannot hide. Whether CI can reach
  the account is not measured.
- H: `POST /api/v2/statements`, which is HTTPS and JSON. Not measured, source:
  [TARGETS.md](TARGETS.md) and the documentation of the SQL API.
- S: SQL. Not measured, source: the same.
- Whether it can be a driver: not decided. No condition of "When it cannot be
  a driver" was shown to hold, and none was shown to fail.
- Scheme in `dburl`: `Name` is `snowflake`, the alias is `sf`, the generator is
  `GenSnowflake`, and the `Dialect` is `snowflake`. `GoPackage` is
  `github.com/snowflakedb/gosnowflake/v2`. `GenSnowflake` writes
  `user:pass@host:port/dbname?options`, which is the DSN of `gosnowflake`.
  Source: `dburl/scheme.go` and `dburl/dsn.go`, read on 2026-10-08.
- Driver of `usql` now: `snowflakedb/gosnowflake/v2` v2.2.0, in
  `usql/drivers/snowflake/snowflake.go`. It only turns the logger off and
  maps `SnowflakeError` to a code and a message. Its `Version` statement is
  `SELECT CURRENT_VERSION()` (`usql/docs`, quoted in dbmeta `docs/USQL.md`),
  and that statement is not measured, because no account was reached then.
- What `dbmeta` has: `models/snowflake` was written from the documentation
  and never run (dbmeta D144). It reads `INFORMATION_SCHEMA`.

## Requests

Not measured. Source: the documentation of the SQL API, which names
`POST /api/v2/statements`, `GET /api/v2/statements/<handle>`, `GET
/api/v2/statements/<handle>?partition=N` and `POST
/api/v2/statements/<handle>/cancel`. The brief for W34 names the body members
`statement`, `timeout`, `database`, `schema`, `warehouse`, `role`,
`parameters` and `bindings`. Each one waits for the account.

Authentication, from the source of `gosnowflake` v1.18.1 (`auth.go`, function
`prepareJWTToken`), which is not the SQL API:

- The JWT is signed with RS256.
- `iss` is `<ACCOUNT>.<USER>.SHA256:<fingerprint>`, and `sub` is
  `<ACCOUNT>.<USER>`.
- The fingerprint is the standard base64 of the SHA-256 of the public key in
  PKIX form.
- The account name is the part of the account before the first dot, in upper
  case. The user name is in upper case.
- `iat` is now, `nbf` is a fixed date in 2015, and `exp` is `iat` plus
  `JWTExpireTimeout`, which is 60 seconds by default.

The SQL API takes the same JWT as `Authorization: Bearer <JWT>` with the
header `X-Snowflake-Authorization-Token-Type: KEYPAIR_JWT`. Not measured.

The recorder can send both headers. A `header` of a request holds the second
one, and the script `auth` value `bearer` sends the password of the URL as the
Bearer token. One fault of the recorder: the command that follows a link,
`follow`, sends basic authentication and not the Bearer token (the function
`get` in `dbimptest/cmd/record/main.go`). A script for Snowflake must read
each partition with a `capture` of the handle in place of `follow`. Also, a
JWT expires after at most one hour, so a long script needs a new JWT.

## The DSN

The URL of `dburl`: not measured, source above. `dbrun` prints
`snowflake://<user>@<account>/<database>?warehouse=<warehouse>&role=<role>&authenticator=SNOWFLAKE_JWT&privateKey=<key>`
(`hosted/hosted.go` in `dbmeta`, read on 2026-10-08). `privateKey` is a
secret in the query, and D94 says that the secret of a driver here is the
password of the URL. This is a fact about the conflict, and the choice
belongs to step 9.

## Responses

Not measured. Source: the brief for W34 and the documentation of the SQL API.
Facts from `gosnowflake` v1.18.1: its own protocol returns the format `json`
or `arrow` in `queryResultFormat` (`query.go`), and it downloads each chunk
from cloud storage (`chunk_downloader.go`). The SQL API is not that protocol.
Whether the SQL API can return a binary encoding is not measured.

## Types

No type is measured. The survey below lists the types that a model named.
The table of step 8a waits for the account.

## Parameters

Not measured. Source: the brief for W34, which names positional `?` with
`type` and `value`.

## Transactions

Not measured. Source: the brief for W34, which says that the SQL API has no
session.

## Errors

Not measured.

## Cancellation and timeouts

Not measured.

## Statements

Not measured. A model named the parameter `MULTI_STATEMENT_COUNT`.

## Principals

`dbrun dsn --json snowflake` lists one principal, the user of the DSN. The
brief for W34 names no ordinary user. Not measured whether the role can
create tables in its schema.

## Flavors

None known. Source: [TARGETS.md](TARGETS.md).

## Interfaces

Not written. It needs code (step 10).

## Faults

Not measured. Source: `gosnowflake` v1.18.1 `CHANGELOG.md` names, for 1.18.2,
a fix for HTTP 307 and 308 answers, which a driver here must follow or
refuse (D8). No other fault is read yet.

## Second opinions

Survey of step 5a, on 2026-10-08:

- Gemini (`gemini-3.1-pro-preview`) answered in a first conversation. Its list
  went into `testdata/snowflake/features.json` with the verdict `not
  measured`. It said that `PUT` and `GET` of local files do not work over the
  SQL API, that a primary key, a foreign key and a unique constraint are not
  enforced, that there is no index, and that the types are written as
  strings: a date as days, a time and a timestamp as seconds with nine
  decimals, `TIMESTAMP_TZ` with an offset in minutes, a binary value as hex,
  and a variant, object and array as JSON text. It marked the type name of
  `VECTOR` as unsure. Each statement is a lead.
- DeepSeek (`deepseek-v4-pro`) timed out twice, on the first try and on the
  retry, with `max_tokens` at 12000. No answer was recorded.
- Drivers read: `gosnowflake` (Go). A driver in another language is not read.
- Step 7 is not done.
- Step 8a is not done. Two models have not reviewed a mapping.

## Open questions

1. The session cannot read the private key. `dbrun dsn --json snowflake`
   masks it. `dbrun dsn --json --reveal snowflake` prints it, and the
   permission system refused that command. Ken must run the measurement from
   a session that is allowed to, or run `--reveal` himself and put the key in
   a file with mode 600 that the session can read.
2. DeepSeek timed out twice. Ask it again, or name another model.
3. The recorder follows a link with basic authentication only. Ken decides
   whether to fix `get` in `dbimptest/cmd/record/main.go`, or to use captures.
