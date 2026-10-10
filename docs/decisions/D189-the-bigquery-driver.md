# D189. The BigQuery driver

Status: Decided. Amended by D195.

These are the answers to the step 9 questions of [BIGQUERY.md](../BIGQUERY.md)
(W35). Ken decided them on 2026-10-10.

1. The package is `bigquery`, and it registers the name `bigquery` with no alias
   (D26, D28 and D30). It uses no binary encoding (D13).
2. The secret is a path to a key file, in a key of the DSN query such as
   `credential_file`. The driver reads the file and signs a JWT with RS256 for
   the token endpoint named by the file. The key text never sits in a URL. A
   caller can also pass a ready access token through a connector. This is the
   alternative to D94 for a secret that is a JSON file.
3. `BeginTx` returns the error of D20 for the first release. A transaction in
   one script request still works, because the driver sends the text as is.
   A session across requests works on the service, and a later release can add it.
4. The driver supports the community emulator release `bigquery-0.8.1` and the
   hosted service. It does not support `bigquery-0.7.2`, which cannot read the
   result of a `jobs.query` job.
5. The driver asks for timestamps as `ISO8601_STRING`. The measurement did not
   check this form at the year 9999, so the integration test must check it, and
   the driver then switches to `useInt64Timestamp`, which is exact, and tells
   Ken. Ken decided this on 2026-10-10.
6. A request with several statements returns the rows of the last statement, as
   the service does. BIGQUERY.md says so. Child jobs through `parentJobId` can
   come later.
7. A JSON column is a decoded Go value, and a JSON null stays distinct from a SQL
   NULL because it arrives as the text `null`. A STRUCT column is a
   `map[string]any`. A struct whose members repeat a name or have none makes the
   driver return an error that names the column. Ken chose the map, and
   he decided on 2026-10-10 to keep the error. A caller who needs such a struct
   casts it in SQL, for example with `TO_JSON_STRING`.
8. The other proposals of step 9 stand as BIGQUERY.md writes them:
   - A NULL and a missing value are both nil.
   - Parameters are named `@p`, bound by the server, with an explicit
     `parameterType` for each.
   - The driver always sends `useLegacySql: false`.
   - The driver reads pages through the job and `getQueryResults` with
     `pageToken`, and it sends the `location` on every follow-up call.
   - When the context ends, the driver calls `jobs.cancel` with the `location`
     of the job.
   - The driver follows no redirect, and it sends the Bearer token only to the
     configured host.
   - The driver serves no flavor.
   - The driver does not retry `jobInternalError`.
