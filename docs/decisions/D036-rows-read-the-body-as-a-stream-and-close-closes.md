# D36. Rows read the body as a stream, and Close closes it

Status: Decided.

This answers Q8, from Ken's first design and a consultation on 2026-09-27.
Ken accepted it on 2026-09-27.
Gemini answered. DeepSeek timed out twice. The source of Go 1.27 settled the
two claims that the design depends on.

The design is this:

1. `Rows` holds the body of the response and a `jsontext.Decoder` that reads
   from it, and no other buffer. `jsontext.Decoder` keeps its own buffer and
   reads from the body into it (measured in `encoding/json/jsontext` in Go
   1.27.1). A `bufio.Reader` in front of it adds only a second copy.
2. `Rows.Next` reads the tokens of one row, and no more.
3. When `Next` reaches the end of the rows, it reads the rest of the
   response, such as the `errors`, `status` and `metrics` of Couchbase. An
   error there comes back from `Next`, so a result cut short by the server
   never looks complete (D21). The body is then at EOF.
4. `Rows.Close` before the end closes the body and reads nothing more. On
   HTTP/1.1 the transport then closes the connection, and does not drain it
   (measured: `bodyEOFSignal.earlyCloseFn` in `net/http/transport.go` of Go
   1.27.1). On HTTP/2 the stream is reset and the connection stays. To drain
   a large result to save one connection costs more than the connection.
5. `Rows.Close` after the end closes a body that is already at EOF, so the
   connection goes back to the pool.
6. A cancelled context aborts the read of the body, and `Next` returns the
   error of the context, never `driver.ErrBadConn`.

A server can keep running a query after the client closes the connection.
Couchbase has `DELETE /admin/active_requests/<id>`, Trino has a `DELETE` on the
next URI, and ArangoDB has a `DELETE` on the cursor. `Close` has no context,
and the library never makes one. So a driver relies on the server noticing the
closed connection, and item 7 of step 6 of [DRIVER.md](../DRIVER.md) measures that
for each product. Gemini said that Couchbase stops such a query, and nothing
measured it. If a product does not stop, its driver records a decision of its
own, and Ken makes it. The choice is between keeping `context.WithoutCancel` of
the context of the query in `Rows`, which breaks the rule that a struct never
holds a context, and a timeout that the connector holds.
