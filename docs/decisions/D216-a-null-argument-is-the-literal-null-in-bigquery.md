# D216. A NULL argument is the literal NULL in BigQuery

Status: Decided. Amends D195.

The `usql` session reported on 2026-10-11 that `INSERT INTO t VALUES (?, ?)` with a NULL
argument fails on the hosted service for a `NUMERIC` or a `TIMESTAMP` column, with `Value has
type STRING which cannot be inserted into column n, which has type NUMERIC`. The cause is item
4 of D195, which sends a NULL as a `STRING` with no value. Ken decided on 2026-10-11 to fix it
(W46).

The driver tried each way to type a NULL parameter on the hosted service, on 2026-10-11, with
one column of each type:

- A `STRING` NULL fits a `STRING`, `DATE`, `TIME`, `DATETIME` and `TIMESTAMP` column. It does
  not fit `INT64`, `FLOAT64`, `NUMERIC`, `BIGNUMERIC`, `BOOL` or `BYTES`.
- An `INT64` NULL fits `INT64`, `FLOAT64`, `NUMERIC` and `BIGNUMERIC`, and nothing else.
- A `FLOAT64`, `BOOL` and `BYTES` NULL fit only their own type. A `NUMERIC` NULL fits
  `NUMERIC`, `BIGNUMERIC` and `FLOAT64`.
- A parameter with no type is refused: `Missing query parameter type`.
- The literal `NULL` in the text of the statement fits a column of every type.

So no one parameter type fits every column, and the driver cannot know the column of a
statement. Decided:

1. The driver writes a NULL argument as the literal `NULL` in place of its placeholder, and it
   sends no parameter for it. The other arguments keep their order, so a positional `?` still
   matches its argument.
2. The driver finds the placeholders with the parser of the root package (D34), with the
   syntax of GoogleSQL: the quotes `'`, `"` and the backtick, the escape with a backslash, and
   the comments `--`, `#` and `/* */`. A `?` or an `@name` inside a literal, a quoted name or
   a comment stays as it is. A triple-quoted string reads as a literal that holds the doubled
   quote, which is right unless its text holds a lone quote.
3. A statement with no NULL argument goes unchanged. A statement whose placeholders the driver
   cannot match with its arguments goes unchanged too, so that the service gives the error.
   An `@name` that appears twice becomes `NULL` at both places.
4. A NULL that the statement uses where a type decides, such as `SELECT ?`, now has the type
   that the service gives to a bare `NULL`, which is `INT64` (recorded: bigquery-100), and not
   `STRING`. A caller that needs a type writes `CAST(? AS type)`.
