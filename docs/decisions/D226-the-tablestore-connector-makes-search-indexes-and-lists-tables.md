# D226. The Tablestore connector makes search indexes and lists tables

Status: Decided on 2026-10-30.

`dbmeta` needs four more calls before its fixtures can drop the SDK of Aliyun: a search index on a
table, the delete of one, the list of the search indexes of a table, and the list of the tables of the
instance. SQL shows only the tables that a mapping names, so it cannot find a table that was made and
never mapped. D225 holds the table methods, and this decision follows its pattern.

1. The connector gets four methods in the package `tablestore`: `CreateSearchIndex`,
   `DeleteSearchIndex`, `ListSearchIndexes` and `ListTables`. Ken approved them on 2026-10-30 in
   chat. They are methods of `*Connector`, as D225 decided, with no new package and no setting in
   the DSN. Each call goes through `Connector.call`, so it follows the signature, redirect and
   retry rules of a statement (D8 and D222), and it is sent once.
2. A `SearchIndex` holds a name and `Fields`. A `SearchField` holds a name, a `SearchFieldType`
   and two flags, `Sort` and `Store`. The types are `SearchLong`, `SearchDouble`, `SearchBoolean`,
   `SearchKeyword` and `SearchText`. `dbmeta` needs only the keyword type. The other four are one
   number each in the request and need no member beyond the name, so the driver offers them. A
   `SearchText` field has no analyzer in the request, and the server picks its default. The
   driver always sends the flag `index` as true, because a field of an index that is not indexed
   has no use here. `Sort` sends `sort_and_agg`, and `Store` sends `store`, and each is sent only
   when it is true. The server turned `store` off for a field that had `Sort` (recorded:
   "describe the search index with the flags of a field"), so the driver passes both as given.
3. `CreateSearchIndex` takes one option, `WithSearchTimeToLive`, in the pattern of D109. It sets
   `time_to_live` in whole seconds. The server accepts -1 or a value of one day or more, and only
   for a table that refuses updates (recorded: "create a search index with a time to live of one
   hour", "create a search index with a time to live"). The driver leaves both checks to the
   server, as D225 does for the time to live of a table. `DeleteSearchIndex` takes the
   `DeleteOption` of D225, and `WithIfExists` ignores `ErrNotFound`.
4. The driver checks what it knows before it sends: a table name, an index name, at least one
   field, a name for each field, a type that exists, and a field name that repeats. It does not
   check that a column of the table has the name of a field, because the server does not (recorded:
   "create a search index on a column that the table lacks"). The server made an index for a name
   that no column has.
5. `ListSearchIndexes` returns the index names of one table, as the server gives them. The answer
   holds the table name and the index name of each index, and the driver reads the index name. A
   table with none gives an empty list. `ListTables` returns the names of every table of the
   instance, as the server gives them. It sends `ListTable`, which `Ping` sends too. Each call
   reads the whole answer into memory, which is a list of names.
6. The server answers HTTP 409 and `OTSObjectAlreadyExist` for a search index that is there, so
   `ErrExists` matches it. It answers HTTP 404 and `OTSObjectNotExist` with the text "search index
   not exist" for a search index that is not there on a table that is, so `ErrNotFound` matches
   it. For a table that is not there, `CreateSearchIndex`, `DeleteSearchIndex` and
   `ListSearchIndexes` all get HTTP 400 and `OTSParameterInvalid` with the text "table [x] does not
   exist". Ken decided that `ErrNotFound` also matches that code with that text. `WithIfExists` of
   `DeleteSearchIndex` ignores only a missing index (code `OTSObjectNotExist`), so a table that is
   not there still fails.
7. `DeleteTable` refuses a table that has a search index: HTTP 400 and `OTSParameterInvalid` with
   the text "table[x] who has at least one search index cannot be deleted" (recorded: "delete a
   table with three search indexes and an index"). A caller deletes each search index first, then
   the table, and that order worked (recorded: "delete a table whose search indexes are deleted").
   `DeleteTable` deletes the secondary indexes with the table, because the same table and index
   name could be made again at once (recorded: "create the table again with the same index"). The
   doc comments of `DeleteTable` and `DeleteSearchIndex` say this. The error has no sentinel of its
   own, which Ken decided.
8. The next release is `v0.23.0`, which Ken decided.
9. The measured facts of 2026-10-30, on a new instance of `dbmeta` in ap-southeast-5: a search
   index is there when `CreateSearchIndex` answers, which took 0.25 to 0.4 seconds. `DescribeSearchIndex`
   shows it at once, in the phase of the incremental sync, with the state "index is running". It showed
   the same three seconds later. `ListSearchIndex` shows it at once. A delete shows at once in
   `ListSearchIndex`. The recordings are `testdata/tablestore/tablestore-search-*.json` and
   `tablestore-delete-*.json`.
10. The driver leaves out the other types of a field (date, geo point, nested, vector, JSON, IP and
    fuzzy keyword), the analyzer and its parameters, the index sort, the number of shards, the
    routing fields, the update of a search index, the describe call, and the copy of a search
    index from another one. Each needs members that `dbmeta` does not need, and a caller can ask for
    them later.

See `docs/TABLESTORE.md`, under "Search indexes and the list of tables", and W50.
