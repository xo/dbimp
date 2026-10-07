# VoltDB

This file holds what is known about VoltDB, and why dbimp writes no driver for
it (W33, D179 and D180). A fact is "measured", with the release and the date,
or "not measured", with its source.

## Summary

Ken named VoltDB as a target for an HTTP driver on 2026-10-08. Step 2 of
[DRIVER.md](DRIVER.md) stopped at H, and no request was sent. Nothing is
recorded under `testdata/voltdb/`.

- R: `dbrun` lists `voltdb-14.1.0` and `voltdb-15.2.0`, and mounts the licence
  file of Ken at `~/.config/dbmeta/licenses/voltdb` (measured, `dbrun list`,
  2026-10-08).
- The entry of `dbmeta` did not start `voltdb-14.1.0` at first. The server
  printed "Command logging is not supported in the Developer Edition", then
  "This process will exit" (measured, `dbrun start` and `podman logs`,
  2026-10-08). The `dbmeta` session fixed that in its release `v0.4.0`.
- H: neither release serves an HTTP interface. The `dbmeta` session found that
  nothing listens on port 8080 on either release, with `/proc/net/tcp`, and
  that `@SystemInformation OVERVIEW` lists no HTTP port. The ports are 21212
  for the client, 21211 for the admin, 3021 for the internal traffic, 7181 for
  ZooKeeper, 11781 for metrics, 5555 for DR and 9092 for topics. A `jsonapi`
  setting in the deployment file was dropped when `init` converted the file to
  YAML, as seen on 15.2.0 (reported by `dbmeta` on 2026-10-08, and not
  measured again in this repository).
- S: not measured, because H failed.
- The scheme in `dburl` is `voltdb`, with the aliases `volt` and `vdb`, the
  generator `GenVoltdb`, and the dialect `voltdb`. The generator gives
  `host:port`, with `localhost` and `21212` as defaults, which is the native
  port (read of `dburl/dsn.go`).
- `usql` uses `github.com/VoltDB/voltdb-client-go/voltdbclient` v1.0.18, which
  speaks the native protocol (read of `usql/go.mod`). Its driver file has no
  `Version` statement (read of `usql/drivers/voltdb/voltdb.go`).

## Second opinions

On 2026-10-08 Ken asked two models whether an HTTP client for VoltDB is
possible. Both said that it is, and neither measured it. The measurement above
shows that they were wrong for these two releases (source: the answers of
`gemini-3.1-pro-preview` and `deepseek-v4-pro`).

- They named the endpoint `/api/1.0/` with the parameters `Procedure`,
  `Parameters`, `User`, `Password` and `Hashedpassword`, and the procedure
  `@AdHoc` with `?` placeholders, on the port 8080 of the image
  `voltdb/voltdb-community`.
- The two models differed on Kerberos, and on the encoding of `VARBINARY` and
  `GEOGRAPHY`.

## Open questions

1. A release of VoltDB that serves the HTTP interface, or a way to turn it on
   in the deployment file of these releases, reopens the question. The
   `dbmeta` session saw the `jsonapi` setting dropped on 15.2.0, and did not
   try other forms of the YAML. Ken decided to drop VoltDB without that check
   (D180).
