# strftime

format timestamp values

## Synopsis

```
strftime(format: string, t: timestamp) -> string
```

## Description

The `strftime` function returns a string representation of timestamp `t`
as specified by the provided string `format`. `format` is a string
containing format directives that dictate how the time string is
formatted.

These directives are supported:

| Directive | Explanation | Example |
|-----------|-------------|---------|
| %A | Weekday as full name | Sunday, Monday, ..., Saturday |
| %a | Weekday as abbreviated name | Sun, Mon, ..., Sat |
| %B | Month as full name | January, February, ..., December |
| %b | Month as abbreviated name | Jan, Feb, ..., Dec |
| %C | Century number (year / 100) as a 2-digit integer | 20 |
| %c | Date and time representation | Tue Jul 30 14:30:15 2024 |
| %D | Equivalent to `%m/%d/%y` | 07/30/24 |
| %d | Day of the month as a zero-padded decimal number | 01, 02, ..., 31 |
| %e | Day of the month as a decimal number (1-31); single digits are preceded by a blank | 1, 2, ..., 31 |
| %F | Equivalent to `%Y-%m-%d` | 2024-07-30 |
| %H | Hour (24-hour clock) as a zero-padded decimal number | 00, 01, ..., 23 |
| %h | Equivalent to `%b` | Jan, Feb, ..., Dec |
| %I | Hour (12-hour clock) as a zero-padded decimal number | 01, 02, ..., 12 |
| %j | Day of the year as a zero-padded decimal number | 001, 002, ..., 366 |
| %k | Hour (24-hour clock) as a decimal number; single digits are preceded by a blank | 0, 1, ..., 23 |
| %l | Hour (12-hour clock) as a decimal number; single digits are preceded by a blank | 1, 2, ..., 12 |
| %M | Minute as a zero-padded decimal number | 00, 01, ..., 59 |
| %m | Month as a zero-padded decimal number | 01, 02, ..., 12 |
| %n | Newline character | \n |
| %p | "ante meridiem" (a.m.) or "post meridiem" (p.m.) | AM, PM |
| %R | Equivalent to `%H:%M` | 18:49 |
| %r | Equivalent to `%I:%M:%S %p` | 06:50:58 PM |
| %S | Second as a zero-padded decimal number | 00, 01, ..., 59 |
| %T | Equivalent to `%H:%M:%S` | 18:50:58 |
| %t | Tab character | \t |
| %U | Week number of the year (Sunday as the first day of the week) | 00, 01, ..., 53 |
| %u | Weekday as a decimal number, range 1 to 7, with Monday being 1 | 1, 2, ..., 7 |
| %V | ISO 8601 week number of the year (Monday as the first day of the week) as a decimal number (01-53) | 01, 02, ..., 53 |
| %v | Equivalent to `%e-%b-%Y` | 31-Jul-2024 |
| %W | Week number of the year (Monday as the first day of the week) | 00, 01, ..., 53 |
| %w | Weekday as a decimal number, range 0 to 6, with Sunday being 0 | 0, 1, ..., 6 |
| %X | Time representation | 14:30:15 |
| %x | Date representation | 07/30/24 |
| %Y | Year with century as a decimal number | 2024 |
| %y | Year without century as a decimal number | 24, 23 |
| %Z | Timezone name | UTC |
| %z | +hhmm or -hhmm numeric timezone (that is, the hour and minute offset from UTC) | +0000 |
| %% | A literal '%' character | % |

The timestamp is always formatted in UTC, and `%c`, `%x`, and `%X` always
use the formats shown, whatever the locale.  There is no directive for
fractional seconds.

## Examples

---

_Print the year number as a string_

```mdtest-spq
# spq
strftime("%Y", this)
# input
2024-07-30T20:05:15.118252Z
# expected output
"2024"
```

---

_Print a date in European format with slashes_

```mdtest-spq
# spq
strftime("%d/%m/%Y", this)
# input
2024-07-30T20:05:15.118252Z
# expected output
"30/07/2024"
```

---

_Print a time of day with a 12-hour clock_

```mdtest-spq
# spq
strftime("%I:%M %p", this)
# input
2024-07-30T20:05:15.118252Z
# expected output
"08:05 PM"
```
