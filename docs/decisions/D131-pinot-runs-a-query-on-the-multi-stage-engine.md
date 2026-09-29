# D131. Pinot runs a query on the multi-stage engine

Status: Decided.

Ken decided this on 2026-09-30, at step 9 of the Apache Pinot driver. The
single-stage engine, the default of the server, returns 10 rows for a query
with no `LIMIT`, with nothing that says that it cut the result, and it runs
no join. The multi-stage engine returned the whole table of 97,889 rows, and
runs joins and subqueries (measured, [PINOT.md](../PINOT.md)).

So each query runs with `useMultistageEngine=true`, and a query without
`LIMIT` returns every row (D21). The key `engine=single` of the DSN, and the
option `WithEngine`, choose the single-stage engine, and the documents of the
driver say that it cuts a result at 10 rows.
