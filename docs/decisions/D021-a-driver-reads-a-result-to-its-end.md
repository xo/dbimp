# D21. A driver reads a result to its end

Status: Decided.

Ken accepted this on 2026-09-27. A driver follows every page, cursor and next
link until the server says that the result is complete. It never returns a
result that the server cut short as if it were complete. Pinot adds `LIMIT 10`
to a selection that has no limit, and a driver that says nothing returns ten
rows. If a server cuts a result and gives no way to read the rest, the driver
returns an error.
