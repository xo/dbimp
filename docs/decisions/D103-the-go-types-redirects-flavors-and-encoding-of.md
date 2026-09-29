# D103. The Go types, redirects, flavors and encoding of ArangoDB

Status: Decided.

The review of D97 found on 2026-09-29 that no decision covers items 4, 10,
11 and 12 of step 9 of [DRIVER.md](../DRIVER.md) for ArangoDB, although the
driver makes each choice. Ken accepted the choices
that the code makes on 2026-09-29. [ARANGODB.md](../ARANGODB.md) holds the facts.

- Item 4, the Go types. The answer is JSON, and the driver reads it with
  `encoding/json/v2`, one token at a time (D25). A number with no fraction
  that fits is an `int64`, and any other number is a `float64`. AQL holds an
  integer above the range of `int64` as a double, so its digits are gone
  before the driver reads it, and the driver cannot give an `*apd.Decimal`
  (measured). A string is a `string`, a boolean a `bool`, `null` is nil, an
  array is `[]any`, and an object is `map[string]any`. AQL has no date, no
  decimal, no UUID and no binary type, so none arrives. No type arrives for a
  column, so each column scans into `any`, with no database type (D89).
- Item 10, redirects. The driver follows no redirect, and sends the
  credentials only to the host of the URL (`dbimp.NewClient`). No recorded
  answer is a redirect.
- Item 11, flavors. No other product speaks this API, so the driver has no
  flavors.
- Item 12, a binary encoding. ArangoDB also speaks VelocyPack, which D13
  names. The driver speaks JSON only, and takes no package for VelocyPack.
- An error is one `*Error` with `errorNum`, because the server sends one
  error for each response (measured). The Couchbase driver holds a list,
  because its server sends one.
