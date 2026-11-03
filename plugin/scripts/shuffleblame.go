// Command shuffleblame finds which earlier test makes a Go test fail when the
// package's tests run in a different order, such as under go test -shuffle.
//
// It builds the package's test binary once, reproduces the failing order, and
// bisects the tests that ran before the failing one until it finds the
// smallest set that still makes it fail. It needs only the Go toolchain.
//
// Usage:
//
//	shuffleblame [flags] [DIR]
//
// Exit codes: 0 when the run finished (with or without findings), 1 with
// -fail when a test failed in any order, 2 when the input or a test run can't
// be trusted, so nothing is reported as clean.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// untrusted is an error that means nothing can be reported as clean.
type untrusted struct{ msg string }

func (e *untrusted) Error() string { return e.msg }

func distrust(format string, args ...any) error {
	return &untrusted{fmt.Sprintf(format, args...)}
}

// Order is one test order: the source order, or a -shuffle seed.
type Order struct {
	Shuffled bool
	Seed     int64
}

func (o Order) flag() string {
	if !o.Shuffled {
		return "off"
	}
	return strconv.FormatInt(o.Seed, 10)
}

func (o Order) String() string {
	if !o.Shuffled {
		return "source order"
	}
	return fmt.Sprintf("shuffle seed %d", o.Seed)
}

func (o Order) MarshalJSON() ([]byte, error) { return json.Marshal(o.flag()) }

// ---------------------------------------------------------------------------
// Parsing go test output

// Section is what one package's test output says.
type Section struct {
	Seed     *int64   // from the "-test.shuffle N" line, nil when absent
	Order    []string // top-level tests in the order they started (verbose only)
	Failed   []string // top-level tests that failed, in order
	Verbose  bool
	Panicked bool
	TimedOut bool
	NoTests  bool
	Passed   bool     // a final PASS line
	Skipped  []string // top-level tests that called t.Skip
	Paused   map[string]bool
	ended    map[string]bool
	output   map[string][]string // lines printed while each top-level test was current
}

var (
	reRun     = regexp.MustCompile(`^=== RUN\s+(\S+)$`)
	rePause   = regexp.MustCompile(`^=== PAUSE\s+(\S+)$`)
	reCont    = regexp.MustCompile(`^=== (?:CONT|NAME)\s+(\S+)$`)
	reEnd     = regexp.MustCompile(`^--- (PASS|FAIL|SKIP): (\S+) \(`)
	reShuffle = regexp.MustCompile(`^-test\.shuffle (-?\d+)$`)
	reRunning = regexp.MustCompile(`^\t\t(\S+) \(`)
	reResult  = regexp.MustCompile(`^(ok  |FAIL|\?   )\t(\S+)(?:[\t ](.*))?$`)
)

func topLevel(name string) string {
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return name
}

// ParseSection reads the output of one test binary run, or one package's
// part of a go test log.
func ParseSection(lines []string) *Section {
	s := &Section{Paused: map[string]bool{}, ended: map[string]bool{}, output: map[string][]string{}}
	failed := map[string]bool{}
	addFail := func(name string) {
		if !failed[name] {
			failed[name] = true
			s.Failed = append(s.Failed, name)
		}
	}
	current := ""
	inRunning := false
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if inRunning {
			if m := reRunning.FindStringSubmatch(line); m != nil {
				addFail(topLevel(m[1]))
				continue
			}
			inRunning = false
		}
		switch {
		case reShuffle.MatchString(line):
			n, err := strconv.ParseInt(reShuffle.FindStringSubmatch(line)[1], 10, 64)
			if err == nil {
				s.Seed = &n
			}
		case reRun.MatchString(line):
			name := reRun.FindStringSubmatch(line)[1]
			s.Verbose = true
			current = topLevel(name)
			if !strings.Contains(name, "/") {
				s.Order = append(s.Order, name)
			}
		case rePause.MatchString(line):
			name := rePause.FindStringSubmatch(line)[1]
			if !strings.Contains(name, "/") {
				s.Paused[name] = true
			}
		case reCont.MatchString(line):
			current = topLevel(reCont.FindStringSubmatch(line)[1])
		case reEnd.MatchString(line):
			m := reEnd.FindStringSubmatch(line)
			s.ended[m[2]] = true
			if m[1] == "FAIL" {
				addFail(m[2])
			}
			if m[1] == "SKIP" && !strings.Contains(m[2], "/") {
				s.Skipped = append(s.Skipped, m[2])
			}
			current = m[2]
		case strings.HasPrefix(line, "panic: test timed out after"):
			s.TimedOut = true
			s.Panicked = true
		case strings.HasPrefix(line, "\trunning tests:") && s.TimedOut:
			inRunning = true
		case strings.HasPrefix(line, "panic: "):
			s.Panicked = true
		case line == "PASS":
			s.Passed = true
		case line == "testing: warning: no tests to run":
			s.NoTests = true
		default:
			if current != "" && strings.HasPrefix(line, "    ") {
				s.output[current] = append(s.output[current], strings.TrimSpace(line))
			}
		}
	}
	return s
}

