# D19. A JSON number is never decoded through float64

Status: Decided.

A float64 holds an integer exactly only up to 2^53. ClickHouse, Druid and
Elasticsearch send 64-bit integers, and DynamoDB sends numbers with up to 38
digits. Each driver reads a number as the raw text of its token from
`jsontext` (D25), and converts the text by the type of its column. A decimal
becomes an `*apd.Decimal` (D33). Ken accepted this on 2026-09-27.
