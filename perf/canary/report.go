package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type status int

const (
	neutral status = iota
	improved
	regressed
)

// metric is one compared quantity (wall time, CPU time, or peak memory) for
// one test.
type metric struct {
	Name   string  `json:"name"`
	Base   float64 `json:"base"`
	Target float64 `json:"target"`
	Change float64 `json:"change"` // relative change, e.g., 0.25 for +25%
	Status string  `json:"status"`
	// Noisy is set for a time when the medians differ by enough to be
	// significant but the runs of the two builds overlap, so the change
	// isn't flagged.  Peak memory isn't marked since its runs overlap
	// routinely as it jumps between levels.
	Noisy  bool `json:"noisy,omitempty"`
	status status
	unit   string
	scale  float64
	format string
}

type row struct {
	*result
	Metrics []*metric `json:"metrics,omitempty"`
	Status  string    `json:"status"`
	status  status
}

type report struct {
	Base      *build   `json:"base"`
	Target    *build   `json:"target"`
	PR        string   `json:"pr,omitempty"`
	Host      string   `json:"host"`
	Runs      int      `json:"runs"`
	Systems   []string `json:"systems"`
	Queries   []string `json:"queries"`
	Threshold float64  `json:"threshold"`
	Verdict   string   `json:"verdict"`
	Rows      []*row   `json:"results"`
	c         config
}

func newReport(c config, base, target *build, results []*result) *report {
	rep := &report{
		Base:      base,
		Target:    target,
		PR:        c.pr,
		Host:      hostInfo(),
		Runs:      c.runs,
		Systems:   systemNamesOf(c.systems),
		Queries:   queryNamesOf(c.queries),
		Threshold: c.threshold,
		c:         c,
	}
	for _, r := range results {
		rep.Rows = append(rep.Rows, rep.classify(r))
	}
	rep.Verdict = rep.verdict()
	return rep
}

func systemNamesOf(systems []system) []string {
	var names []string
	for _, sys := range systems {
		names = append(names, sys.name)
	}
	return names
}

func (rep *report) classify(r *result) *row {
	row := &row{result: r}
	if r.BaseErr != "" || r.TargetErr != "" {
		row.Status = "failed"
		return row
	}
	minDelta := rep.c.minDelta.Seconds()
	row.Metrics = []*metric{
		rep.compare("wall", r, func(m measurement) float64 { return m.Wall }, minDelta, "s", 1, "%.3f"),
		rep.compare("cpu", r, measurement.cpu, minDelta, "s", 1, "%.3f"),
	}
	if median(r.Base, rss) > 0 && median(r.Target, rss) > 0 {
		row.Metrics = append(row.Metrics, rep.compare("memory", r, rss, float64(rep.c.minMemDelta), "MB", 1<<20, "%.0f"))
	}
	for _, m := range row.Metrics {
		if m.status > row.status {
			row.status = m.status
		}
	}
	row.Status = statusName(row.status)
	return row
}

func rss(m measurement) float64 { return float64(m.MaxRSS) }

func (rep *report) compare(name string, r *result, f func(measurement) float64, minDelta float64, unit string, scale float64, format string) *metric {
	b, t := median(r.Base, f), median(r.Target, f)
	m := &metric{Name: name, Base: b, Target: t, unit: unit, scale: scale, format: format}
	if b > 0 {
		m.Change = t/b - 1
	}
	// A change is significant only if the medians differ by the relative
	// threshold and the absolute minimum and the runs of the two builds
	// don't overlap at all, which guards against metrics such as peak
	// memory that can jump between levels from one run to the next.
	thr := rep.c.threshold
	noisy := name != "memory"
	bmin, bmax := minMax(r.Base, f)
	tmin, tmax := minMax(r.Target, f)
	switch {
	case b > 0 && t/b >= 1+thr && t-b >= minDelta:
		if tmin > bmax {
			m.status = regressed
		} else {
			m.Noisy = noisy
		}
	case b > 0 && t/b <= 1/(1+thr) && b-t >= minDelta:
		if tmax < bmin {
			m.status = improved
		} else {
			m.Noisy = noisy
		}
	}
	m.Status = statusName(m.status)
	return m
}

func minMax(ms []measurement, f func(measurement) float64) (float64, float64) {
	if len(ms) == 0 {
		return 0, 0
	}
	lo, hi := f(ms[0]), f(ms[0])
	for _, m := range ms[1:] {
		lo, hi = min(lo, f(m)), max(hi, f(m))
	}
	return lo, hi
}

func statusName(s status) string {
	switch s {
	case regressed:
		return "regressed"
	case improved:
		return "improved"
	}
	return "neutral"
}

