# D114. Only Couchbase gives JSON text

Status: Decided.

Ken decided on 2026-09-29 that this stays a difference of the Couchbase
driver. It gives the JSON text of an object or an array that is scanned
into a `*[]byte` or a `*jsontext.Value` (D39), and sends a `[]byte`
argument as base64 (D44), because the values of Couchbase are JSON, and it
stores bytes as base64 text.

The other drivers have real types for these values, and give none of this.
On 2026-09-29, no other driver took a `*jsontext.Value` or gave JSON text
for a `*[]byte`. A later driver whose values are JSON, and which has no
types of its own, can take the rule of Couchbase in a decision of its own.
