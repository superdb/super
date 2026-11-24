# Date/Times

The `timestamp` type represents a signed 64-bit number of nanoseconds since the
Unix epoch, i.e., an instant in UTC.

The `duration` type represents a signed 64-bit number of nanoseconds.

>[!NOTE]
> Other SQL date and time types (e.g., `date`, `time` for a time of day, and
> `interval`) are reserved for a future version of SuperSQL.  At that time,
> detailed syntax and semantics for these types will be documented here.
> Until then, a SQL `DATE` literal such as `DATE '2020-01-01'`, or a SQL cast
> like `CAST(x AS DATE)`, produces a `timestamp` at midnight UTC.
