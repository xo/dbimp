# D113. The Go types of SurrealDB that D53 left out

Status: Amends D53.

Ken decided on 2026-09-29 that D53 follows the code, which loses no digits
and treats three types alike:

- A CBOR integer outside the range of `int64` reads as an `*apd.Decimal`,
  which keeps each digit. D53 said that an integer is an `int64`, which
  holds for every integer in its range.
- A `uuid.UUID` and a `time.Duration` scan into a string as their text of
  SurrealQL, as a `RecordID` does (D70).
