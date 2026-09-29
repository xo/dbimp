# D124. A Databend decimal argument is a JSON string

Status: Amends D120.

Ken decided this on 2026-09-29, while the Databend driver was written. D120
sent a `*apd.Decimal` argument as a JSON number with every digit. The server
reads a JSON number with a fraction as a `Float64`, so
`1234567890123456789012345678.0123456789` inserted into a `DECIMAL(38, 10)`
came back as `1234567890123456752549132460.6797053952`. Sent as a JSON
string, the same value came back with every digit, because the column and
`::DECIMAL` cast the string. Both releases answered the same (measured with
`curl` on 2026-09-29, [DATABEND.md](../DATABEND.md)).

So a `*apd.Decimal` goes as a JSON string with every digit. An `int64` and a
`uint64` stay JSON numbers, which keep their digits up to
`18446744073709551615` (measured). The rest of D120 holds.
