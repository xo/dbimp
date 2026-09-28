# D85. The InfluxDB driver takes INSERT of the influx shell

Status: Amends D78.

Ken decided on 2026-09-28 that the InfluxDB driver writes data with the
statement that the `influx` shell takes, from the InfluxQL page on managing
data in the documents of InfluxDB 2:

```text
INSERT [INTO <database>[.<retention-policy>]] <line-protocol>
```

No server parses INSERT. `/query` lists the statements that it expects, and
INSERT is not one of them (measured). The shell turns the statement into a
write of line protocol, and the driver does the same.

## INSERT

- A statement that starts with the word INSERT, in any case, is one, in both
  dialects. SQL on InfluxDB 3 refuses every INSERT with
  `DML not supported: Insert Into` (measured), so this takes nothing away.
- The driver sends the line protocol to `POST /write`, which every release
  has (measured), with `db` and `rp` from INTO, or from the DSN when INTO is
  absent. With no database, the statement is an error.
- The driver parses each line, with its escapes, so that a bad line is an
  error before anything is sent. Several lines are several points, and a
  line that starts with `#` is a comment.
- The driver binds each argument. A placeholder `$name` or `$1` stands where
  a value stands: a tag value, a field value, or the timestamp. The driver
  writes the argument as a literal of line protocol for its place. A tag value
  is text with its commas, equal signs and spaces escaped. A field is `1i`
  for an integer, `1u` for an unsigned integer, a float, `true` or `false`,
  or a string in double quotes. A timestamp is a `time.Time` or an integer,
  in nanoseconds. A NULL leaves its tag, its field or its timestamp out of
  the line. A line with no field left is an error.
- Without arguments, the lines go as they are written, so `$x` is text.
- A placeholder inside a string field is text, as in SQL.

This amends D78, which says that the driver does not check a statement. The
driver now reads the first word of each statement, to find INSERT. It still
checks no other statement against the language or the release.

## DELETE

Ken had no preference between the forms of DELETE on InfluxDB 3 Core, so
D78 holds. The driver sends DELETE to the server on every release:

- InfluxDB 1 and 2 run `DELETE FROM <measurement> WHERE ...` themselves
  (measured, 1.13.1-037 and 2.9.1-037).
- InfluxDB 3 Core refuses it in InfluxQL and in SQL, and on
  `/api/v3/query_influxql` (measured on 3.11.5 on 2026-09-28). Its HTTP API
  deletes only a whole database or a whole table (the Core API reference).
  Row deletion is an Enterprise feature from 3.11, which applies a delete up
  to 24 hours later (the Enterprise guides).

The driver does not emulate DELETE on InfluxDB 3 Core. A SELECT finds the
rows, but a point has no id, and Core has no request that deletes one point.
The only emulation is to read the rows that stay, delete the table, and
write them back. That is not atomic, a failure in the middle loses data, a
write during it is lost, and it drops the caches of the table.
