# D202. Answers for Exasol after step 2

Status: Decided.

Step 2 for Exasol found that it can be a driver ([EXASOL.md](../EXASOL.md), W44). Ken decided
on 2026-10-11:

1. The work goes on to steps 3 to 9 of [DRIVER.md](../DRIVER.md). H passes as a WebSocket,
   as item 1 of D200 allows.
2. `dbimptest` records and replays the messages of a WebSocket before step 6. An exchange
   is one request frame and its reply frame, with the type of each frame (text or binary).
   Each exchange is a JSON object in its own file, as the HTTP exchanges are. `Replay` for a
   WebSocket starts a TLS server in the test, and it matches the command of the request
   (`login`, `execute`, `fetch`, `closeResultSet` and so on). The password of the login is
   encrypted with a new key each time, so it is left out of the match. The design goes in
   [DESIGN.md](../DESIGN.md) before the recorder, so that another target can share it.
3. The driver of Exasol supports compression. It sends `useCompression` and reads the zlib
   frames that the server sends in binary frames, with `compress/zlib`. This is not the
   extension permessage-deflate. The default of the driver, on or off, is a question of step 9.
4. The client of the WebSocket in the root package reads whole messages up to its limit of
   64 MiB, and it does not stream. Streaming reads wait until a measurement needs them.
