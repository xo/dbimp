# D108. Every wire format meets the driver at the row

Status: Decided.

Ken decided this on 2026-09-29, for every driver now and later, after
Gemini and DeepSeek reviewed his first idea and each other's designs. His
first idea was one `Encoder` and one `Decoder` interface that JSON, CBOR and
each later format implement, perhaps with `ReadFrom(*bufio.Reader)` and
`WriteTo(*bufio.Writer)`. Both models advised against a shared interface at
the level of the token. It costs a call through an interface for each token
of a large result, which blocks inlining, and each JSON driver walks an
envelope of its own anyway: the signature of Couchbase, the series of
InfluxQL, the cursor of ArangoDB and the typed JSON of Neo4j. Arrow,
protobuf and CSV are not trees of tokens at all.

The path:

- The shared contract is the row: `driver.Rows`, which has three methods.
  Each driver reads its answer into rows in its own package.
- Each wire format has a concrete decoder, and a driver calls it directly.
  JSON is `jsontext.Decoder`. CBOR is `dbimp.CBORDecoder`. A later format,
  such as VelocyPack, gets a concrete decoder in the root package when a
  driver needs it (D4 and D13).
- A decoder takes an `io.Reader`, and wraps it in a `bufio.Reader` unless it
  is one already, as `dbimp.NewCBORDecoder` does.
- No type implements `io.ReaderFrom` or `io.WriterTo`, because their
  contract is to read or write to the end, and a result streams (D25).
- An encoder stays concrete, such as `jsontext.Encoder` or
  `dbimp.CBOREncoder`, because a request body is small, and its form is the
  form of its product.
- The rules of D18 for the shape of a row are concrete helpers of each
  format, such as `dbimp.ObjectRows` for JSON, and not an interface.
- A driver that speaks two formats has a concrete reader for each. Its rows
  code meets them through an interface of three methods or fewer, defined
  where it is consumed (D6). SurrealDB is the first: `cborSets` and
  `jsonSets` read the result sets, and each walks the answer with its own
  decoder.
