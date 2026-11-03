// Package tests runs shuffleblame the way the skill and the README do: as a
// single Go file, on real packages built here and on the example module.
package tests

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "shuffleblame-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "shuffleblame")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// Build the single file, as the skill runs it.
	out, err := exec.Command("go", "build", "-o", bin, "../plugin/scripts/shuffleblame.go").CombinedOutput()
	if err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// shuffleblame runs the tool from the repository root.
func shuffleblame(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = ".."
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, out.String(), errOut.String()
}

// module writes a throwaway Go module and returns its directory.
func module(t *testing.T, path string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module " + path + "\n\ngo 1.22\n"
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// readmeExample is one `$ command` in the README and the output under it.
type readmeExample struct {
	line int
	cmd  string
	want string
}

func readmeExamples(t *testing.T) []readmeExample {
	f, err := os.Open("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	var examples []readmeExample
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "    $ ") {
			continue
		}
		ex := readmeExample{line: i + 1, cmd: strings.TrimPrefix(lines[i], "    $ ")}
		var out []string
		j := i + 1
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.HasPrefix(l, "    $ ") || (l != "" && !strings.HasPrefix(l, "    ")) {
				break
			}
			out = append(out, strings.TrimPrefix(l, "    "))
		}
		for len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		ex.want = strings.Join(out, "\n")
		examples = append(examples, ex)
		i = j - 1
	}
	return examples
}

// TestReadmeExamples runs every `$ ` line in the README from the repository
// root and checks that the output shown under it is what the command prints.
func TestReadmeExamples(t *testing.T) {
	examples := readmeExamples(t)
	if len(examples) < 4 {
		t.Fatalf("only %d README examples found", len(examples))
	}
	for _, ex := range examples {
		t.Run(fmt.Sprintf("line%d", ex.line), func(t *testing.T) {
			cmd := exec.Command("sh", "-c", ex.cmd)
			cmd.Dir = ".."
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("README line %d: %s: %v\n%s", ex.line, ex.cmd, err, out)
			}
			got := strings.TrimRight(string(out), "\n")
			if got != ex.want {
				t.Fatalf("README line %d: %s\n--- README\n%s\n--- actual\n%s", ex.line, ex.cmd, ex.want, got)
			}
		})
	}
}

func TestExampleExitCodes(t *testing.T) {
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"examples/shop/pricing"}, 0},
		{[]string{"-fail", "examples/shop/pricing"}, 1},
		{[]string{"-fail", "-log", "examples/logs/shuffle-4.txt", "examples/shop/pricing"}, 1},
		{[]string{"examples/shop/ledger"}, 0},
		{[]string{"-fail", "examples/shop/ledger"}, 0},
		{[]string{"-fail", "-seed", "2", "examples/shop/ledger"}, 0},
	}
	for _, c := range cases {
		code, out, errOut := shuffleblame(t, c.args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d\n%s%s", c.args, code, c.code, out, errOut)
		}
	}
}

