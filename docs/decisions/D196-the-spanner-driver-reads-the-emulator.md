# D196. The Spanner driver reads the emulator

Status: Decided. Adds to D187, D191 and D195.

Ken asked on 2026-10-11 that the Spanner integration tests run on the emulator of
`dbmeta` (D187 item 2). `dbmeta` v0.13.0 starts it as the release
`spanneremulator-1.5.58`. The `dbimp` session measured it, and the facts are in
[SPANNER.md](../SPANNER.md) under "Measured on the emulator". This file holds what the
session did. Ken has not reviewed it.

1. The driver reads both forms of `executeStreamingSql`. The hosted service answers one JSON
   array of `PartialResultSet` messages. The emulator answers one JSON object for each message,
   each wrapped in the member `result`, with new lines between them. The driver reads the first
   token of the body. A `[` is the array, and a `{` is the sequence of objects. Both forms go
   one token at a time (D25), and both keep the rules for `chunkedValue`, `resumeToken`,
   `stats` and `last`. An object with the member `error` ends the result with that error, as
   an element of the array does.
2. The driver reads the three forms of an error that the emulator writes: the member `error`
   with no `status`, the members `code` and `message` with no `error` (HTTP 500 and code 13 is
   a fault of the gateway), and an empty body. Each one is an `*Error` with the HTTP status and
   the text. A status that the body does not name comes from the gRPC number. An empty body
   keeps the text of the HTTP status. No error is a parse error any more.
3. A commit on a multiplexed session sends the member `precommitToken` once an answer carried
   it, also when it holds no token. The emulator commits only then. The hosted service always
   gave a token, so its behavior is the same.
4. The recordings of the emulator are in `testdata/spanner/`, with the file names that start
   with `spanner-spanneremulator-1.5.58-`, and the manifest holds them under the release
   `spanneremulator-1.5.58`. The script is small, and it is not the script of the hosted
   service. Item 8 has an absent reason, because the emulator checks no credential. No other
   principal exists. The replay tests of the hosted recordings read the files with a number
   after `spanner-`, and the replay tests of the emulator read the release `spanneremulator-1.5.58`.
5. CI runs the integration tests on `spanneremulator-1.5.58`. The workflow takes `spanner` out of
   the list of hosted drivers, and adds a list of emulated drivers. A driver in that list does not
   own the dbrun product of its own name, because the product `spanner` is Spanner Omni, which
   speaks gRPC only. The release comes from the recorded releases of the manifest, so the
   workflow names no release. The workflow pins `DBMETA_COMMIT` to v0.13.0 of `dbmeta`.
6. The integration tests tell the emulator by a loopback host in the DSN. A test skips on the
   emulator, with its reason, when the emulator differs from the service in what the test holds:
   a refusal with no text, an `INTERVAL` column, `FORCE_INDEX` on a null filtered index, a read
   at a time, and the cancel of a DDL operation. Tests of errors accept an error with no name and
   the HTTP status that the name stands for. Two limits of the emulator change a size, an array of
   16000 elements at most.
7. The hosted service did not run the new code when the change was written. The replay tests
   of the hosted recordings passed, and the change to the commit is the only one that touches
   the hosted path. The main session then ran the integration suite on the hosted instance, on
   2026-10-11, and it passed.
