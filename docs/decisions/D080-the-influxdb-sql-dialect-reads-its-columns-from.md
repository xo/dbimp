# D80. The InfluxDB SQL dialect reads its columns from DESCRIBE

Status: Amends D77.

Ken decided on 2026-09-28 how the dialect `influxdb` learns the columns of a
result. Step 6 measured the three facts that D77 left open, on InfluxDB
3.9.13 and 3.11.5:

- `/api/v3/query_sql` leaves out the key of a NULL. `SELECT NULL AS a, 1 AS b`
  returns `[{"b":1}]`. So the first object does not name every column, and a
  key can arrive first in a later row.
- A result with no rows is `[]`, so it names no column.
- The keys keep the order of the statement. `SELECT *` gives the columns of
  the table in order by name.

`DESCRIBE` followed by a `SELECT` returns one object for each column, in the
order of the statement, with its name and its Arrow type, such as
`Timestamp(ns)`, `UInt64` or `Dictionary(Int32, Utf8)`. It names the columns
of a result with no rows, and it takes the same `params` as the statement.
It refuses every statement that is not a `SELECT`, such as `SHOW TABLES`,
with HTTP 400 on 3.9.13 and HTTP 405 on 3.11.5.

## The key describe

The key `describe` of the DSN has two values:

| `describe` | What the driver does |
| --- | --- |
| `always` | It sends `DESCRIBE` and the statement, with the same parameters, and then the statement. The columns and their types come from `DESCRIBE`. A key that a row does not hold is NULL. A key that `DESCRIBE` did not name is `dbimp.ErrExtraColumn`. This is the default. |
| `disable` | It sends only the statement, and reads the result by D77. The columns are the keys of the first object, and a later key is `dbimp.ErrExtraColumn`. |

With `always`, if `DESCRIBE` fails for any reason, the driver sends the
statement alone and reads it as `disable` does. It never reads the text of
the error, because the status differs by release. A statement that is wrong
then fails with its own error from the server.

The types from `DESCRIBE` choose the Go type of each column, so a timestamp
is a `time.Time` and an unsigned integer is a `uint64`. The type table of
step 10 holds each one.

## NaN and infinity

A float that is not finite comes only from a SQL expression, such as
`v / 0.0`, because line protocol refuses `NaN` (measured on 3.11.5 on
2026-09-29). The JSON writes NaN, +Inf and -Inf alike, as an explicit
`null`, and leaves out the key of a real NULL. Only `format=csv` writes them
as text: `NaN`, `inf` and `-inf`.

Ken decided on 2026-09-29:

- An explicit `null` in a float column is `math.NaN()`, and never a NULL. So
  +Inf and -Inf read as NaN in the JSON of SQL. A key that the row does not
  hold stays a NULL.
- Where a float column holds the text of a value that is not finite, the
  text reads as its value: `NaN` as `math.NaN()`, `Infinity` and `inf` as
  `math.Inf(1)`, and `-Infinity` and `-inf` as `math.Inf(-1)`, in any case.

The driver learns which columns are floats from DESCRIBE, so with
`describe=disable` an explicit `null` is a NULL. InfluxQL gives no NaN,
because it answers 0 for a division by zero (measured).

The key applies only to the dialect `influxdb`. InfluxQL names its columns,
so the driver ignores the key when `sqlmode` chooses InfluxQL (D78).

Ken did not take a third value, `csv`, which reads the header of
`format=csv`. The header names no column for a result with no rows, every
value is text, and a NULL and an empty string are the same field.
