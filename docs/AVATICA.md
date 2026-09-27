# Avatica

This file holds what is known about Apache Calcite Avatica, for a driver
that D74 places after libSQL. Its work item comes when its turn comes
(D73). The headings are the template of [DRIVER.md](DRIVER.md).

This is the draft of step 3. No server has run yet, because `dbrun` has no
entry for Avatica, so every fact here is "not measured" and names its
source. The sources, each read on 2026-09-27, are these:

- "The JSON reference" is `site/_docs/json_reference.md` in
  `github.com/apache/calcite-avatica`.
- "The Go driver" is `github.com/apache/calcite-avatica-go/v5` v5.4.0,
  which `usql` uses: its code, its `docs/go_client_reference.md` and its
  `docker-compose.yml`.
- "The Druid documents" are `docs/api-reference/sql-jdbc.md` and
  `docs/configuration/index.md` in `github.com/apache/druid`.
- "GitHub" and "Docker Hub" are the tags that each one listed.
- "Gemini" is `gemini-3.8-flash`.

## Summary

- Avatica is a wire protocol, and not a database. A client sends JDBC calls
  to an Avatica server over HTTP, and the server runs each one on a
  database behind it through JDBC (the JSON reference). So one driver serves
  every product that speaks it, and each product is a flavor (D74).
- The flavors are the standalone Avatica server, the Apache Phoenix Query
  Server and Apache Druid (Gemini, the Go driver and the Druid documents).
  Gemini said that Apache Kylin, Apache Drill, Apache Ignite, Dremio, Apache
  Hive and Apache Solr do not speak Avatica, although some of them use
  Calcite inside. Gemini also named Alibaba Cloud Lindorm as a flavor. It is
  a cloud service, so it fails R.
- The latest release of Avatica is 1.29.0 (GitHub). The latest release of
  the Phoenix Query Server is 6.0.0 (GitHub). The latest release of Druid is
  37.0.0 (Docker Hub).
- `dburl` has the scheme `avatica`, with the alias `phoenix` and the default
  URL `http://localhost:8765/`. Its `GoPackage` is the Go driver. `usql` has
  `drivers/avatica`, which imports the Go driver at v5.4.0 and reads the
  code and the message of its `ResponseError`.
- `dbmeta` has no model for Avatica. Its D66 says that Avatica has no catalog
  of its own, because it stands in front of another database.
- D24 counts a driver that replaces one of `usql` as a way to cut the
  dependencies of `usql`. The Go driver brings protobuf, a Kerberos library
  and a digest library (its `go.mod`).
- `dbrun` has no entry for any flavor. The `dbmeta` session was asked on
  2026-09-27 for an entry for each of the three, with the facts under
  Flavors.
- R, H and S are not measured. H is likely, because each flavor takes HTTP.
  S is likely, because the statement is the SQL of the database behind the
  server. R depends on the image of each flavor, under Flavors.

## Requests

- Each call is one `POST` of one message (the JSON reference). The path is
  `/` for the standalone server and the Phoenix Query Server (Gemini), and
  `/druid/v2/sql/avatica/` for JSON on Druid, or
  `/druid/v2/sql/avatica-protobuf/` for protobuf (the Druid documents).
- In JSON, the field `request` names the call, such as `openConnection`, and
  the response names itself in `response` (the JSON reference). In protobuf,
  a `WireMessage` holds the name of the Java class of the message and its
  bytes, with `Content-Type: application/x-google-protobuf` (the Go
  driver). The Go driver speaks protobuf only (the Go driver and Gemini).
- The calls are these, with their fields (the JSON reference):
  - `openConnection` (`connectionId`, `info`), where the client chooses the
    id of the connection. `closeConnection` (`connectionId`).
  - `connectionSync` (`connectionId`, `connProps`), which sets `autoCommit`,
    `readOnly`, `transactionIsolation`, `catalog` and `schema`.
  - `createStatement` (`connectionId`), which returns a `statementId`, and
    `closeStatement` (`connectionId`, `statementId`).
  - `prepareAndExecute` (`connectionId`, `statementId`, `sql`,
    `maxRowCount`), and `prepare` (`connectionId`, `sql`, `maxRowCount`) with
    `execute` (`statementHandle`, `parameterValues`, `maxRowCount`).
  - `fetch` (`connectionId`, `statementId`, `offset`, `fetchMaxRowCount`).
  - `commit` and `rollback` (`connectionId`).
  - `executeBatch` (`connectionId`, `statementId`, `parameterValues`) and
    `prepareAndExecuteBatch` (`connectionId`, `statementId`, `sqlCommands`).
  - `syncResults` (`connectionId`, `statementId`, `state`, `offset`).
  - `databaseProperties`, `getCatalogs`, `getSchemas`, `getTables`,
    `getTableTypes`, `getColumns` and `getTypeInfo`, the metadata of JDBC.
