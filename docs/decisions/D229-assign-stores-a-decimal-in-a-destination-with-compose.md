# D229. Assign stores a decimal in a destination with Compose

Status: Decided on 2026-11-04.

The `cassandra` session reported that a driver which implements `driver.RowsColumnScanner` cannot scan a
decimal into a type that has the method `Compose`. `database/sql` does this for a driver that is not a
`RowsColumnScanner`: `convertAssign` takes a source with `Decompose` and a destination with `Compose`, and
it calls `dest.Compose(src.Decompose(nil))`. A `RowsColumnScanner` replaces `convertAssign`, and
`dbimp.Assign` handed the decimal to it as text, so the scan failed with "unsupported Scan, storing
driver.Value type string". Ken told this session on 2026-11-04 to find out what needed to change.

1. `dbimp.Assign` stores an `*apd.Decimal` in a destination that has the method `Compose(form byte,
   negative bool, coefficient []byte, exponent int32) error`, and it does so before it turns the decimal
   into text. It reads the parts through `Decompose`, as `database/sql` does. It keeps the scale of the
   value, so 1.50 stays 1.50.
2. An error of `Compose` is wrapped with `%w` and names the destination.
3. A destination that is a `*apd.Decimal`, a `*any` or a `*string` has its own branch or its own path, and
   they do not change.
4. The argument side is not part of this decision. A driver here binds an `*apd.Decimal` in its
   `CheckNamedValue`, and `database/sql` never turns a value with only `Decompose` into an argument.