// unfinished lists top-level tests that started and never reported a result.
func (s *Section) unfinished() []string {
	var out []string
	for _, name := range s.Order {
		if !s.ended[name] {
			out = append(out, name)
		}
	}
	return out
}

// PackageResult is one package's part of a go test log.
type PackageResult struct {
	Package     string
	Status      string // "ok", "FAIL", "?" or "skip"
	BuildFailed bool
	NoTestFiles bool
	Lines       []string
}

// Log is a parsed go test log.
type Log struct {
	Packages []*PackageResult
	CutOff   bool // test output after the last package result
}

func (l *Log) find(pkg string) *PackageResult {
	var found *PackageResult
	for _, p := range l.Packages {
		if p.Package == pkg {
			found = p
		}
	}
	return found
}

func hasTestMarkers(lines []string) bool {
	for _, line := range lines {
		if reRun.MatchString(line) || reEnd.MatchString(line) || reShuffle.MatchString(line) || strings.HasPrefix(line, "panic: ") {
			return true
		}
	}
	return false
}

// ParseLog reads the saved output of go test, plain, -v or -json.
func ParseLog(data []byte) (*Log, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, distrust("the log is empty")
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, distrust("the log has NUL bytes, so it is binary or UTF-16 text, not go test output")
	}
	var all []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		all = append(all, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, distrust("can't read the log: %v", err)
	}
	first := ""
	for _, l := range all {
		if t := strings.TrimSpace(l); t != "" {
			first = t
			break
		}
	}
	if strings.HasPrefix(first, "{") {
		return parseJSONLog(all)
	}
	return parseTextLog(all), nil
}

func parseTextLog(lines []string) *Log {
	log := &Log{}
	var section []string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		m := reResult.FindStringSubmatch(line)
		if m == nil {
			section = append(section, line)
			continue
		}
		p := &PackageResult{Package: m[2], Status: strings.TrimSpace(m[1]), Lines: section}
		rest := m[3]
		p.BuildFailed = strings.Contains(rest, "[build failed]") || strings.Contains(rest, "[setup failed]")
		p.NoTestFiles = strings.Contains(rest, "[no test files]")
		log.Packages = append(log.Packages, p)
		section = nil
	}
	log.CutOff = hasTestMarkers(section)
	return log
}

type testEvent struct {
	Action      string
	Package     string
	Test        string
	Output      string
	FailedBuild string
}

func parseJSONLog(lines []string) (*Log, error) {
	log := &Log{}
	byPkg := map[string]*PackageResult{}
	var pkgOrder []string
	var text []string
	partial := map[string]string{}
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "{") {
			text = append(text, line)
			continue
		}
		var ev testEvent
		if err := json.Unmarshal([]byte(t), &ev); err != nil {
			return nil, distrust("line %d of the JSON log isn't a complete go test -json event, so the log is cut off or edited", i+1)
		}
		if ev.Package == "" {
			continue
		}
		p := byPkg[ev.Package]
		if p == nil {
			p = &PackageResult{Package: ev.Package}
			byPkg[ev.Package] = p
			pkgOrder = append(pkgOrder, ev.Package)
		}
		switch ev.Action {
		case "output", "build-output":
			// Output may be split mid-line across events.
			s := partial[ev.Package] + ev.Output
			parts := strings.Split(s, "\n")
			partial[ev.Package] = parts[len(parts)-1]
			p.Lines = append(p.Lines, parts[:len(parts)-1]...)
		case "pass", "fail", "skip":
			if ev.Test == "" {
				p.Status = map[string]string{"pass": "ok", "fail": "FAIL", "skip": "?"}[ev.Action]
				if ev.FailedBuild != "" {
					p.BuildFailed = true
				}
			}
		}
	}
	for _, name := range pkgOrder {
		p := byPkg[name]
		if rest := partial[name]; rest != "" {
			p.Lines = append(p.Lines, rest)
		}
		if p.Status == "" {
			// Started and never finished.
			log.CutOff = true
			continue
		}
		for _, l := range p.Lines {
			if m := reResult.FindStringSubmatch(l); m != nil && m[2] == name {
				if strings.Contains(m[3], "[build failed]") || strings.Contains(m[3], "[setup failed]") {
					p.BuildFailed = true
				}
				if strings.Contains(m[3], "[no test files]") {
					p.NoTestFiles = true
				}
			}
		}
		log.Packages = append(log.Packages, p)
	}
	// Older Go versions print build errors as plain text inside -json output.
	if len(text) > 0 {
		tl := parseTextLog(text)
		log.Packages = append(log.Packages, tl.Packages...)
		log.CutOff = log.CutOff || tl.CutOff
	}
	return log, nil
}

