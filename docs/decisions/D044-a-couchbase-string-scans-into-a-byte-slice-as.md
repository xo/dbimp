# D44. A Couchbase string scans into a byte slice as base64

Status: Amends D39.

Ken decided this on 2026-09-27, and it amends D39. A JSON document has no
type for bytes, so bytes are stored as a base64 string, which is how json/v2
encodes a `[]byte` argument (D40). Nothing in a response marks such a string
([COUCHBASE.md](../COUCHBASE.md)).

When the value of a column is a JSON string and the destination is a
`*[]byte`, a `*sql.RawBytes` or a `*sql.Null[[]byte]`, the driver decodes the
string with the standard base64 encoding, with padding, which is the one
that json/v2 writes. If the string is not valid base64, the driver copies
the bytes of the string as they are, and returns no error. So a `[]byte`
makes a round trip, and text that is not base64 still scans.

Text that happens to be valid base64, such as `abcd`, is decoded too. That
is the cost of the rule, and a caller that wants the text scans into a
`*string`. A value that is not a string, such as an object or an array,
still scans into a `*[]byte` as its JSON text, as D39 says.
