# D50. The SurrealDB driver sends each statement to POST /rpc

Status: Decided.

Ken accepted this on 2026-09-27. It decides items 6 and 8 of step 9 for SurrealDB. The driver sends each
statement as the RPC method `query`, with the text and the arguments as
`params`, to `POST /rpc`. `POST /sql` binds a parameter of its query string
as a string, so `$x` of `?x=5` is `"5"` (measured on 3.3.0). `/rpc` keeps the
type of each argument (measured on 2.7.0 and 3.3.0).

Each request sends `Surreal-NS` and `Surreal-DB`, from the path of the URL
(D48).

SurrealQL has named parameters only, and `$1` is a syntax error (measured on
both releases). So `sql.Named("x", v)` binds `$x`, and a positional argument
is an error that says to use `sql.Named`. The driver never rewrites the text
of a statement. Ken decided the treatment of a positional argument on
2026-09-27.

The server sends the whole response at once. 3.3.0 sent a `Content-Length`
and 2.7.0 sent chunks, and neither pages. 100000 records arrived in one body
of 3.4 MB (measured). The driver still reads the body one item at a time as
`Rows.Next` asks (D21 and D25).