func (rep *report) count(f func(*row) bool) int {
	var n int
	for _, r := range rep.Rows {
		if f(r) {
			n++
		}
	}
	return n
}

func (rep *report) verdict() string {
	switch {
	case rep.count(func(r *row) bool { return r.Status == "failed" || r.OutputMismatch }) > 0:
		return "attention"
	case rep.count(func(r *row) bool { return r.status == regressed }) > 0:
		return "regressed"
	case rep.count(func(r *row) bool { return r.status == improved }) > 0:
		return "improved"
	}
	return "neutral"
}

func (r *row) noisy() bool {
	return slices.ContainsFunc(r.Metrics, func(m *metric) bool { return m.Noisy })
}

func (rep *report) title() string {
	total := len(rep.Rows)
	regressions := rep.count(func(r *row) bool { return r.status == regressed })
	improvements := rep.count(func(r *row) bool { return r.status == improved })
	problems := rep.count(func(r *row) bool { return r.Status == "failed" || r.OutputMismatch })
	var parts []string
	if problems > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d tests failed or changed their output", problems, total))
	}
	if regressions > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d tests regressed", regressions, total))
	}
	if improvements > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d tests improved", improvements, total))
	}
	icon := "⚪"
	switch rep.Verdict {
	case "attention":
		icon = "⚠️"
	case "regressed":
		icon = "🔴"
	case "improved":
		icon = "🟢"
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("no significant changes in %d tests", total))
	}
	s := fmt.Sprintf("%s Perf canary: %s", icon, strings.Join(parts, ", "))
	if noisy := rep.count((*row).noisy); noisy > 0 {
		s += fmt.Sprintf(" (%d noisy)", noisy)
	}
	return s
}

// targetDesc describes the target build. For a merge commit, such as the
// one GitHub creates to test a pull request, it names the merged commit,
// since the merge commit itself may never be seen outside the run.
func (rep *report) targetDesc() string {
	t := rep.Target
	pr := ""
	if rep.PR != "" {
		pr = fmt.Sprintf("[PR #%s](%s)", path.Base(rep.PR), rep.PR)
	}
	switch {
	case t.Merged == "" && pr == "":
		return "`" + t.Version + "`"
	case t.Merged == "":
		return fmt.Sprintf("%s (`%s`)", pr, t.Version)
	case pr == "":
		return fmt.Sprintf("`%s` merged into the base (merge commit `%s`)", t.Merged, t.Version)
	}
	return fmt.Sprintf("%s (`%s`) merged into the base (merge commit `%s`)", pr, t.Merged, t.Version)
}

func (rep *report) markdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n\n", rep.title())
	sb.WriteString("| | |\n|-|-|\n")
	fmt.Fprintf(&sb, "| Base | `%s` |\n", rep.Base.Version)
	fmt.Fprintf(&sb, "| Target | %s |\n", rep.targetDesc())
	if rep.Base.Rev == rep.Target.Rev || rep.Base.Rev == rep.Target.commit {
		sb.WriteString("| Note | Base and target are the same commit (A/A run), so any flagged change is noise |\n")
	}
	fmt.Fprintf(&sb, "| Host | %s |\n", rep.Host)
	if len(rep.Systems) < len(systems) {
		fmt.Fprintf(&sb, "| Systems | %s (others skipped) |\n", strings.Join(rep.Systems, ", "))
	}
	if len(rep.Queries) < len(queryFiles) {
		fmt.Fprintf(&sb, "| Queries | %s (others skipped) |\n", strings.Join(rep.Queries, ", "))
	}
	runs := fmt.Sprintf("Median of %d runs per build, alternating builds", rep.Runs)
	if rep.Runs == 1 {
		runs = "1 run per build"
	}
	if slices.Contains(rep.Systems, "super-bsup") {
		runs += " (BSUP load: 1 run)"
	}
	fmt.Fprintf(&sb, "| Method | %s. Significant: medians differ by ±%.0f%% and at least %s (time) or %d MB (peak memory), and every target run is worse (or better) than every base run. |\n\n",
		runs, 100*rep.Threshold, rep.c.minDelta, rep.c.minMemDelta>>20)
	rep.section(&sb, "🔴 Regressions", func(r *row) bool { return r.status == regressed })
	rep.section(&sb, "🟢 Improvements", func(r *row) bool { return r.status == improved })
	var problems []string
	for _, r := range rep.Rows {
		if r.BaseErr != "" {
			problems = append(problems, fmt.Sprintf("- **%s** failed with base: `%s`", r.Test, shortErr(r.BaseErr)))
		}
		if r.TargetErr != "" {
			problems = append(problems, fmt.Sprintf("- **%s** failed with target: `%s`", r.Test, shortErr(r.TargetErr)))
		}
		if r.OutputMismatch {
			problems = append(problems, fmt.Sprintf("- **%s** output differs between base and target (timings may not be comparable)", r.Test))
		}
	}
	if len(problems) > 0 {
		fmt.Fprintf(&sb, "### ⚠️ Failures and output differences\n\n%s\n\n", strings.Join(problems, "\n"))
	}
	sb.WriteString("### All results\n\n")
	sb.WriteString("| Test | Wall time (s) | CPU time, user+sys (s) | Peak memory (MB) | |\n|-|-|-|-|-|\n")
	for _, r := range rep.Rows {
		if r.Status == "failed" {
			fmt.Fprintf(&sb, "| %s | | | | ❌ |\n", r.Test)
			continue
		}
		cells := []string{"", "", ""}
		for i, m := range r.Metrics {
			cells[i] = m.cell()
		}
		icon := statusIcon(r.status)
		if r.OutputMismatch {
			icon += " ⚠️"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s |\n", r.Test, cells[0], cells[1], cells[2], icon)
	}
	sb.WriteString("\n")
	if rep.count((*row).noisy) > 0 {
		sb.WriteString("〰️ *noisy*: the medians differ by enough to count, but the runs of the two builds overlap, so the change isn't flagged. This usually means something else on the machine slowed some of the runs. Re-run to check.\n\n")
	}
	return sb.String()
}

