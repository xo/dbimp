# D201. Five more catalog statements for Cosmos DB

Status: Decided.

`dbmeta` asked on 2026-10-11 for five more catalog statements, so that its model of Cosmos DB
can fill the kinds that the first nine statements leave empty. Ken decided on 2026-10-11 to
add all five (W45). This adds to item 17 of D190, and it follows the same rules: a statement
is a `SELECT` against a reserved name, with a `WHERE` on the names of the database and of the
container, the driver only reads, and each statement gives one flat row for each object, with
JSON only for what is open ended.

1. `$index_paths` gives one row for each path of one entry of the indexing policy: the
   database, the container, the path, the kind (`included`, `excluded`, `composite`,
   `spatial`, `vector` or `full_text`), the order of a path of a composite index, the
   position of the path inside its composite index, the id of the composite group, the
   spatial types, and the type and the dimensions of a vector index where the policy has
   them.
2. `$unique_keys` gives one row for each unique key (the database, the container and the
   position of the key), and `$unique_key_paths` gives one row for each path of a key (the
   position of the key, the position of the path and the path).
3. `$routines` gives the stored procedures and the user defined functions in one set of rows:
   the database, the container, the kind (`stored_procedure` or `function`), the id, the body,
   the resource id, the etag and the timestamp. The triggers stay in `$triggers`.
4. `$partition_key_ranges` gives one row for each range: the database, the container, the id,
   the minimum (inclusive), the maximum (exclusive), the status and the parents.
5. `$session` gives one row with what the DSN says and no request: the host, the account, the
   database and the container. A key of Cosmos DB has no user name, so the statement has no
   user, and it does not say whether the key is the primary or a read-only key, because the
   driver cannot tell it without a request that fails for the read-only key.
6. A key that is read-only still gets HTTP 401 on `$users` and `$permissions`, as before.