// The ledger tests restore what they change, so every order passes, and the
// report says what it covered rather than that the package is clean.
func TestHealthyPackageStaysQuiet(t *testing.T) {
	code, out, _ := shuffleblame(t, "-runs", "40", "examples/shop/ledger")
	if code != 0 || !strings.Contains(out, "Every test passed in all 41 orders.") || !strings.Contains(out, "can still exist") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func jsonReport(t *testing.T, args ...string) map[string]any {
	t.Helper()
	code, out, errOut := shuffleblame(t, append([]string{"-json"}, args...)...)
	if code != 0 {
		t.Fatalf("%v: exit %d\n%s%s", args, code, out, errOut)
	}
	var rep map[string]any
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return rep
}

func findings(rep map[string]any) []map[string]any {
	var out []map[string]any
	list, _ := rep["findings"].([]any)
	for _, f := range list {
		out = append(out, f.(map[string]any))
	}
	return out
}

func polluters(f map[string]any) string {
	var names []string
	list, _ := f["polluters"].([]any)
	for _, p := range list {
		names = append(names, p.(string))
	}
	return strings.Join(names, ",")
}

// A test that fails only after two other tests have both run gets both.
func TestTwoPollutersTogether(t *testing.T) {
	dir := module(t, "example.com/two", map[string]string{
		"two.go": "package two\n\nvar Hits int\n",
		"two_test.go": `package two

import "testing"

func TestUnderLimit(t *testing.T) {
	if Hits >= 2 {
		t.Fatalf("Hits = %d, want under 2", Hits)
	}
}
func TestA(t *testing.T) { Hits++ }
func TestB(t *testing.T) { Hits++ }
func TestC(t *testing.T) {}
func TestD(t *testing.T) {}
func TestE(t *testing.T) {}
`,
	})
	rep := jsonReport(t, "-seed", "off", "-test", "TestUnderLimit", dir)
	if len(findings(rep)) != 0 {
		t.Fatalf("source order should pass: %v", rep)
	}
	rep = jsonReport(t, "-runs", "30", dir)
	fs := findings(rep)
	if len(fs) != 1 || fs[0]["kind"] != "polluted" || polluters(fs[0]) != "TestA,TestB" && polluters(fs[0]) != "TestB,TestA" {
		t.Fatalf("got %v", fs)
	}
}

// A polluter in an external _test package, and a parallel victim.
func TestExternalPackageAndParallelVictim(t *testing.T) {
	dir := module(t, "example.com/ext", map[string]string{
		"ext.go": "package ext\n\nvar Mode = \"strict\"\n",
		"ext_test.go": `package ext_test

import (
	"testing"

	"example.com/ext"
)

func TestLoose(t *testing.T) { ext.Mode = "loose" }
func TestStrict(t *testing.T) {
	t.Parallel()
	if ext.Mode != "strict" {
		t.Fatalf("Mode = %q", ext.Mode)
	}
}
func TestOther(t *testing.T) {}
`,
	})
	rep := jsonReport(t, dir)
	fs := findings(rep)
	if len(fs) != 1 || fs[0]["test"] != "TestStrict" || polluters(fs[0]) != "TestLoose" {
		t.Fatalf("got %v", fs)
	}
	where := fmt.Sprint(fs[0]["where"])
	if !strings.Contains(where, "TestLoose is at ext_test.go:9") {
		t.Fatalf("where = %s", where)
	}
	// The printed command must reproduce the failure.
	cmd := exec.Command("sh", "-c", fs[0]["reproduce"].(string))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("%s passed:\n%s", fs[0]["reproduce"], out)
	}
}

// A test that fails on its own is reported as such, with no test blamed.
func TestFailsAlone(t *testing.T) {
	dir := module(t, "example.com/alone", map[string]string{
		"a_test.go": "package alone\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) { t.Fatal(\"always\") }\nfunc TestFine(t *testing.T) {}\n",
	})
	rep := jsonReport(t, dir)
	fs := findings(rep)
	if len(fs) != 1 || fs[0]["kind"] != "fails-alone" || polluters(fs[0]) != "" {
		t.Fatalf("got %v", fs)
	}
	if code, _, _ := shuffleblame(t, "-fail", dir); code != 1 {
		t.Fatalf("-fail exit %d, want 1", code)
	}
}

// A test that registers something global fails the second time it runs in
// the same process. A log from go test -count=2 shows it.
func TestRunsTwice(t *testing.T) {
	dir := module(t, "example.com/reg", map[string]string{
		"reg.go": `package reg

import "fmt"

var names = map[string]bool{}

func Register(n string) error {
	if names[n] {
		return fmt.Errorf("%s already registered", n)
	}
	names[n] = true
	return nil
}
`,
		"reg_test.go": `package reg

import "testing"

func TestRegister(t *testing.T) {
	if err := Register("orders"); err != nil {
		t.Fatal(err)
	}
}
func TestOther(t *testing.T) {}
`,
	})
	cmd := exec.Command("go", "test", "-count=2", ".")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	logFile := filepath.Join(t.TempDir(), "count2.txt")
	os.WriteFile(logFile, out, 0o644)
	rep := jsonReport(t, "-log", logFile, dir)
	fs := findings(rep)
	if len(fs) != 1 || fs[0]["kind"] != "runs-twice" || fs[0]["failsWhenRunTwice"] != true {
		t.Fatalf("got %v\nlog:\n%s", fs, out)
	}
}

// A hanging test is stopped by -timeout and reported, not cleared.
func TestTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	dir := module(t, "example.com/hang", map[string]string{
		"h_test.go": "package hang\n\nimport (\"testing\"; \"time\")\n\nfunc TestHang(t *testing.T) { time.Sleep(time.Hour) }\nfunc TestFine(t *testing.T) {}\n",
	})
	code, out, errOut := shuffleblame(t, "-runs", "0", "-repeat", "1", "-timeout", "1s", "-fail", dir)
	if code != 1 || !strings.Contains(out, "TestHang fails on its own") {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
}

