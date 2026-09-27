# D18. A result that is not a table becomes rows by three rules

Status: Decided.

This answers Q3. Ken accepted it on 2026-09-27.

`database/sql` reads the columns before the first row, so the column set is
fixed at that point. A `dbmeta` model reads each row by position, so every
row must have the same columns. The three rules are these:

1. If the server sends column metadata, the columns are that metadata, in its
   order.
2. If the server sends only objects, the columns are the keys of the first
   object, in the order that they appear in the response. Read the keys with
   `jsontext.Decoder.ReadToken` (D25), and never decode a row into a map. A key
   that a later row lacks is nil. A key that only a later row has is an error,
   and never a new column.
3. If the query returns whole documents, nodes, paths or points with no
   projection, the result has one column that holds each value. The column
   has the name that the server gives it, and never the name of a wrapper.

The union of the fields of every row is never the column set, because it
needs the whole result before the first row. Each driver decides whether a
missing key and a JSON null are different, in a decision of its own, as
n1ql D24 did for Couchbase MISSING. The `dbmeta` session proposed these rules.
