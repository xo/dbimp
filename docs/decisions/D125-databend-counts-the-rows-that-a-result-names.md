# D125. Databend counts the rows that a result names

Status: Decided.

Ken decided this on 2026-09-29, at step 17a of the Databend driver. A
statement that changes rows returns columns such as
`number of rows inserted`, `number of rows updated` and
`number of rows deleted`, and `MERGE INTO` returns one for each action
([DATABEND.md](../DATABEND.md)). `RowsAffected` is the sum of those columns
in the first row.

`REPLACE INTO` returns no such column. For such a statement,
`RowsAffected` returns an error that wraps `dbimp.ErrNotSupported`, where
Couchbase counts every statement by `mutationCount`. The final stats hold
`write_progress.rows`, which counts the rows that the statement wrote and not
the rows that it changed: it was 2 for an `UPDATE` of one row (recorded).
So the driver does not read it, and gives no count that it cannot know
(D8).
