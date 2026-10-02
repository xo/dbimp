# D168. The OpenSearch driver

Status: Decided.

These are the items of step 9 of [DRIVER.md](../DRIVER.md) for OpenSearch
(W29). Each fact is in [OPENSEARCH.md](../OPENSEARCH.md). D163 decides that
the driver is read only, and that it documents the caps of the server.

1. The package and the name that it registers are `opensearch` (D26, D28
   and D162).
2. The DSN is `opensearch://user:password@host:9200`, with no path. The
   keys are `tls` (false by default) and `fetch_size` (1000 by default). Any
   other key is refused.
3. The Go types are the type table of OPENSEARCH.md. A `timestamp` and a
   `datetime` are a `time.Time` in UTC. A `geo_point` and an
   `integer_range` are a `map[string]any`, because the server sends an
   object, and hard rule 3 forbids its JSON text. A field with several
   values arrives as a JSON array under a scalar type, and the driver gives
   it as a `[]any`, as Ken decided on 2026-10-02.
4. NULL and a missing value are one value, nil.
5. The driver writes each argument as a literal with the escaper of the
   root package (D34), because the server writes each value into the text
   itself, and 2.19.6 changes a string with a quote or a backslash. Ken decided this on
   2026-10-02.
6. `BeginTx` returns `dbimp.ErrNotSupported`, because OpenSearch has no
   transactions.
7. Each plain `SELECT` sends `fetch_size`, and the driver reads each page
   one token at a time to the last cursor. When the caller closes the rows
   before the end, the driver sends `/_plugins/_sql/close`. An error with
   HTTP 200 reads its status from the body. An error on a later page wraps
   `dbimp.ErrIncomplete` (D21 and D107). The caps of D163 are in the
   documentation of the driver.
8. The SQL plugin has no way to stop a statement. When the context ends, the
   driver closes the request and the cursor. Whether the server stops a
   statement when the client leaves is not measured.
9. The driver follows no redirect, and sends the credentials to the host of
   the DSN only.
10. The driver serves no flavor. The release comes from `GET /`, or from the
    header of 3.8.0, where an answer differs between releases.
11. The driver uses JSON only.

On 2.19.6 the ordinary user of `dbmeta` cannot page, because a cursor needs
the right to search every index, and cannot read the version. Both wait
for `dbmeta`.