- Authentication is none, HTTP Basic, HTTP Digest or SPNEGO with Kerberos,
  by the choice of the server (the Go driver and Gemini). Gemini said that
  Avatica 1.28 and later also take a bearer token. Druid uses its own
  authenticators (the Druid documents).

## The DSN

- Step 9 decides the URL (D27 and D35). The Go driver takes
  `http://host:port[/schema][?key=value]`, with the keys `authentication`,
  `avaticaUser`, `avaticaPassword`, `principal`, `keytab`, `krb5Conf`,
  `krb5CredentialsCache`, `location`, `maxRowsTotal`, `frameMaxSize`,
  `transactionIsolation` and `batching` (the Go driver). Its scheme is
  `http`, which D35 does not allow here, and `dburl` passes that URL
  through.
- The path of a flavor differs: `/` or `/druid/v2/sql/avatica/`. Step 9
  decides how the URL names it.

## Responses

- A statement returns a `ResultSetResponse`, or `executeResults` with a list
  of them (the JSON reference). It holds `signature`, `firstFrame` and
  `updateCount`.
- `signature` holds `columns`, one `ColumnMetaData` for each column, with
  `label`, `columnName`, `nullable`, `precision`, `scale` and `type`. So the
  columns and their types arrive before the rows, and rule 1 of D18 applies.
- A `Frame` holds `offset`, `done` and `rows`, an array of arrays. The client
  pages with `fetch` from the next `offset` until `done` is true (the JSON
  reference). So a large result is many requests, and each request is one
  frame.
- On Druid, `druid.sql.avatica.maxRowsPerFrame` is 5,000 by default, and
  `druid.sql.avatica.minRowsPerFrame` is 100. `druid.sql.avatica.fetchTimeoutMs`,
  5,000 by default, makes a slow fetch return an empty frame, and the client
  polls again (the Druid documents).
- Every response can hold `rpcMetadata` with `serverAddress`, the server that
  answered (the JSON reference).

## Types

Step 10 writes the type table. `AvaticaType` has `type` (`scalar`, `array`
or `struct`), `id`, `name`, `rep` and, for an array, `component` (the JSON
reference). In JSON, a value is encoded by its `Rep` (the JSON reference):

- A boolean is a JSON boolean. Each integer and float type, and `NUMBER`,
  is a JSON number. A decimal is a `NUMBER` (not measured whether its digits
  survive).
- A string is a JSON string, and `BYTE_STRING` is a string in base64.
- `JAVA_SQL_DATE` is a number of days since the epoch. `JAVA_SQL_TIME` is a
  number of milliseconds since midnight. `JAVA_SQL_TIMESTAMP` and
  `JAVA_UTIL_DATE` are numbers of milliseconds since the epoch. So a time
  loses anything finer than a millisecond, and a timestamp has no zone.
- NULL is `null`.
- An array, a struct and a multiset are written by Jackson, in a form that
  the reference does not give.

The Go driver says that Avatica and the database ignore the zone of a
timestamp, so a client sets `location` to read it (the Go driver).

## Parameters

- A statement names each parameter as `?`, and `execute` binds them in order
  in `parameterValues`, a list of `TypedValue`, each with `type` and `value`
  (the JSON reference). There are no named parameters.
- Druid takes `?` with a prepared statement (the Druid documents).

## Transactions

- `connectionSync` sets `autoCommit` and `transactionIsolation`, and `commit`
  and `rollback` end a transaction (the JSON reference). The isolation is 0
  for none, 1, 2, 4 or 8, as in JDBC.
