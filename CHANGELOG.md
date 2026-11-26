# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.0] - 2026-11-25

### Highlights

- **The algebraic data model and a blog:** The docs now open with a chapter on [the algebraic data model](https://superdb.org/adm.html) behind SuperDB, and a new [blog](https://superdb.org/blog/intro.html) starts with posts on the sum type hiding in GitHub Archive JSON and on JSON's map-or-record problem.
- **Faster aggregations:** For faster performance, aggregations and `distinct` now use a vectorized hash table, and scans of BSUP files read less data for queries that use only a value's type, a type assertion, or some of a record's fields.
- **AI agents via MCP:** `super db mcp` and `super db serve -mcp` let AI agents describe, query, and load a database through the Model Context Protocol.
- **Working with sum types:** Type assertions (e.g., `x.(int64) ?? 0`), `match` expressions on a value's type, functions that construct types, and type narrowing in the type checker make values of union types easier to work with.
- **Consistent comparisons:** Comparisons are now type-checked by the same rules at compile time and at run time, and values of every type have an order.

### **BREAKING CHANGES**

Users of earlier releases should expect to regenerate any saved binary files and databases from their original sources, and to update some queries. If you have valuable saved data you can't easily regenerate, please speak up on [community Slack](https://www.brimdata.io/join-slack/) or [open an issue](https://github.com/superdb/super/issues) for assistance.

- BSUP files and databases written by earlier releases, or by builds of `main` since v0.4.0, can't be read by this release and fail with a `version mismatch` error. The BSUP format has changed for faster type handling, and the database version has advanced with it ([#7605], [#7609], [#7619], [#7726], [#7727]).
- Comparisons are no longer polymorphic. Comparing values of different types (e.g., `1 == "a"`, which was `false`) now converts the operands when possible and is otherwise an error, reported by the compiler when the types are known and as an error value at run time otherwise. The `in` operator now reports the same errors at run time as at compile time. `compare()` still compares values of any types ([#7663], [#7667]).
- Static type checking is stricter. These are now compile errors: arithmetic on a union with a non-numeric type (e.g., `x+1` where `x` is `int64|string`; narrow it first with a type assertion like `x.(int64) ?? 0` or a filter like `where is(x, <int64>)`), a reference inside `ok()` to a field that no input can have, and `LIKE` on a value that isn't a string (e.g., `bytes`, where it was silently `false`) ([#7652], [#7670], [#7698]).
- After an outer join, the side that may be unmatched is now typed as possibly missing, so a bare reference such as `right.color` is a compile error where it was `error("no such field right")` at run time. Use `ok()` or `?.`, which now turns a missing field into `none`, e.g., `this?.right?.color ?? "unpainted"` ([#7704]).
- `unnest` no longer treats a two-field record specially, so `unnest {id,a}` is now an error. Use `unnest a with id`, which puts the element first (`{a:1,id:1}`), or name the element with `:=`, as in `unnest item:=a with id` ([#7600]).
- `grep()` now always treats its pattern as a regular expression, so a pattern without metacharacters is now case-sensitive, where it was a case-insensitive substring search (e.g., `grep("BAR", "foobar")` is now `false`). Start the pattern with `(?i)` to ignore case. Search expressions such as `search BAR` are unchanged ([#7723]).
- The unnamed columns of SQL `VALUES` rows and tuples are now named `column1`, `column2`, and so on, as in PostgreSQL, where they were `c0`, `c1` (e.g., `values (1,2)` is now `{column1:1,column2:2}`) ([#7669]).

### Added

- Model Context Protocol (MCP) support for AI agents: `super db mcp` runs an MCP server over standard input and output, and `super db serve -mcp` adds an MCP endpoint to a running service (see also `-mcp.readonly` and `-mcp.include`). Its tools list and describe tables, run queries, and load data, including CSV with a `delim` argument ([#7602], [#7677]).
- Type assertions: `e.(T)` is `e` if it has type `T` and `none` otherwise, e.g., `x.(int64) ?? 0`, or `x.(Push)?.ref` to pick a member of a sum type ([#7592]).
- `match` expressions select a result by the type of a value, e.g., `match this when int64 "int" when string "str" else "other"` ([#7595], [#7604]).
- New functions that construct types from other types: `array_type()`, `enum_type()`, `error_type()`, `fusion_type()`, `map_type()`, `named_type()`, `option_type()`, `record_type()`, `set_type()`, and `union_type()`, e.g., `array_type(<int64>)` is `<[int64]>` ([#7579]).
- The type checker now narrows types after a filter, e.g., after `where is(x, <int64>)`, `x` is treated as an `int64` ([#7629]).
- New `supertypeof()` function, which returns a value's type including any fusion types, which `typeof()` leaves out ([#7577]).
- New `has()` function, which tests whether a record (or record type) has a field or a map has a key ([#7635]).
- Records can now be cast to maps (e.g., `map{string:any}`) and maps to records ([#7591]).
- Materialized subqueries: `let q = materialized (...)`, `with t as materialized (...)`, and `values materialized (...)` run their query once and reuse its output ([#7640]).
- The `output` operator can now end a subquery (including the body of `unnest ... into`), sending its values to the query's output ([#7639]).
- `from` with a computed (f-string) URL now accepts the `method`, `headers`, and `body` options, and when `from` has a parent, these options are evaluated for each input value ([#7624]).
- New `delim` option on `from` for CSV sources, e.g., `from data.csv (delim ';')`, and on SQL `FROM` ([#7675]).
- `super -c <query> -- -` and `super db -c <query> -` read the lines of standard input into the `args` constant ([#7574]).

### Changed

- A `none` value (e.g., from `ok()` or `?.`) is now dropped from query output, and a pipe subquery that produces no values is now `none` rather than `null`, so it disappears from a record, array, or map (e.g., `{s:(unnest a | where this > 1)}` is `{}` where it was `{s:null}`). SQL subqueries still give `null` ([#7584], [#7635]).
- `optionize()` now treats a field whose type includes `null` like a missing field (e.g., `{y?:string}` rather than `{y?:string|null}`), and casting `null` to an option type that doesn't admit `null` now gives `none` ([#7590]).
- Accessing a field of a `null` record now gives `null` rather than an error, and the fields of a nullable record (e.g., `null|{b:int32}`) can now be accessed ([#7613], [#7617]).
- The comparison operators `<`, `<=`, `>`, and `>=` now order values of every type, including `bool` (`false < true`), `net`, arrays, sets, maps, records, enums, and type values, and NaN now sorts after all other numbers in comparisons, `compare()`, and `sort` ([#7664], [#7665], [#7674], [#7685], [#7687], [#7689], [#7690], [#7706], [#7707], [#7709], [#7711]).
- `under()` of a value of a named union type now removes only the name, so the value keeps its union type (e.g., `under(1::U)` is `1::(int64|string)` where it was `1`) ([#7620]).
- JSON input is now batched by size as well as by count, with a batch ending at 16 MB of input by default (set with the new `-json.inputcapbytes` flag), so inputs with large values use much less memory ([#7701]).

### Fixed

- Fix `distinct` returning duplicate values when it ran in parallel (e.g., on BSUP files) ([#7589]).
- Fix `switch` sending a value down a case whose result was `null` or not a Boolean, and a related panic ([#7632]).
- Fix filters after an outer join returning extra rows, e.g., `LEFT JOIN ... WHERE p.color='red'` returned every left row. An outer join is now turned into an inner join when such a filter can't be true for unmatched values, so the filter is still pushed down ([#7703], [#7705]).
- Fix subtracting two timestamps, which gave a timestamp rather than a duration ([#7593]).
- Fix `bucket()` returning constant values unchanged (e.g., timestamps in a BSUP frame that all have the same value, or SQL `CAST(... AS DATE)` of a constant), and rounding up rather than down before 1970 and for negative durations ([#7713], [#7716]).
- Fix `position()` counting bytes rather than characters ([#7720]).
- Fix search expressions and `grep()` matching the wrong values inside arrays, sets, and maps after a filter, `search` panics on nulls and on unions inside errors, and `has_error()` panics and wrong results after a filter ([#7644], [#7645], [#7646]).
- Fix the JSON reader splitting a number that straddled a 512 KiB buffer boundary, and typing an empty array as `[missing]` when the same field is a non-empty array elsewhere. Fix JSON output panicking on infinite and NaN floats, which are now written as the strings `"+Inf"`, `"-Inf"`, and `"NaN"` ([#7578], [#7724], [#7725]).
- Fix reading BSUP frames larger than 100 MB, a race that could panic with `type "foo" redefined` or give wrong values when a query reads the same BSUP file more than once, and lost type names and panics when projecting fields from a union of named record types ([#7598], [#7642], [#7651]).
- Fix `super db` panics loading values of a named record type into a sorted table and compacting a table of mixed types, a crash when filtering a table with more than one data object, and the service's `/compile` endpoint ([#7661], [#7686], [#7699]).
- Fix `unnest` of an array expression whose elements all disappear (e.g., spreads of non-arrays), and panics after `unnest` or `values` over values of mixed types ([#7612], [#7672]).
- Fix casting `none` to a named type, which gave a wrong value (e.g., `-128::named`), and the field name derived from a `?.` expression (e.g., `put o?.y` added a field named `"o?.y"`) ([#7601], [#7676]).
- Fix SQL queries dropping columns when the input mixes a named record type with other record types ([#7603]).
- Fix writing Parquet when a field from `fuse` is `null` wherever it's present, which failed with `unsupported type: not implemented: support for DENSE_UNION` ([#7694]).
- Fix a leading UTF-8 byte order mark (as written by Excel and Windows Notepad) ending up in the first CSV or TSV column name, or making JSON, SUP, and Zeek input fail to parse or be auto-detected. It's now skipped ([#7668]).
- Fix subqueries over a computed `from` (e.g., `[from f"{this}"]`) returning nothing or hanging after an earlier value hit an error ([#7662]).
- Fix a record spread of a union value, which contributed no fields ([#7719]).
- Fix panics: in `substring()`; when a user function calls an unknown function; when a recursive function is used as an operator (now an error asking for `values` or `where`); when indexing gives an error (e.g., `[1][2]`); in a conditional expression over database data; and in `from` with a computed path over mixed-type input ([#7583], [#7627], [#7658], [#7659], [#7660], [#7678]).

### Performance

- Faster aggregations and `distinct`, using a new vectorized hash table ([#7607], [#7631], [#7643], [#7648], [#7650], [#7654], [#7655], [#7656]).
- Scans of BSUP files read only types, not values, for queries that use only a value's type (e.g., `typeof()` or `kind()`), and read only the selected member of a union for a type assertion ([#7653], [#7657], [#7683]).
- Fields of a `values` record that nothing downstream uses are no longer read from files ([#7587], [#7673]).
- Faster `in` with a constant list, and faster comparisons of dictionary-encoded columns with constants ([#7679], [#7680], [#7696], [#7721]).
- Faster field access through unions with `missing`, as in fused JSON data ([#7708]).
- Faster queries over values of mixed types and unions ([#7691], [#7692], [#7693], [#7717]).
- Faster conditional (`?:`) expressions, notably in recursive functions, and less memory used by `any()` ([#7586], [#7596]).

[#7574]: https://github.com/superdb/super/pull/7574
[#7577]: https://github.com/superdb/super/pull/7577
[#7578]: https://github.com/superdb/super/pull/7578
[#7579]: https://github.com/superdb/super/pull/7579
[#7583]: https://github.com/superdb/super/pull/7583
[#7584]: https://github.com/superdb/super/pull/7584
[#7586]: https://github.com/superdb/super/pull/7586
[#7587]: https://github.com/superdb/super/pull/7587
[#7589]: https://github.com/superdb/super/pull/7589
[#7590]: https://github.com/superdb/super/pull/7590
[#7591]: https://github.com/superdb/super/pull/7591
[#7592]: https://github.com/superdb/super/pull/7592
[#7593]: https://github.com/superdb/super/pull/7593
[#7595]: https://github.com/superdb/super/pull/7595
[#7596]: https://github.com/superdb/super/pull/7596
[#7598]: https://github.com/superdb/super/pull/7598
[#7600]: https://github.com/superdb/super/pull/7600
[#7601]: https://github.com/superdb/super/pull/7601
[#7602]: https://github.com/superdb/super/pull/7602
[#7603]: https://github.com/superdb/super/pull/7603
[#7604]: https://github.com/superdb/super/pull/7604
[#7605]: https://github.com/superdb/super/pull/7605
[#7607]: https://github.com/superdb/super/pull/7607
[#7609]: https://github.com/superdb/super/pull/7609
[#7612]: https://github.com/superdb/super/pull/7612
[#7613]: https://github.com/superdb/super/pull/7613
[#7617]: https://github.com/superdb/super/pull/7617
[#7619]: https://github.com/superdb/super/pull/7619
[#7620]: https://github.com/superdb/super/pull/7620
[#7624]: https://github.com/superdb/super/pull/7624
[#7627]: https://github.com/superdb/super/pull/7627
[#7629]: https://github.com/superdb/super/pull/7629
[#7631]: https://github.com/superdb/super/pull/7631
[#7632]: https://github.com/superdb/super/pull/7632
[#7635]: https://github.com/superdb/super/pull/7635
[#7639]: https://github.com/superdb/super/pull/7639
[#7640]: https://github.com/superdb/super/pull/7640
[#7642]: https://github.com/superdb/super/pull/7642
[#7643]: https://github.com/superdb/super/pull/7643
[#7644]: https://github.com/superdb/super/pull/7644
[#7645]: https://github.com/superdb/super/pull/7645
[#7646]: https://github.com/superdb/super/pull/7646
[#7648]: https://github.com/superdb/super/pull/7648
[#7650]: https://github.com/superdb/super/pull/7650
[#7651]: https://github.com/superdb/super/pull/7651
[#7652]: https://github.com/superdb/super/pull/7652
[#7653]: https://github.com/superdb/super/pull/7653
[#7654]: https://github.com/superdb/super/pull/7654
[#7655]: https://github.com/superdb/super/pull/7655
[#7656]: https://github.com/superdb/super/pull/7656
[#7657]: https://github.com/superdb/super/pull/7657
[#7658]: https://github.com/superdb/super/pull/7658
[#7659]: https://github.com/superdb/super/pull/7659
[#7660]: https://github.com/superdb/super/pull/7660
[#7661]: https://github.com/superdb/super/pull/7661
[#7662]: https://github.com/superdb/super/pull/7662
[#7663]: https://github.com/superdb/super/pull/7663
[#7664]: https://github.com/superdb/super/pull/7664
[#7665]: https://github.com/superdb/super/pull/7665
[#7667]: https://github.com/superdb/super/pull/7667
[#7668]: https://github.com/superdb/super/pull/7668
[#7669]: https://github.com/superdb/super/pull/7669
[#7670]: https://github.com/superdb/super/pull/7670
[#7672]: https://github.com/superdb/super/pull/7672
[#7673]: https://github.com/superdb/super/pull/7673
[#7674]: https://github.com/superdb/super/pull/7674
[#7675]: https://github.com/superdb/super/pull/7675
[#7676]: https://github.com/superdb/super/pull/7676
[#7677]: https://github.com/superdb/super/pull/7677
[#7678]: https://github.com/superdb/super/pull/7678
[#7679]: https://github.com/superdb/super/pull/7679
[#7680]: https://github.com/superdb/super/pull/7680
[#7683]: https://github.com/superdb/super/pull/7683
[#7685]: https://github.com/superdb/super/pull/7685
[#7686]: https://github.com/superdb/super/pull/7686
[#7687]: https://github.com/superdb/super/pull/7687
[#7689]: https://github.com/superdb/super/pull/7689
[#7690]: https://github.com/superdb/super/pull/7690
[#7691]: https://github.com/superdb/super/pull/7691
[#7692]: https://github.com/superdb/super/pull/7692
[#7693]: https://github.com/superdb/super/pull/7693
[#7694]: https://github.com/superdb/super/pull/7694
[#7696]: https://github.com/superdb/super/pull/7696
[#7698]: https://github.com/superdb/super/pull/7698
[#7699]: https://github.com/superdb/super/pull/7699
[#7701]: https://github.com/superdb/super/pull/7701
[#7703]: https://github.com/superdb/super/pull/7703
[#7704]: https://github.com/superdb/super/pull/7704
[#7705]: https://github.com/superdb/super/pull/7705
[#7706]: https://github.com/superdb/super/pull/7706
[#7707]: https://github.com/superdb/super/pull/7707
[#7708]: https://github.com/superdb/super/pull/7708
[#7709]: https://github.com/superdb/super/pull/7709
[#7711]: https://github.com/superdb/super/pull/7711
[#7713]: https://github.com/superdb/super/pull/7713
[#7716]: https://github.com/superdb/super/pull/7716
[#7717]: https://github.com/superdb/super/pull/7717
[#7719]: https://github.com/superdb/super/pull/7719
[#7720]: https://github.com/superdb/super/pull/7720
[#7721]: https://github.com/superdb/super/pull/7721
[#7723]: https://github.com/superdb/super/pull/7723
[#7724]: https://github.com/superdb/super/pull/7724
[#7725]: https://github.com/superdb/super/pull/7725
[#7726]: https://github.com/superdb/super/pull/7726
[#7727]: https://github.com/superdb/super/pull/7727

## [0.4.0] - 2026-10-31

### Highlights

- **Vector runtime only:** For faster performance, all queries now run in the vector runtime, and many functions and operators gained native vector implementations.
- **Unified binary format:** The row-based BSUP and column-based CSUP formats are merged into a single BSUP format that can hold both. Files and databases from earlier releases are not readable by this release (see below).
- **Persistent database revived:** Tables (formerly pools) can now be created and loaded with SQL `CREATE TABLE` and `INSERT INTO`, including typed tables that check every value loaded into them, and `CREATE SUPER TABLE` makes a table of any SuperSQL type, such as a sum type computed from the data. `attach`/`detach` select a database, tables can be unsorted, and queries against them are type-checked.
- **Option types and none values** replace `error("missing")`, with new `??` and `?.` operators and `ok()`/`is_ok()` functions.
- **Stronger type checking:** Static type checking of inputs by default where possible, and strict checking of unions.
- **Formats from file extensions:** The format of a local file is now determined by its extension rather than auto-detected (`-i auto` still requests auto-detection). Likewise, the output format now follows the extension given to `-o`.

### **BREAKING CHANGES**

Users of earlier releases should expect to regenerate any saved binary files and databases from their original sources, and to update some queries. If you have valuable saved data you can't easily regenerate, please speak up on [community Slack](https://www.brimdata.io/join-slack/) or [open an issue](https://github.com/superdb/super/issues) for assistance.

- The binary formats have been unified. The column-oriented format formerly called CSUP is now called BSUP, and the row-oriented format formerly called BSUP lives on as row frames inside the new BSUP, which can mix row and column frames in one file. Binary output is now column-oriented by default (`-rows` writes row frames). `super` cannot read BSUP or CSUP files written by earlier releases, and the `csup` format name and the `-bsup.*` flags are gone ([#6844], [#6994], [#7050], [#7145], [#7181], [#7289], [#7361], [#7389], [#7394], [#7399], [#7434], [#7573]).
- Databases created by earlier releases cannot be opened. Data objects and metadata are now stored in the new BSUP format, and object and commit IDs are now UUIDv7 rather than KSUID ([#7182], [#7401], [#7424], [#7425], [#7427], [#7430], [#7573]).
- Database pools are now called _tables_ in commands, flags, and queries ([#7472]).
- `super db auth` and `super db login` are replaced by `super db attach` and `super db detach`, and the `SUPER_DB` environment variable has been removed. Commands use the attached database unless `-attach` is given ([#7467], [#7511], [#7519]).
- The `super db vector` commands have been removed ([#7395]).
- The vector runtime is now the only runtime. The `-sam` and `-vam` flags and the `SUPER_RUNTIME` environment variable have been removed ([#7072]).
- The format of a local file is now determined from its extension (including `.gz`-compressed files) or from `-i`. Auto-detection is now the default only for standard input and for HTTP, HTTPS, and S3 URLs without a recognized extension, so a local file with a missing or unrecognized extension now requires `-i`, which can be `-i auto` to request auto-detection (or `format auto` on a `from` operator) ([#7501], [#7514], [#7520], [#7531]).
- The `-samplesize` flag has been removed. BSUP, Parquet, Arrow, and SUP files are now statically type-checked by default, `-static` extends this to all inputs, and `-dynamic` turns it off. Type checking is now strict about unions and about fields that may be missing, so a query over heterogeneous data may need `ok()` where it previously ran. For example, `values y` over a statically typed input is now an error ("field may be missing (consider ok())") if `y` is absent from some values ([#7292], [#7312], [#7528], [#7539], [#7541], [#7564]).
- Referencing a missing field now produces a "no such field" error rather than `error("missing")`. The `has()` and `missing()` functions are replaced by `ok()` and `is_ok()`, and the `quiet()` function and special handling of `error("quiet")` have been removed ([#7232], [#7288], [#7290], [#7297], [#7502], [#7508]).
- SUP syntax changes: Named types are now declared with `type` statements (e.g., `type port=uint16`) instead of inline (e.g., `80::(port=uint16)`), sets and maps are written `set[...]` and `map{...}` instead of `|[...]|` and `|{...}|`, and a none value is written `none` instead of `_`. The `-persist` flag has been removed since named types can no longer be redefined ([#6790], [#6821], [#6828], [#7258]).
- Unions are now flat: An anonymous union can no longer contain another anonymous union (a named union still can) ([#6809]).
- The non-aggregate `max()` and `min()` functions are renamed `greatest()` and `least()`, as in other SQL implementations ([#6963], [#6965], [#6966]).
- The type fusion operators and functions have been renamed: `blend` is now `fuse`, the former `fuse` (which produces fusion types) is now `superfuse`, `unblend()` is now `defuse()`, and the former `defuse()` is now `superdefuse()`. Because the names were swapped, a query that uses `fuse` or `defuse()` still runs but now does what `blend` or `unblend()` did. This restores the meaning `fuse` had before v0.3.0 ([#7563]).
- The `time` type is renamed `timestamp`, following Spark SQL and BigQuery, and is still a nanosecond-precision instant in UTC. The name `time` now refers to the SQL time-of-day type, which isn't implemented yet, so a cast such as `x::time` now produces an error ([#7510]).
- The JSUP format has been removed. The Python client has moved to [superdb-python](https://github.com/superdb/superdb-python) and now uses Arrow IPC to receive query results ([#6838], [#6858], [#7028]).
- The `table` output format has been removed ([#7440]).
- `flatten()` now returns `{path,value}` records rather than `{key,value}`, and it now descends into arrays, sets, and maps, using integer positions and map keys as path elements. `unflatten()` takes the new form. Similarly, `fields()` is replaced by `paths()`, which returns the same paths as `flatten()`, e.g., `paths({a:[1,2]})` is `[["a",0],["a",1]]` where `fields()` returned `[["a"]]` ([#7567], [#7571]).
- The `grep()` function and search expressions no longer match field names ([#7343]).
- The S3 endpoint environment variable is now `AWS_ENDPOINT_URL_S3` instead of `AWS_S3_ENDPOINT`, following AWS convention ([#7307]).
- The `-fusemem` flag has been removed ([#7078]).

### Added

- Option types (e.g., `option(int64)`) and `none` values, along with the `??` none-coalescing and `?.` optional-chaining operators ([#6881], [#6894], [#6900], [#6903], [#7249], [#7254], [#7269], [#7279], [#7311], [#7327], [#7358], [#7380], [#7478], [#7491], [#7505], [#7522], [#7552]).
- Recursive types, e.g., `type JSON=string|float64|bool|null|[JSON]|map{string:JSON}` ([#6841], [#7458]).
- Native `uuid` type ([#7512]).
- Reserved types for SQL compatibility, such as `date`, `time`, `interval`, `decimal`, `int128`, `geometry`, and `geography`, which give these types a place in the type system and binary format ahead of their implementation. Casting to one of them currently produces a "type not yet implemented" error ([#7510]).
- The `any` type, for use in type expressions; a value cast to `any` becomes a `fusion(all)` value ([#6827], [#6978], [#7063], [#7089], [#7554]).
- A type declaration can bind a name to a type computed by a query, e.g., `type T = (from a.sup | fuse(this))` ([#7558]).
- The dot operator and string indexing can navigate into type values, e.g., `<{a:{b:string}}>.a.b` and `<{a:{b:string}}>["a"]["b"]` are `<string>`, including through named, option, and fusion types, into array, set, and map element types, and across the members of a union ([#7565], [#7570]).
- New `keys()` and `values()` functions return the field names and values of a record, or the keys and values of a map ([#7571]).
- New [`optionize()`](https://superdb.org/super-sql/functions/types/optionize.html) function, which turns `missing|T` unions into `option(T)`, e.g., to make the type from `fuse()` have optional fields ([#7556]).
- New [`sum_type()`](https://superdb.org/super-sql/functions/types/sum_type.html) function, which forms a union of named types from a map of names to types ([#7557]).
- New [`array_agg`](https://superdb.org/super-sql/aggregates/array_agg.html) aggregate function, including support for `distinct` ([#7165], [#7225]).
- Method-style call syntax, where the value before the dot becomes the first argument, e.g., `s.upper()` is equivalent to `upper(s)`, `s.split(",")` to `split(s, ",")`, and calls can be chained, as in `s.trim().upper()` ([#7281]).
- Command-line arguments after `--` are passed to the query as constants ([#7364]).
- The output format is now inferred from the file extension given to `-o` when `-f` isn't specified ([#7331]).
- `super compile` now accepts `-i` ([#7538]).
- A `from` operator that reads files can now have a parent operator, scanning its files once per input value ([#7317]).
- `from` with an expression argument (e.g., a URL built with an f-string) now works in `super db` queries ([#7294]).
- Database tables can now be created without a sort key (the new default) ([#7438]).
- SQL `CREATE TABLE`, `DROP TABLE`, and `INSERT INTO` statements for database tables, with column types, `NOT NULL`, and `ORDER BY`, plus `CREATE SUPER TABLE` for a table of any SuperSQL type (e.g., a sum type from `sum_type()`) and `super db create -type`. Values inserted or loaded into a typed table are cast to its type, and nothing is committed if any value can't be cast. A query can now also be a script of semicolon-separated statements that share declarations, with a status record output after each statement ([#7562]).
- New `-objectcap`, `-framecap`, and `-inputcap` flags control the size of database storage objects, BSUP frames, and batches of input ([#7437]).
- Queries against database tables are now type-checked, and `:objects` output now includes each object's type ([#7513]).
- Named types in `superdefuse()` ([#6767]).
- Enum types in `upcast()` and `downcast()` ([#6853], [#6864]).
- The Parquet reader now supports the map type ([#7098]).

### Changed

- Fusion values are now automatically defused in all output formats except BSUP. Use the new `-fusion` flag to see them in SUP output. The `superfuse` operator now defuses any fusion values in its input ([#7036], [#7037]).
- Type fusion now preserves named types, produces simpler fused types, and computes the same fused type regardless of input order. A field that is absent from some inputs now has a union type with `missing` (e.g., `int64|missing`) rather than becoming an optional field, and `optionize()` converts such a type to one with optional fields. An empty array, set, or map now has element type `none` rather than `null` ([#6760], [#6766], [#6777], [#6798], [#6819], [#7374], [#7375], [#7491], [#7553]).
- In SQL, a reference to a missing field now evaluates to `null` ([#7295]).
- `collect()` now returns an empty array when there's nothing to collect, and [`collect()`](https://superdb.org/super-sql/aggregates/collect.html), [`array_agg()`](https://superdb.org/super-sql/aggregates/array_agg.html), and [`union()`](https://superdb.org/super-sql/aggregates/union.html) now keep `null` values, and `collect()` and `array_agg()` preserve input order ([#7165], [#7166], [#7167], [#7392], [#7405]).
- `join` no longer treats two `null` keys as matching, per SQL semantics ([#7238]).
- The JSON reader now reports an error for duplicate field names (the last value was previously kept), and its parse errors now include line numbers ([#6868], [#7111], [#7112]).
- JSON output now omits fields whose value is `none` ([#6780], [#7322]).
- `typeof()` now returns the subtype of a fusion value ([#7147], [#7158]).
- Casting a record to a union of record types now converts it to the member it best matches, converting field values as needed, where previously it had to match a member exactly. Casting a missing value or field to an option type now gives `none` ([#7555]).
- `under()` of a type value now unwraps one level of an array, set, option, union, or map type, e.g., `under(<[int64]>)` is `<int64>` where it was previously `<[int64]>` ([#7572]).
- A format argument on a `from` operator still takes precedence, but `-i` now takes precedence over a file's extension ([#7344]).
- File paths in output and messages are now shown without a `file://` prefix ([#7515]).
- A clearer error message when a `from` operator has a parent but its argument isn't an expression ([#7082]).
- The GitHub organization and Go module path are now `github.com/superdb/super` ([#7377]).
- Homebrew installation is now `brew install super` from the core Homebrew casks instead of our own tap ([#6796], [#6835]).
- Windows release binaries are now signed ([#6761]).
- Go 1.27 or later is now required to build `super` ([#6778], [#7231], [#7318]).

### Fixed

- Fix many problems with fusion values in operators, functions, and expressions (e.g., `put`, `rename`, `unnest`, `collect()`, `typeof()`, casts, comparisons, and search expressions) ([#6941], [#6990], [#7001], [#7019], [#7031], [#7032], [#7034], [#7043], [#7044], [#7045], [#7051], [#7133], [#7134], [#7136], [#7137], [#7138], [#7142], [#7143], [#7144], [#7146], [#7150], [#7151], [#7153], [#7154], [#7155], [#7156], [#7159], [#7171], [#7177], [#7179], [#7180], [#7183], [#7187], [#7188], [#7189], [#7191], [#7193], [#7195], [#7198], [#7199], [#7200], [#7201], [#7202], [#7207], [#7219], [#7221], [#7222], [#7381]).
- Fix many problems in type fusion and in the `defuse()`, `superdefuse()`, `upcast()`, `downcast()`, and `infer` functions and operators ([#6768], [#6770], [#6771], [#6774], [#6786], [#6815], [#6845], [#6848], [#6849], [#6854], [#6869], [#6871], [#6882], [#6904], [#6930], [#6932], [#6937], [#6979], [#6998], [#6999], [#7000], [#7005], [#7006], [#7008], [#7011], [#7021], [#7030], [#7129], [#7162]).
- Fix many functions, operators, and expressions that mishandled union-typed values ([#6836], [#6837], [#6888], [#6889], [#6901], [#6902], [#6905], [#6911], [#6912], [#6913], [#6914], [#6922], [#6934], [#7022], [#7026], [#7038]).
- Fix incorrect results (missing or extra values) when filtering columnar files with union-typed fields, and a related panic ([#7100], [#7118], [#7119], [#7124]).
- Fix incorrect or inconsistent aggregation results on columnar (formerly CSUP) input, and on aggregates with `distinct` ([#6782], [#6793], [#6843]).
- Fix incorrect results from a `switch` containing an aggregate ([#6953]).
- Fix deadlocks in some queries, such as `from /dev/stdin | top 1 a` and `fork` combined with `sort` ([#7455], [#7456]).
- Fix a columnar (formerly CSUP) writer bug that could misalign values in union columns ([#6784], [#6908]).
- Fix `super db manage` so it compacts data again ([#7523]).
- Fix a panic in `super db ls` on a database with no tables ([#7485]).
- Fix Parquet reading of null-typed values, writing of optional fields, and projection of lists, maps, and empty projections ([#7097], [#7104], [#7169], [#7241], [#7460]).
- Fix Arrow writer handling of named types and of a union of null and a nested union ([#6772], [#7012]).
- Fix SQL queries that returned errors or too few columns on some BSUP input (e.g., `select *` with named types, or nested field references) ([#7545], [#7548]).
- Fix `none` appearing in CSV output for a missing field, and spurious "wrong number of fields" errors reading non-comma-delimited CSV files ([#7239], [#7483]).
- Fix reading `/dev/stdin` by name on macOS ([#7049], [#7067]).
- Fix `date_part()` and `strftime()` handling of unions and nulls ([#6884], [#6885]).
- Fix `<=` and `>=` comparisons of `bool` values ([#6976]).
- Fix `collect_map()` handling of a named type inside a union ([#6967]).
- Fix panics and lost named types in `count()` grouped by a string ([#7080], [#7084]).
- Fix a precision bug when casting to `float16` or `float32` ([#7160]).
- Fix the Zeek reader treating a time or interval with more than nine fractional digits as zero ([#7496]).
- Fix duplicate fields in record expressions with spreads, and a panic on `{...r,a:none}` ([#7530]).
- Fix duplicate error messages for `cut` field assignments without a right-hand side ([#7087]).
- Fix `group by` ordinals when a column alias matches an input column name, and a panic when used with `select *` ([#7326], [#7435]).
- Fix a panic on SQL `select` with conflicting column aliases (e.g., `select 1 as a, 2 as a, 3 as a_1`) ([#7250]).
- Fix `select *` failing with "cannot be used for dynamic inputs" on some BSUP input ([#7463]).
- Fix a panic in `unnest {a,b}` when `b` is an empty array ([#7208]).
- Fix empty field names produced by lambdas in record expressions ([#7266]).
- Fix a function call that failed depending on the order of references to a `const` ([#7321]).
- Fix named types on constant values ([#7547]).
- Fix SUP output of a named type whose underlying type is another named type ([#6811]).
- Fix panics: in `collect()`, `any()`, or `superfuse()` downstream of `sort`; after a `from` with an expression argument; when outputting an error-valued constant; and when evaluating a recursive function concurrently ([#7323], [#7406], [#7416], [#7417]).

### Performance

- A new, faster JSON reader ([#6764], [#6789], [#6817], [#6818], [#6847], [#6867], [#6935], [#6981], [#7113], [#7157], [#7161], [#7194], [#7226], [#7462]).
- Native vector implementations of `defuse()`, `superdefuse()`, `upcast()`, `downcast()`, `collect()`, `greatest()`, `least()`, `fuse`, and `superfuse`, which previously used the sequential runtime ([#6773], [#6820], [#6829], [#6856], [#6863], [#7035]).
- Faster hash joins ([#7252], [#7253], [#7255]).
- Better parallelism on sorted database tables for queries that use `values`, which includes every SQL `SELECT` ([#7559]).
- Faster `unnest` ([#7246]).
- Faster queries on records with nullable fields ([#7340]).
- Faster Parquet reading, including when it's type-checked ([#7115], [#7277], [#7345]).
- BSUP and Parquet inputs stay in vector form throughout a query, and BSUP files now record their fused type so type checking doesn't need to scan the data ([#7090], [#7094], [#7105]).
- Filter and search pushdown into BSUP files ([#7450], [#7453], [#7550]).
- Faster queries with subqueries, including caching of uncorrelated subqueries ([#7248], [#7351]).
- Faster database ingest when the sort key is a primitive type ([#7429]).

### Security

- Upgrade `github.com/golang-jwt/jwt/v4` to v4.5.2 (CVE-2024-51744, CVE-2025-30204) ([#7099]).
- Upgrade `github.com/apache/arrow-go/v18` to v18.8.0, which pulls in a fixed `github.com/apache/thrift` (CVE-2026-43871) ([#7309]).

[#6760]: https://github.com/superdb/super/pull/6760
[#6761]: https://github.com/superdb/super/pull/6761
[#6764]: https://github.com/superdb/super/pull/6764
[#6766]: https://github.com/superdb/super/pull/6766
[#6767]: https://github.com/superdb/super/pull/6767
[#6768]: https://github.com/superdb/super/pull/6768
[#6770]: https://github.com/superdb/super/pull/6770
[#6771]: https://github.com/superdb/super/pull/6771
[#6772]: https://github.com/superdb/super/pull/6772
[#6773]: https://github.com/superdb/super/pull/6773
[#6774]: https://github.com/superdb/super/pull/6774
[#6777]: https://github.com/superdb/super/pull/6777
[#6778]: https://github.com/superdb/super/pull/6778
[#6780]: https://github.com/superdb/super/pull/6780
[#6782]: https://github.com/superdb/super/pull/6782
[#6784]: https://github.com/superdb/super/pull/6784
[#6786]: https://github.com/superdb/super/pull/6786
[#6789]: https://github.com/superdb/super/pull/6789
[#6790]: https://github.com/superdb/super/pull/6790
[#6793]: https://github.com/superdb/super/pull/6793
[#6796]: https://github.com/superdb/super/pull/6796
[#6798]: https://github.com/superdb/super/pull/6798
[#6809]: https://github.com/superdb/super/pull/6809
[#6811]: https://github.com/superdb/super/pull/6811
[#6815]: https://github.com/superdb/super/pull/6815
[#6817]: https://github.com/superdb/super/pull/6817
[#6818]: https://github.com/superdb/super/pull/6818
[#6819]: https://github.com/superdb/super/pull/6819
[#6820]: https://github.com/superdb/super/pull/6820
[#6821]: https://github.com/superdb/super/pull/6821
[#6827]: https://github.com/superdb/super/pull/6827
[#6828]: https://github.com/superdb/super/pull/6828
[#6829]: https://github.com/superdb/super/pull/6829
[#6835]: https://github.com/superdb/super/pull/6835
[#6836]: https://github.com/superdb/super/pull/6836
[#6837]: https://github.com/superdb/super/pull/6837
[#6838]: https://github.com/superdb/super/pull/6838
[#6841]: https://github.com/superdb/super/pull/6841
[#6843]: https://github.com/superdb/super/pull/6843
[#6844]: https://github.com/superdb/super/pull/6844
[#6845]: https://github.com/superdb/super/pull/6845
[#6847]: https://github.com/superdb/super/pull/6847
[#6848]: https://github.com/superdb/super/pull/6848
[#6849]: https://github.com/superdb/super/pull/6849
[#6853]: https://github.com/superdb/super/pull/6853
[#6854]: https://github.com/superdb/super/pull/6854
[#6856]: https://github.com/superdb/super/pull/6856
[#6858]: https://github.com/superdb/super/pull/6858
[#6863]: https://github.com/superdb/super/pull/6863
[#6864]: https://github.com/superdb/super/pull/6864
[#6867]: https://github.com/superdb/super/pull/6867
[#6868]: https://github.com/superdb/super/pull/6868
[#6869]: https://github.com/superdb/super/pull/6869
[#6871]: https://github.com/superdb/super/pull/6871
[#6881]: https://github.com/superdb/super/pull/6881
[#6882]: https://github.com/superdb/super/pull/6882
[#6884]: https://github.com/superdb/super/pull/6884
[#6885]: https://github.com/superdb/super/pull/6885
[#6888]: https://github.com/superdb/super/pull/6888
[#6889]: https://github.com/superdb/super/pull/6889
[#6894]: https://github.com/superdb/super/pull/6894
[#6900]: https://github.com/superdb/super/pull/6900
[#6901]: https://github.com/superdb/super/pull/6901
[#6902]: https://github.com/superdb/super/pull/6902
[#6903]: https://github.com/superdb/super/pull/6903
[#6904]: https://github.com/superdb/super/pull/6904
[#6905]: https://github.com/superdb/super/pull/6905
[#6908]: https://github.com/superdb/super/pull/6908
[#6911]: https://github.com/superdb/super/pull/6911
[#6912]: https://github.com/superdb/super/pull/6912
[#6913]: https://github.com/superdb/super/pull/6913
[#6914]: https://github.com/superdb/super/pull/6914
[#6922]: https://github.com/superdb/super/pull/6922
[#6930]: https://github.com/superdb/super/pull/6930
[#6932]: https://github.com/superdb/super/pull/6932
[#6934]: https://github.com/superdb/super/pull/6934
[#6935]: https://github.com/superdb/super/pull/6935
[#6937]: https://github.com/superdb/super/pull/6937
[#6941]: https://github.com/superdb/super/pull/6941
[#6953]: https://github.com/superdb/super/pull/6953
[#6963]: https://github.com/superdb/super/pull/6963
[#6965]: https://github.com/superdb/super/pull/6965
[#6966]: https://github.com/superdb/super/pull/6966
[#6967]: https://github.com/superdb/super/pull/6967
[#6976]: https://github.com/superdb/super/pull/6976
[#6978]: https://github.com/superdb/super/pull/6978
[#6979]: https://github.com/superdb/super/pull/6979
[#6981]: https://github.com/superdb/super/pull/6981
[#6990]: https://github.com/superdb/super/pull/6990
[#6994]: https://github.com/superdb/super/pull/6994
[#6998]: https://github.com/superdb/super/pull/6998
[#6999]: https://github.com/superdb/super/pull/6999
[#7000]: https://github.com/superdb/super/pull/7000
[#7001]: https://github.com/superdb/super/pull/7001
[#7005]: https://github.com/superdb/super/pull/7005
[#7006]: https://github.com/superdb/super/pull/7006
[#7008]: https://github.com/superdb/super/pull/7008
[#7011]: https://github.com/superdb/super/pull/7011
[#7012]: https://github.com/superdb/super/pull/7012
[#7019]: https://github.com/superdb/super/pull/7019
[#7021]: https://github.com/superdb/super/pull/7021
[#7022]: https://github.com/superdb/super/pull/7022
[#7026]: https://github.com/superdb/super/pull/7026
[#7028]: https://github.com/superdb/super/pull/7028
[#7030]: https://github.com/superdb/super/pull/7030
[#7031]: https://github.com/superdb/super/pull/7031
[#7032]: https://github.com/superdb/super/pull/7032
[#7034]: https://github.com/superdb/super/pull/7034
[#7035]: https://github.com/superdb/super/pull/7035
[#7036]: https://github.com/superdb/super/pull/7036
[#7037]: https://github.com/superdb/super/pull/7037
[#7038]: https://github.com/superdb/super/pull/7038
[#7043]: https://github.com/superdb/super/pull/7043
[#7044]: https://github.com/superdb/super/pull/7044
[#7045]: https://github.com/superdb/super/pull/7045
[#7049]: https://github.com/superdb/super/pull/7049
[#7050]: https://github.com/superdb/super/pull/7050
[#7051]: https://github.com/superdb/super/pull/7051
[#7063]: https://github.com/superdb/super/pull/7063
[#7067]: https://github.com/superdb/super/pull/7067
[#7072]: https://github.com/superdb/super/pull/7072
[#7078]: https://github.com/superdb/super/pull/7078
[#7080]: https://github.com/superdb/super/pull/7080
[#7082]: https://github.com/superdb/super/pull/7082
[#7084]: https://github.com/superdb/super/pull/7084
[#7087]: https://github.com/superdb/super/pull/7087
[#7089]: https://github.com/superdb/super/pull/7089
[#7090]: https://github.com/superdb/super/pull/7090
[#7094]: https://github.com/superdb/super/pull/7094
[#7097]: https://github.com/superdb/super/pull/7097
[#7098]: https://github.com/superdb/super/pull/7098
[#7099]: https://github.com/superdb/super/pull/7099
[#7100]: https://github.com/superdb/super/pull/7100
[#7104]: https://github.com/superdb/super/pull/7104
[#7105]: https://github.com/superdb/super/pull/7105
[#7111]: https://github.com/superdb/super/pull/7111
[#7112]: https://github.com/superdb/super/pull/7112
[#7113]: https://github.com/superdb/super/pull/7113
[#7115]: https://github.com/superdb/super/pull/7115
[#7118]: https://github.com/superdb/super/pull/7118
[#7119]: https://github.com/superdb/super/pull/7119
[#7124]: https://github.com/superdb/super/pull/7124
[#7129]: https://github.com/superdb/super/pull/7129
[#7133]: https://github.com/superdb/super/pull/7133
[#7134]: https://github.com/superdb/super/pull/7134
[#7136]: https://github.com/superdb/super/pull/7136
[#7137]: https://github.com/superdb/super/pull/7137
[#7138]: https://github.com/superdb/super/pull/7138
[#7142]: https://github.com/superdb/super/pull/7142
[#7143]: https://github.com/superdb/super/pull/7143
[#7144]: https://github.com/superdb/super/pull/7144
[#7145]: https://github.com/superdb/super/pull/7145
[#7146]: https://github.com/superdb/super/pull/7146
[#7147]: https://github.com/superdb/super/pull/7147
[#7150]: https://github.com/superdb/super/pull/7150
[#7151]: https://github.com/superdb/super/pull/7151
[#7153]: https://github.com/superdb/super/pull/7153
[#7154]: https://github.com/superdb/super/pull/7154
[#7155]: https://github.com/superdb/super/pull/7155
[#7156]: https://github.com/superdb/super/pull/7156
[#7157]: https://github.com/superdb/super/pull/7157
[#7158]: https://github.com/superdb/super/pull/7158
[#7159]: https://github.com/superdb/super/pull/7159
[#7160]: https://github.com/superdb/super/pull/7160
[#7161]: https://github.com/superdb/super/pull/7161
[#7162]: https://github.com/superdb/super/pull/7162
[#7165]: https://github.com/superdb/super/pull/7165
[#7166]: https://github.com/superdb/super/pull/7166
[#7167]: https://github.com/superdb/super/pull/7167
[#7169]: https://github.com/superdb/super/pull/7169
[#7171]: https://github.com/superdb/super/pull/7171
[#7177]: https://github.com/superdb/super/pull/7177
[#7179]: https://github.com/superdb/super/pull/7179
[#7180]: https://github.com/superdb/super/pull/7180
[#7181]: https://github.com/superdb/super/pull/7181
[#7182]: https://github.com/superdb/super/pull/7182
[#7183]: https://github.com/superdb/super/pull/7183
[#7187]: https://github.com/superdb/super/pull/7187
[#7188]: https://github.com/superdb/super/pull/7188
[#7189]: https://github.com/superdb/super/pull/7189
[#7191]: https://github.com/superdb/super/pull/7191
[#7193]: https://github.com/superdb/super/pull/7193
[#7194]: https://github.com/superdb/super/pull/7194
[#7195]: https://github.com/superdb/super/pull/7195
[#7198]: https://github.com/superdb/super/pull/7198
[#7199]: https://github.com/superdb/super/pull/7199
[#7200]: https://github.com/superdb/super/pull/7200
[#7201]: https://github.com/superdb/super/pull/7201
[#7202]: https://github.com/superdb/super/pull/7202
[#7207]: https://github.com/superdb/super/pull/7207
[#7208]: https://github.com/superdb/super/pull/7208
[#7219]: https://github.com/superdb/super/pull/7219
[#7221]: https://github.com/superdb/super/pull/7221
[#7222]: https://github.com/superdb/super/pull/7222
[#7225]: https://github.com/superdb/super/pull/7225
[#7226]: https://github.com/superdb/super/pull/7226
[#7231]: https://github.com/superdb/super/pull/7231
[#7232]: https://github.com/superdb/super/pull/7232
[#7238]: https://github.com/superdb/super/pull/7238
[#7239]: https://github.com/superdb/super/pull/7239
[#7241]: https://github.com/superdb/super/pull/7241
[#7246]: https://github.com/superdb/super/pull/7246
[#7248]: https://github.com/superdb/super/pull/7248
[#7249]: https://github.com/superdb/super/pull/7249
[#7250]: https://github.com/superdb/super/pull/7250
[#7252]: https://github.com/superdb/super/pull/7252
[#7253]: https://github.com/superdb/super/pull/7253
[#7254]: https://github.com/superdb/super/pull/7254
[#7255]: https://github.com/superdb/super/pull/7255
[#7258]: https://github.com/superdb/super/pull/7258
[#7266]: https://github.com/superdb/super/pull/7266
[#7269]: https://github.com/superdb/super/pull/7269
[#7277]: https://github.com/superdb/super/pull/7277
[#7279]: https://github.com/superdb/super/pull/7279
[#7281]: https://github.com/superdb/super/pull/7281
[#7288]: https://github.com/superdb/super/pull/7288
[#7289]: https://github.com/superdb/super/pull/7289
[#7290]: https://github.com/superdb/super/pull/7290
[#7292]: https://github.com/superdb/super/pull/7292
[#7294]: https://github.com/superdb/super/pull/7294
[#7295]: https://github.com/superdb/super/pull/7295
[#7297]: https://github.com/superdb/super/pull/7297
[#7307]: https://github.com/superdb/super/pull/7307
[#7309]: https://github.com/superdb/super/pull/7309
[#7311]: https://github.com/superdb/super/pull/7311
[#7312]: https://github.com/superdb/super/pull/7312
[#7317]: https://github.com/superdb/super/pull/7317
[#7318]: https://github.com/superdb/super/pull/7318
[#7321]: https://github.com/superdb/super/pull/7321
[#7322]: https://github.com/superdb/super/pull/7322
[#7323]: https://github.com/superdb/super/pull/7323
[#7326]: https://github.com/superdb/super/pull/7326
[#7327]: https://github.com/superdb/super/pull/7327
[#7331]: https://github.com/superdb/super/pull/7331
[#7340]: https://github.com/superdb/super/pull/7340
[#7343]: https://github.com/superdb/super/pull/7343
[#7344]: https://github.com/superdb/super/pull/7344
[#7345]: https://github.com/superdb/super/pull/7345
[#7351]: https://github.com/superdb/super/pull/7351
[#7358]: https://github.com/superdb/super/pull/7358
[#7361]: https://github.com/superdb/super/pull/7361
[#7364]: https://github.com/superdb/super/pull/7364
[#7374]: https://github.com/superdb/super/pull/7374
[#7375]: https://github.com/superdb/super/pull/7375
[#7377]: https://github.com/superdb/super/pull/7377
[#7380]: https://github.com/superdb/super/pull/7380
[#7381]: https://github.com/superdb/super/pull/7381
[#7389]: https://github.com/superdb/super/pull/7389
[#7392]: https://github.com/superdb/super/pull/7392
[#7394]: https://github.com/superdb/super/pull/7394
[#7395]: https://github.com/superdb/super/pull/7395
[#7399]: https://github.com/superdb/super/pull/7399
[#7401]: https://github.com/superdb/super/pull/7401
[#7405]: https://github.com/superdb/super/pull/7405
[#7406]: https://github.com/superdb/super/pull/7406
[#7416]: https://github.com/superdb/super/pull/7416
[#7417]: https://github.com/superdb/super/pull/7417
[#7424]: https://github.com/superdb/super/pull/7424
[#7425]: https://github.com/superdb/super/pull/7425
[#7427]: https://github.com/superdb/super/pull/7427
[#7429]: https://github.com/superdb/super/pull/7429
[#7430]: https://github.com/superdb/super/pull/7430
[#7434]: https://github.com/superdb/super/pull/7434
[#7435]: https://github.com/superdb/super/pull/7435
[#7437]: https://github.com/superdb/super/pull/7437
[#7438]: https://github.com/superdb/super/pull/7438
[#7440]: https://github.com/superdb/super/pull/7440
[#7450]: https://github.com/superdb/super/pull/7450
[#7453]: https://github.com/superdb/super/pull/7453
[#7455]: https://github.com/superdb/super/pull/7455
[#7456]: https://github.com/superdb/super/pull/7456
[#7458]: https://github.com/superdb/super/pull/7458
[#7460]: https://github.com/superdb/super/pull/7460
[#7462]: https://github.com/superdb/super/pull/7462
[#7463]: https://github.com/superdb/super/pull/7463
[#7467]: https://github.com/superdb/super/pull/7467
[#7472]: https://github.com/superdb/super/pull/7472
[#7478]: https://github.com/superdb/super/pull/7478
[#7483]: https://github.com/superdb/super/pull/7483
[#7485]: https://github.com/superdb/super/pull/7485
[#7491]: https://github.com/superdb/super/pull/7491
[#7496]: https://github.com/superdb/super/pull/7496
[#7501]: https://github.com/superdb/super/pull/7501
[#7502]: https://github.com/superdb/super/pull/7502
[#7505]: https://github.com/superdb/super/pull/7505
[#7508]: https://github.com/superdb/super/pull/7508
[#7510]: https://github.com/superdb/super/pull/7510
[#7511]: https://github.com/superdb/super/pull/7511
[#7512]: https://github.com/superdb/super/pull/7512
[#7513]: https://github.com/superdb/super/pull/7513
[#7514]: https://github.com/superdb/super/pull/7514
[#7515]: https://github.com/superdb/super/pull/7515
[#7519]: https://github.com/superdb/super/pull/7519
[#7520]: https://github.com/superdb/super/pull/7520
[#7522]: https://github.com/superdb/super/pull/7522
[#7523]: https://github.com/superdb/super/pull/7523
[#7528]: https://github.com/superdb/super/pull/7528
[#7530]: https://github.com/superdb/super/pull/7530
[#7531]: https://github.com/superdb/super/pull/7531
[#7538]: https://github.com/superdb/super/pull/7538
[#7539]: https://github.com/superdb/super/pull/7539
[#7541]: https://github.com/superdb/super/pull/7541
[#7545]: https://github.com/superdb/super/pull/7545
[#7547]: https://github.com/superdb/super/pull/7547
[#7548]: https://github.com/superdb/super/pull/7548
[#7550]: https://github.com/superdb/super/pull/7550
[#7552]: https://github.com/superdb/super/pull/7552
[#7553]: https://github.com/superdb/super/pull/7553
[#7554]: https://github.com/superdb/super/pull/7554
[#7555]: https://github.com/superdb/super/pull/7555
[#7556]: https://github.com/superdb/super/pull/7556
[#7557]: https://github.com/superdb/super/pull/7557
[#7558]: https://github.com/superdb/super/pull/7558
[#7559]: https://github.com/superdb/super/pull/7559
[#7562]: https://github.com/superdb/super/pull/7562
[#7563]: https://github.com/superdb/super/pull/7563
[#7564]: https://github.com/superdb/super/pull/7564
[#7565]: https://github.com/superdb/super/pull/7565
[#7567]: https://github.com/superdb/super/pull/7567
[#7570]: https://github.com/superdb/super/pull/7570
[#7571]: https://github.com/superdb/super/pull/7571
[#7572]: https://github.com/superdb/super/pull/7572
[#7573]: https://github.com/superdb/super/pull/7573

## [0.3.0] - 2026-03-20

### **BREAKING CHANGE**

With the advance of the BSUP format to v2, `super` will no longer read BSUP v1 format as input. There is currently no backward compatibility in `super` for reading BSUP v1 inputs. If you have valuable saved BSUP v1 files that you cannot easily regenerate into v2 from their original sources, please speak up on [community Slack](https://www.brimdata.io/join-slack/) or [open an issue](https://github.com/superdb/super/issues) for assistance.

### Added

- New `debug` operator ([#6685], [#6694], [#6680], [#6691], [#6726]).
- New `infer` operator ([#6704]).
- New `defuse` function ([#6698]).
- New `unblend` function ([#6725]).
- New `db vacate` command ([#6706], [#6747]).
- Fusion types ([#6713]).
- Named types in `upcast` function ([#6752]).
- Optional fields in record expressions ([#6702]).

### Changed

- Change license to [SuperDB Source Available License v1.0](https://github.com/superdb/super/blob/9343c50f2cdaf39ecfb3f90a458c552d3d0f8681/LICENSE.md) ([#6755]).
- In `collect` and `union` aggregate functions, `error("quiet")` values are now dropped and `error("missing")` values are preserved ([#6710]).
- `null` values are now ignored in `concat` function and f-strings ([#6730]).
- macOS `super` release artifacts are now signed and notarized ([#6703]).
- The BSUP format has been advanced to version 2 ([#6713]).

### Fixed

- Fix `unnest` bug when to-be-unnested array is in a union ([#6692]).
- Fix precision bug when casting from float16 or float32 to string ([#6722]).

[#6680]: https://github.com/superdb/super/pull/6680
[#6685]: https://github.com/superdb/super/pull/6685
[#6691]: https://github.com/superdb/super/pull/6691
[#6692]: https://github.com/superdb/super/pull/6692
[#6694]: https://github.com/superdb/super/pull/6694
[#6698]: https://github.com/superdb/super/pull/6698
[#6702]: https://github.com/superdb/super/pull/6702
[#6703]: https://github.com/superdb/super/pull/6703
[#6704]: https://github.com/superdb/super/pull/6704
[#6706]: https://github.com/superdb/super/pull/6706
[#6710]: https://github.com/superdb/super/pull/6710
[#6713]: https://github.com/superdb/super/pull/6713
[#6722]: https://github.com/superdb/super/pull/6722
[#6725]: https://github.com/superdb/super/pull/6725
[#6726]: https://github.com/superdb/super/pull/6726
[#6730]: https://github.com/superdb/super/pull/6730
[#6747]: https://github.com/superdb/super/pull/6747
[#6752]: https://github.com/superdb/super/pull/6752
[#6755]: https://github.com/superdb/super/pull/6755

## [0.2.0] - 2026-02-27

### **BREAKING CHANGE**

With the advance of the BSUP format to v1, `super` will no longer read BSUP v0 format as input. There is currently no backward compatibility in `super` for reading BSUP v0 inputs. If you have valuable saved BSUP v0 files that you cannot easily regenerate into v1 from their original sources, please speak up on [community Slack](https://www.brimdata.io/join-slack/) or [open an issue](https://github.com/superdb/super/issues) for assistance.

### Added

- New `upcast` function ([#6634]).
- Line numbers are now shown for SUP parsing errors ([#6630]).
- Optional fields in records ([#6669]).

### Changed

- Upgrade to `github.com/apache/arrow-go/v18@v18.5.1` ([#6625]).
- Type fusion now ensures any fused unions contain at most one instance of each kind of complex type ([#6651]).
- Type decoration for self-describing error values in SUP are now omitted ([#6656]).
- By default, `super` now reads the first 1000 values when reading from an input file to infer type information used to type check the query. This can cause type errors when data being referenced occurs later in the input. The `-samplesize` flag can be used to adjust this ([#6667]).
- The BSUP format has been advanced to version 1 ([#6674]).

### Removed

- Typed nulls are no longer supported, with nullable values now represented with a union that includes type `null` ([#6633]).

### Fixed

- vam: Casting an error value now propagates the error ([#6602]).
- vam: Fix an issue where a mixed type error was not returned if the aggregation received a number first followed by a string ([#6618]).
- vam: Fix a `rename` operator issue where nested records were not getting assigned updated types ([#6623]).
- Fix a deadlock that could occur when running a group-by aggregation on BSUP input ([#6624]).
- The BSUP union tag is now encoded as a uvarint, in line with the specification ([#6660]).

[#6602]: https://github.com/superdb/super/pull/6602
[#6618]: https://github.com/superdb/super/pull/6618
[#6623]: https://github.com/superdb/super/pull/6623
[#6624]: https://github.com/superdb/super/pull/6624
[#6625]: https://github.com/superdb/super/pull/6625
[#6630]: https://github.com/superdb/super/pull/6630
[#6633]: https://github.com/superdb/super/pull/6633
[#6634]: https://github.com/superdb/super/pull/6634
[#6651]: https://github.com/superdb/super/pull/6651
[#6656]: https://github.com/superdb/super/pull/6656
[#6660]: https://github.com/superdb/super/pull/6660
[#6667]: https://github.com/superdb/super/pull/6667
[#6669]: https://github.com/superdb/super/pull/6669
[#6674]: https://github.com/superdb/super/pull/6674

## [0.1.0] - 2026-01-30

### Added

- Initial release (see the [project documentation](https://superdb.org/intro.html)).

[0.4.0]: https://github.com/superdb/super/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/superdb/super/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/superdb/super/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/superdb/super/releases/tag/v0.1.0
