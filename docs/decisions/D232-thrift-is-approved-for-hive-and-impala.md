# D232. Thrift is approved for Hive and Impala

Status: Decided on 2026-11-09.

Ken approved on 2026-11-09 a binary encoding for Apache Hive and Apache Impala under D13. The
encoding is the Thrift binary protocol, which HiveServer2 speaks. Hive takes it over HTTP when
`hive.server2.transport.mode` is `http`. Impala takes it over HTTP on its own port.

The encoder and the decoder are written in this repository with the standard library, so no package
is added. `github.com/apache/thrift` is not a dependency. The code goes in the root package when a
second driver needs it, as D49 did for CBOR. One codec serves both products, because both speak the
`TCLIService` of HiveServer2. The driver needs about eight calls: `OpenSession`, `ExecuteStatement`,
`GetOperationStatus`, `FetchResults`, `GetResultSetMetadata`, `CancelOperation`, `CloseOperation` and
`CloseSession`.

This decision does not settle the name of the package, the name that it registers, the DSN, or the
flavors. Those are items of step 9 of [DRIVER.md](../DRIVER.md) and are open questions at the end of
[HIVE.md](../HIVE.md). Ken must decide them.