// FromLog picks the order and the failing tests for pkg out of a log.
func FromLog(log *Log, pkg string) (Order, *Section, error) {
	p := log.find(pkg)
	if p == nil {
		if log.CutOff {
			return Order{}, nil, distrust("the log has no result line for %s and ends partway through a package's test output, so it is cut off", pkg)
		}
		if len(log.Packages) == 0 {
			return Order{}, nil, distrust("the log isn't go test output: it has no package result line such as \"FAIL\\t%s\"", pkg)
		}
		var names []string
		for _, q := range log.Packages {
			names = append(names, q.Package)
		}
		return Order{}, nil, distrust("the log has no result for %s. It has: %s", pkg, strings.Join(names, ", "))
	}
	if p.BuildFailed {
		return Order{}, nil, distrust("the log shows %s failed to build, so no test ran", pkg)
	}
	if p.NoTestFiles {
		return Order{}, nil, distrust("the log shows %s has no test files", pkg)
	}
	if p.Status != "FAIL" {
		return Order{}, nil, distrust("the log shows %s passed, so there is no failure to explain", pkg)
	}
	s := ParseSection(p.Lines)
	if len(s.Failed) == 0 {
		s.Failed = s.unfinished()
	}
	if len(s.Failed) == 0 {
		return Order{}, nil, distrust("the log shows %s failed without naming a failing test. TestMain, an init function, os.Exit or a panic outside a test can do that, and the order of tests can't explain it", pkg)
	}
	o := Order{}
	if s.Seed != nil {
		o = Order{Shuffled: true, Seed: *s.Seed}
	}
	return o, s, nil
}

// ---------------------------------------------------------------------------
// Running the test binary

// Package describes the package under test, from go list.
type Package struct {
	ImportPath   string
	Name         string
	Dir          string
	TestGoFiles  []string
	XTestGoFiles []string
	Incomplete   bool
	Error        *struct{ Err string }
	Module       *struct{ Dir string }
}

// Runner runs one compiled test binary.
type Runner struct {
	Bin     string
	Dir     string
	Timeout time.Duration
	Tests   []string // every top-level Test function, from -test.list
	Runs    int      // binary runs so far
}

// Result is one run of the test binary.
type Result struct {
	*Section
	Ran    map[string]bool
	Exit   int
	Output string
}

func runCmd(ctx context.Context, dir, name string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
		err = nil
	}
	if ctx.Err() != nil {
		return buf.String(), -1, ctx.Err()
	}
	return buf.String(), code, err
}

func listPackage(dir string, buildFlags []string) (*Package, error) {
	args := append([]string{"list", "-json"}, buildFlags...)
	args = append(args, ".")
	out, code, err := runCmd(context.Background(), dir, "go", args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, distrust("go list failed in %s, so nothing was tested:\n%s", dir, indent(tail(out, 15)))
	}
	var p Package
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return nil, distrust("can't read go list output for %s: %v", dir, err)
	}
	if p.Error != nil {
		return nil, distrust("%s isn't a buildable package: %s", dir, p.Error.Err)
	}
	if len(p.TestGoFiles)+len(p.XTestGoFiles) == 0 {
		return nil, distrust("%s has no test files, so there is nothing to run", p.ImportPath)
	}
	return &p, nil
}

func build(p *Package, tmp string, buildFlags []string) (string, error) {
	bin := filepath.Join(tmp, "pkg.test")
	args := append([]string{"test", "-c", "-o", bin}, buildFlags...)
	args = append(args, ".")
	out, code, err := runCmd(context.Background(), p.Dir, "go", args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", distrust("the tests of %s don't build, so no test ran:\n%s", p.ImportPath, indent(tail(out, 15)))
	}
	if _, err := os.Stat(bin); err != nil {
		return "", distrust("go test -c wrote no test binary for %s:\n%s", p.ImportPath, indent(tail(out, 15)))
	}
	return bin, nil
}

func (r *Runner) list() error {
	out, code, err := runCmd(context.Background(), r.Dir, r.Bin, "-test.list", "^Test")
	if err != nil {
		return err
	}
	if code != 0 {
		return distrust("the test binary failed to list its tests (exit %d), so TestMain or an init function fails before any test:\n%s", code, indent(tail(out, 15)))
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Test") && !strings.ContainsAny(line, " \t") {
			r.Tests = append(r.Tests, line)
		}
	}
	if len(r.Tests) == 0 {
		return distrust("the test binary listed no Test functions, so there is nothing to run. Either the package has none, or its TestMain doesn't call m.Run")
	}
	return nil
}

func runPattern(names []string) string {
	return "^(" + strings.Join(names, "|") + ")$"
}

