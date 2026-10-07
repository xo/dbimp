# D181. SELECT version() for databases with no query for it

Status: Decided.

`dbmeta` and `usql` read the release of a database with `SELECT version()`.
Some products give the release only over HTTP, so this repository adds the
statement for them. Ken decided it on 2026-10-08. It is the part of the
read-only SQL layer that D171 left for later, and it covers this one
statement.

1. A driver answers `SELECT version()` itself only when its product has no
   SQL or query that returns the release. A product that has such a query
   gets no support, even when its statement has another name or form. Trino
   gets none, because it answers `SELECT version()`. Presto gets it, because
   it has no such function. The driver for a product that answers the
   statement with a different meaning, such as InfluxDB 3, gets none, because
   the server answers it.
2. The driver reads the release from the HTTP endpoint or header that carries
   it. It does not use a new request when the answer of an earlier request of
   the connection already holds it.
3. The statement gives one row and one column. The column is named `version`
   and holds a string, such as `9.5.3`, as the product writes the release.
   The driver recognizes the statement by hand, with no parser: it ignores
   case, the white space around it and one final semicolon, and it accepts no
   argument and no other text. Any other statement goes to the product.
4. A user that the product refuses the release to gets the error of the
   product, as every other statement does, and the driver adds no other
   check. If the product gives the release to one kind of user only, such as
   an administrator, another user gets the error of the HTTP request.
5. The support is part of the driver, so it needs no option. It does not
   change `ExecContext`: only a query returns the row.
6. Shared code that more than one driver needs goes in the root package (D4).
   Each driver says in its document which statement it answers, from which
   endpoint, and for which user.
7. A statement option such as `WithDatabase` is ignored for this statement and
   is not checked. A statement that holds anything besides the recognized
   form, such as an argument, goes to the product.
8. The recognizer accepts any run of white space between `SELECT` and
   `version()`, and white space before the final semicolon. It accepts nothing
   else.
9. A driver can keep the release for the life of the object that cannot change
   it: the connector, when one connector serves one server and one release
   (the Presto flavor of Trino), or the connection (OpenSearch). SurrealDB
   reads the release with the RPC method `version`, because the driver has the
   code and a recording for it already.
10. The measurement of 2026-10-08 gave these results, and Ken decided them the
    same day:
    - Elasticsearch, OpenSearch, Solr, SurrealDB and the Presto flavor of
      Trino answer the statement.
    - Druid gets no support, because `SELECT server_type, version FROM
      sys.servers` returns the release. The ordinary user gets HTTP 403 from
      that query and from `GET /status`.
    - ArangoDB gets no support, because `RETURN VERSION()` returns the release
      to both users.
    - Pinot gets no support. Pinot has no SQL for the release, and only `GET
      /version` on the Controller carries it. The Controller is a second port
      that the DSN does not name, and the DSN gets no key for it.
    - Avatica gets no support, on either flavor. HSQLDB answers `VALUES
      (DATABASE_VERSION())`, and Phoenix has no SQL for the release, but the
      driver does not send `databaseProperties`, which gives it.
