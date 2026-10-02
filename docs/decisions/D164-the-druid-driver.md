# D164. The Druid driver

Status: Decided.

These are the items of step 9 of [DRIVER.md](../DRIVER.md) for Apache Druid
(W25). Each fact is in [DRUID.md](../DRUID.md). D163 decides that the driver
is read only.

1. The package and the name that it registers are `druid` (D26 and D28).
2. The DSN is `druid://user:password@host:8888`, with no path, because Druid
   has no database to choose. The keys are `tls` (false by default),
   `timezone` (UTC by default, sent as `sqlTimeZone`) and `timeout` (none by
   default, sent as the `timeout` of the query context). Any other key is
   refused. The alternative is to pass each unknown key into the query
   context.
3. The Go types are the type table of DRUID.md, with two changes from
   D135, which Ken decided on 2026-10-02:
   - `DECIMAL` is a `float64`, because the server computes it as a double.
     Gemini and DeepSeek both said so (D136).
   - A multi-value `VARCHAR` with more than one value is a `[]any` of
     strings, decoded from the JSON text that the server sends, and one
     value stays a `string`, because hard rule 3 forbids JSON text in place
     of a value.
4. NULL and a missing value are one value, nil.
5. The server binds each argument as a typed `{type, value}`, from the Go
   type of the argument. The alternative is the escaper of the root package
   (D34).
6. `BeginTx` returns `dbimp.ErrNotSupported`, because Druid has no
   transactions.
7. The driver reads `arrayLines`, with the rows of the header, one line at
   a time. An error after some rows ends the answer with HTTP 200 and no
   text, so a missing last empty line is an error that wraps
   `dbimp.ErrIncomplete` (D21 and D107).
8. Each query carries a `sqlQueryId` of its own. When the context ends, the
   driver sends `DELETE /druid/v2/sql/{sqlQueryId}`, with the context
   without its end and a limit of its own, because the server runs a query
   on when the client leaves.
9. The driver follows no redirect, and sends the credentials to the host of
   the DSN only.
10. The driver serves no flavor. Imply serves the same API, by Gemini (not
    measured).
11. The driver uses JSON only.

Two facts wait for `dbmeta`, and not for this decision: `dbmeta_user`
cannot read the version, and the nano quickstart has two task slots.
