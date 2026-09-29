# D77. An InfluxDB result is an array of objects, read by rule 2 of D18

Status: Amended by D80.

Ken decided on 2026-09-28 that the InfluxDB driver reads a result in JSON
as it reads any other JSON result that is an array of objects. The columns
are the keys of the first object, in the order in which they arrive. Each
row is decoded with `encoding/json/v2`, one token at a time (D25). This is
rule 2 of D18, which `dbimp.ObjectRows` reads, as the Couchbase driver does
for `SELECT *` and the SurrealDB driver does for a record (D52).

[INFLUXDB.md](../INFLUXDB.md) names three facts that step 6 measures
before the driver is written. None is measured yet:

- The JSON writer of InfluxDB 3 leaves out the key of a NULL (from its
  source). So a NULL in the first row removes that column, and rule 2 of D18
  refuses the same key in a later row as `dbimp.ErrExtraColumn`.
- A result with no rows is `[]`, so it has no columns. `tblfmt` writes such
  a result set as `psql` does from `v0.19.1` (tblfmt D31), so `usql` shows it
  (Q12).
- Whether the writer keeps the order of the columns of the statement.

If step 6 shows the first fact, the driver fails a query whose first row
holds a NULL. Ken decides then whether that holds.

Step 6 showed the first fact, and D80 settles it. The driver sends
`DESCRIBE` first by default, and this decision holds with `describe=disable`.

Note of 2026-09-29: step 6 measured all three facts, and D80 settles them.