// badPackages are packages whose tests can't be trusted to say anything about
// order. Each sits at example.com/shop/pricing, so the real logs match it.
var badPackages = map[string]map[string]string{
	"does not compile": {
		"pricing/p.go":      "package pricing\n\nfunc F() int { return \"x\" }\n",
		"pricing/p_test.go": "package pricing\n\nimport \"testing\"\n\nfunc TestFormatDefault(t *testing.T) {}\n",
	},
	"test does not compile": {
		"pricing/p.go":      "package pricing\n",
		"pricing/p_test.go": "package pricing\n\nimport \"testing\"\n\nfunc TestFormatDefault(t *testing.T) { undefined() }\n",
	},
	"no test files": {
		"pricing/p.go": "package pricing\n",
	},
	"only examples": {
		"pricing/p.go":      "package pricing\n",
		"pricing/p_test.go": "package pricing\n\nimport \"fmt\"\n\nfunc ExampleX() {\n\tfmt.Println(1)\n\t// Output: 1\n}\n",
	},
	"TestMain skips m.Run": {
		"pricing/p.go":      "package pricing\n",
		"pricing/p_test.go": "package pricing\n\nimport \"testing\"\n\nfunc TestMain(m *testing.M) {}\nfunc TestFormatDefault(t *testing.T) {}\n",
	},
	"TestMain exits 1": {
		"pricing/p.go":      "package pricing\n",
		"pricing/p_test.go": "package pricing\n\nimport (\"os\"; \"testing\")\n\nfunc TestMain(m *testing.M) { os.Exit(1) }\nfunc TestFormatDefault(t *testing.T) {}\n",
	},
	"TestMain ignores failures": {
		"pricing/p.go":      "package pricing\n",
		"pricing/p_test.go": "package pricing\n\nimport (\"os\"; \"testing\")\n\nfunc TestMain(m *testing.M) { m.Run(); os.Exit(0) }\nfunc TestFormatDefault(t *testing.T) { t.Fatal(\"no\") }\n",
	},
	"TestMain fails after tests": {
		"pricing/p.go":      "package pricing\n",
		"pricing/p_test.go": "package pricing\n\nimport (\"flag\"; \"os\"; \"testing\")\n\nfunc TestMain(m *testing.M) { code := m.Run(); if f := flag.Lookup(\"test.list\"); f != nil && f.Value.String() == \"\" { os.Exit(3) }; os.Exit(code) }\nfunc TestFormatDefault(t *testing.T) {}\n",
	},
	"every test skips": {
		"pricing/p.go":      "package pricing\n",
		"pricing/p_test.go": "package pricing\n\nimport \"testing\"\n\nfunc TestFormatDefault(t *testing.T) { t.Skip(\"needs a database\") }\n",
	},
	"init panics": {
		"pricing/p.go":      "package pricing\n\nfunc init() { panic(\"no config\") }\n",
		"pricing/p_test.go": "package pricing\n\nimport \"testing\"\n\nfunc TestFormatDefault(t *testing.T) {}\n",
	},
}

