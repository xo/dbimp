# D177. The rest of the ClickHouse driver

Status: Decided.

These are the other items of step 9 of [DRIVER.md](../DRIVER.md) for
ClickHouse (W32, D174 and D176). Each fact is in
[CLICKHOUSE.md](../CLICKHOUSE.md). Ken decided them on 2026-10-07.

1. The Go types are the type table of CLICKHOUSE.md, with these two new
   kinds in [TYPES.md](../TYPES.md):
   - A `big integer` is an integer of 128 or 256 bits, and its Go type is
     `*big.Int`. `Int128`, `Int256`, `UInt128` and `UInt256` have this kind.
   - An `ip address` is an IPv4 or an IPv6 address, and its Go type is
     `netip.Addr`. `IPv4` and `IPv6` have this kind. An `IPv4` that the
     server maps into an `IPv6` stays an `IPv6` address.
   Every other type has the Go type of its kind. A geometry is a nested
   `[]any` of `float64`. A `Variant`, a `Dynamic` and a `JSON` are the decoded
   JSON value. `Time` and `Time64` are `time.Duration`. `DateTime` and
   `DateTime64` are `time.Time`, read with `date_time_output_format=iso`, so
   the offset is in the text. `FixedString` is a string.
2. The DSN is `clickhouse://user:password@host:8123/database`. The path is the
   database, and it is optional. The key is `tls` (false by default, and the
   default port is 8443 with `tls=true`). Any other key is refused. The
   driver registers the name `clickhouse` (D28), as `ClickHouse/clickhouse-go`
   does, so a program can link only one of the two. The secret is the password
   of the URL (D94).
3. `BeginTx` fails with `dbimp.ErrNotSupported` (D20), because the server
   answers every `BEGIN` with HTTP 501.
4. The driver follows no redirect. It sends the user and the password in a
   Basic header on every request, and never the headers `X-ClickHouse-User`
   and `X-ClickHouse-Key`, which the server refuses with a Basic header.
5. ClickHouse is one product with no flavor and no DSN key for it. The driver
   runs `SELECT version()` once for each connection, and reads the header
   `X-ClickHouse-Exception-Tag` of a response, to choose the framing of an
   error after rows (D176).