// Run runs the named tests (all when names is nil) in order o, count times.
// It refuses a run whose output doesn't account for its exit code.
func (r *Runner) Run(names []string, o Order, count int) (*Result, error) {
	// -test.paniconexit0 is what go test sets, so os.Exit(0) in a test fails.
	args := []string{"-test.v", "-test.paniconexit0", "-test.count=" + strconv.Itoa(count),
		"-test.shuffle=" + o.flag(), "-test.timeout=" + r.Timeout.String()}
	want := r.Tests
	if names != nil {
		args = append(args, "-test.run="+runPattern(names))
		want = names
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.Timeout+time.Minute)
	defer cancel()
	r.Runs++
	out, code, err := runCmd(ctx, r.Dir, r.Bin, args...)
	if err != nil {
		return nil, distrust("a test run didn't finish within %s: %v", r.Timeout+time.Minute, err)
	}
	s := ParseSection(strings.Split(out, "\n"))
	res := &Result{Section: s, Ran: map[string]bool{}, Exit: code, Output: out}
	for _, n := range s.Order {
		res.Ran[n] = true
	}
	if code != 0 {
		if len(s.Failed) == 0 || s.Panicked {
			for _, n := range s.unfinished() {
				if !contains(s.Failed, n) {
					s.Failed = append(s.Failed, n)
				}
			}
		}
		if len(s.Failed) == 0 {
			return nil, distrust("the test binary exited with status %d but no test failed. TestMain, an init function, os.Exit or a panic outside a test can do that:\n%s", code, indent(tail(out, 15)))
		}
		return res, nil
	}
	if len(s.Failed) > 0 {
		return nil, distrust("the test binary exited 0 but its output names failing tests (%s), so its output can't be trusted", strings.Join(s.Failed, ", "))
	}
	if !s.Passed {
		return nil, distrust("the test binary exited 0 without printing PASS, so it stopped before its tests finished:\n%s", indent(tail(out, 15)))
	}
	if u := s.unfinished(); len(u) > 0 {
		return nil, distrust("the test binary exited 0 while %s was still running", strings.Join(u, ", "))
	}
	for _, n := range want {
		if !res.Ran[n] {
			return nil, distrust("the test binary exited 0 without running %s. TestMain may not call m.Run, or it exits early:\n%s", n, indent(tail(out, 15)))
		}
	}
	return res, nil
}

// fails runs before+victim in order o and reports whether the victim failed.
func (r *Runner) fails(before []string, victim string, o Order) (bool, *Result, error) {
	names := append(append([]string{}, before...), victim)
	res, err := r.Run(names, o, 1)
	if err != nil {
		return false, nil, err
	}
	if contains(res.Failed, victim) {
		return true, res, nil
	}
	if !res.Ran[victim] {
		var who []string
		for _, n := range res.Failed {
			who = append(who, n)
		}
		return false, res, distrust("%s never ran, because %s failed and stopped the test binary", victim, strings.Join(who, ", "))
	}
	return false, res, nil
}

// ---------------------------------------------------------------------------
// Finding the polluter

// Finding explains one failing test.
type Finding struct {
	Test      string   `json:"test"`
	Order     Order    `json:"order"`
	Kind      string   `json:"kind"` // polluted, fails-alone, not-reproduced, runs-twice
	Polluters []string `json:"polluters,omitempty"`
	Where     []string `json:"where,omitempty"`
	Alone     [2]int   `json:"alone"` // failures, runs
	After     [2]int   `json:"after"` // failures, runs with the polluters
	RunsTwice bool     `json:"failsWhenRunTwice"`
	Failure   []string `json:"failure,omitempty"`
	Reproduce string   `json:"reproduce,omitempty"`
	Note      string   `json:"note,omitempty"`
}

func (r *Runner) countFails(before []string, victim string, o Order, n int) (int, *Result, error) {
	failed := 0
	var last *Result
	for i := 0; i < n; i++ {
		f, res, err := r.fails(before, victim, o)
		if err != nil {
			return 0, nil, err
		}
		if f {
			failed++
			last = res
		}
	}
	return failed, last, nil
}

// minimize finds the smallest subset of candidates, kept in order, that
// still makes victim fail when run before it.
func (r *Runner) minimize(candidates []string, victim string, o Order) ([]string, error) {
	c := candidates
	for len(c) > 1 {
		half := len(c) / 2
		f, _, err := r.fails(c[:half], victim, o)
		if err != nil {
			return nil, err
		}
		if f {
			c = c[:half]
			continue
		}
		f, _, err = r.fails(c[half:], victim, o)
		if err != nil {
			return nil, err
		}
		if f {
			c = c[half:]
			continue
		}
		break
	}
	// Needs tests from both halves: drop one at a time.
	for i := 0; len(c) > 1 && i < len(c); {
		without := append(append([]string{}, c[:i]...), c[i+1:]...)
		f, _, err := r.fails(without, victim, o)
		if err != nil {
			return nil, err
		}
		if f {
			c = without
		} else {
			i++
		}
	}
	return c, nil
}

