# D194. Rule 4 names three more drivers

Status: Decided.

Ken decided on 2026-10-11 that hard rule 4 of AGENTS.md names three more places
where a driver keeps a context, as it names Snowflake and Athena (D183 and
D192).

1. The rows of a BigQuery query keep the context of the statement, because the
   request for each next page needs it (D189).
2. The rows of a Cosmos DB query keep the context of the statement, because the
   request for each later page needs it (D190).
3. A Spanner transaction keeps the context of `BeginTx`, because `Commit` and
   `Rollback` take no context. The rows of a DML statement that runs outside a
   transaction keep the context of the statement, because the commit happens
   when the rows end or close (D191 and D195).

Each driver stops its work when the context ends, and `Close` releases what the
context held.
