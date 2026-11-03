---
name: finding-order-dependent-go-tests
description: Finds which earlier Go test makes another test fail when tests run in a different order, such as under go test -shuffle=on, in CI, or with the whole package but not alone. Replays the failing order, bisects the tests that ran before it, confirms the culprit with repeated runs, and prints a two-test go test command that reproduces it. Use when the user says a Go test passes alone but fails with the package, fails only in CI or only with -shuffle, looks flaky, or asks which test pollutes another, or shares a failed go test log with a -test.shuffle line.
---

# Finding order-dependent Go tests

A Go test that fails only after another test ran is usually not the buggy one. An earlier test changed shared state (a package variable, a global flag set, a registry, an environment variable, a default client) and didn't restore it. The bundled tool finds that earlier test. It needs Go 1.22 or newer and runs the package's own tests, so use it only on code the user would run `go test` on.

## Pick the mode

1. **The user has a failed CI or terminal log:** save the whole output of the failing `go test` run (plain, `-v` or `-json`) to a file, and pass it with `-log`. The tool takes the seed from the `-test.shuffle N` line. Ask for the full log if it was pasted in part: a cut-off log is refused.
2. **The user knows the seed:** pass `-seed N`.
3. **Neither:** run with no flags. It tries source order and shuffle seeds 1 to 20. Raise `-runs` to search more orders.

Pass the directory of one package, such as `./internal/cache`. For a test that only fails with `-race` or build tags, pass `-race` or `-tags` too. Ask before running tests that talk to real databases or services.

## Run the tool

    go run "${CLAUDE_PLUGIN_ROOT}/scripts/shuffleblame.go" [-log FILE | -seed N] [-test NAME] ./path/to/pkg

- `-json` gives the full report for further work.
- Exit code 2 means the log or the package can't be trusted: a failed build, a cut-off or foreign log, a `TestMain` or `init` that fails outside any test, or every test skipped. Report the message. Never present a run that ended with exit code 2 as "no order dependence".

## Explain each result

1. **"X fails after Y":** open Y (the tool prints its file and line) and find the shared state it changes without restoring: package variables, `flag.CommandLine`, `os.Setenv`, `http.DefaultClient`, `log.SetOutput`, registries, caches, files in a fixed path. Suggest restoring it with `t.Cleanup` or using `t.Setenv`, or making X set the state it needs. With several names, Y needs all of them.
2. **"fails when run twice":** the test leaves state behind for itself. The same fixes apply, inside that test.
3. **"fails on its own":** order doesn't explain it. Debug it as a normal failure.
4. **"didn't reproduce":** probably flaky, or the log came from a different environment. Say so; don't blame a test.
5. Run the printed reproduce command once to show the user the failure before changing code.

## Report

1. Lead with the culprit, the state it changes, and the reproduce command.
2. Propose the smallest fix in the culprit (usually a `t.Cleanup`), and offer to make it.
3. Say which orders were tried. A quiet search covers those orders only.

## Don't

- Don't call a package free of order dependence because a search was quiet.
- Don't blame the failing test when the tool names another one.
- Don't edit tests or code unless the user asks.
