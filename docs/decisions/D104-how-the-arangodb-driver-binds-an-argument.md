# D104. How the ArangoDB driver binds an argument

Status: Decided.

The review of D97 found on 2026-09-29 that no decision covers item 6 of
step 9 of [DRIVER.md](../DRIVER.md) for ArangoDB, although the driver makes
the choice. Ken accepted this on 2026-09-29, and decided that the parser of D34 finds
the placeholders.

The server binds each argument from `bindVars` (measured), so the driver
never puts an argument into the text of a query (D34).

- `sql.Named("k", v)` fills `@k`. A positional argument n fills `@n`,
  because `@1` is a name in AQL (measured).
- A named argument that the query uses as `@@k` names a collection. Its key
  in `bindVars` is `@k` (measured). The parser of D34 finds each `@k` and
  `@@k`, and skips strings, names in backticks and comments, so a string or
  a comment that holds `@@k` does not count. For AQL, its `Syntax` knows the
  `//` comment, `@@k` as a placeholder of its own, and a name that starts
  with a digit. A query that the parser cannot read, such as one with a
  string that has no end, binds every argument as a value, and the server
  reports its fault.
- A value keeps its Go type in JSON. A `uint64` and an `*apd.Decimal` keep
  every digit. A time is a string in RFC 3339, in UTC. A slice or a map is an
  array or an object.
- NaN, an infinity, and a decimal that is not finite are refused, because
  JSON has no form for them. A `[]byte` is refused with
  `dbimp.ErrNotSupported`, because AQL has no binary type. The Couchbase
  driver sends a `[]byte` as base64 (D44), because Couchbase stores bytes as
  base64 text. AQL has no such convention, so the driver does not invent
  one.
