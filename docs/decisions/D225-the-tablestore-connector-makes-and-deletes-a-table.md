# D225. The Tablestore connector makes and deletes a table

Status: Decided. Ken decided it on 2026-10-30.

SQL cannot make a Tablestore table. It can only map a table that exists to a name (D224). The
`dbmeta` fixtures need to make and delete tables, and they imported the SDK of Aliyun for it.
Ken asked for a small helper in this package on 2026-10-30, and decided each point below on that
day, the first two before the code and the others after the live measurement.

1. The helper is three methods of `*Connector` in the package `tablestore`: `CreateTable`,
   `DeleteTable` and `CreateIndex`. Ken chose methods of the connector over a package such as
   `tablestore/admin`. A method uses the signature, the redirect rule and the retry rule of
   `Connector.call`, so a call of the API follows the rules of a statement (D8 and D222). The
   package keeps its one registered name, and the methods add no setting to the DSN (D7).
2. A `Table` holds a name, the primary key columns, the defined columns and the indexes. A
   primary key column is an `Integer`, a `String` or a `Binary`. A defined column is one of
   those or a `Double` or a `Boolean`. The connector checks what it knows before it sends: a
   name, at least one key, the type of each column, a name that repeats, and an index that names
   a column that the table does not have. The server checks the rest.
3. Each method takes options as variadic arguments at the end, in the pattern of D109, so that a
   call with none keeps the defaults. The types are `TableOption`, `IndexOption` and
   `DeleteOption`, each `dbimp.Option` of its own struct, so that a caller cannot hand an option
   to the wrong call. The defaults are what the first version fixed: a time to live of -1, one
   version, zero reserved throughput, a global index that the server fills with the rows that
   the table holds, and a delete that fails for a table that is not there. Ken decided that a
   caller can change each of them, and the members that the request has and that a caller of
   `dbmeta` or `usql` can need:
   - `CreateTable`: `WithTimeToLive`, `WithMaxVersions`, `WithReservedThroughput`,
     `WithDeviation`, `WithAllowUpdate`, `WithStream` and `WithLocalTransaction`.
   - `CreateIndex`: `WithBaseData`. The update mode of an index is the field `Sync` of `Index`,
     and not an option, because it describes the index and the same index can be a member of a
     `Table`.
   - `DeleteTable`: `WithIfExists`, which the request does not have, because the request holds
     only the name. It is the client form of `DROP TABLE IF EXISTS`.
   The server settings that the methods do not offer are the bloom filter, the block size, the
   encryption, the partitions and an index that is not global (Ken decided that no local index
   is needed). An option with a value that the protocol cannot hold, such as a time of a fraction
   of a second, fails the call with `dbimp.ErrInvalidValue` before it sends.
4. An `Index` holds a name, the columns of its key, the defined columns that its table holds,
   and `Sync`. It is global. `CreateTable` takes the indexes of the `Table`, and `CreateIndex`
   adds one to a table that exists. There is no `DropIndex`, because Ken decided that it is not
   needed, and `DeleteTable` removes the indexes with the table.
5. The server answers HTTP 409 with the code `OTSObjectAlreadyExist` for a table or an index that
   is there, and HTTP 404 with the code `OTSObjectNotExist` for one that is not (recorded:
   "create a table that is there", "delete a table that is not there"). `*Error` matches the
   constants `ErrExists` and `ErrNotFound` by these codes. `DeleteTable` of a table that is not
   there is an error, because the server says so, unless `WithIfExists` is given.
6. A call is sent once, as every call of the driver is (D8). The methods do not retry, which Ken
   decided. A reset of the connection before the answer reaches the caller, and a caller who
   tries `CreateTable` again can then read `ErrExists` for a table that the first call made.
7. The measured facts: the table of a `CreateTable` took 0.25 to 0.55 seconds to answer, and a
   `CREATE TABLE` mapping and an `INSERT` worked at once. A name was free for `CreateTable` again
   at once after `DeleteTable`. The index shows in `DescribeTable` at once. Ken decided that the
   content of an index is not measured. The server refuses a time to live under one day:
   `OTSParameterInvalid: TimeToLive cannot be less than 86400.` A table made with a time to live
   of one day, three versions and no update showed those values in `DescribeTable`. The options
   for the throughput, the deviation, the stream and the local transaction are checked on the
   wire against the protocol, and not against the server.
8. The next release is `v0.22.0`, which Ken decided.

See `docs/TABLESTORE.md`, under "Making and deleting a table", and W49.
