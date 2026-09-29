# D107. ErrIncomplete means a failure after a row

Status: Amends D42.

Ken decided this on 2026-09-29, after Gemini and DeepSeek were asked. Both
gave the same rule. `dbimp.ErrIncomplete` tells a caller that it holds part
of a result: at least one row of a result set reached it, and then the
result set failed or was cut short. It is not a sign of any failure, so that
a caller can rely on `errors.Is(err, dbimp.ErrIncomplete)`.

- A driver wraps `dbimp.ErrIncomplete` only when a row of the result set
  reached the caller before the error.
- A failure before the first row, such as a syntax error, a statement that a
  cancel stopped before it sent a row, or an error of Neo4j after a row with
  no values, is an ordinary error, and does not wrap it.
- A failed statement of SurrealDB is a result set of its own, so it wraps it
  only after a row of that result set. An answer that holds no result at all
  is a fault of the form of the answer, and wraps `dbimp.ErrInvalidValue`.

D42 said that a Couchbase error after the last row, or the status
`stopped`, wraps `dbimp.ErrIncomplete`, even with no row. That changes for
Couchbase, the only driver of a release that wrapped it for a failure with
no row, so the next release says so.
