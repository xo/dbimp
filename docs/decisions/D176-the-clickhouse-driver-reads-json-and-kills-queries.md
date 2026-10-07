# D176. The ClickHouse driver reads JSON and kills queries

Status: Decided.

These are five items of step 9 of [DRIVER.md](../DRIVER.md) for ClickHouse
(W32 and D174). Each fact is in [CLICKHOUSE.md](../CLICKHOUSE.md). Ken decided
them on 2026-10-07. The other items of step 9 wait for Ken: the Go types of
step 8a, the DSN, transactions, redirects and credentials, and the flavors.

1. The driver reads `JSONCompactEachRowWithNamesAndTypes` with
   `encoding/json/v2` and `encoding/json/jsontext`, one token at a time. It
   sends these settings on every request:
   `output_format_json_quote_64bit_integers=0`,
   `output_format_json_quote_denormals=1`, `date_time_output_format=iso`,
   `output_format_json_named_tuples_as_objects=0` and
   `http_write_exception_in_output_format=0`. The text allows invalid UTF-8,
   and the driver keeps each number as text until it knows the type of the
   column. The driver reads no binary encoding, so it needs no approval under
   D13. The value of a `Variant` has no member type, a `Dynamic` and the paths
   of a `JSON` have no type, an instant has no zone, and an
   `AggregateFunction` has no value. `RowBinaryWithNamesAndTypes` can come
   later behind the same Go types if a caller needs it.
2. A parameter is a typed parameter of the server. The driver writes
   `{pN:Type}` in the statement and sends `param_pN` in the query string,
   with the type chosen from the Go type of the argument. The driver does not
   write an argument as a literal.
3. The driver reads an error after rows from the marker in the stream.
   `http_write_exception_in_output_format=0` makes the server write the text
   `__exception__` on 25.3 and 25.8, and the tagged trailer on 26.9, which
   the header `X-ClickHouse-Exception-Tag` names. A status other than 200 is
   an error, even when rows came first. A result that ends with no marker and
   no end of its rows is cut short, and the error wraps `dbimp.ErrIncomplete`.
   The driver does not set `wait_end_of_query`, so a result still streams.
4. The driver sends a `query_id` with each statement. When the context ends,
   and when the caller closes the rows before the end, the driver sends `KILL
   QUERY` for that `query_id`. It does not rely on the disconnect, which does
   not stop a query on 26.9.
5. The driver passes `UPDATE` to the server as the caller wrote it. A server
   that refuses it answers with its error, and the error reaches the caller.
   CLICKHOUSE.md says that `ALTER TABLE ... UPDATE` works on each release and
   runs in the background unless the caller sets `mutations_sync=2`.
   `RowsAffected` is not known for any statement, because
   `X-ClickHouse-Summary` holds 0 for an update and a delete.
