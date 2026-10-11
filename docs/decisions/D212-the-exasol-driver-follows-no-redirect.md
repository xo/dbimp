# D212. The Exasol driver follows no redirect

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 10.

The driver follows no redirect. It sends the user and the password only to the host and the port of the DSN, in the login. The client of the WebSocket in the root package returns a redirect as an error, so no other host receives them.
