# D172. Druid reads a few types of the round trip on a push

Status: Decided.

Ken decided this on 2026-10-07, after the integration jobs of Druid timed
out in CI. Each write to Druid is a task of the server that takes 5 to 12
seconds, so the round trip of every type takes more than an hour for each
release, and Go stops a test after 10 minutes unless the command gives
`-timeout`.

- A push runs the round trip of three types of Druid: `BIGINT`, `VARCHAR` and
  `TIMESTAMP`. The test skips the other types, and names the variable
  `DBIMP_FULL` in the reason.
- The run of the schedule, and a manual run (`workflow_dispatch`), set
  `DBIMP_FULL`, and run every type. Before a release, a manual run shows
  that every type passes on each release.
- The workflow gives `-timeout 150m` to the tests of every driver, so that
  the full run can finish, and the quick run stays far under it.
