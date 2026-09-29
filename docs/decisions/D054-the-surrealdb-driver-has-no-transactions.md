# D54. The SurrealDB driver has no transactions

Status: Decided.

Ken accepted this on 2026-09-27. It decides item 7 of step 9 for SurrealDB. `BeginTx` returns
`ErrNotSupported`. The RPC methods `begin` and `commit` do not exist over
HTTP, on either release, and every request is a transaction of its own
(measured). The Go SDK refuses an interactive transaction over HTTP too.
`BEGIN`, `COMMIT` and `CANCEL` still work inside the text of one request
(measured), and D20 allows that, because the server
runs it.
