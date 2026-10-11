# D200. Exasol is a target, and the root package gets a WebSocket client

Status: Decided. Amended by D217.

Ken decided on 2026-10-11, at step 1 of [DRIVER.md](../DRIVER.md), that Exasol is the
next target (W44). `usql` reaches Exasol with `exasol/exasol-driver-go`, and `dbmeta`
has the entry `exasol-2026.2.0` that `dbrun` starts. Exasol takes JSON over a
WebSocket, and the standard library has no WebSocket client.

1. Exasol is in scope although it is not plain HTTP. D14 asks for databases that have an
   HTTP or HTTPS interface, and a WebSocket starts with an HTTP request that asks for an
   upgrade. A target that takes a query that a person can type, and JSON text frames over
   a WebSocket, meets the tests R, H and S of D16 in that sense. Step 2 measures it, and
   Ken decides if the result changes the scope.
2. The root package gets a minimal WebSocket client, written here with the standard
   library only. It does what RFC 6455 asks of a client: the upgrade with `net/http`,
   text and binary frames, masking, fragmentation, ping and pong, close, and the limit
   of a message. It adds no dependency (hard rule 6 and D13), and a later driver can
   share it (D4). Ken chose this on 2026-10-11 in place of the packages
   `github.com/coder/websocket` and `github.com/gorilla/websocket`.
3. The measurement and the recordings need the exchanges of a WebSocket, and the
   recorder records HTTP. Step 6 for this target extends `dbimptest` first, and the design
   goes in [DESIGN.md](../DESIGN.md) before the driver.
