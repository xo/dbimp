# D66. The Neo4j driver reads each result as it arrives, and its errors at the end

Status: Decided.

Ken accepted this on 2026-09-27. It decides item 8 of step 9 for Neo4j, with
D21.

- The driver reads the body one token at a time: `fields` first, then each
  row of `values`, then the members that follow (D25). The columns keep the
  order of the statement (measured).
- A response with HTTP 202 can hold `errors` after the rows (measured). The
  error reaches the caller from `Rows.Next` and `Rows.Err`, after the rows
  that arrived before it. `Rows.Close` reads the body to its end, so the
  error is never lost.
- A row with fewer values than `fields` is an error. The server sends such a
  row before an error, such as a vector in typed JSON v1.0 (measured).
- An error is a `*neo4j.ResponseError`, with the HTTP status and each
  `neo4j.Error`, which has the `Code` and the `Message` of the server, as in
  the Couchbase driver and D55. A response that is not JSON is a
  `*dbimp.StatusError`.
- `RowsAffected` and `LastInsertId` return `ErrNotSupported`. The counters
  of Neo4j count nodes, relationships and properties apart, and none of them
  is a count of rows.
- `bookmarks`, `notifications` and the plan of `EXPLAIN` and `PROFILE` are
  not read. One server needs no bookmark.