// Explain works out why victim failed in run full, which used order o.
func (r *Runner) Explain(victim string, o Order, full *Result, repeat int) (*Finding, error) {
	f := &Finding{Test: victim, Order: o}
	alone, _, err := r.countFails(nil, victim, o, repeat)
	if err != nil {
		return nil, err
	}
	f.Alone = [2]int{alone, repeat}
	if alone > 0 {
		f.Kind = "fails-alone"
		_, res, _ := r.fails(nil, victim, o)
		if res != nil {
			f.Failure = failureLines(res, victim)
		}
		return f, nil
	}
	twice, err := r.Run([]string{victim}, o, 2)
	if err != nil {
		return nil, err
	}
	f.RunsTwice = len(twice.Failed) > 0

	var candidates []string
	if full.Paused[victim] {
		// A parallel test resumes after every sequential test has run.
		for _, n := range full.Order {
			if n != victim {
				candidates = append(candidates, n)
			}
		}
	} else {
		for _, n := range full.Order {
			if n == victim {
				break
			}
			candidates = append(candidates, n)
		}
	}
	failedAgain, _, err := r.fails(candidates, victim, o)
	if err != nil {
		return nil, err
	}
	if !failedAgain {
		f.Kind = "not-reproduced"
		if f.RunsTwice {
			f.Kind = "runs-twice"
		}
		return f, nil
	}
	set, err := r.minimize(candidates, victim, o)
	if err != nil {
		return nil, err
	}
	after, res, err := r.countFails(set, victim, o, repeat)
	if err != nil {
		return nil, err
	}
	f.After = [2]int{after, repeat}
	if after == 0 {
		f.Kind = "not-reproduced"
		f.Note = fmt.Sprintf("%s failed after %s once, then passed %d times in a row, so it is flaky", victim, strings.Join(set, ", "), repeat)
		return f, nil
	}
	f.Kind = "polluted"
	f.Polluters = set
	f.Failure = failureLines(res, victim)
	return f, nil
}

func failureLines(res *Result, victim string) []string {
	lines := res.output[victim]
	if len(lines) > 6 {
		lines = append(lines[:6:6], "...")
	}
	return lines
}

// ---------------------------------------------------------------------------
// Report

// Report is the result of one shuffleblame run.
type Report struct {
	Package  string     `json:"package"`
	Dir      string     `json:"dir"`
	Tests    int        `json:"tests"`
	Source   string     `json:"source"`
	Orders   []Order    `json:"ordersTried"`
	Failing  []string   `json:"failing"`
	Findings []*Finding `json:"findings"`
	Skipped  []string   `json:"skipped,omitempty"`
	Warnings []string   `json:"warnings,omitempty"`
	Runs     int        `json:"testBinaryRuns"`
}

func reproduce(dirArg string, buildFlags []string, names []string, o Order) string {
	parts := []string{"go test -count=1"}
	if strings.HasPrefix(dirArg, "-C ") {
		parts = []string{"go " + strings.TrimSuffix(dirArg, " .") + " test -count=1"}
		dirArg = "."
	}
	parts = append(parts, buildFlags...)
	parts = append(parts, "-run '"+runPattern(names)+"'")
	if o.Shuffled {
		parts = append(parts, "-shuffle="+o.flag())
	}
	parts = append(parts, dirArg)
	return strings.Join(parts, " ")
}

