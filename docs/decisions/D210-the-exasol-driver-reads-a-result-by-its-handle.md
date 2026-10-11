# D210. The Exasol driver reads a result by its handle

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 8, under D21.

A result of many rows has a `resultSetHandle`. The driver reads it with `fetch`, from the position of the next row, with `numBytes` of about one MiB. The key `fetchsize` sets it in KiB (D205). When `Rows.Close` runs, the driver sends `closeResultSet`, also after the last row.

A page of an answer holds one list of values for each column, so the driver keeps one decoder for each column and reads one value from each to make a row (D25). The first message of an `execute` holds up to 64 MiB, which the server decides, and the driver reads it whole.