- The Go driver says that a server supports transactions only if its
  database does, and suggests the isolation 4 for Phoenix 4.7 and later.
  Druid names no transactions (the Druid documents).

## Errors

- An error is `ErrorResponse`, with `response` set to `error`, and with
  `exceptions` (the Java stack traces as strings), `errorMessage`,
  `errorCode`, `sqlState` and `severity` (the JSON reference).
- An unknown or expired connection gives `NoSuchConnectionException`, an
  unknown statement gives `NoSuchStatementException`, and a fetch after the
  results expired gives `MissingResultsException` (Gemini). A `fetch`
  response also has `missingStatement` and `missingResults` (the JSON
  reference).
- The HTTP status of an error is not measured.

## Cancellation and timeouts

- The protocol has no request that cancels a running statement (the JSON
  reference and Gemini). Gemini said that `Statement.cancel` in the Java
  client only sets a flag on the client.
- The server keeps each connection and statement in a cache that expires. In
  the standalone server and the Phoenix Query Server, the settings are
  `avatica.connectioncache.*` and `avatica.statementcache.*`, with an expiry
  of 10 minutes by default (Gemini). On Druid,
  `druid.sql.avatica.connectionIdleTimeout` is `PT5M`,
  `druid.sql.avatica.maxConnections` is 25, and
  `druid.sql.avatica.maxStatementsPerConnection` is 4 (the Druid documents).
- Whether the server stops a query when the client leaves is not measured.

## Statements

- `prepareAndExecuteBatch` takes a list of statements, and every other call
  takes one (the JSON reference).
- `usql` allows the comments `/* */` and `--` for Avatica (`usql`).

## Principals

- Each flavor has its own users. The standalone server with HSQLDB and the
  Phoenix Query Server run with no authentication by default (Gemini). Step 4
  finds out which flavor can have an ordinary user.

## Flavors

- The standalone server: the image `apache/calcite-avatica-hypersql`, tag
  1.29.0 (Docker Hub), which the Apache Calcite project builds. The Go
  driver tests with it at 1.26.0, with the argument
  `-u jdbc:hsqldb:mem:public`. It listens on 8765 (the Go driver).
- The Phoenix Query Server: the Go driver tests with
  `ghcr.io/boostport/hbase-phoenix-all-in-one:2.0-5.0`, which runs
  ZooKeeper, HBase and the Query Server in one container, on port 8765. That
  image is not built by the Apache Phoenix project, and its newest tag on
  Docker Hub is from 2023 (Docker Hub). Gemini said that it needs 2 GB to
  4 GB of memory.
- Druid: the image `apache/druid`, tag 37.0.0 (Docker Hub). The Router, on
  port 8888, keeps a client on one Broker, because Brokers share no state
  of a connection (the Druid documents). `druid.sql.avatica.enable` is true
  by default (the Druid documents). TARGETS.md says that its quickstart can
  exceed the 4 GB limit of `dbmeta`.
- Step 9 decides how the driver tells the flavors apart from what the server
  says, such as `databaseProperties` (DRIVER.md, "Flavors").

## Interfaces

Step 10 writes the interface table from the code.

## Faults

These are faults of the Go driver that a driver here does not repeat (the
Go driver):

- A `fetch` of the next frame uses `context.Background`, so the context of
  the caller does not stop it (`rows.go`).
- Each response is read whole with `io.ReadAll` (`http_client.go`).
- Its DSN is an `http` URL, so its scheme is not the name of the driver
  (D35).
- It needs protobuf, a Kerberos library and a digest library (`go.mod`),
  where D13 allows the standard library, apd and a binary encoding that Ken
  approves.
- It speaks protobuf only, so a person cannot read a request (Gemini).
- It keeps no cookies, so a load balancer with sticky sessions can send a
  call to another server, which does not know the connection (Gemini).
- The zone of a time is taken from the key `location`, not from the server
  (the Go driver).

## Second opinions

Step 7 fills this section.

## Open questions

- Which flavors the driver serves, and whether each one passes R, are
  questions of steps 2 and 4.
- JSON or protobuf, and whether protobuf needs Ken's approval as a binary
  encoding for this driver (D13), are decisions of step 9.
- The URL of the DSN, and how it names the path of a flavor, are decisions
  of step 9.
