# D160. Avatica keeps the calendar of the server and binds no array

Status: Decided.

Ken decided this on 2026-10-01, after step 14 of the Avatica driver. He
answered open questions 1 and 2 of [AVATICA.md](../AVATICA.md) with no.

- The driver does not convert a `DATE` before 1582-10-15. The server sends
  the days of the Julian calendar of Java, and the driver reads them in the
  Gregorian calendar, so `DATE '0001-01-01'` reads as 0000-12-30 (AVATICA.md,
  Types).
- The driver binds no `ARRAY` and no `INTERVAL` as an argument. A `[]any`
  fails, and a `dbimp.Interval` goes as its text of ISO 8601, which HSQLDB
  refuses. A statement can make either one in SQL, as
  `avatica/features_integration_test.go` does with `SEQUENCE_ARRAY` and
  `CAST(? AS INTERVAL DAY TO SECOND)`.
