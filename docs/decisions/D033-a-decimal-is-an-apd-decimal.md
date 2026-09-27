# D33. A decimal is an apd.Decimal

Status: Decided.

Ken decided this on 2026-09-27, and it answers Q6. A decimal is held as an
`apd.Decimal` from `github.com/cockroachdb/apd/v3`. It is the only dependency
that is not the standard library or a binary encoding (D13).

The candidates were compared on 2026-09-27. `apd/v3` had its latest release,
v3.2.3, on 2026-03-23, by the Go module proxy. It uses only the standard
library, and it holds arbitrary precision with a context that sets the
precision and the rounding. It has NaN and infinity, which the `Decimal128`
of MongoDB needs. CockroachDB uses it. `shopspring/decimal` had no release
after 2024-04-12, and `govalues/decimal` holds at most 19 digits, which is
too few for DynamoDB and ClickHouse.

A driver hands a decimal to `database/sql` as an `*apd.Decimal`, and a new
one for each value. An `apd.Decimal` holds a pointer to its digits once they
are large, so a copy of the value shares them with the original.
[DESIGN.md](../DESIGN.md) says how `dbimp.Assign` stores one.

`go.mod` requires `apd/v3` v3.2.3, from the first code that uses it, in the
root package.
