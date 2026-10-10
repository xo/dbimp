# D186. Spanner gets no driver here

Status: Amends D185. Amended by D187.

Ken decided on 2026-10-10, at step 2 of [DRIVER.md](../DRIVER.md), that dbimp
writes no driver for Google Cloud Spanner. It fails H: the only Spanner server
that `dbrun` starts serves no REST API (D16, D17 and D185).

This amends D185, which named Spanner as a target for a REST driver.

## Why

- The entry `spanner-2026.r4-lts` is Spanner Omni. Its published port, 15000,
  serves gRPC over HTTP/2 only: HTTP/1.1 gets "Received HTTP/0.9", HTTPS gets
  "wrong version number", plain HTTP/2 with JSON is reset with `INTERNAL_ERROR`,
  and gRPC-Web is reset too (measured by the agent of this repository with
  `curl`, 2026-10-10, SPANNER.md).
- The `dbmeta` session measured the other ports of Omni from inside its
  container on the same day. Omni listens on 15000 to 15012, 15014, 15015 and
  15025. Every port except 15012 refuses plain HTTP/1.1. The port 15012 answers
  with an internal status page, a 200 HTML page titled "Spanner", and answers 404
  to the paths of the REST API. Omni has no REST or gateway flag, and the
  quickstart names no REST (reported by `dbmeta`, 2026-10-10).
- The classic Cloud Spanner emulator serves REST on port 9020, but `dbmeta`
  dropped it (dbmeta D215), so no server here can test a REST driver.
- A driver that speaks gRPC needs a binary encoding, which D185, D13 and hard
  rule 6 forbid without the approval of Ken for one database, and Ken did not
  give it.
- `usql` reaches Spanner with `googleapis/go-sql-spanner` already, so nothing
  is lost for a user.

## When this can change

A server that `dbrun` starts and that serves the REST API of Spanner, or the
approval of Ken for a driver on gRPC, reopens the question. The measurement
starts again at step 2.