func (rep *report) section(sb *strings.Builder, heading string, match func(*row) bool) {
	var lines []string
	for _, r := range rep.Rows {
		if !match(r) {
			continue
		}
		var parts []string
		for _, m := range r.Metrics {
			if m.status != neutral {
				parts = append(parts, fmt.Sprintf("%s %s → %s %s (**%s**)", metricLabel(m.Name), m.value(m.Base), m.value(m.Target), m.unit, pct(m.Change)))
			}
		}
		lines = append(lines, fmt.Sprintf("- **%s**: %s", r.Test, strings.Join(parts, ", ")))
	}
	if len(lines) > 0 {
		fmt.Fprintf(sb, "### %s\n\n%s\n\n", heading, strings.Join(lines, "\n"))
	}
}

func (m *metric) value(v float64) string {
	return fmt.Sprintf(m.format, v/m.scale)
}

func (m *metric) cell() string {
	s := fmt.Sprintf("%s → %s %s", m.value(m.Base), m.value(m.Target), pct(m.Change))
	if m.Noisy {
		s += " 〰️ *noisy*"
	}
	if m.status != neutral {
		s = fmt.Sprintf("%s → %s **%s** %s", m.value(m.Base), m.value(m.Target), pct(m.Change), statusIcon(m.status))
	}
	return s
}

func metricLabel(name string) string {
	switch name {
	case "wall":
		return "wall time"
	case "cpu":
		return "CPU time"
	}
	return "peak memory"
}

func statusIcon(s status) string {
	switch s {
	case regressed:
		return "🔴"
	case improved:
		return "🟢"
	}
	return ""
}

// maxErrLen limits how much of an error the report shows.  An error can
// include a huge type, and the "Perf canary results" check's summary is
// limited to 65,535 characters.  The full errors are in results.json.
const maxErrLen = 300

func shortErr(err string) string {
	line, _, multiline := strings.Cut(err, "\n")
	if r := []rune(line); len(r) > maxErrLen {
		return string(r[:maxErrLen]) + "…"
	}
	if multiline {
		return line + " …"
	}
	return line
}

func pct(change float64) string {
	return fmt.Sprintf("%+.0f%%", 100*change)
}

// rereport reclassifies the raw results in a results.json file written by
// an earlier run using the thresholds in c, which is handy for tuning them.
func rereport(c config, path string) (*report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var old struct {
		Base    *build    `json:"base"`
		Target  *build    `json:"target"`
		PR      string    `json:"pr"`
		Host    string    `json:"host"`
		Runs    int       `json:"runs"`
		Systems []string  `json:"systems"`
		Queries []string  `json:"queries"`
		Results []*result `json:"results"`
	}
	if err := json.Unmarshal(b, &old); err != nil {
		return nil, err
	}
	c.runs = old.Runs
	c.pr = old.PR
	rep := newReport(c, old.Base, old.Target, old.Results)
	rep.Host = old.Host
	if old.Systems != nil {
		rep.Systems = old.Systems
	}
	if old.Queries != nil {
		rep.Queries = old.Queries
	}
	return rep, nil
}

func (rep *report) write(dir string) error {
	if err := writeJSON(filepath.Join(dir, "results.json"), rep); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "summary.md"), rep.markdown())
}
