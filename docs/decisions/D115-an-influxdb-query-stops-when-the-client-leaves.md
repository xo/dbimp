# D115. An InfluxDB query stops when the client leaves

Status: Decided.

Ken decided on 2026-09-29 that the InfluxDB driver closes the body when the
context ends, as D36 says, and sends nothing more, on every release. D36
asks for a decision of its own where a server does not stop a query when
the client leaves. Every release of InfluxDB stops one (measured on
2026-09-29):

- InfluxDB 1.13.1: a query that took 3.3 seconds in full was gone from
  `SHOW QUERIES` half a second after the client left at 1 second, with and
  without chunks.
- InfluxDB 2.9.1, which has no `SHOW QUERIES`: the same query executed for
  1.8 seconds in full and for 1.0 second when the client left at 1 second,
  by `influxql_service_executing_duration_seconds` of `/metrics`.
- InfluxDB 3.11.5 records such a query with the phase `cancel` in
  `system.queries` (measured, 3.11.5-045).

So `KILL QUERY` of InfluxDB 1 is not needed.
