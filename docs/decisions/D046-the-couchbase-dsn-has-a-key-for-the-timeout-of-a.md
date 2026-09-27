# D46. The Couchbase DSN has a key for the timeout of a transaction

Status: Amends D38.

Ken decided this on 2026-09-27, and it amends D38. The DSN takes the key
`txtimeout`, a duration such as `30m`, and `WithTransactionTimeout` sets it
for one transaction, by the order of D40. The driver sends it with
`BEGIN WORK`. If the DSN has no such key, the driver sends none, and the
server ends a transaction 15 seconds after it began.

Fifteen seconds is too short for a person who types statements into an open
transaction in `usql`. The server takes any length: 2 minutes, 20 minutes
and 1 hour were accepted on 8.0.3 on 2026-09-27, and the documentation names
no maximum. The interactive shell of Couchbase, `cbq`, uses 2 minutes. So
`usql` sets a long default, such as 30 minutes, when the URL has none, and
that change is part of the move of step 16 of [DRIVER.md](../DRIVER.md). The
driver keeps the default of the server for every other caller, because an
abandoned transaction holds its state on the server until its timeout.
