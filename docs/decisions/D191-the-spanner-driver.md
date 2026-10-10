# D191. The Spanner driver

Status: Decided. Amended by D195.

These are the answers to the step 9 questions of [SPANNER.md](../SPANNER.md)
(W38). Ken decided them on 2026-10-10. They build on D185 and D187.

1. The package is `spanner`, and it registers the name `spanner` with no alias
   (D26, D28 and D30). It uses no binary encoding (D13). It reads and writes JSON
   over the REST API of Spanner, and it never speaks gRPC.
2. Development uses the hosted instance `dbimp-spanner`, which `dbsetup`
   provides. The integration tests run on the Cloud Spanner emulator when
   `dbmeta` has its entry, and on the hosted instance only where a person
   supplies it. The free instance expires on 2027-01-08 and has no SLA.
3. The driver uses one multiplexed session for each connector, and it makes a new
   one when the server answers `NOT_FOUND`.
4. A broken stream returns its error, and the driver does not resume it with the
   `resumeToken`. When the context ends during a DDL statement, the driver calls
   `operations:cancel` on the operation.
5. The secret is a path to a service account key file, in a key of the DSN
   query such as `credential_file`. The driver signs a JWT with RS256 for the
   token endpoint, with the scopes `spanner.admin` and `spanner.data`. A caller
   can also pass a ready access token through a connector. The key text never
   sits in a URL.
6. `BeginTx` calls `beginTransaction`, for read-write or read-only. An `ABORTED`
   answer (HTTP 409 with a `retryDelay`) goes to the caller, who retries the
   whole function. A commit of a read-only transaction sends nothing.
7. The driver turns each `?` into `@p1`, `@p2` with the root parser and always
   sends `paramTypes`. A caller can write `@name` with `sql.Named`. A statement
   that mixes both forms is an error.
8. The driver detects a DDL statement and sends it to `updateDatabaseDdl`. It
   polls the operation until it is done, and an error of the operation is the
   error of `Exec`.
9. A JSON column is a decoded Go value. A FLOAT32 column is a `float64`, as the
   server writes the widened value, which converts back exactly. An INTERVAL
   value is a `dbimp.Interval`, from its ISO 8601 text. A column cannot have the
   INTERVAL type, so the value only comes from an expression.
10. The first version covers GoogleSQL and the 13 types that SPANNER.md maps. A
    column of the type ENUM or PROTO makes the driver return an error that names
    the column. A TOKENLIST column cannot be selected. The driver refuses a
    database of the PostgreSQL dialect when it connects.
11. The other proposals of step 9 stand as SPANNER.md writes them:
    - The driver reads every result with `executeStreamingSql`, because
      `executeSql` has no paging and fails above 10 MB.
    - It joins the pieces of a `chunkedValue` as plain text before it decodes
      base64.
    - Several statements in one request are refused, as the service answers HTTP
      501.
    - The Bearer token goes only to the configured host.
    - The driver serves no flavor and sends no request for a version.
12. The Spanner driver makes these choices, which Ken accepted on 2026-10-11:
    - The keys of the DSN query are `credential_file` and `tls`. User information
      is refused. `tls` is false for `localhost` and a loopback address, and true
      for any other host. The default port is 443 with TLS and 9020 without.
    - `WithTimeout` above zero returns `dbimp.ErrNotSupported`, because the REST API
      has no timeout on the server.
    - A session that the server lost (HTTP 404 `NOT_FOUND` on a session) is dropped
      and the error wraps `driver.ErrBadConn`, so that `database/sql` tries again on
      a new session. Inside a transaction the driver does not try again.
    - A `float32` binds as FLOAT32, a slice binds as ARRAY (a nil slice is a typed
      NULL array), and a map or a `jsontext.Value` binds as JSON. A STRUCT parameter
      is refused with an error that says so. A zero `dbimp.Interval` is sent as
      `P0Y`.
    - `RepeatableRead` sends `isolationLevel: REPEATABLE_READ` in `beginTransaction`.
      A live test measures it, and the level returns `ErrNotSupported` if the
      service refuses the member. `Default` and `Serializable` send nothing, and the
      other levels return `ErrNotSupported`.
