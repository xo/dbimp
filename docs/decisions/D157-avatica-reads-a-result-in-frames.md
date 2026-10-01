# D157. Avatica reads a result in frames

Status: Decided.

Ken decided this on 2026-10-01, at step 9 of the Avatica driver. This is
items 5 and 8 of step 9 of [DRIVER.md](../DRIVER.md) for Avatica.

- One `driver.Conn` is one connection of Avatica, which `openConnection`
  opens with an id that the driver chooses, and `closeConnection` ends.
  Each connection sets `autoCommit` to true with `connectionSync`, because
  the Phoenix Query Server opens one with it false (AVATICA.md,
  Transactions).
- A query with no arguments sends `prepareAndExecute` with
  `maxRowsInFirstFrame`, because without it the server answers an empty
  first frame marked `done` (AVATICA.md, Responses). The driver reads each
  frame one token at a time (D25), and asks for the next one with `fetch`
  from the offset after the last row, until a frame is `done`.
- An error while the driver fetches a frame after some rows wraps
  `dbimp.ErrIncomplete` (D107).
- NULL and a value that is missing are one value (D18).
