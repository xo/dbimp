# D56. A SurrealDB query stops when its context ends

Status: Decided.

Ken accepted this on 2026-09-27. It decides items 9 and 10 of step 9 for SurrealDB. The server stops a
query when the client disconnects. `SLEEP 3s; CREATE cancel:x` was left after
1 second, and `cancel:x` did not exist 4 seconds later, on 2.7.0 and 3.3.0
(measured). So the driver closes the body when the context ends, as D36
says, and sends nothing more.

The driver follows no redirect, and sends the credentials only to the host of
the URL. The server sent no redirect in any measurement.

No other product speaks this interface, so the driver has no flavors (item
11).

Note of 2026-09-29: the request carries the context, so `net/http` stops
the request and the read of the body when the context ends. The driver does
not close the body itself then (D36).
