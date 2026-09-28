# D86. RoundTrip serves a database that cannot do every step

Status: Decided.

Step 14a runs `dbimptest.RoundTrip` for each type. InfluxDB cannot do three
of its steps, as step 6 measured:

- InfluxQL returns the name and the time of a series before each value
  (D81), and RoundTrip scanned one column.
- A write that leaves a field out keeps its old value, because InfluxDB
  merges the fields of a point. So a point cannot be updated to NULL. A
  timestamp is the time of the point, so a new time is a second point and
  not an update.
- InfluxDB 3 Core cannot delete one point (D85).

Ken decided on 2026-09-28 that RoundTrip skips an update to NULL with a log
line. He accepted the rest of this decision, which extends that choice, on
2026-09-29:

- `Column` names the column of the value in the row that Select returns. It
  is 0 by default.
- `SkipUpdate` returns why the update from one value to the next cannot run.
  RoundTrip logs the reason, and skips that update and the read after it. It
  covers an update to NULL, and an update of a timestamp.
- An empty `Delete` says that the database cannot delete one row. RoundTrip
  logs it, and skips the delete and the check that the row is gone.

Each field keeps the old behaviour when it is not set, so the round trips of
Couchbase, SurrealDB and Neo4j do not change. The tests of RoundTrip hold a
store in memory for each field.
