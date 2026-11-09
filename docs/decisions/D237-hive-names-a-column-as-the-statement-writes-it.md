# D237. Hive names a column as the statement writes it

Status: Decided on 2026-11-09.

The `usql` session reported on 2026-11-09 that Hive names the columns of `select * from t` as `t.id` and
`t.name`, because the key `hive.resultset.use.unique.column.names` is true by default. `usql` writes the names of
the columns into an `INSERT` for `\copy`, and Hive refuses `t.id` there. `psql` shows `id`.

The driver sends `hive.resultset.use.unique.column.names` as `false` in the `confOverlay` of each
`ExecuteStatement` when `GetInfo` named the server Hive. The key goes with the statement and changes no state of
the session. `WithParameter` sets it again for one statement. Impala has no such key, and the driver sends
nothing to Impala or to a server that it does not know. Hive then names a second column of the same name `id_1`,
and Impala keeps both as `id` (measured on 4.2.1 and 4.5.3 on 2026-11-09).

A server that restricts the keys that a statement can set could refuse the key. The default image of Hive does
not, and a caller that meets the refusal can use a DSN of a server that allows it. This is not measured on such a
server.

Ken decided it on 2026-11-09.
