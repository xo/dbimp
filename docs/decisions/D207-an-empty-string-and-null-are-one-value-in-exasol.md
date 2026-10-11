# D207. An empty string and NULL are one value in Exasol

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 5, under D18.

The server stores an empty string as NULL (recorded: "select an empty string"). The driver returns nil for it, and so for a bound empty string, which the server stores as NULL. A caller that needs to tell the two apart cannot. [EXASOL.md](../EXASOL.md) says so.
