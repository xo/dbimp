# D49. The SurrealDB driver speaks CBOR, written here, and JSON as an option

Status: Decided.

Ken decided this on 2026-09-27, and approved CBOR for SurrealDB by D13. It
decides item 12 of step 9 for SurrealDB.

The driver sends each request and reads each response in CBOR, with
`Content-Type` and `Accept` set to `application/cbor`. JSON writes a
decimal, a datetime, a duration, a UUID and a record id as strings, NONE as
null, and bytes as an array of numbers, and 2.7 writes a range as the dump
of a Rust enum (measured on 2.7.0 and 3.3.0 on 2026-09-27). CBOR keeps each
of them with a tag. Both SDKs of SurrealDB speak CBOR.

The encoder and the decoder of CBOR are written in the root package, with
the standard library only, so no dependency is added (D13). The decoder
reads one item at a time, as `jsontext` does, so a result is never held in
memory (D25). A later driver that speaks CBOR uses the same code.

The DSN key `encoding=json` makes the driver speak JSON, so that a person
can read the requests and the responses while they debug. In JSON, a value
arrives as the JSON type that the server wrote, and the driver does not
guess the type of a string. The default is `encoding=cbor`.
