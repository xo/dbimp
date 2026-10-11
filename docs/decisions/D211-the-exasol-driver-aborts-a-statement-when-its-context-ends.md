# D211. The Exasol driver aborts a statement when its context ends

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 9, under D36.

When the context of a statement ends, the driver sends `abortQuery`. The command has no answer of its own. The statement that ran answers with the code `R0003`, and the driver reads that answer before it sends the next frame. The connection stays usable. The error wraps the error of the context, and it holds the error of the server.

The driver sends `abortQuery` in the same form as every other frame, so with compression on, it is a binary frame of zlib data. A text frame ended the session in a measurement of 2026-10-11, with the error `Text data frame was received, but compression is enabled`.

Ken gave no limit for a server that does not answer the abort. The driver waits 10 seconds, and then it closes the session, so that the caller does not wait for ever. That limit is the proposal of the driver. The question stays open in [EXASOL.md](../EXASOL.md).
