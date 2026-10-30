# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.3.0]: https://github.com/superdb/super/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/superdb/super/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/superdb/super/releases/tag/v0.1.0
