# D234. The types and the DSN of the hive driver

Status: Decided on 2026-11-09.

Ken accepted on 2026-11-09 the type mapping of step 8a and the DSN keys for the package `hive` (W56, D232 and
D233). These are items 3 and 4 of step 9 of [DRIVER.md](../DRIVER.md). Each fact is in [HIVE.md](../HIVE.md).

1. The Go types are the mapping of HIVE.md:
   - BOOLEAN is `bool`. TINYINT, SMALLINT, INT and BIGINT are `int64`. FLOAT and DOUBLE are `float64`. A FLOAT
     is a `float64` as the server sends it, so 1.1 stays 1.1.
   - DECIMAL is `*apd.Decimal` with the scale of its column (D228).
   - STRING, VARCHAR and CHAR are `string`. A CHAR keeps its padding.
   - BINARY is `[]byte`.
   - DATE is `dbimp.Date`. TIMESTAMP is `dbimp.LocalDateTime`. TIMESTAMPLOCALTZ is `time.Time`.
   - INTERVAL_YEAR_MONTH and INTERVAL_DAY_TIME are `dbimp.Interval`.
   - ARRAY, MAP and STRUCT are decoded from the JSON text that Hive sends. An ARRAY is `[]any`. A MAP and a
     STRUCT are `map[string]any`, so a STRUCT loses the order of its fields. A value inside them has a JSON type,
     so a DATE inside an ARRAY is a string.
   - UNIONTYPE is the text of the server, and a NULL type is `nil`.
2. The DSN is `hive://user:password@host:port/database?httpPath=cliservice`. The scheme is `hive` for both
   flavors. The path is the database, and it is optional. The default port is 10001, which is the HTTP port of
   Hive. A caller of Impala names the port, 28000 in `dbrun`. The key `httpPath` names the path of the
   endpoint, and its default is `cliservice`. The key `tls` makes the driver use HTTPS, and its default is
   false. Any other key is refused, and so is a key that repeats. The secret is the password of the URL (D94).
   The driver is HTTP only, so it has no key `transport`, `auth` or `service`, which `gohive` has.

The key `tls` is not in the list that Ken read. The driver needs it to reach a server that uses HTTPS, and every
other driver here has it. Ken can remove it.
