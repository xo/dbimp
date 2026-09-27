# D55. The errors and the results of a SurrealDB write

Status: Decided.

Ken accepted this on 2026-09-27. It decides how the SurrealDB driver reports an error, with item 8 of step
9.

- A response with a status other than 200 is an error from `QueryContext`
  or `ExecContext`. A parse error anywhere in the text is HTTP 400, and no
  statement runs. A wrong password is HTTP 401 (measured).
- An error of the RPC call, such as a parse error in `/rpc`, is HTTP 200 with
  an `error` object, and it is an error from `QueryContext`.
- A statement that fails is HTTP 200 with `"status": "ERR"` for that
  statement, and the other statements still run (measured). The error
  reaches the caller when the rows reach that result set. `ExecContext`
  reads every result, and returns the first error.
- `RowsAffected` and `LastInsertId` return `ErrNotSupported`. The server
  sends no count, and a `DELETE` returns an empty array.

Ken decided on 2026-09-27 that the errors take the form of the errors of the
Couchbase driver, so that a caller such as `usql` reads both drivers in the
same way. A failed statement, a failed RPC call, and a request that the
server refuses with a body of JSON, such as HTTP 400, are each a
`*ResponseError`. It holds the HTTP status, the status of the statement,
such as `ERR`, and each `Error`, which `errors.As` finds. An `Error` is a
value with `Code`, `Kind` and `Msg`. `Kind` is the kind that 3.x names,
which Couchbase has no form for. A response that is neither JSON nor CBOR,
such as the plain text of HTTP 401, is a `*dbimp.StatusError`, as in the
Couchbase driver.
