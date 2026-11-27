# Zeek

SuperDB includes functionality and reference configurations specific to working
with logs from the [Zeek](https://zeek.org/) open source network security
monitoring tool.

Zeek events can be managed and searched nicely by SuperDB because:
* Zeek's [type system](types.md) is compatible with SuperDB;
* Zeek's [TSV format](logs.md) is readable by SuperDB so these logs
can be ingested and searched in the super formats; and
* Zeek's [JSON format](logs.md), while losing the type-richness of
  Zeek's TSV format, can be turned back into richly typed data
  with data-shaping logic defined as SuperSQL scripts.
