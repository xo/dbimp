# D78. One InfluxDB driver, with the dialects influxdb and influxql

Status: Amended by D85.

Ken decided on 2026-09-28 the scope of the InfluxDB driver, which is part
of step 9 of [DRIVER.md](../DRIVER.md). Gemini and DeepSeek reviewed the
plan on the same day. [INFLUXDB.md](../INFLUXDB.md) holds the facts, and
none of them is measured yet.

## One driver and two dialects

One driver, `influxdb`, serves InfluxDB 1, InfluxDB 2, and InfluxDB 3 and
later, as flavors ("Flavors" in DRIVER.md). It speaks two languages, and
each one is a dialect:

- `influxdb` is SQL. The driver sends it to `/api/v3/query_sql`, which only
  InfluxDB 3 and later have. D77 says how the driver reads its result.
- `influxql` is InfluxQL. The driver sends it to `/query`, which InfluxDB 1
  has, InfluxDB 2 has through its v1 compatibility API, and InfluxDB 3 has
  too. The answer of `/query` names its columns, so rule 1 of D18 applies.
  InfluxDB 3 also has `/api/v3/query_influxql`, and the driver does not use
  it, because its answer names no column.

The driver does not speak Flux, although Flux meets S (D75). Flux is in
maintenance, and InfluxDB 3 does not have it.

`influxql` is a second dialect of the same product, in the way that
ScyllaDB is a flavor of the `cql` dialect in `dbmeta` (dbmeta D91). The two
languages differ, which ScyllaDB and Cassandra do not. So `dburl` and
`dbmeta` hold two dialects for the one driver name, and each owns how it
does that (D5). `dburl` holds them as two schemes (dburl D29). The scheme
`influxdb` has the driver and the dialect `influxdb`. The scheme `influxql`
has the driver and the dialect `influxql`, and its generator writes a DSN
whose scheme is `influxdb` and names the Go driver `influxdb`. So one
registered driver serves both schemes, and `usql` needs an entry for each
of the two.

## The key sqlmode

The key `sqlmode` of the DSN says how the driver chooses the language when
it connects. It works as `sslmode` does for PostgreSQL:

| `sqlmode` | What the driver does |
| --- | --- |
| `disable` | It speaks InfluxQL, and sends no request to learn the release. |
| `allow` | It speaks SQL, and sends no request to learn the release. A server without SQL answers with an error, and the driver relays it. |
| `prefer` | It sends `GET /ping` when it connects, and speaks SQL to InfluxDB 3 and later, and InfluxQL to InfluxDB 1 and InfluxDB 2. This is the default. |
| `require` | It sends `GET /ping` when it connects, and speaks SQL. The connection fails on InfluxDB 1 and InfluxDB 2. |

`GET /ping` answers on each release with the header `X-Influxdb-Version`,
which is `1.13.1`, `v2.9.1`, `3.9.13` or `3.11.5` (measured). InfluxDB 2
puts a `v` before the number. The driver sends it once for each new connection, and
keeps no cache, so a new connection sees a server that was upgraded. On
InfluxDB 3, `/ping` answers HTTP 401 without a token, as the Core API
reference says (measured on 3.9.13 and 3.11.5, D82).

With `disable` and `allow`, the driver sends no ping, so the key `version`
of the DSN, `1`, `2` or `3`, says which release it talks to, and so which
form of authentication it sends. Its default is `3`. The driver reads
`sqlmode` and `version`, and sends neither to the server.

## What the driver leaves to others

- The driver does not check a statement against the language or the
  release. A server that cannot run a statement answers with an error, and
  the driver relays it.
- The driver exports `Version`, which reads the release from `/ping`, and
  `Dialect`, which says which dialect the connection speaks. A caller calls
  each one inside `Conn.Raw`, as for SurrealDB (D57). So `usql` learns the
  release and the dialect of a connection without opening a second one.
- `usql` chooses the dialect that it prefers.
- The generator of the scheme `influxql` of `dburl` sets `sqlmode=disable`
  in the DSN, unless the URL names `sqlmode` itself (dburl D29). Ken decided
  there that `dburl` adds nothing else: no generator adds `sqlmode=prefer`
  or `version`. So the defaults of the driver, `prefer` and `3`, apply
  whenever the URL does not name the keys.

## The tests

The integration tests run the `influxql` dialect on InfluxDB 1, InfluxDB 2
and InfluxDB 3, and the `influxdb` dialect on InfluxDB 3 only. They test
each value of `sqlmode` on each release. On InfluxDB 2, `/query` needs a
mapping from the database and the retention policy to a bucket, called
DBRP (not measured), and the `Init` of its `dbrun` entry makes it.

## What is still open

D79 names the releases that the tests run. D82 names the URL of the DSN and
the authentication on each release. D80 and D81 say how each dialect reads
its result.
