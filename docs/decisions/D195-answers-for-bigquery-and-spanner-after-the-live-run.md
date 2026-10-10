# D195. Answers for BigQuery and Spanner after the live run

Status: Decided. Amends D189 and D191.

These are answers of Ken after the first live run of the drivers. He decided
them on 2026-10-11.

1. A JSON null and a SQL NULL of BigQuery are both nil. This amends item 7 of
   D189, which said that a JSON null stays distinct. A caller who must tell them
   apart selects `TO_JSON_STRING`, which gives the text `null`.
2. The Spanner driver sends a nil argument with no `paramType`, because the
   server infers the type. This is an exception to item 7 of D191. A live test
   checks a NULL for each type, and the driver sends `STRING` for a NULL that the
   server refuses.
3. A Spanner DML statement that runs outside a transaction runs as
   `beginTransaction`, the statement and `commit`. The rows commit when they end
   or close, also when the caller closes the rows of `THEN RETURN` early, so that
   `QueryRow` keeps its write. An error rolls back. This adds to item 6 of D191.
4. The BigQuery driver makes these choices, which Ken accepted on 2026-10-11:
   - The first request sends `timeoutMs` of 3000, so that the driver can poll. A
     context that ends during that first request cannot cancel the job, because
     no job id exists yet.
   - Parameters are `NAMED` when every argument has a name and `POSITIONAL` when
     none has. A mix is refused. A NULL is a `STRING` with no value, so a
     statement must `CAST` it. ARRAY and STRUCT are not bound.
   - A field with an unknown or an empty type reads as text.
   - A value finer than a microsecond is refused when the driver sends it, and a
     `time.Time` goes as UTC.
   - The keys of the DSN query beyond `credential_file` are `endpoint`,
     `disable_auth`, `scopes`, `location`, `timeout` and `max_results`. The user of
     the URL is ignored, and a password is refused.
   - `Exec` reads only the head of the answer, and `Ping` is a dry run of
     `SELECT 1`.
   - The driver answers no `SELECT version()`.
   - The integration tests of the release `bigquery-0.7.2` skip with a reason,
     because D189 does not support it.
5. The Spanner driver runs DML through `executeStreamingSql`, as item 11 of D191
   says for every result. A live test on the hosted instance showed the count
   in `stats`.
