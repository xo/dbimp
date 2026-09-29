# D119. Databend reads nested values by the schema, and speaks JSON only

Status: Decided.

Ken decided these on 2026-09-29, at step 9 of the Databend driver:

- An `Array`, a `Map` and a `Tuple` arrive as text of SQL, such as
  `[1,NULL,3]`, `{"a":1,"b":NULL}` and `(1,"x")`, and not as JSON
  ([DATABEND.md](../DATABEND.md)). The driver parses that text by the type
  in `schema`, such as `Array(Int32 NULL)`, and gives decoded values: an
  `Array` is `[]any`, a `Map` is `map[string]any` for a key of `String` and
  `map[any]any` for any other key, and a `Tuple` is `[]any`. Each element
  has the Go type of D118. Text in place of a value is what hard rule 3
  forbids.
- A `Bitmap` arrives as the text `<bitmap binary>`, whatever
  `binary_output_format` and the result mode say, on both releases
  (measured). JSON carries none of its bytes, so reading a `Bitmap` value
  fails with an error that wraps `dbimp.ErrNotSupported` and says so. A NULL
  still reads as nil. Ken first asked for base64 kept as a string, as D44
  does for bytes in Couchbase, and chose this when the measurement showed
  that no bytes arrive. The Arrow item in [BACKLOG.md](../BACKLOG.md)
  checks whether Arrow carries them.
- The driver reads JSON only. Arrow, a binary encoding (D13), waits in
  [BACKLOG.md](../BACKLOG.md) at the lowest priority. The reader of the
  answer is shaped as the readers of SurrealDB are (D108): the rows code
  meets a concrete reader of each format through an interface of three
  methods or fewer, so that an Arrow reader can come later beside the JSON
  one, and the driver stays alike with SurrealDB.