func describeOrders(orders []Order) string {
	var seeds []int64
	source := false
	for _, o := range orders {
		if o.Shuffled {
			seeds = append(seeds, o.Seed)
		} else {
			source = true
		}
	}
	var parts []string
	if source {
		parts = append(parts, "source order")
	}
	if len(seeds) == 1 {
		parts = append(parts, fmt.Sprintf("shuffle seed %d", seeds[0]))
	} else if len(seeds) > 1 {
		contiguous := true
		for i := 1; i < len(seeds); i++ {
			if seeds[i] != seeds[i-1]+1 {
				contiguous = false
			}
		}
		if contiguous {
			parts = append(parts, fmt.Sprintf("shuffle seeds %d-%d", seeds[0], seeds[len(seeds)-1]))
		} else {
			var s []string
			for _, n := range seeds {
				s = append(s, strconv.FormatInt(n, 10))
			}
			parts = append(parts, "shuffle seeds "+strings.Join(s, ", "))
		}
	}
	return strings.Join(parts, " and ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// WriteText prints the report for people.
func (rep *Report) WriteText(w io.Writer) {
	fmt.Fprintf(w, "shuffleblame: %s, %s\n", rep.Package, plural(rep.Tests, "test", "tests"))
	fmt.Fprintf(w, "Orders: %s (%s)\n", describeOrders(rep.Orders), rep.Source)
	for _, warn := range rep.Warnings {
		fmt.Fprintf(w, "Warning: %s\n", warn)
	}
	if len(rep.Skipped) > 0 {
		names := rep.Skipped
		if len(names) > 5 {
			names = append(names[:5:5], "...")
		}
		fmt.Fprintf(w, "Skipped, so not checked: %s (%s)\n", plural(len(rep.Skipped), "test", "tests"), strings.Join(names, ", "))
	}
	if len(rep.Findings) == 0 {
		what := "Every test passed"
		if len(rep.Skipped) > 0 {
			what = "Every test that ran passed"
		}
		if len(rep.Orders) == 1 {
			fmt.Fprintf(w, "\n%s in %s.\n", what, rep.Orders[0])
		} else {
			fmt.Fprintf(w, "\n%s in all %d orders.\n", what, len(rep.Orders))
		}
		fmt.Fprintln(w, "That covers only these orders. A test that fails in another order can still exist.")
		return
	}
	for _, f := range rep.Findings {
		fmt.Fprintln(w)
		switch f.Kind {
		case "polluted":
			fmt.Fprintf(w, "%s fails after %s (%s)\n", f.Test, joinAnd(f.Polluters), f.Order)
			fmt.Fprintf(w, "  alone: passed %d of %d runs\n", f.Alone[1]-f.Alone[0], f.Alone[1])
			fmt.Fprintf(w, "  after %s: failed %d of %d runs\n", joinAnd(f.Polluters), f.After[0], f.After[1])
			for _, where := range f.Where {
				fmt.Fprintf(w, "  %s\n", where)
			}
		case "fails-alone":
			fmt.Fprintf(w, "%s fails on its own (%s), so the order of tests doesn't explain it\n", f.Test, f.Order)
			fmt.Fprintf(w, "  alone: failed %d of %d runs\n", f.Alone[0], f.Alone[1])
		case "runs-twice":
			fmt.Fprintf(w, "%s fails when it runs a second time in the same process (-count=2), but not after the tests that ran before it (%s)\n", f.Test, f.Order)
		case "not-explained":
			fmt.Fprintf(w, "%s failed (%s), but %s\n", f.Test, f.Order, f.Note)
		default:
			fmt.Fprintf(w, "%s failed in %s, but didn't fail again with the same tests before it, so no test was blamed\n", f.Test, f.Order)
			if f.Note != "" {
				fmt.Fprintf(w, "  %s\n", f.Note)
			}
		}
		if f.RunsTwice && f.Kind != "runs-twice" {
			fmt.Fprintf(w, "  It also fails when it runs twice in a row (-count=2), so it leaves state behind itself\n")
		}
		if len(f.Failure) > 0 {
			fmt.Fprintln(w, "  failure:")
			for _, l := range f.Failure {
				fmt.Fprintf(w, "    %s\n", l)
			}
		}
		if f.Reproduce != "" {
			fmt.Fprintf(w, "  reproduce:\n    %s\n", f.Reproduce)
		}
	}
	counts := map[string]int{}
	for _, f := range rep.Findings {
		counts[f.Kind]++
	}
	fmt.Fprintln(w)
	var parts []string
	if n := counts["polluted"]; n > 0 {
		parts = append(parts, plural(n, "test fails because of an earlier test", "tests fail because of earlier tests"))
	}
	if n := counts["runs-twice"]; n > 0 {
		parts = append(parts, plural(n, "test fails when run twice", "tests fail when run twice"))
	}
	if n := counts["fails-alone"]; n > 0 {
		parts = append(parts, plural(n, "test fails on its own", "tests fail on their own"))
	}
	if n := counts["not-explained"]; n > 0 {
		parts = append(parts, plural(n, "failure wasn't explained", "failures weren't explained"))
	}
	if n := counts["not-reproduced"]; n > 0 {
		parts = append(parts, plural(n, "failure didn't reproduce", "failures didn't reproduce"))
	}
	fmt.Fprintf(w, "%s. %s.\n", strings.Join(parts, ", "), plural(rep.Runs, "test binary run", "test binary runs"))
}

func joinAnd(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// definitions maps each Test function to "file:line" in the package's tests.
func definitions(p *Package, names []string) map[string]string {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	out := map[string]string{}
	files := append(append([]string{}, p.TestGoFiles...), p.XTestGoFiles...)
	re := regexp.MustCompile(`^func (Test\w*)\(`)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(p.Dir, file))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if m := re.FindStringSubmatch(line); m != nil && want[m[1]] {
				out[m[1]] = fmt.Sprintf("%s:%d", file, i+1)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// main

type options struct {
	dir       string
	log       string
	seed      string
	runs      int
	firstSeed int64
	repeat    int
	test      string
	race      bool
	tags      string
	timeout   time.Duration
	fail      bool
	json      bool
	max       int
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

func readLog(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}

func run(opt options, stdin io.Reader, stdout io.Writer) (int, error) {
	var buildFlags []string
	if opt.race {
		buildFlags = append(buildFlags, "-race")
	}
	if opt.tags != "" {
		buildFlags = append(buildFlags, "-tags="+opt.tags)
	}
	info, err := os.Stat(opt.dir)
	if err != nil || !info.IsDir() {
		return 2, distrust("%s isn't a directory. Pass the directory of one package, such as ./internal/cache", opt.dir)
	}

	// Read the log before building, so a bad log fails fast.
	var logData []byte
	if opt.log != "" {
		logData, err = readLog(opt.log, stdin)
		if err != nil {
			return 2, distrust("can't read the log: %v", err)
		}
	}

	pkg, err := listPackage(opt.dir, buildFlags)
	if err != nil {
		return 2, err
	}
	tmp, err := os.MkdirTemp("", "shuffleblame")
	if err != nil {
		return 2, err
	}
	defer os.RemoveAll(tmp)

	rep := &Report{Package: pkg.ImportPath, Dir: opt.dir}
	var logSection *Section
	var orders []Order
	switch {
	case opt.log != "":
		parsed, err := ParseLog(logData)
		if err != nil {
			return 2, err
		}
		o, s, err := FromLog(parsed, pkg.ImportPath)
		if err != nil {
			return 2, err
		}
		if opt.seed != "" {
			return 2, distrust("-seed and -log can't be used together: the log sets the order")
		}
		logSection = s
		orders = []Order{o}
		name := opt.log
		if name == "-" {
			name = "standard input"
		}
		rep.Source = "from the log " + name
		if s.Seed == nil {
			rep.Warnings = append(rep.Warnings, "the log has no -test.shuffle line, so it is taken to be from a run in source order")
		}
	case opt.seed != "":
		if opt.seed == "off" {
			orders = []Order{{}}
		} else {
			n, err := strconv.ParseInt(opt.seed, 10, 64)
			if err != nil {
				return 2, distrust("-seed must be a number or off, not %q", opt.seed)
			}
			orders = []Order{{Shuffled: true, Seed: n}}
		}
		rep.Source = "from -seed"
	default:
		orders = []Order{{}}
		for i := 0; i < opt.runs; i++ {
			orders = append(orders, Order{Shuffled: true, Seed: opt.firstSeed + int64(i)})
		}
		rep.Source = "searched"
	}

	bin, err := build(pkg, tmp, buildFlags)
	if err != nil {
		return 2, err
	}
	r := &Runner{Bin: bin, Dir: pkg.Dir, Timeout: opt.timeout}
	if err := r.list(); err != nil {
		return 2, err
	}
	rep.Tests = len(r.Tests)
	if opt.test != "" && !contains(r.Tests, opt.test) {
		return 2, distrust("%s has no test named %s", pkg.ImportPath, opt.test)
	}

	type victim struct {
		name string
		o    Order
		full *Result
	}
	var victims []victim
	seen := map[string]bool{}
	for _, o := range orders {
		rep.Orders = append(rep.Orders, o)
		full, err := r.Run(nil, o, 1)
		if err != nil {
			return 2, err
		}
		for _, n := range full.Skipped {
			if !contains(rep.Skipped, n) {
				rep.Skipped = append(rep.Skipped, n)
			}
		}
		failed := full.Failed
		if logSection != nil {
			failed = logSection.Failed
			if len(logSection.Order) > 0 && !sameOrder(logSection.Order, full.Order) {
				rep.Warnings = append(rep.Warnings, "the tests ran in a different order here than in the log, so the set of tests differs (build tags, GOOS, or changed code). Results use the order here")
			}
		}
		for _, n := range failed {
			if seen[n] || (opt.test != "" && n != opt.test) {
				continue
			}
			seen[n] = true
			if !contains(r.Tests, n) {
				note := "only Test functions are explained"
				if logSection != nil && !contains(full.Failed, n) {
					note = "it isn't a test of this package here"
				}
				rep.Findings = append(rep.Findings, &Finding{Test: n, Order: o, Kind: "not-explained", Note: note})
				rep.Failing = append(rep.Failing, n)
				continue
			}
			rep.Failing = append(rep.Failing, n)
			victims = append(victims, victim{n, o, full})
		}
	}
	if len(rep.Skipped) == len(r.Tests) {
		return 2, distrust("every test in %s called t.Skip, so no order was checked", pkg.ImportPath)
	}
	if len(victims) > opt.max {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d tests failed; only the first %d are explained (raise -max)", len(victims), opt.max))
		victims = victims[:opt.max]
	}
	for _, v := range victims {
		f, err := r.Explain(v.name, v.o, v.full, opt.repeat)
		if err != nil {
			return 2, err
		}
		if f.Kind == "polluted" {
			defs := definitions(pkg, append(append([]string{}, f.Polluters...), f.Test))
			for _, n := range append(append([]string{}, f.Polluters...), f.Test) {
				if d, ok := defs[n]; ok {
					f.Where = append(f.Where, fmt.Sprintf("%s is at %s", n, d))
				}
			}
			f.Reproduce = reproduce(pkgArg(pkg, opt.dir), buildFlags, append(append([]string{}, f.Polluters...), f.Test), v.o)
		} else if f.Kind == "fails-alone" {
			f.Reproduce = reproduce(pkgArg(pkg, opt.dir), buildFlags, []string{f.Test}, Order{})
		} else if f.Kind == "runs-twice" {
			f.Reproduce = strings.Replace(reproduce(pkgArg(pkg, opt.dir), buildFlags, []string{f.Test}, Order{}), "-count=1", "-count=2", 1)
		}
		rep.Findings = append(rep.Findings, f)
	}
	sort.SliceStable(rep.Findings, func(i, j int) bool {
		return kindRank(rep.Findings[i].Kind) < kindRank(rep.Findings[j].Kind)
	})
	rep.Runs = r.Runs
	if logSection != nil && len(rep.Findings) == 0 {
		return 2, distrust("none of the tests that failed in the log (%s) is a test of %s here", strings.Join(logSection.Failed, ", "), pkg.ImportPath)
	}
	if opt.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return 2, err
		}
	} else {
		rep.WriteText(stdout)
	}
	if opt.fail && len(rep.Findings) > 0 {
		return 1, nil
	}
	return 0, nil
}

func kindRank(k string) int {
	switch k {
	case "polluted":
		return 0
	case "runs-twice":
		return 1
	case "fails-alone":
		return 2
	case "not-reproduced":
		return 3
	}
	return 4
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// pkgArg is how to name the package's directory in a go test command run
// from the current directory: a relative path inside the current module, or
// "-C DIR ." when the package is in another module.
func pkgArg(p *Package, dir string) string {
	cwd, err := os.Getwd()
	if err == nil && p.Module != nil && p.Module.Dir != "" {
		if rel, err := filepath.Rel(p.Module.Dir, cwd); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if cwdMod := goModDir(cwd); cwdMod == p.Module.Dir {
				return dirArg(dir)
			}
		}
	}
	return "-C " + dirArg(dir) + " ."
}

// goModDir finds the directory of the go.mod that governs dir.
func goModDir(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// dirArg is the directory as a go test argument.
func dirArg(dir string) string {
	if filepath.IsAbs(dir) || dir == "." || strings.HasPrefix(dir, "./") || strings.HasPrefix(dir, "../") {
		return filepath.ToSlash(dir)
	}
	return "./" + filepath.ToSlash(dir)
}

const usage = `Usage: shuffleblame [flags] [DIR]

Finds which earlier test makes a Go test fail when the package's tests run in
another order, such as under go test -shuffle. DIR is the directory of one
package (default ".").

With -log, it explains the failing tests in a saved go test log (plain, -v or
-json), using the shuffle seed printed in it. With -seed, it uses that order.
Otherwise it tries source order and -runs shuffle seeds, starting at -first-seed.

Exit codes: 0 finished, 1 a test failed and -fail is set, 2 the input or a run
can't be trusted.

Flags:
`

func main() {
	fs := flag.NewFlagSet("shuffleblame", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
	}
	var opt options
	fs.StringVar(&opt.log, "log", "", "saved go test output to explain, or - for standard input")
	fs.StringVar(&opt.seed, "seed", "", "shuffle seed to run, or off for source order")
	fs.IntVar(&opt.runs, "runs", 20, "shuffle seeds to try when searching")
	fs.Int64Var(&opt.firstSeed, "first-seed", 1, "first shuffle seed to try when searching")
	fs.IntVar(&opt.repeat, "repeat", 3, "runs used to confirm a test passes alone and fails after its polluter")
	fs.StringVar(&opt.test, "test", "", "only explain this failing test")
	fs.BoolVar(&opt.race, "race", false, "build the tests with -race")
	fs.StringVar(&opt.tags, "tags", "", "build tags, as for go test -tags")
	fs.DurationVar(&opt.timeout, "timeout", 10*time.Minute, "timeout for each run of the test binary")
	fs.BoolVar(&opt.fail, "fail", false, "exit 1 when any test failed")
	fs.BoolVar(&opt.json, "json", false, "print JSON")
	fs.IntVar(&opt.max, "max", 10, "most failing tests to explain")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "shuffleblame: pass one package directory; flags go before it")
		os.Exit(2)
	}
	opt.dir = "."
	if fs.NArg() == 1 {
		opt.dir = fs.Arg(0)
	}
	if opt.runs < 0 || opt.repeat < 1 || opt.max < 1 || opt.timeout <= 0 {
		fmt.Fprintln(os.Stderr, "shuffleblame: -runs must be 0 or more, and -repeat, -max and -timeout more than 0")
		os.Exit(2)
	}
	code, err := run(opt, os.Stdin, os.Stdout)
	if err != nil {
		var u *untrusted
		if errors.As(err, &u) {
			fmt.Fprintf(os.Stderr, "shuffleblame: nothing was blamed or cleared: %s\n", u.msg)
		} else {
			fmt.Fprintf(os.Stderr, "shuffleblame: %v\n", err)
		}
		os.Exit(2)
	}
	os.Exit(code)
}
