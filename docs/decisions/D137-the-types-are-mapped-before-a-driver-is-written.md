# D137. The types are mapped before a driver is written

Status: Decided.

Ken decided this on 2026-09-30, after D135 and D136. The drivers gave the
same kind of type different Go types, such as a date as a `time.Time` in one
and a `neo4j.Date` in another, because each driver mapped its types alone,
while its code was written.

- [TYPES.md](../TYPES.md) holds one list of kinds, each with its Go type,
  and a table that maps every type of every driver onto them. A kind is a
  family of types that means the same thing in any database. A new kind
  needs a decision.
- The type table of each product document has the column Kind, which
  `dbimptest.Kinds` sets. `TestEveryTypeHasAKind` fails for a kind that
  [TYPES.md](../TYPES.md) does not name, and `TestTheTypeMatrixIsCurrent`
  writes the table of every driver from the product documents.
- Step 8a of [DRIVER.md](../DRIVER.md) maps the types of a new driver after
  its measurements and before its decisions. The agent builds the mapping,
  asks at least two models to review it, writes the type table, and stops.
  Ken reviews the mapping before any code is written. Step 10 then generates
  the table from the code, and the two must agree.
- A shared type goes in the root package when at least two drivers need the
  same kind, and Go has no type for it. D138 proposes the first ones.
