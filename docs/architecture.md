# Architecture

`shuffleblame` is one standard-library Go file, `plugin/scripts/shuffleblame.go`, plus a skill that tells Claude when and how to run it.

## Flow

1. **Read the log** (with `-log`). `ParseLog` reads plain, `-v` or `-json` `go test` output. It splits text at package result lines (`ok  \tpkg`, `FAIL\tpkg`), and groups JSON events by package. `FromLog` picks the package being checked, and refuses a log that is empty, binary, cut off before the package's result, from another package, a failed build, a passing run, or a failure that names no test. It takes the seed from `-test.shuffle N`; with no such line the run used source order.
2. **Build once.** `go list -json` finds the package, its module and its test files. `go test -c` builds one test binary into a temporary directory. A build failure exits 2.
3. **List tests.** The binary's `-test.list ^Test` gives the top-level tests. None listed means no tests, or a `TestMain` that never calls `m.Run`.
4. **Run.** `Runner.Run` runs the binary in the package directory with `-test.v`, `-test.paniconexit0`, `-test.count`, `-test.shuffle` and `-test.run '^(A|B|C)$'`. `ParseSection` reads the output. A run is refused when its exit code and output disagree: non-zero with no failing test, zero with a failing test, zero without `PASS`, zero with a test still running, or zero without running every requested test.
5. **Collect victims.** Each failing test in the log, the `-seed` run, or each order of the search, once.
6. **Explain.** `Runner.Explain` runs the victim alone `-repeat` times (any failure means "fails on its own"), and once with `-count=2`. The candidates are the tests that started before it in the failing run, or all others for a parallel victim. It replays candidates plus victim; if that passes, the failure didn't reproduce. `minimize` halves the candidates while one half still fails, then drops one test at a time if the failure needs tests from both halves. The result is confirmed with `-repeat` more runs.
7. **Report.** Text or JSON. File and line of each test come from the package's `_test.go` files. The reproduce command uses `go -C DIR` when the package is in another module than the current directory.

## Why subsets keep the order

The testing package shuffles its whole list of tests with the seed before `-run` filters it. So for a fixed seed, any subset runs in the same relative order as in the full run. This is what lets the tool bisect without reimplementing Go's shuffle. `TestReadmeExamples` and the integration tests depend on it, on Go 1.22 and on the current release.

## Why the checks are strict

A tool like this can say "every test passed" for the wrong reason: the package didn't build, `TestMain` returned before running tests, every test skipped, or the log was cut off before the failure. Each check in steps 1 to 4 exists so that none of these produces a quiet report or exit code 0. See `docs/decisions/0002-refuse-untrusted-input.md`.
