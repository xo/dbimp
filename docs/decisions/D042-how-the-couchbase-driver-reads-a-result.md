# D42. How the Couchbase driver reads a result

Status: Decided.

Ken accepted this on 2026-09-27.

A result is one response, and it does not page. The driver reads the
`signature` for the columns, then each row of `results` as `Rows.Next` asks
(D36). After the last row it reads the rest of the response. If `errors` is
not empty, or `status` is not `success`, `Rows.Next` returns an error that
wraps `dbimp.ErrIncomplete` and holds each code and message (D21). A
`status` of `stopped` is an error for the same reason, because the result
is cut short.

The driver relies on the server to stop a query when the client
disconnects, which it does on every release (D36). `Rows.Close` sends no
cancel. The driver uses no binary encoding, because the server offers none.
Couchbase Analytics waits for a later work item, because `dbrun` does not
publish its port.