// A failing example is reported, never hidden behind "Every test passed".
func TestFailingExample(t *testing.T) {
	dir := module(t, "example.com/ex", map[string]string{
		"e_test.go": "package ex\n\nimport (\"fmt\"; \"testing\")\n\nfunc TestFine(t *testing.T) {}\n\nfunc Example() {\n\tfmt.Println(1)\n\t// Output: 2\n}\n",
	})
	code, out, errOut := shuffleblame(t, "-runs", "1", "-fail", dir)
	if code != 1 || !strings.Contains(out, "Example failed (source order), but only Test functions are explained") || strings.Contains(out, "Every test") {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
}

// Skipped tests are named, never counted as passing.
func TestSkippedTests(t *testing.T) {
	dir := module(t, "example.com/sk", map[string]string{
		"s_test.go": "package sk\n\nimport \"testing\"\n\nfunc TestNeedsDB(t *testing.T) { t.Skip(\"no database\") }\nfunc TestFine(t *testing.T) {}\n",
	})
	code, out, _ := shuffleblame(t, "-runs", "2", "-fail", dir)
	if code != 0 || !strings.Contains(out, "Skipped, so not checked: 1 test (TestNeedsDB)") || !strings.Contains(out, "Every test that ran passed in all 3 orders.") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// Every bad package exits 2 with nothing on standard output, alone and with
// a real log, with and without -fail.
func TestBadPackages(t *testing.T) {
	for name, files := range badPackages {
		copied := map[string]string{}
		for k, v := range files {
			copied[k] = v
		}
		dir := filepath.Join(module(t, "example.com/shop", copied), "pricing")
		for _, extra := range [][]string{nil, {"-fail"}, {"-log", "examples/logs/shuffle-2-verbose.txt"}, {"-fail", "-log", "examples/logs/shuffle-4.txt"}, {"-seed", "4"}} {
			args := append(append([]string{}, extra...), dir)
			code, out, errOut := shuffleblame(t, args...)
			if code != 2 || out != "" || !strings.Contains(errOut, "nothing was blamed or cleared") {
				t.Errorf("%s %v: exit %d, want 2 with no report\nstdout:\n%s\nstderr:\n%s", name, extra, code, out, errOut)
			}
		}
	}
}

// Every bad log exits 2 against the real package, with and without -fail.
func TestBadLogsAgainstRealPackage(t *testing.T) {
	logs := []string{
		"tests/testdata/logs/build-failed.txt",
		"tests/testdata/logs/build-failed.json",
		"tests/testdata/logs/testmain-exit.txt",
		"tests/testdata/logs/testmain-exit.json",
		"tests/testdata/logs/passed.txt",
		"tests/testdata/logs/passed-source-order-verbose.txt",
		"tests/testdata/bad/empty.txt",
		"tests/testdata/bad/blank.txt",
		"tests/testdata/bad/prose.txt",
		"tests/testdata/bad/random.bin",
		"tests/testdata/bad/utf16.txt",
		"tests/testdata/bad/go-vet-failed.txt",
		"tests/testdata/bad/other-package.txt",
		"tests/testdata/bad/not-json.json",
		"tests/testdata/does-not-exist.txt",
	}
	for _, l := range logs {
		for _, fail := range []bool{false, true} {
			args := []string{"-log", l, "examples/shop/pricing"}
			if fail {
				args = append([]string{"-fail"}, args...)
			}
			code, out, errOut := shuffleblame(t, args...)
			if code != 2 || out != "" {
				t.Errorf("%v: exit %d, want 2 with no report\nstdout:\n%s\nstderr:\n%s", args, code, out, errOut)
			}
		}
	}
	// A real log whose tests aren't in this package: the panic log names a
	// test that only existed in the copy it was made from.
	code, out, errOut := shuffleblame(t, "-log", "tests/testdata/logs/panic-verbose.txt", "examples/shop/pricing")
	if code != 0 || !strings.Contains(out, "TestPanicsAfterEuro failed (shuffle seed 2), but it isn't a test of this package here") {
		t.Errorf("panic log: exit %d\n%s%s", code, out, errOut)
	}
	// The ledger package passed in this log.
	code, out, _ = shuffleblame(t, "-log", "examples/logs/shuffle-4.txt", "examples/shop/ledger")
	if code != 2 || out != "" {
		t.Errorf("passing package in log: exit %d\n%s", code, out)
	}
}

// Truncated logs, cut at every line, never produce a report.
func TestCutLogsThroughTheTool(t *testing.T) {
	data, err := os.ReadFile("../examples/logs/shuffle-2-verbose.txt")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	tmp := t.TempDir()
	for i := 0; i < len(lines)-2; i++ { // the last two lines are the result and "FAIL"
		f := filepath.Join(tmp, fmt.Sprintf("cut%d.txt", i))
		os.WriteFile(f, []byte(strings.Join(lines[:i], "")), 0o644)
		code, out, _ := shuffleblame(t, "-fail", "-log", f, "examples/shop/pricing")
		if code != 2 || out != "" {
			t.Fatalf("log cut after %d lines: exit %d\n%s", i, code, out)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{"examples/shop/pricing", "examples/shop/ledger"},
		{"README.md"},
		{"does-not-exist"},
		{"-seed", "abc", "examples/shop/pricing"},
		{"-seed", "2", "-log", "examples/logs/shuffle-2.json", "examples/shop/pricing"},
		{"-test", "TestNope", "examples/shop/pricing"},
		{"-repeat", "0", "examples/shop/pricing"},
		{"-nosuchflag"},
	}
	for _, args := range cases {
		code, out, _ := shuffleblame(t, args...)
		if code != 2 || out != "" {
			t.Errorf("%v: exit %d, want 2\n%s", args, code, out)
		}
	}
}

// A real program: the standard library's container/list and strings tests
// pass in source order and 5 shuffled orders, so nothing is blamed.
func TestStandardLibraryStaysQuiet(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standard library tests")
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSpace(string(out))
	for _, pkg := range []string{"container/list", "strings"} {
		code, rep, errOut := shuffleblame(t, "-runs", "5", "-fail", filepath.Join(root, "src", pkg))
		if code != 0 || !strings.Contains(rep, "Every test passed in all 6 orders.") {
			t.Errorf("%s: exit %d\n%s%s", pkg, code, rep, errOut)
		}
	}
}
