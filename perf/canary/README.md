# Perf canary

The perf canary gives early warning of performance changes by comparing two
builds of `super` on the same machine in the same run. It loads and queries
the small "ghasample" data set (22,845 JSON values, one of each unique type
in a day of [GitHub Archive](https://www.gharchive.org/) data) with each
build and reports significant differences in BSUP load time, query time, and
peak memory.

Because both builds run back to back on the same machine, the comparison is
meaningful even on a noisy machine such as a hosted GitHub Actions runner.
The absolute times are not comparable across runs or machines.

## Running locally

```
make perf
```

By default this compares:

* **base**: the merge-base of `HEAD` with `origin/main` (run `git fetch` first
  so it's current), and
* **target**: the working tree, including uncommitted changes.

Either can be set to any commit or ref:

```
make perf BASE=c473eb5b7 TARGET=12ac576b2
```

To run faster, test only some of the systems described [below](#whats-measured)
with a comma-separated list, e.g., to skip the slowest one,
`super-json-static`, which accounts for more than half of the run time:

```
make perf SYSTEMS=super-bsup,super-json,super-parquet
```

Likewise, to run only some of the queries in [`queries/`](queries), list
them by file name without the extension, e.g.:

```
make perf SYSTEMS=super-bsup QUERIES=union
```

Other options can be passed with `PERF_CANARY_FLAGS`, e.g.,
`make perf PERF_CANARY_FLAGS="-keep -threshold 0.15"`. Run
`go run ./perf/canary -h` to see them all.

A run of all the systems with the default of 5 measured runs per query
takes about 6–7 minutes on a GitHub-hosted runner, and longer on slower
machines. The test data (about 170 MB) is downloaded once and cached in the
directory given by `-datadir` (by default `super-perf-canary` under the
user cache directory, e.g., `~/.cache` on Linux or `~/Library/Caches` on
macOS). The only other requirements are `git` and `go`.

When it finishes, it prints the report and saves a copy as `summary.md`,
along with the raw measurements (`results.json`) and each query's output.
These go in a new temporary directory, whose path is printed at the end,
or in the directory given by `-out`.

## Running in GitHub Actions

The perf canary [workflow](../../.github/workflows/perf-canary.yaml) runs
automatically:

* **On pull requests**, it compares the PR (merged into `main`) with the tip
  of `main`. To finish in about the same time as the other CI, it skips the
  `super-json-static` tests. Like other pull request workflows, it tests
  the merge commit GitHub creates to preview the result of merging the PR.
  That commit isn't on any branch and is replaced when the PR or `main`
  changes, so the report names the PR's own commit along with it.
* **On pushes to `main`**, it compares the new tip of `main` with the
  previous one, including all the tests, so a regression in
  `super-json-static` is caught as soon as it merges.

Changes that touch only `book/` or Markdown files are skipped.

The job itself never fails because of what it finds. Instead, it adds a
separate **Perf canary results** check to the commit, which is:

* failed (red) if any test regressed, failed, or changed its output, or
* successful (green) otherwise.

The check's title gives the verdict (e.g., "🔴 Perf canary: 2 of 16 tests
regressed") and its details show the full report, which also appears in
the job summary. The raw results are saved as an artifact. (A pull request
from a fork can't add the check, so its report is only in the job summary.)

The workflow can also be started manually from the Actions tab to compare
any two commits. Choose the branch to test and, optionally, the base and
target commits, the number of runs, and the systems and queries to test.

## What's measured

For each build, the canary:

1. builds `super`,
2. converts the JSON data to BSUP (`super -i json -o <file>.bsup`),
   measured once, and
3. runs each of the five queries in [`queries/`](queries) against four
   inputs:

   | Test | Input | Flags |
   |-|-|-|
   | `super-bsup` | the BSUP file written by that build | `-i bsup` |
   | `super-json` | `ghasample.json.gz` | `-i json` |
   | `super-json-static` | `ghasample.json.gz` | `-static -i json` |
   | `super-parquet` | `ghasample.parquet` | `-i parquet` |

Each query is run once per build as a warmup, then the given number of
times per build, alternating between the builds (base, target, target,
base, ...) so any drift in machine performance affects both equally. The
report uses the median wall time, CPU time (user + system), and peak
resident memory of each.

A change is flagged as significant only if all of these hold:

* the median changes by at least the `-threshold` (default 25%),
* the median changes by at least an absolute amount (default 25 ms for
  times and 20 MB for memory), so tiny, fast queries don't trigger on
  noise, and
* every target run is worse than every base run (or better, for an
  improvement). This matters most for peak memory, which can jump between
  levels from one run to the next as the garbage collector runs at
  different times.

Because of that last rule, use at least 5 runs (the default). With fewer,
noise alone is much more likely to separate the two builds' runs.

When the median wall or CPU time differs by enough to count but the runs
overlap, the change isn't flagged, and the report marks it 〰️ *noisy*
instead. (Peak memory isn't marked, since its runs overlap routinely.) This
usually means something else on the machine (another program, or a busy
hosted runner) slowed down some of the runs. Since the builds alternate,
such a slowdown hits both builds, but the medians can still land far apart
by chance. For example, in this local run, the machine got busy partway
through:

| Run | 1 | 2 | 3 | 4 | 5 |
|-|-|-|-|-|-|
| base | 4.871 s | 4.839 s | 5.229 s | 8.686 s | 10.906 s |
| target | 4.818 s | 4.807 s | 7.462 s | 7.681 s | 12.324 s |

The medians (5.229 → 7.462 s) differ by 43%, but the first two runs show
the builds are the same speed. Noise can also hide a real change, so if a
test you care about is marked 〰️ *noisy*, re-run it, e.g., with `SYSTEMS`
and `QUERIES` set to just that test.

To try different thresholds on an earlier run without re-running it, pass
its `results.json` to `-rereport`, e.g.,
`go run ./perf/canary -rereport results.json -threshold 0.15`.

The output of each query is also compared between the builds. A difference
is flagged because it usually means the timings aren't comparable (e.g., a
query got faster because it stopped returning results).

## What it can miss

The defaults are deliberately cautious. Hosted runners vary from run to run,
and a check that raised false alarms would soon be ignored. In calibration
runs that compared a commit with itself on hosted runners, median times
moved by up to about 6% and median peak memory by up to about 32%, so the
rules above leave a wide margin for noise. The price is that some things can
get slower even when the check is green:

* **Changes under the threshold aren't flagged.** In one test, a fake
  150 ms startup delay made `super-bsup search+` 23% slower (0.611 →
  0.751 s), and it wasn't flagged.
* **The fastest queries are covered by the absolute minimum rather than the
  percentage.** Some `super-parquet` queries take only about 25 ms on a
  hosted runner, so even doubling their time wouldn't be flagged.
* **Gradual creep isn't caught.** Each change is compared only with the
  commit before it, so a series of small regressions, each under the
  threshold, can add up unnoticed.
* **Coverage is narrow.** It runs five queries on one small data set in four
  formats. Regressions that only show up with large data, other formats or
  features (e.g., CSV, `super db`, joins), or other kinds of queries won't
  be seen. Full benchmarks are still the place to catch those.
* **Pull request runs skip `super-json-static`,** so a regression there is
  only reported once the change reaches `main`.

So if you've changed code you expect to affect performance, look at the
"All results" table even when the check is green. It shows every change,
flagged or not. To look more closely, re-classify a run with a lower
threshold using `-rereport` (see above), or run the canary locally with more
runs (`-runs 10`).
