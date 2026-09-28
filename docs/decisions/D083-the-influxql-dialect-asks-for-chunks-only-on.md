# D83. The InfluxQL dialect asks for chunks only on InfluxDB 1

Status: Decided.

Ken decided on 2026-09-28 how the dialect `influxql` asks `/query` for a
result and reads it. This is item 8 of step 9 of [DRIVER.md](../DRIVER.md)
(D21). Step 6 measured these facts on InfluxDB 1.11.8, 1.13.1, 2.8.0, 2.9.1,
3.9.13, 3.10.6 and 3.11.5:

- With `chunked=true`, InfluxDB 1 sends one JSON document for each chunk of
  10,000 rows as the query makes them. A series that continues in the next
  chunk holds `"partial":true`. Each statement keeps its result.
- InfluxDB 2 answers `chunked=true` with a `Content-Length`, so the server
  builds the whole answer before it sends it. Chunks give no streaming there.
- InfluxDB 3 leaves out the result of every statement that has no series
  when `chunked=true` is set. `SELECT * FROM nothere; SELECT f FROM types`
  returns only the result with `statement_id` 1. Without `chunked`, it
  returns both.
- InfluxDB 2 leaves out the result of `DELETE` and of `DROP MEASUREMENT` in
  both forms. `DELETE FROM gap; SELECT ...; DROP MEASUREMENT x; SELECT ...`
  returns only the results with `statement_id` 1 and 3.
- The JSON does not say whether a number is a float or an integer. A float
  field that holds 0 arrives as `0`. `/query` names no types.

## Chunks

The key `chunked` of the DSN has two values:

| `chunked` | What the driver does |
| --- | --- |
| `prefer` | It sends `chunked=true` to InfluxDB 1, and asks InfluxDB 2 and InfluxDB 3 for one document. This is the default. |
| `disable` | It asks every release for one document. |

The release comes from `GET /ping`, or from the key `version` when `sqlmode`
is `disable` or `allow` (D78). The driver reads either form one token at a
time (D25), so a document is never held whole in the memory of the client.

A series that holds `"partial":true` continues in the next chunk, and the
driver reads it as one result set (D81).

## The gaps in statement_id

Each result names its `statement_id`. When the next result skips one or
more ids, such as from 1 to 3, the driver gives an empty result set, with no
columns and no rows, for each id that is missing. So the result sets keep
the order of the statements. A missing statement after the last result
cannot be seen, and the driver gives nothing for it.

## Numbers

The driver decodes a number of InfluxQL by its text:

- A number with no fraction and no exponent is an `int64`, or a `uint64`
  when it is above the range of `int64`.
- Any other number is a `float64`.

So a float column can give an `int64` for a whole value, and a caller that
scans it into a `float64` gets the same value. SQL does not need this rule,
because `DESCRIBE` gives the type of each column (D80).

## The column time

The value of a column named `time` is a `time.Time`, which the driver reads
from its text in RFC 3339 (measured on every release). Its location is the
offset that the text names: UTC for `Z`, and a fixed zone for the offset of a
`tz()` clause, such as `+09:00`. A number in that column stays a number,
because the driver sends no `epoch` and so the server writes none. Ken added
this rule to this decision on 2026-09-29, as the type table of step 10 holds
it.

The integration tests run a statement with no series on each release, in
each form, and a `DELETE` among other statements on InfluxDB 2.
