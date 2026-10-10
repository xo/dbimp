# D190. The Cosmos DB driver

Status: Decided in part.

These are the answers of Ken to some step 9 questions of
[COSMOS.md](../COSMOS.md) (W36). He decided them on 2026-10-10. The other
proposals of step 9 are open, because Ken wants to read the document first.

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

