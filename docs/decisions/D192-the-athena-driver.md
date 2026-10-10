# D192. The Athena driver

Status: Decided.

These are the answers to the step 9 questions of [ATHENA.md](../ATHENA.md)
(W37). Ken decided them on 2026-10-10.

1. The package is `athena`, and it registers the name `athena` with no alias (D26,
   D28 and D30). It uses no binary encoding (D13). Requests are signed with
   Signature Version 4 for the service `athena`, as the `dynamodb` driver signs
   its own.
2. The integration tests run on a hosted account, only where a person supplies it,
   as for Snowflake (D182). The tests skip when the DSN variable is empty.
3. A value of the type ARRAY, MAP or ROW is the text that the server sends, in a
   `string`, of the kind `other`. The text has no escaping, so the driver does
   not parse it.
4. The driver binds parameters with `ExecutionParameters`. It keeps each `?` in
   the text, and it writes each value as an escaped SQL literal. It does not use
   a prepared statement.
5. The rows of an Athena query store the context for the `NextToken` of each page
   after the first. Hard rule 4 of AGENTS.md names them, as it names the rows
   of a Trino, a Databend and a DynamoDB query.
6. The driver gives no answer to a version request, as D181 says. The `usql`
   query for the version is refused by Athena.
7. A `TIMESTAMP WITH TIME ZONE` value with a named zone is a `time.Time` in that
   zone, from `time.LoadLocation`. The driver imports `time/tzdata`, so the zones
   exist on a machine that has none.
8. A query that ends `CANCELLED` is an error that names the state and its reason,
   also when the driver did not stop it. The driver polls `GetQueryExecution`
   from 100 ms and backs off to 1 s.
9. A fifth live pass runs on the hosted account, with the catalog and the
   Lambda connector that `dbsetup` made, to settle federated queries.
10. The other proposals of step 9 stand as ATHENA.md writes them:
    - The DSN is `athena://[key:secret@]host/<database>`, with the keys
      `workgroup`, `output`, `token` for the session token and `catalog`. The
      region comes from the host.
    - An empty string and a NULL are two values, as the wire shows `""` against
      no value.
    - The driver skips the header row by the statement type. The first row of a
      DML result, such as `SELECT` and `EXPLAIN`, is the header. A UTILITY
      result, such as `SHOW` and `DESCRIBE`, has none.
    - The driver calls `StopQueryExecution` when the context ends, and it sends
      a new `ClientRequestToken` for each statement.
    - `BeginTx` returns the error of D20, because the service refuses `START
      TRANSACTION`.
    - A request holds one statement, because two give HTTP 400.
    - The driver serves no flavor.
11. The type table of ATHENA.md stands for the rows that item 3 and item 7 do not
    cover. Both INTERVAL types are a `dbimp.Interval`, `IPADDRESS` is a
    `netip.Addr` (D177), `UUID` is a `uuid.UUID`, `GEOMETRY` is a string of WKT,
    `TIME WITH TIME ZONE` is a `dbimp.OffsetTime`, `TIME` is a `dbimp.LocalTime`,
    `TIMESTAMP` is a `dbimp.LocalDateTime`, `DATE` is a `dbimp.Date`, `DECIMAL` is
    a `*apd.Decimal`, `VARBINARY` is a `[]byte`, `JSON` is a decoded value and
    `UNKNOWN` is nil. Ken decided this on 2026-10-10.
