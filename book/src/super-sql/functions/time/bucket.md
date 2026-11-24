# bucket

quantize a timestamp or duration value into buckets of equal time spans

## Synopsis

```
bucket(val: timestamp, span: duration) -> timestamp
bucket(val: duration, span: duration) -> duration
```

## Description

The `bucket` function quantizes a timestamp or duration `val`
into buckets that are equally spaced as specified by `span`.
Bucket boundaries are multiples of `span` counted from zero, which for a
timestamp is the Unix epoch, `1970-01-01T00:00:00Z`.  So a `1h` or `1d`
span starts each bucket on the hour or at midnight UTC, but a `7d` span
starts each bucket on a Thursday, the day of the week of the epoch.
If `span` is zero, `val` is returned unchanged.

## Examples

---

_Bucket a couple times to hour intervals_

```mdtest-spq
# spq
values bucket(this::timestamp, 1h)
# input
2020-05-26T15:27:47Z
"5/26/2020 3:27pm"
# expected output
2020-05-26T15:00:00Z
2020-05-26T15:00:00Z
```

---

_Count values in each hour_

```mdtest-spq
# spq
count() by hour:=bucket(ts, 1h) | sort hour
# input
{ts:2020-05-26T15:27:47Z}
{ts:2020-05-26T15:59:01Z}
{ts:2020-05-26T16:02:13Z}
# expected output
{hour:2020-05-26T15:00:00Z,count:2}
{hour:2020-05-26T16:00:00Z,count:1}
```

---

_Weekly buckets start on Thursdays_

```mdtest-spq {data-layout="stacked"}
# spq
values {ts, day:strftime("%A", ts), week:bucket(ts, 7d), week_day:strftime("%A", bucket(ts, 7d))}
# input
{ts:2024-07-28T23:00:00Z}
{ts:2024-07-30T10:00:00Z}
# expected output
{ts:2024-07-28T23:00:00Z,day:"Sunday",week:2024-07-25T00:00:00Z,week_day:"Thursday"}
{ts:2024-07-30T10:00:00Z,day:"Tuesday",week:2024-07-25T00:00:00Z,week_day:"Thursday"}
```

---

_Bucket durations into 15-minute spans_

```mdtest-spq
# spq
values bucket(this, 15m)
# input
1h7m
22m30s
14m59s
# expected output
1h
15m
0s
```

---

_Timestamps before 1970 round down to the start of their bucket too_

```mdtest-spq
# spq
values {ts, day:bucket(ts, 1d)}
# input
{ts:1969-07-20T20:17:40Z}
{ts:1969-12-31T23:30:00Z}
# expected output
{ts:1969-07-20T20:17:40Z,day:1969-07-20T00:00:00Z}
{ts:1969-12-31T23:30:00Z,day:1969-12-31T00:00:00Z}
```
