# D214. The Exasol driver compresses by default

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 12. It sets the default that D202 item 3 left open, and it differs from the proposal of the document, which was off.

Compression is on by default. The login sends `useCompression` and the attribute `compressionEnabled`, and then the driver sends each frame as a binary frame of zlib data, with `compress/zlib`. The key `compression=false` turns it off. The driver reads a text frame and a binary frame whatever it asked for, because the server answered a text frame with a compressed frame (recorded: "a text frame after compression was turned on"). The login messages are not compressed.
