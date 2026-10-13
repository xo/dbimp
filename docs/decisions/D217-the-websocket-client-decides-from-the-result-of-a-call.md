# D217. The WebSocket client decides from the result of a call

Status: Decided. Amends D200.

An independent review of the WebSocket client in `websocket.go` on 2026-10-11 found these faults.
Each one has a test.

1. A context that ended after a call did its work, and before the guard of the call stopped,
   closed the connection. The next call, with a new context, then failed with `closed pipe`.
   The write side had the same fault. The guard sets a write deadline when the connection has
   one, but the body of the response of `net/http` has none, so in production the guard always
   closed the socket. The test used `net.Pipe`, which has a write deadline, and so it missed this.
2. The reader held each frame in a `bytes.Buffer` and joined the fragments with `append`, so the
   peak was two or three times the limit of 64 MiB for each connection.
3. A message had no limit on its number of frames, and a server could send millions of empty
   frames.
4. `Close(1005, ...)` put the code 1005 on the wire. RFC 6455 section 7.4.1 forbids it.
5. A close frame with the code 1014 was a protocol error, but IANA registered 1014.
6. A TCP end at the start of a frame returned an error that no document described.
7. The default client used the proxy of the environment, which cannot carry the upgrade of a
   `ws` URL over a plain HTTP proxy, and it ignored a `Host` header. A failed dial left the idle
   connections of its own transport open.

Decided:

1. The client keeps the `net.Conn` that carries the upgrade. It takes it from the `GotConn` hook
   of `httptrace`, so it also works with a client of the caller. A guard sets a deadline in the
   past on that connection when the context ends, and a deadline interrupts a blocked call. The
   guard never closes the connection. If the call returned no error, the guard removes the
   deadline, so a context that ends late changes nothing. If the call failed with a deadline
   error and the context ended, the call returns the error of the context and closes the
   connection, as before (D200). A connection that has no deadline, which only a test makes, is
   closed instead.
2. A context that is done before a call starts makes the call return its error, and the
   connection stays open, because nothing was sent or read.
3. A write takes its guard under the lock of the writer, so the pong and the close frames that
   `Read` sends use the context of `Read`, and two writers never share one deadline.
4. A frame grows the buffer of its message only as bytes arrive, by a quarter of its size for each step, at most 4 MiB, and by at most 1 MiB at first,
   and each fragment goes into the same buffer. The `drain` of `Close` discards the payload. The
   Exasol driver bounds an inflated answer by the same limit as the message, and not by twice
   that limit.
5. One call to `Read` accepts at most one million frames, control frames included. Past that, it
   sends the code 1009 and returns an error that wraps `ErrMessageTooBig`.
6. `Close(CloseNoStatus, ...)` sends a close frame with no code. `Close` refuses any other code
   that an endpoint cannot send (1004, 1006, 1015, 1016 to 2999 and from 5000) with an error that
   wraps `ErrInvalidValue`, and it sends nothing. A server close frame can carry 1012, 1013 and
   1014.
7. A TCP end at the start of a frame stays an error that wraps `io.EOF`. The Exasol driver reads
   that as a lost connection. `Read` documents it.
8. The default client uses no proxy. The new option `WithWebSocketProxy` sets one, and a caller
   with its own client sets the field of its transport. A `Host` header sets `Host` of the
   request. A dial with the default client calls `CloseIdleConnections` on every exit.

The exported API gains one function, `WithWebSocketProxy`.
