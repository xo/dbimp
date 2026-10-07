# D175. The Trino and Presto driver

Status: Decided.

These are the items of step 9 of [DRIVER.md](../DRIVER.md) for Trino and
Presto (W31 and D173). Each fact is in [TRINO.md](../TRINO.md). Ken decided
them on 2026-10-07.

1. The package and the name that it registers are `trino` (D26, D28 and
   D173). Presto is a flavor.
2. The DSN is `trino://user@host:8080/catalog/schema`. The path is the
   catalog and the schema, and each is optional. The keys are `tls` (false by
   default, and the default port is 8443 with `tls=true`), `source`,
   `timezone`, `timeout`, `session.<name>` for a property of the session, and
   `flavor`. Any other key is refused. The secret is the password of the URL
   (D94). The default port is 8080.
3. The Go types are the type table of TRINO.md:
   - A `TIME` or a `TIMESTAMP` can have up to 12 digits of fraction. The
     driver drops the digits beyond the ninth when they are all zero, and
     fails for a value with a non-zero digit there.
   - A `NUMBER` of Trino 483 is an `*apd.Decimal`, which holds NaN and the
     infinities.
   - `IPADDRESS` is the kind `other`, with a string.
   - Presto sends an `ARRAY`, a `MAP` and a `ROW` as strings of JSON text,
     and the driver decodes them with the type of the column, so that both
     flavors give the same Go values.
4. NULL and a JSON null are one value, nil (D18).
5. The driver sends a prepared statement in the prepared-statement header,
   and then `EXECUTE name USING <literals>`, so that the server parses each
   `?`, on both flavors. The literals are escaped as the product writes them.
6. `BeginTx` starts a transaction with `START TRANSACTION`, with the header
   `X-Trino-Transaction-Id: NONE`, keeps the id of the answer on the
   connection, and sends it with each statement. `Commit` and `Rollback` send
   `COMMIT` and `ROLLBACK`. The documents name what each catalog does: the
   `memory` catalog of Trino refuses a write in a transaction, and on Presto a
   `ROLLBACK` does not undo a write there.
7. The driver polls `nextUri` until a page has none, and reads each page one
   token at a time. An error arrives in the body with HTTP 200, and after some
   rows it wraps `dbimp.ErrIncomplete` (D21 and D107). When the caller closes
   the rows before the end, the driver sends `DELETE` to the `nextUri`.
8. The server never stops an abandoned query, so when the context ends, the
   driver sends `DELETE` to the `nextUri`, with the context without its end
   and a limit of its own (D36).
9. The driver follows no redirect, and sends the user header and the
   credentials to the host of the DSN only. It uses the path and the query of
   a `nextUri` with the host of the DSN, and never the host that the server
   names.
10. The driver tells the flavors apart from `GET /v1/info`, where the version
    of Presto has a dash and a commit, and from the slug of the `nextUri`.
    The key `flavor` of the DSN skips the request, as the key `version` does
    for InfluxDB (D78).
11. The driver sends the client capability `PARAMETRIC_DATETIME` to Trino
    always, because without it the server cuts each time and timestamp to
    three digits before the driver sees them. It also sends `NUMBER` and
    `VARIANT` to a release that has them, so that Trino 483 sends those
    types as they are. Presto has no capability, and the driver sends none to
    it. Ken decided this on 2026-10-07.
12. The driver uses JSON only. It is not read only: it sends what the SQL
    takes, and UPDATE and DELETE work on a catalog that supports them. The
    tests cover INSERT, SELECT and CREATE TABLE AS on the `memory` catalog,
    which refuses the rest on all three releases.

Ken added these on 2026-10-07, after step 17a:

13. A statement that is not in a transaction follows item 7: an early `Close`
    sends `DELETE` to the `nextUri`. In a transaction, the `DELETE` aborts the
    transaction on Trino, and the common `QueryRow` closes its rows early, so
    `Close` first reads up to four pages of at most 64 KiB each, and sends the
    `DELETE` only when the result is larger than that.
14. A zone name that Go cannot load, in a `TIMESTAMP WITH TIME ZONE`, is an
    error for that row, and names the zone.
15. `WithParameter` sets a session property, because Trino has no body of a
    request to set a key in. `WithDatabase` sets the catalog, and `WithSchema`
    sets the schema. `WithReadonly(true)` fails with `dbimp.ErrNotSupported`.
    The default `source` is `dbimp`, and the default time zone is that of the
    server. A zero `dbimp.Interval` is written as `DAY TO SECOND`.

