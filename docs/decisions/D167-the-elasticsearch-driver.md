# D167. The Elasticsearch driver

Status: Decided.

These are the items of step 9 of [DRIVER.md](../DRIVER.md) for
Elasticsearch (W28). Each fact is in [ELASTICSEARCH.md](../ELASTICSEARCH.md).
D163 decides that the driver is read only, because SQL in Elasticsearch
takes no write.

1. The package and the name that it registers are `elasticsearch` (D26, D28
   and D162).
2. The DSN is `elasticsearch://user:password@host:9200`, with no path. The
   keys are `tls` (false by default), `auth` (`basic` by default, or
   `apikey`, which sends the password as an API key, D94), `fetch_size`
   (1000 by default), `time_zone` (UTC by default), `field_multi_value_leniency` (false by
   default) and `catalog`. Any other
   key is refused.
3. The Go types are the type table of ELASTICSEARCH.md. The ten day-time
   intervals are a `time.Duration`, because the server writes them in hours,
   such as `PT48H`, and the year-month intervals are a `dbimp.Interval`. Ken
   decided this on 2026-10-02, though Avatica gives every interval a
   `dbimp.Interval`.
4. NULL and a missing value are one value, nil.
5. The driver sends positional arguments in `params`, as a JSON array. It
   refuses a `uint64` above the range of `int64`, because the server cuts
   such a value to the largest `long` with no sign.
6. `BeginTx` returns `dbimp.ErrNotSupported`, because Elasticsearch has no
   transactions.
7. The driver reads each page one token at a time, follows the `cursor` to
   the last page, and sends `/_sql/close` when the caller closes the rows
   before the end. An error on a later page wraps `dbimp.ErrIncomplete`
   (D21 and D107).
8. The server cancels the task when the client leaves (measured), so the
   driver closes the request when the context ends.
9. A field with several values fails the statement unless
   `field_multi_value_leniency` is on, and then only its first value
   arrives. The driver leaves it off, so that no value is lost with no
   sign, and the DSN key `field_multi_value_leniency` lets the caller turn
   it on. Ken decided this on 2026-10-02.
10. The driver follows no redirect, and sends the credentials to the host of
    the DSN only.
11. The driver serves no flavor. OpenSearch has a driver of its own (D162).
12. The driver uses JSON only. CBOR and Smile exist, and need no approval.

The ordinary user cannot read the version, which waits for `dbmeta`.
