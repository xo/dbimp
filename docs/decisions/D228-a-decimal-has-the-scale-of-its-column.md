# D228. A decimal has the scale of its column

Status: Decided on 2026-11-04.

The `tblfmt` session reported that `usql` printed `toDecimal64(1.5, 2)` on ClickHouse 26.9 as `1.5`.
`tblfmt` v0.21.2 writes an `*apd.Decimal` with every digit of its coefficient, as `psql` writes a
numeric, so the scale that the value carries is the scale that the user sees. ClickHouse sends a
decimal without its trailing zeros, and the value had the exponent -1 and not -2. Ken told this session
on 2026-11-04 to survey every driver and to fix them all in the same way (D33 says that a decimal is an
`*apd.Decimal`, and this decision adds the scale).

1. A decimal that the driver returns has the scale that the type of its column names. A `Decimal(10, 2)`
   column gives 1.50, never 1.5. The exponent of the value is minus the scale of the column.
2. The root package has `PadScale(d *apd.Decimal, scale int64) *apd.Decimal`. It gives the value the
   exponent that is minus the scale, and it adds zeros after the point to do so. It never rounds and
   never drops a digit. It returns its argument as it is for a NaN, an infinity, a scale of 0 or less,
   and a value that already has that many digits after the point or more. It never changes its argument.
3. A driver calls `PadScale` where it turns the text of a value into a decimal, with the scale that the
   server names for the column. Where the server names no scale, the driver does not call it.
4. The survey ran each recorded exchange of each driver through `PadScale` on 2026-11-04. Two servers
   sent a value with fewer digits than its scale. ClickHouse sends `1.5` for the value 1.50 of a
   `Decimal(9, 2)`. Exasol sends `1.5` for DECIMAL(10,2), and `0` for DECIMAL(10,2) and DECIMAL(18,5).
   The recordings of the other drivers already hold the full scale, so the call changes no value there.
   The drivers `avatica`, `databend`, `databricks`, `drill`, `maxcompute`, `snowflake`, `athena` and
   `trino` call it all the same, because it costs nothing and because a later release of a server
   can drop the zeros.
5. These drivers do not call it, and the reason is written beside each:
   - `bigquery` and `spanner` name no scale for a value, because NUMERIC has a fixed scale of 9 on the
     server and the server sends the text with no trailing zeros (recorded). Ken decided on 2026-11-04
     not to pad them to 9 digits, because the scale is not one that a person wrote for the column.
   - `pinot` and `influxdb` name no scale in the metadata of a column.
   - `druid`, `dynamodb`, `surrealdb`, `cosmos`, `couchbase`, `elasticsearch`, `opensearch`, `solr`,
     `tablestore`, `rqlite`, `libsql` and `neo4j` have no decimal type that has a scale.
