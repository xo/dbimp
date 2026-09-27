# D70. A SurrealDB RecordID writes its SurrealQL form as text

Status: Proposed. Amends D53.

This amends D53. The `dbmeta` session found the fault through `usql` at
commit `54c12a4`, on 3.3.0. A record id in its own column prints as
`author:ursula`, because `tblfmt` calls its `String` method. A record id
inside an array prints as `{"Table": "book", "ID": "earthsea"}`. `tblfmt`
writes an array as JSON, and JSON writes the fields of the struct.

`RecordID` gets the method `MarshalText`, which writes the text of `String`.
Then `encoding/json` and `encoding/json/v2` write a record id as a string,
such as `"book:earthsea"`, inside an array or an object.
`TestRecordIDText` holds it.

The CBOR encoder of the driver finds a `RecordID` by its type first, so an
argument keeps tag 8 (D53). With `encoding=json`, a `RecordID` argument was
an object with the keys `Table` and `ID`, and it is now a string. The server
reads neither form as a record id, because JSON has no form for one (D49).

`RecordID` gets no `UnmarshalText`. It needs a parser for the key of a
record id, which can be an array or an object, and no caller asked for it.
A `time.Duration` inside an array still prints as a number of nanoseconds.
The standard library owns that type, so the driver cannot give it a text
form.
