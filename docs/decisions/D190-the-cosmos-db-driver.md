# D190. The Cosmos DB driver

Status: Decided.

These are the answers of Ken to the step 9 questions of
[COSMOS.md](../COSMOS.md) (W36). He decided them on 2026-10-10.

1. The driver sends a query as it is, and it does not plan or merge a query
   across partitions. The real gateway answers an aggregate, `TOP`, `ORDER BY`,
   `OFFSET LIMIT`, `DISTINCT` and `GROUP BY` across partitions with HTTP 400 and
   substatus 1004. The driver returns that error, and COSMOS.md tells the
   caller to name the partition key. A plain `SELECT` works.
2. The driver reads only. `Exec` returns the error of D20 for a statement that
   writes. W40 records the need for a small parser for `INSERT`, `UPDATE` and
   `DELETE`, which this driver and possibly others will need.
3. The columns of a row are the keys of the first row. A later row that has no
   such key gives nil for it, and a later row that has a key that the first row
   lacks makes the driver return an error that names the key. This follows D18
   and hard rule 3, so that no value is lost with no sign.
4. The DSN is `cosmos://x:<key>@host/<db>/<container>`. The master key is the
   password (D94). TLS verification is on, and a key of the query lets a caller
   accept the certificate of the emulator.
5. The driver strips one trailing semicolon from a statement, because both the
   emulator and the service answer HTTP 400 `SC1010` for it.
6. The integration tests of CI run on the emulator `cosmos-EN20260907`. The
   hosted account is for the checks that the emulator cannot show, and it runs
   only where a person supplies it.
7. A JSON number is an `int64` when its text is an integer that fits, a
   `float64` when it has a fraction or an exponent, and a `*apd.Decimal` for an
   integer that is too large for `int64` and has no exponent. The driver reads
   the exponent that the hosted account writes with three digits, such as
   `e+019`, with its own parser. The service computes a value in a double with 53
   bits, so a computed value can lose digits before the driver sees it.
8. A nested array is a `[]any`, and a nested object is a `map[string]any`. A map
   loses the order of its keys, so only the columns of the top level keep the
   order of the statement.
9. A date, a UUID, a binary value and a decimal are plain strings, because the
   server cannot tell them from other text. COSMOS.md and the README show three
   cases: a UUID scans into a `uuid.UUID` through its own `Scan` method, a date
   is scanned into a `string` and parsed by the caller, and a binary value
   scans into a `[]byte` as its base64 text and is decoded by the caller.
10. A value that is `undefined` is nil, as a missing attribute is (D18). A
    GeoJSON value is an ordinary object, so it is a `map[string]any`. It was not
    measured.
11. The driver sends the version `2018-12-31` in `X-Ms-Version`. It is the version
    measured from end to end. A container with a hierarchical partition key was
    refused at that version, so the first release does not support such a
    container.
12. The other proposals of step 9 stand as COSMOS.md writes them:
    - The package is `cosmos`, and it registers the name `cosmos` with no alias.
    - Parameters are named, such as `@p`.
    - `BeginTx` returns the error of D20, because a batch is atomic only inside
      one request and needs a write language (W40).
    - When the context ends, the driver abandons the request, because Cosmos DB
      has no call to cancel one.
    - The master key signs each request, and it goes only to the configured host.
    - TLS verification is on, and a key of the query accepts the certificate of the
      emulator.
    - The driver serves no flavor, sends no request for a version and uses no
      binary encoding.
13. The tests for the refusal of the gateway (the 400 and the 429) read a second
    variable, `COSMOS_HOSTED_DSN`, and skip when it is empty (rule 9). The tests
    of the emulator read the normal variable.
14. `Exec` always fails with `ErrNotSupported`, also for a `SELECT`, because the
    driver reads only. A caller uses `Query`. Ken decided this on 2026-10-11.
15. A query whose rows are not objects, such as `SELECT VALUE c.n`, has one column
    named `$1`, which is the name that the service gives an expression with no
    alias.
16. The keys of the DSN query are `tls`, `insecure`, `pagesize` and `partitionkey`.
    The path can be empty or can hold only the database, and `WithDatabase` and
    `WithContainer` fill what is missing. `partitionkey` sets the value of the
    partition key that a query uses, so that the query stays in one partition.
17. The driver answers read-only catalog statements, one flat set of rows for
    each, through `QueryContext`. A statement is a `SELECT` against a reserved
    name: `$databases`, `$containers`, `$stored_procedures`, `$triggers`,
    `$functions`, `$offers`, `$users`, `$permissions` and `$account`, with a
    `WHERE` on the names of the database and of the container. The columns are
    in COSMOS.md. This is for `dbmeta`, which needs the partition key, the policies
    and the throughput of a container, and which keeps to the driver that `dburl`
    names. Ken decided this on 2026-10-11. The driver still writes nothing (W40).
