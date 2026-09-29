# D99. A long ArangoDB query has its tag at the start

Status: Amends D90.

Ken decided this on 2026-09-29. D90 ends each query with the comment of
`cancel=tag`, on a line of its own, and the driver finds a query to kill by
that comment in `/_api/query/current`. That list keeps only the first 4096
bytes of the text of a query, which is `maxQueryStringLength` of
`/_api/query/properties`, and puts `... (<n>)` in place of the rest. A query
of 6000 bytes lost the comment at its end, so the driver could not find it
(measured on 3.12.12 on 2026-09-29).

- When the query, with the comment at its end, is 4096 bytes or less, the
  comment stays at the end, as D90 says.
- Otherwise the comment goes at the start, on a line of its own:
  `// dbimp:<id>-<seq>` and a line break, before the text of the caller.
  The lines of an error in such a query move by one, and its columns do not.
- The driver finds a query by either form: a text that ends with the line of
  the comment, or one that starts with it.

The driver uses the default of 4096, and does not read
`/_api/query/properties`. A server that keeps less loses the comment of a
shorter query. `TestIntegrationCancel` kills a query of 6000 bytes, and
fails when the comment is at the end.
