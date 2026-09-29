# D111. Three facts of the InfluxDB driver

Status: Decided.

Ken decided on 2026-09-29 to record these, which the code held and no
decision named:

- The port does not change with `tls=true`. InfluxDB serves HTTPS on the
  port that it serves HTTP on (the configuration of InfluxDB, not
  measured), so the driver uses 8086 or 8181, as D82 says, with or without
  TLS. Couchbase moves to 18093, because its server has a port for TLS.
- `RowsAffected` and `LastInsertId` return `dbimp.ErrNotSupported`, because
  InfluxDB counts no rows and has no id of an insert.
- InfluxQL binds a positional argument: the driver sends ordinal n as the
  key `"1"` of `params`, and `$1` takes it, on 1.13.1, 2.9.1 and 3.11.5
  (measured on 2026-09-29), as SQL does on InfluxDB 3.
