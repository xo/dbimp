# D183. The Snowflake driver

Status: Decided.

These are the items of step 9 of [DRIVER.md](../DRIVER.md) for Snowflake (W34
and D182). Each fact is in [SNOWFLAKE.md](../SNOWFLAKE.md). Ken decided them
on 2026-10-09, after step 8a showed that a driver is possible over HTTP and
JSON with no Arrow.

1. The package and the name that it registers are `snowflake` (D26 and D28).
   The driver speaks the SQL REST API, `/api/v2/statements`, and not the
   private protocol of `gosnowflake`. It reads JSON only and needs no binary
   encoding (D13).
2. The DSN is
   `snowflake://user:key@<org>-<account>.snowflakecomputing.com/database/schema`.
   The password is the private key of the user, as base64url text of the PKCS8
   DER bytes, so the secret is the password of the URL (D94). The path is the
   database and the schema, and each is optional. The keys are `role`,
   `warehouse`, `timeout` (the timeout of a statement, in seconds) and
   `timezone`. The connection is always TLS, with the port 443 by default. Any
   other key is refused (D27).
3. The login is a key-pair JWT only. The driver signs it with RS256, with the
   claims of SNOWFLAKE.md, and signs a new one before the old one expires. A
   login with another kind of token is not part of this driver and needs its own
   measurement and decision.
4. The Go types are the type table of SNOWFLAKE.md: a `fixed` of scale 0 and
   precision 18 or less is an `int64`, and any other `fixed` is an
   `*apd.Decimal`. A `real` is a `float64`, and the texts `NaN`, `inf` and
   `-inf` become the matching values. A `text` is a `string`, a `binary` is a
   `[]byte`, and a `boolean` is a `bool`. A `date` is a `dbimp.Date`, a `time`
   is a `dbimp.LocalTime`, a `timestamp_ntz` is a `dbimp.LocalDateTime`, and a
   `timestamp_ltz` and a `timestamp_tz` are a `time.Time`. A `variant` is the
   decoded JSON value, an `object` is a `map[string]any`, an `array` is a
   `[]any`, a `geography` and a `geometry` are the `map[string]any` of their
   GeoJSON, and a `vector` is a `dbimp.Vector[float64]`, because the metadata
   names no element type. The driver sends no output-format parameter, so each
   value keeps the form that SNOWFLAKE.md names.
5. NULL is nil (D18).
6. A parameter is a typed binding of the server. The driver writes each
   argument as the member of `bindings` that its Go type names: an `int64` is
   `FIXED`, a `float64` is `REAL`, a `string` is `TEXT`, a `bool` is `BOOLEAN`,
   a `[]byte` is `BINARY` as hex, a `dbimp.Date` is `DATE` in milliseconds, a
   `dbimp.LocalTime` is `TIME` in nanoseconds, a `dbimp.LocalDateTime` is
   `TIMESTAMP_NTZ`, and a `time.Time` is `TIMESTAMP_TZ`, with the nanoseconds
   and the offset. A `*apd.Decimal` is `FIXED`, with the decimal text. A NULL is
   a `TEXT` with the value `null`. A `variant` is not a type of a binding, so a
   map or a list fails with `dbimp.ErrArguments`.
7. `BeginTx` fails with `dbimp.ErrNotSupported` (D20), because the server
   refuses `BEGIN` alone and each request is its own session.
8. The driver reads the first partition from the answer, and each later one
   with `GET`, in order and one at a time. It reads each token of a partition
   as it arrives (D25), and an error in a later partition wraps
   `dbimp.ErrIncomplete` (D21 and D107).
9. The driver starts each statement with `?async=true` and polls the status
   until it ends, so that the client always holds the handle. When the context
   ends, and when the caller closes the rows before the end, the driver sends
   `POST /api/v2/statements/<handle>/cancel` (D8).
10. The driver follows no redirect, and sends the JWT to the host of the DSN
    only.
11. Snowflake is one product with no flavor.
12. The driver needs no binary encoding.
13. The tests of the driver replay the recorded exchanges in CI. The
    integration tests read `SNOWFLAKE_DSN` and skip when it is empty (hard rule
    9), and a person runs them on an account. The workflow has no job for
    Snowflake and no secret, because it is a hosted service that `dbrun` does
    not start.
14. The rows of a Snowflake query fetch the next partitions with the context of
    the statement, through a closure, so hard rule 4 of AGENTS.md names them, as
    it names the other rows that fetch pages.
15. Ken reviewed the choices that the first version made where items 1 to 13
    were silent, on 2026-10-10:
    - A `fixed` type has two entries in `features.json` and two rows in the type
      table: one for scale 0 and precision 18 or less, which is an `int64` of the
      kind integer, and one for the rest, which is an `*apd.Decimal` of the kind
      decimal.
    - A `geography` and a `geometry` both have the database type `OBJECT`, as
      the metadata says, and a caller reads the real type from the column of the
      table.
    - A request of several statements is refused with `dbimp.ErrNotSupported`
      for now, so the first version has no `MULTI_STATEMENT_COUNT` and no
      transaction.
    - The key `timeout` takes a duration with a unit, such as `60s`, like the
      other drivers, and a bare number is refused. The driver rounds it up to
      whole seconds for the server. This amends item 2.
    - A `timestamp_ltz` value has the location of the key `timezone` or of
      `WithTimeZone`, and `time.Local` when none is named, so that the value
      follows the system or the variable `TZ`. The documentation advises
      `timezone=UTC` for a server. The tests name the zone, so that they do not
      depend on the machine.
    - The poll of a running statement waits 25 ms, then 50 ms, 100 ms, 200 ms,
      400 ms and 500 ms, and stays at 500 ms.
    - The driver sends no cancel on an early close after the last row of the
      last partition, because the statement has ended. It sends the cancel in
      every other early close and when the context ends.
    - The account of the claims is the host without its suffix, cut at the first
      dot and written in upper case, as `gosnowflake` does and as the key-pair
      documentation of Snowflake says. Only the form `<org>-<account>` is
      measured.
    - `WithParameter` refuses the nine parameters that change the text of a
      value, and a column of a type that the driver has no Go type for fails
      the row with `dbimp.ErrNotSupported` (D135).
16. The driver reads the text `1` and `0` as a `bool`, besides `true` and
    `false`, in a column of the type `boolean`. The server writes a boolean that
    way in the last `SELECT` of a piped statement. This follows the choice of
    D178 item 17 to read the value that arrives, and it is a fix of the first
    version (reported by `dbmeta` on 2026-10-10).
