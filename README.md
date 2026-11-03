# shuffleblame: find the Go test that makes another test fail under -shuffle

[![CI](https://github.com/GillMacedo/shuffleblame/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/shuffleblame/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**A Claude Code skill and Go CLI for Go developers whose tests pass alone but fail with `go test -shuffle=on`, in CI, or in a different order. It names the earlier test that leaves state behind (a package variable, a global flag set, a registry, an environment variable), shows the failure, and prints a two-test `go test` command that reproduces it.**

A test that fails only after another test has run is hard to debug, because the failing test is usually not the one with the bug. The culprit is an earlier test that changed shared state and didn't put it back. Python has `detect-test-pollution` and RSpec has `--bisect` for this. Go has `-shuffle` and a seed, and leaves the rest to you. `shuffleblame` builds the package's test binary once, replays the failing order, and bisects the tests that ran first until one test (or the smallest set) is left. It confirms the result by running the failing test alone and after the culprit several times.

## What you can ask Claude

- "TestFormatDefault fails in CI with -shuffle=on but passes on my machine. Which test breaks it?"
- "Here's the CI log of the failed go test run. Find the test that pollutes the others."
- "Do any tests in ./internal/cache depend on the order they run in?"
- "Is this test flaky, or does it depend on another test?"
- "Which test do I need to fix so we can turn on -shuffle in CI?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/shuffleblame
    claude plugin install shuffleblame@shuffleblame-marketplace

Or as a CLI with Go 1.22 or newer. It has no dependencies:

    go run github.com/GillMacedo/shuffleblame/plugin/scripts@latest ./internal/cache

## Example

`examples/shop/pricing` is a small made-up package with two tests that change package variables and don't restore them. Its tests pass in source order, so `go test` alone never shows the problem. From a clone of this repo:

    $ go run ./plugin/scripts examples/shop/pricing
    shuffleblame: example.com/shop/pricing, 6 tests
    Orders: source order and shuffle seeds 1-20 (searched)

    TestFormatDefault fails after TestEuroStore (shuffle seed 2)
      alone: passed 3 of 3 runs
      after TestEuroStore: failed 3 of 3 runs
      TestEuroStore is at pricing_test.go:23
      TestFormatDefault is at pricing_test.go:5
      failure:
        pricing_test.go:7: Format(1999, "") = "19.99 EUR", want "19.99 USD"
      reproduce:
        go -C ./examples/shop/pricing test -count=1 -run '^(TestEuroStore|TestFormatDefault)$' -shuffle=2 .

    TestRatesStartEmpty fails after TestCacheRate (shuffle seed 4)
      alone: passed 3 of 3 runs
      after TestCacheRate: failed 3 of 3 runs
      TestCacheRate is at pricing_test.go:30
      TestRatesStartEmpty is at pricing_test.go:17
      failure:
        pricing_test.go:19: CachedRates() = 1, want 0
      reproduce:
        go -C ./examples/shop/pricing test -count=1 -run '^(TestCacheRate|TestRatesStartEmpty)$' -shuffle=4 .

    2 tests fail because of earlier tests. 43 test binary runs.

The reproduce command works because Go shuffles the whole test list with the seed before `-run` filters it, so any subset keeps its relative order.

### From a CI log

When CI already failed, pass its log. `shuffleblame` takes the seed from the `-test.shuffle` line and the failing tests from the log, and reads plain, `-v` and `-json` output. `examples/logs/shuffle-2.json` is real `go test -json -shuffle=2 ./...` output:

    $ go run ./plugin/scripts -log examples/logs/shuffle-2.json examples/shop/pricing
    shuffleblame: example.com/shop/pricing, 6 tests
    Orders: shuffle seed 2 (from the log examples/logs/shuffle-2.json)

    TestFormatDefault fails after TestEuroStore (shuffle seed 2)
      alone: passed 3 of 3 runs
      after TestEuroStore: failed 3 of 3 runs
      TestEuroStore is at pricing_test.go:23
      TestFormatDefault is at pricing_test.go:5
      failure:
        pricing_test.go:7: Format(1999, "") = "19.99 EUR", want "19.99 USD"
      reproduce:
        go -C ./examples/shop/pricing test -count=1 -run '^(TestEuroStore|TestFormatDefault)$' -shuffle=2 .

    1 test fails because of an earlier test. 13 test binary runs.

### A healthy package stays quiet

`examples/shop/ledger` also changes a package variable, but restores it with `t.Cleanup`. Every order passes, and the report says what it covered instead of calling the package clean:

    $ go run ./plugin/scripts examples/shop/ledger
    shuffleblame: example.com/shop/ledger, 4 tests
    Orders: source order and shuffle seeds 1-20 (searched)

    Every test passed in all 21 orders.
    That covers only these orders. A test that fails in another order can still exist.

### Real projects

Run on the default branches of open-source projects on 2026-11-03, with `-runs 10`. Full output is in `examples/real/`. These can't run offline, so they aren't `$` examples.

On `urfave/cli` v3 (commit 7dad5b6), `TestHandleExitCoder_MultiErrorWithFormat` sets the package-level `ErrWriter` to a buffer and restores only `OsExiter`, so two later tests read an empty buffer:

    TestHandleExitCoder_Default fails after TestHandleExitCoder_MultiErrorWithFormat (shuffle seed 2)
      alone: passed 3 of 3 runs
      after TestHandleExitCoder_MultiErrorWithFormat: failed 3 of 3 runs
      TestHandleExitCoder_MultiErrorWithFormat is at errors_test.go:249
      TestHandleExitCoder_Default is at command_test.go:3664

On `spf13/pflag` (commit f8a9906), `TestIgnoreUnknownFlags` calls `ResetForTesting`, which replaces the global flag set and drops the flags `TestEverything` expects:

    TestEverything fails after TestIgnoreUnknownFlags (shuffle seed 2)
      alone: passed 3 of 3 runs
      after TestIgnoreUnknownFlags: failed 3 of 3 runs

Both were checked by hand: the printed two-test command fails, and the failing test passes alone. In the root packages of `spf13/viper`, `spf13/cobra` and `sirupsen/logrus`, every test that ran passed in all 11 orders tried. One test each in viper and logrus skipped itself and is named in the report.

### Input it won't trust

A log of a failed build, a cut-off log, or a run that failed outside any test is refused with exit code 2, and nothing is blamed or cleared:

    $ go run ./plugin/scripts -log tests/testdata/logs/build-failed.txt examples/shop/pricing 2>&1 | head -n 1
    shuffleblame: nothing was blamed or cleared: the log shows example.com/shop/pricing failed to build, so no test ran

    $ head -n 8 examples/logs/shuffle-2-verbose.txt | go run ./plugin/scripts -log - examples/shop/pricing 2>&1 | head -n 1
    shuffleblame: nothing was blamed or cleared: the log has no result line for example.com/shop/pricing and ends partway through a package's test output, so it is cut off

    $ go run ./plugin/scripts -log tests/testdata/logs/testmain-exit.txt examples/shop/pricing 2>&1 | head -n 1
    shuffleblame: nothing was blamed or cleared: the log shows example.com/shop/pricing failed without naming a failing test. TestMain, an init function, os.Exit or a panic outside a test can do that, and the order of tests can't explain it

The same goes for the package itself: one that doesn't build, has no tests, skips every test, panics in `init`, or has a `TestMain` that skips `m.Run`, exits with its own code, or hides a failure behind `os.Exit(0)`.

## Usage

    shuffleblame [flags] [DIR]

`DIR` is the directory of one package (default `.`). Flags go before it.

- **Search** (default): run source order and shuffle seeds `-first-seed` to `-first-seed + -runs - 1` (1 to 20), then explain each test that failed.
- `-log FILE`: explain the failing tests in a saved `go test` log, in its order. `-` reads standard input.
- `-seed N`: use one order, a seed or `off` for source order.
- `-test NAME`: only explain this test.
- `-repeat N`: runs used to confirm a test passes alone and fails after its culprit (default 3).
- `-race`, `-tags LIST`: build flags, the same as for `go test`.
- `-timeout D`: timeout for each run of the test binary (default 10m).
- `-max N`: most failing tests to explain (default 10).
- `-fail`: exit 1 when any test failed in any order.
- `-json`: print the full report as JSON.

Exit codes: 0 when the run finished, 1 with `-fail` when a test failed, 2 when the input or a test run can't be trusted. `go run` turns every non-zero exit code into 1, so build the tool with `go build` if you need to tell them apart.

## What it reports

| Result | Meaning |
|---|---|
| fails after X | The test passes alone and fails after X every time. With several names, it fails only after all of them |
| fails when run twice | The test fails on its second run in one process (`-count=2`) but not after the tests before it. It leaves state behind for itself |
| fails on its own | The test fails alone too, so order doesn't explain it |
| failed, but not explained | An example or fuzz seed failed. Only `Test` functions are explained, but the failure is reported |
| didn't reproduce | It failed once, then not again with the same tests before it. Probably flaky, or the environment differs from where the log came from |

It also notes when a blamed test also fails when run twice, names skipped tests (they weren't checked), and warns when the local order differs from the log's, which means the set of tests differs.

## Limits

- **One package at a time**, and only `Test` functions. Benchmarks, examples and fuzz seed corpora aren't checked.
- **A search tries only the orders it lists.** A quiet report covers those orders, not every order.
- **Parallel tests**: a test with `t.Parallel()` runs after all sequential tests, so every other test is a candidate. Races between parallel tests can make results vary, which the repeat runs show as fewer than 3 of 3.
- **Pollution through files, databases or network services** is found only if it also happens on this machine. `shuffleblame` runs your tests, so they need whatever they need under `go test`.
- **It runs the package's tests.** Only use it on code you'd run `go test` on.
- **Not tested** on Windows or macOS hosts, in GitHub Actions, in a live Claude Code session, or through `go run ...@latest`, which needs the published repo.

## FAQ

**How do I find which Go test pollutes another?** Run `shuffleblame ./pkg`, or `shuffleblame -log ci.log ./pkg` with the CI log. It bisects the tests that ran before the failing one.

**How do I reproduce a `go test -shuffle` failure?** Use the seed from the `-test.shuffle N` line: `go test -shuffle=N ./pkg`. `shuffleblame` then cuts it down to the two tests that matter.

**My test passes alone but fails with the whole package. Why?** Usually an earlier test changed a package variable, a global flag set, `http.DefaultClient`, an environment variable or a registry and didn't restore it. Restore it with `t.Cleanup`, or use `t.Setenv`.

**Is there a Go version of detect-test-pollution or rspec --bisect?** That's what this is, for `go test`.

**Is it an MCP server?** No. It's a skill with one Go source file, so `go run` is all it needs.

## Tested on

- **The example module** in `examples/shop`, in search mode, with `-seed`, and with plain, `-v` and `-json` logs.
- **Real projects**, cloned on 2026-11-03: findings in `urfave/cli` and `spf13/pflag` checked by hand as above; `spf13/viper`, `spf13/cobra` and `sirupsen/logrus` quiet. The standard library's `container/list` and `strings` stay quiet in the integration tests.
- **Packages built in the tests**: a culprit in an external `_test` package with a parallel victim, a test that needs two earlier tests, a test that fails when run twice (from a `-count=2` log), a test that fails alone, a hanging test stopped by `-timeout`, and skipped tests.
- **Bad input** with and without `-fail`, all exit 2 with no report: logs of a failed build (plain and `-json`), a `TestMain` that exits 1, a passing run, a log for another package, empty and blank files, random bytes, prose, UTF-16, `go vet` output, a broken JSON line, and a missing file. Packages that don't build, whose tests don't build, with no test files, only examples, every test skipped, a panicking `init`, and a `TestMain` that skips `m.Run`, exits 1, exits 0 after a failure, or exits 3 after passing; each alone, with a real log and with `-seed`.
- **Cut logs**: four real logs cut at every byte, in the unit tests, and at every line through the tool. All are refused until the package's result line is complete.
- **Go versions**: all tests pass on Go 1.27.3 and Go 1.22.12, both linux/arm64.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/shuffleblame/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes, and a small problem may become a follow-up issue instead. A PR with no new commit 14 days after a change request is closed, and you can reopen it. PRs that change CI, agent settings, npm or Python dependencies, vendored code or build files can't be merged this way, and a comment says so. The same goes for anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about two hours, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `shuffleblame` saved you some prompts, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
