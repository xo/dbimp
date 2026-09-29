# D90. ArangoDB streams each cursor, and stops it

Status: Amends D36 and D45, and amended by D99.

D99 puts the comment at the start of a query that, with the comment at its
end, is longer than 4096 bytes.

Ken decided on 2026-09-29 how the ArangoDB driver reads a result and stops a
query. This is items 8 and 9 of step 9 of [DRIVER.md](../DRIVER.md). These
facts are measured on 3.12.12:

- Without `stream`, the server computes the whole result before the first
  batch. With `stream`, it computes each batch when the client asks, and an
  error after some rows arrives as the answer to a later fetch, with HTTP 500.
- `result` comes before `hasMore` and `id` in each batch, and each batch has
  a `Content-Length`, so the server builds one batch at a time.
- A closed connection does not stop a query. `DELETE` on a streaming cursor
  stops it. A comment at the start of the query stays in the text that
  `/_api/query/current` lists, and `DELETE /_api/query/<id>` kills it. An
  ordinary user with `rw` on the database can do both for its own queries.

So:

- Every query sends `options.stream: true`, and `batchSize` from the DSN
  (D93). `count` and `fullCount` do not work with it.
- The driver reads each batch one token at a time (D25). When a batch ends
  and `hasMore` is true, it fetches the next one with
  `POST /_api/cursor/<id>`.
- The rows keep the context of `QueryContext` for their life, and each fetch
  uses it. `database/sql` closes the rows when that context ends, so the rows
  live no longer than it. Ken approved this exception to hard rule 4, as D69
  keeps the context of `BeginTx`. It amends D45, which named the first
  exception. D69 and D91 name the others.
- When the context ends, or `Rows.Close` comes before the end, the driver
  sends `DELETE` on the cursor. The id of the cursor and `hasMore` follow the
  rows of a batch (measured), so in the middle of the first batch the driver
  does not know the id. In a later batch it knows the id from the batch
  before, and deletes the cursor. In the first batch:
  - Each batch comes with a `Content-Length`. If at most 1 MiB of the batch
    is left, the driver reads the rest of that batch, learns the id, and
    deletes the cursor. The server sent the whole batch already, so this
    reads nothing of the result past the one batch. It amends item 4 of D36,
    by which `Rows.Close` before the end reads nothing more.
  - Otherwise it kills the query by its comment, below. A kill marks the
    query `killed`, but its cursor stays, and holds its collections, until
    the next fetch or the end of its `ttl`, 30 seconds by default (measured
    on 3.12.12 on 2026-09-29). The driver does not know the id, so it cannot
    fetch. A drop of the collection waits for it. With `cancel=none`,
    the driver sends nothing, and the cursor lives until its `ttl` too.
- With the key `cancel=tag`, the default, each query ends with a line of its
  own that holds the comment `// dbimp:<id>`, where the id names the
  connection and the query, because one connection can hold more than one
  open cursor. When the context ends while the first request still runs, the
  driver finds the query by the comment in `/_api/query/current`, and kills
  it, as D67 does for Neo4j. With `cancel=none`, it sends nothing, and the query
  runs on to its end.
- Ken chose a comment at the start, as D67 put it for Neo4j. The `usql` session
  then found on 2026-09-29 that the comment at the start of a Neo4j statement
  moves the position in each error by its length, and shows in the message.
  So the comment of ArangoDB comes at the end, on a line of its own. The
  positions in an error then stay those of the text of the caller, and a line
  comment cannot close a comment or a string that the caller left open. The
  measured fact holds for any place: the text that `/_api/query/current`
  lists is the whole query.
