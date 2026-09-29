# D120. Databend binds parameters on the server

Status: Amended by D124.

Ken decided this on 2026-09-29, at step 9 of the Databend driver.
This is item 6 of step 9 of [DRIVER.md](../DRIVER.md) for Databend. The
server binds `params` from the body on both releases
([DATABEND.md](../DATABEND.md)), so the driver writes no argument into the
text of a statement, and needs no escaper (D34).

- Positional arguments go as a JSON array, in their order, and fill each
  `?` in order.
- Named arguments, from `sql.Named`, go as a JSON object, and fill each
  `:name`.
- A statement with both kinds is an error that wraps `dbimp.ErrArguments`,
  because `params` is an array or an object, never both.
- A value is encoded as JSON: a number, a string, a boolean, `null`, an
  array or an object. A `*apd.Decimal` and a `uint64` go as a JSON number
  with every digit, and a `time.Time` as a string that the server casts.
- JSON has no bytes, and `params` has no form for a binary value. So a
  `[]byte` is refused with `dbimp.ErrNotSupported`, as D104 does for
  ArangoDB, and the driver invents no convention.
