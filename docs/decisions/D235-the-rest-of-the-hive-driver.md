# D235. The rest of the hive driver

Status: Decided on 2026-11-09.

These are the items 5 to 11 of step 9 of [DRIVER.md](../DRIVER.md) for the package `hive` (W56, D232, D233 and
D234). Ken accepted them on 2026-11-09. Each fact is in [HIVE.md](../HIVE.md).

1. NULL and a missing value are one value, `nil` (D18). A set bit in the bitmap of a column is a NULL. A column
   of the type NULL is `nil` for every row.
2. The server binds no parameter. The driver puts each argument into the text of the statement as a literal,
   with the parser for placeholders of the root package (D34) and an escaper that is written in the package
   (D34). A string is in single quotes with a backslash before a backslash and a single quote. A number is its
   text. A bool is `true` or `false`. A `[]byte` is `cast(unhex('<hex>') as binary)`. A `dbimp.Date` is
   `cast('<date>' as date)`, and a `dbimp.LocalDateTime` is `cast('<text>' as timestamp)`. A step before the
   driver is complete measures the literals on both flavors, and a literal that fails there is a decision to
   change.
3. `BeginTx` returns `dbimp.ErrNotSupported` (D20). Neither flavor has a statement for a transaction over this
   interface.
4. The driver reads a result with `FetchResults` and `FETCH_NEXT` in pages of 1000 rows, and ends at the first
   empty page, because Hive reports `hasMoreRows` false on every page (D21). It decodes a page as it arrives and
   never holds more than one page. It reads `hasResultSet` of the operation handle, and for a statement without a
   result set it fetches nothing.
5. The driver sends `CancelOperation` when the context of a statement ends, from a second request, and then
   `CloseOperation` and `CloseSession`. A fetch that blocks on Impala ends with an error status after the
   cancel (D36). A closed connection stops no work that the server runs, so the driver never relies on it.
6. The driver follows no redirect, and sends the credentials to the host of the DSN only.
7. The driver serves Hive and Impala as flavors (D233). It tells them apart with `GetInfo` and the info type 13
   when the session opens, which answers `Hive` or `Impala`, and it reads the version with the info type 18. It
   never reads the DSN for this. The flavor decides the first state of an operation to wait through (7 on Impala)
   and the form of the error.
8. The driver sends `OpenSession` with the protocol version 9 and accepts the version that the server returns
   (9 on Hive, 5 on Impala). It reads a Thrift exception as an error and `TStatus` 3 as an error with the
   `sqlState`, the `errorCode` and the message, in the `Error` type of the package (D230 and D231).

A decision that Ken changes is written as a new decision, and this one is then amended.
