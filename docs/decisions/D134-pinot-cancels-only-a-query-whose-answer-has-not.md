# D134. Pinot cancels only a query whose answer has not arrived

Status: Amends D133.

Ken decided this on 2026-09-30, at step 12 of the Apache Pinot driver.

D133 says that the driver cancels a query when its context ends, or when its
rows close before their end. The second case never finds a query to cancel.
The Broker builds the whole answer, and writes it only after the query ends
(the source, `BrokerResponse.java`, in [PINOT.md](../PINOT.md)). A client
that left a query after 2 seconds had no answer yet, and the query ran on
(measured). So once the rows exist, the query has ended on the server, and a
`DELETE /query/<id>?client=true` answers HTTP 404, as it did for an id that
the Broker does not know (recorded).

The decision:

- The driver sends the cancel of D133 when the context ends before the
  answer arrives. It sends nothing when the context ends while it reads the
  answer, or when the rows close before their end, because the query has
  ended.
- So the rows hold no context, and hard rule 4 of AGENTS.md needs no
  exception for Pinot, where Databend has one (D123).

The driver does this, in `pinot/conn.go` and `pinot/rows.go`, and
`TestCancel` and `TestIntegrationCancel` hold it.
