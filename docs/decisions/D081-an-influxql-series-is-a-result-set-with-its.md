# D81. An InfluxQL series is a result set, with its name and its tags

Status: Decided.

Ken decided on 2026-09-28 how the dialect `influxql` maps the answer of
`/query` to result sets. Step 6 measured the answer on InfluxDB 1.13.1,
2.9.1 and 3.11.5.

The answer has one result for each statement, in order. A result holds zero
or more series. A series has a `name`, which is the measurement, its own
`columns` and `values`, and a `tags` object when the statement groups by a
tag. `GROUP BY host` gives one series for each value of `host`, and the tag
is not in `columns`. `SELECT * FROM a, b` and `SHOW TAG KEYS` give one series
for each measurement, and the columns of two series can differ.

Each series is one result set, and `Rows.NextResultSet` moves to the next
one, across the statements in order. The columns of a set are:

1. `name`, which holds the name of the series.
2. Each key of `tags`, in the order of the object.
3. The columns of the series, in order.

This is the form of the `csv` output of the `influx` command of InfluxDB 1.
The column `name` is in every set, so `SHOW DATABASES` has two columns that
are both called `name`. `database/sql` allows it.

A statement whose result holds no series, such as `CREATE DATABASE` or a
`SELECT` from a measurement that does not exist, is one result set with no
columns and no rows. A result that holds `error` is the error of that
statement, and it reaches the caller when the reader comes to it (D8).

D83 says how the driver asks for the answer, how it reads a series that
continues across chunks, and how it fills a gap in `statement_id`.
