# Formats

This section defines the [super data model](model.md), which is SuperDB's
implementation of the [algebraic data model](../adm.md) (ADM), along with
the two formats that serialize it:

* [Super (SUP)](sup.md) is a human-readable text format.  It is a strict
  superset of JSON syntax, so every JSON document is also a SUP value.
* [Super Binary (BSUP)](bsup.md) is an efficient binary format for a
  sequence of arbitrarily-typed values, in row form, a bit like
  [Parquet's Variant encoding](https://parquet.apache.org/docs/file-format/types/variantencoding/),
  or in columnar form, somewhat like
  [Parquet](https://parquet.apache.org/),
  [ORC](https://orc.apache.org/), or
  [Arrow](https://arrow.apache.org/).

Both formats are self-describing: the types travel with the values, so
there is no schema to agree on before data can be written or read.

And because both formats implement the same data model, converting between
the human-readable form and the binary row or columnar form loses no
information.  A value has the same type whichever format it is in.
