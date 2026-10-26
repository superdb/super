# Input

The `super` command and the `super db load` commands take input arguments.

A `super db -c` query can refer to HTTP inputs but not file-system paths
or stdin.

The inputs for the `super` command may be specified either
* within the query itself, e.g., using a
[from](../super-sql/operators/from.md) operator or a
SQL [FROM](../super-sql/sql/from.md) clause,
* as command-line arguments indicated by one more `<path>` parameters, or
* from standard input when the `<path>` argument is specified as dash (`-`).

The `super db load` command does not run a query and
takes input only as `<path>` arguments.

A `<path>` argument can be
* standard input or
* file-system path relative to the directory in which `super` runs, or
* HTTP, HTTPS, or S3 URLs.

Command-line `<path>` arguments are treated as if a
[from](../super-sql/operators/from.md) operator precedes
the provided query, e.g.,
```
super -c "FROM example.json | SELECT a,b,c"
```
is equivalent to
```
super -c "SELECT a,b,c" example.json
```
and both are equivalent to the classic SQL
```
super -c "SELECT a,b,c FROM example.json"
```
When multiple input files are specified, they are processed in the order given as
if the data were provided by a single, concatenated `FROM` clause.

If no input is specified,
the query is fed a single `null` value analogous to SQL's default
input of a single empty row of an unnamed table.  This provides a convenient means
to run standalone examples or compute results like a calculator, e.g.,
```mdtest-command
super -s -c '1+1'
```
is [shorthand](../super-sql/operators/intro.md#shortcuts)
for `values 1+1` and emits
```mdtest-output
2
```

## Format Detection

In general, `super` _just works_ when it comes to automatically inferring
the data formats of its inputs.

For files with a well known extension (like `.json`, `.parquet`, `.sup` etc.),
the format is implied by the extension.

For standard input and for HTTP, HTTPS, and S3 inputs whose path has
no well known extension (e.g., a REST API endpoint), `super` attempts
to detect the format by reading and parsing some of the data.

For other inputs, or
to override these format inference heuristics, `-i` may be used to specify
the input formats of all files or the `(format)` option of a data source
specified in a [from](../super-sql/operators/from.md) operator.
The `(format)` option overrides `-i`.

When `-i` is used, all of the input files must have the same format.
Without `-i`, each file format is determined independently so you can
mix and match input formats.

For example, suppose this content is in a file `sample.csv`:
```mdtest-input sample.csv
a,b
1,foo
2,bar
```
and this content is in `sample.json`
```mdtest-input sample.json
{"a":3,"b":"baz"}
```
then the command
```mdtest-command
super -s sample.csv sample.json
```
would produce this output in the default SUP format
```mdtest-output
{a:1.,b:"foo"}
{a:2.,b:"bar"}
{a:3,b:"baz"}
```
Note that the `line` format cannot be automatically detected and
requires `-i` or `(format line)` for reading.

>[!NOTE]
> Parquet and CSUP require a seekable input and cannot be operated upon
> when read on standard input.

## Type Checking

When possible, the compiler determines the
[fused type](../super-sql/type-fusion.md) of each input to statically
type check the query.  An input whose type isn't determined is
_dynamically typed_ and its type errors are found at run time.

By default, the compiler determines the type of an input for local file
when the format carries its type, i.e., BSUP, Parquet, and Arrow.
Other formats (e.g., JSON and CSV, or compressed files) are dynamically
typed by default.

The `-static` flag causes all inputs to be statically typed.  For formats
that don't carry types or for non-seekable inputs, the compiler reads an input
as needed to compute its fused type, then the query scans from memory
when it runs.

The `-dynamic` flag causes all inputs to be dynamically typed.

Database tables are statically typed unless `-dynamic` is given since the
database keeps the fused type of each table.
