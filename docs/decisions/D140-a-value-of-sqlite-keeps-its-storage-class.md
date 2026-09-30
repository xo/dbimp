# D140. A value of SQLite keeps its storage class

Status: Decided.

Step 8a of W22 found that the declared type of an SQLite column does not
bind the values that it holds (RQLITE.md, Types). An `INTEGER` column of
rqlite held the `REAL` 1.5, and a `DOUBLE` column held the text `'abc'`. The
declared type is what rqlite names in `types`, and D135 gives each value the
Go type that fits it. Ken decided on 2026-09-30 how the two rules meet.

- The Go type of a column follows the rules of affinity of SQLite for its
  declared type: a name with `INT` is `int64`, a name with `CHAR`, `CLOB` or
  `TEXT` is `string`, `BLOB` is `[]byte`, a name with `REAL`, `FLOA` or
  `DOUB` is `float64`, and any other name, such as `NUMERIC`, `DECIMAL(10,2)`
  or `UUID`, is a number, `int64` or `float64`.
- `BOOLEAN` is `bool`. `DATE` is `dbimp.Date`, `DATETIME` is
  `dbimp.LocalDateTime`, and `TIMESTAMP` is `time.Time`, because the server
  adds a `Z` to a date and to a time that stored no zone. A `DATE` whose
  text holds a time, and a `DATETIME` whose text holds an offset, lose that
  part.
- A value whose JSON form cannot have the Go type of its column keeps the Go
  type of its JSON form: a `string`, a `[]byte` from an array, an `int64` or
  a `float64` from a number, or a `bool`. SQLite stores such a value on
  purpose, unless the table is `STRICT`, so an error makes ordinary data
  unreadable. D136 makes such a value an error for Couchbase, where the
  signature binds the value.
- `ANY`, the type of a `STRICT` column that takes every storage class, is
  not a supported type. It has no kind and no row in the type table. A value
  of an `ANY` column, and of an expression whose type is `""`, reads by its
  JSON form, as above, and its scan type is `interface {}`.

Gemini and DeepSeek reviewed each point on 2026-09-30 (RQLITE.md, Second
opinions). Both proposed a new kind for `ANY`, which Ken did not take.
