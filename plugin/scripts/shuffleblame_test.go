package main

import (
	"bytes"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const pricing = "example.com/shop/pricing"

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fromLog(data []byte, pkg string) (Order, *Section, error) {
	log, err := ParseLog(data)
	if err != nil {
		return Order{}, nil, err
	}
	return FromLog(log, pkg)
}

func isUntrusted(err error) bool {
	var u *untrusted
	return errors.As(err, &u)
}

// The real logs in examples/logs are read the same way in every format.
func TestRealLogs(t *testing.T) {
	cases := []struct {
		file   string
		seed   int64
		failed []string
		order  []string
	}{
		{"examples/logs/shuffle-4.txt", 4, []string{"TestRatesStartEmpty", "TestFormatDefault"}, nil},
		{"examples/logs/shuffle-2-verbose.txt", 2, []string{"TestFormatDefault"},
			[]string{"TestRatesStartEmpty", "TestCacheRate", "TestEuroStore", "TestFormatDefault", "TestLargeAmounts", "TestFormatExplicit"}},
		{"examples/logs/shuffle-2.json", 2, []string{"TestFormatDefault"},
			[]string{"TestRatesStartEmpty", "TestCacheRate", "TestEuroStore", "TestFormatDefault", "TestLargeAmounts", "TestFormatExplicit"}},
	}
	for _, c := range cases {
		o, s, err := fromLog(readFile(t, c.file), pricing)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if !o.Shuffled || o.Seed != c.seed {
			t.Errorf("%s: order %v, want seed %d", c.file, o, c.seed)
		}
		if !reflect.DeepEqual(s.Failed, c.failed) {
			t.Errorf("%s: failed %v, want %v", c.file, s.Failed, c.failed)
		}
		if !reflect.DeepEqual(s.Order, c.order) {
			t.Errorf("%s: order %v, want %v", c.file, s.Order, c.order)
		}
	}
}

// The ledger package passed in both multi-package logs.
func TestPassedPackageInLogIsRefused(t *testing.T) {
	for _, f := range []string{"examples/logs/shuffle-4.txt", "examples/logs/shuffle-2.json", "tests/testdata/logs/passed.txt"} {
		_, _, err := fromLog(readFile(t, f), "example.com/shop/ledger")
		if !isUntrusted(err) || !strings.Contains(err.Error(), "passed") {
			t.Errorf("%s: got %v, want a refusal saying ledger passed", f, err)
		}
	}
}

// Every log of a failed build, a failed setup, a passing run, or anything
// that isn't go test output is refused with a reason.
func TestBadLogs(t *testing.T) {
	cases := map[string]string{
		"tests/testdata/logs/build-failed.txt":                "failed to build",
		"tests/testdata/logs/build-failed.json":               "failed to build",
		"tests/testdata/logs/testmain-exit.txt":               "without naming a failing test",
		"tests/testdata/logs/testmain-exit.json":              "without naming a failing test",
		"tests/testdata/logs/passed-source-order-verbose.txt": "passed",
		"tests/testdata/bad/empty.txt":                        "empty",
		"tests/testdata/bad/blank.txt":                        "empty",
		"tests/testdata/bad/prose.txt":                        "isn't go test output",
		"tests/testdata/bad/random.bin":                       "",
		"tests/testdata/bad/utf16.txt":                        "NUL",
		"tests/testdata/bad/go-vet-failed.txt":                "isn't go test output",
		"tests/testdata/bad/other-package.txt":                "has no result for example.com/shop/pricing",
		"tests/testdata/bad/not-json.json":                    "isn't a complete go test -json event",
	}
	for file, want := range cases {
		_, _, err := fromLog(readFile(t, file), pricing)
		if err == nil || !isUntrusted(err) {
			t.Errorf("%s: got %v, want a refusal", file, err)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want it to mention %q", file, err, want)
		}
	}
}

// A log cut anywhere before its package result line is refused. A log cut
// after it reads the same as the whole log.
func TestCutLogsAreNeverTrusted(t *testing.T) {
	files := []string{
		"examples/logs/shuffle-4.txt",
		"examples/logs/shuffle-2-verbose.txt",
		"examples/logs/shuffle-2.json",
		"tests/testdata/logs/panic-verbose.txt",
	}
	for _, f := range files {
		data := readFile(t, f)
		wantO, wantS, err := fromLog(data, pricing)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		// The point after which the result for pricing is complete.
		marker := []byte("FAIL\t" + pricing)
		if strings.HasSuffix(f, ".json") {
			marker = []byte(`"Action":"fail","Package":"` + pricing + `","Elapsed"`)
		}
		end := bytes.LastIndex(data, marker)
		if end < 0 {
			t.Fatalf("%s: no result marker", f)
		}
		end += len(marker)
		if strings.HasSuffix(f, ".json") {
			end = bytes.IndexByte(data[end:], '\n') + end
		}
		for cut := 0; cut < len(data); cut++ {
			o, s, err := fromLog(data[:cut], pricing)
			if cut < end {
				if err == nil || !isUntrusted(err) {
					t.Fatalf("%s cut at byte %d of %d: got seed %v, failed %v; want a refusal", f, cut, len(data), o, s.Failed)
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s cut at byte %d after the result: %v", f, cut, err)
			}
			if o != wantO || !reflect.DeepEqual(s.Failed, wantS.Failed) {
				t.Fatalf("%s cut at byte %d after the result: got %v %v, want %v %v", f, cut, o, s.Failed, wantO, wantS.Failed)
			}
		}
	}
}

// Random bytes and random lines from real logs never produce victims.
func TestGarbageLogs(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	real := strings.Split(string(readFile(t, "examples/logs/shuffle-2-verbose.txt")), "\n")
	for i := 0; i < 300; i++ {
		b := make([]byte, rng.Intn(400))
		rng.Read(b)
		if _, _, err := fromLog(b, pricing); err == nil {
			t.Fatalf("random bytes %q were accepted", b)
		}
		// Real lines in a random order, without the result line.
		var lines []string
		for j := 0; j < rng.Intn(20); j++ {
			l := real[rng.Intn(len(real))]
			if !strings.HasPrefix(l, "FAIL\t") && !strings.HasPrefix(l, "ok  \t") {
				lines = append(lines, l)
			}
		}
		if _, _, err := fromLog([]byte(strings.Join(lines, "\n")), pricing); err == nil {
			t.Fatalf("shuffled lines without a result were accepted:\n%s", strings.Join(lines, "\n"))
		}
	}
}

func TestParseSectionPanicAndTimeout(t *testing.T) {
	s := ParseSection(strings.Split(string(readFile(t, "tests/testdata/logs/panic-verbose.txt")), "\n"))
	if !s.Panicked || !reflect.DeepEqual(s.Failed, []string{"TestFormatDefault", "TestPanicsAfterEuro"}) {
		t.Errorf("panic log: panicked %v, failed %v", s.Panicked, s.Failed)
	}
	timeout := "=== RUN   TestA\n--- PASS: TestA (0.00s)\n=== RUN   TestSlow\n=== RUN   TestSlow/sub\npanic: test timed out after 1s\n\trunning tests:\n\t\tTestSlow/sub (1s)\n\t\tTestSlow (1s)\n\ngoroutine 1 [running]:\n"
	s = ParseSection(strings.Split(timeout, "\n"))
	if !s.TimedOut || !reflect.DeepEqual(s.Failed, []string{"TestSlow"}) {
		t.Errorf("timeout: timed out %v, failed %v", s.TimedOut, s.Failed)
	}
}

func TestSubtestFailuresBlameTheTopLevelTest(t *testing.T) {
	out := "=== RUN   TestA\n=== RUN   TestA/x\n    a_test.go:3: boom\n--- FAIL: TestA (0.00s)\n    --- FAIL: TestA/x (0.00s)\nFAIL\n"
	s := ParseSection(strings.Split(out, "\n"))
	if !reflect.DeepEqual(s.Failed, []string{"TestA"}) || !reflect.DeepEqual(s.Order, []string{"TestA"}) {
		t.Errorf("failed %v order %v", s.Failed, s.Order)
	}
}

func TestDescribeOrders(t *testing.T) {
	cases := []struct {
		orders []Order
		want   string
	}{
		{[]Order{{}}, "source order"},
		{[]Order{{Shuffled: true, Seed: 7}}, "shuffle seed 7"},
		{[]Order{{}, {Shuffled: true, Seed: 1}, {Shuffled: true, Seed: 2}, {Shuffled: true, Seed: 3}}, "source order and shuffle seeds 1-3"},
		{[]Order{{Shuffled: true, Seed: 5}, {Shuffled: true, Seed: 9}}, "shuffle seeds 5, 9"},
	}
	for _, c := range cases {
		if got := describeOrders(c.orders); got != c.want {
			t.Errorf("describeOrders(%v) = %q, want %q", c.orders, got, c.want)
		}
	}
}

func TestReproduceCommand(t *testing.T) {
	got := reproduce(dirArg("internal/cache"), []string{"-race"}, []string{"TestA", "TestB"}, Order{Shuffled: true, Seed: -3})
	want := "go test -count=1 -race -run '^(TestA|TestB)$' -shuffle=-3 ./internal/cache"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if got := reproduce(".", nil, []string{"TestA"}, Order{}); got != "go test -count=1 -run '^(TestA)$' ." {
		t.Errorf("source order: %q", got)
	}
}
