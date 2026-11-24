# now

the current time

## Synopsis

```
now() -> timestamp
```

## Description

The `now` function takes no arguments and returns the current UTC time as a value of type `timestamp`.

This is useful to timestamp events in a data pipeline, e.g.,
when generating errors that are marked with their time of occurrence:
```
switch
  case ... ( ... )
  default ( values error({ts:now(), ...}) )
```

## Examples

_Running this command_
```
super -s -c 'values now()'
```
_produces this value (as of the last time this document was updated)_
```
2026-11-23T22:20:16.539417Z
```
