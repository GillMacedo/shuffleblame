# shuffleblame

Claude Code skill and Go CLI that finds which earlier Go test makes another test fail when the package's tests run in a different order, such as under `go test -shuffle`, and prints a two-test command that reproduces it.

## Architecture
`plugin/scripts/shuffleblame.go` holds all the logic in one standard-library file, so `go run` needs no setup. `ParseLog` and `FromLog` read a saved `go test` log (plain, `-v`, `-json`) and refuse cut-off, foreign or failed-build logs. `listPackage` and `build` run `go list` and `go test -c` once. `Runner.Run` runs the test binary with `-test.v`, `-test.shuffle` and `-test.run`, and refuses any run whose output doesn't explain its exit code. `Runner.Explain` checks the failing test alone, replays the tests before it, and `minimize` bisects them. `plugin/skills/finding-order-dependent-go-tests/SKILL.md` tells Claude when and how to run it. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet, unit tests, and integration tests in `tests/`, which build throwaway packages, run the tool on them and on `examples/`, and run every `$ ` line in the README and check its output
- `make lint`: plugin and marketplace validation
- `make demo`: run the examples

## Conventions
- Standard library only. The tool runs `go` and the compiled test binary, nothing else, and never uses the network.
- A log or package that can't be trusted exits 2 with "nothing was blamed or cleared". Never report "Every test passed" or exit 0 for it. Add every new kind of bad input to `tests/testdata/` and to `TestBadLogs`, `TestBadLogsAgainstRealPackage` or `TestBadPackages`.
- A blame needs: the test passes alone in every repeat, and fails after the culprit in at least one. Don't weaken this.
- Files in `examples/logs/` and `examples/real/` are real output. `examples/README.md` says what made each one.
- README examples are generated from real output. If output changes, update the README, or `TestReadmeExamples` fails.
- Code must build and pass tests on Go 1.22.

## Do and don't
- Do say which orders were tried. A quiet search covers those orders only.
- Don't call a package free of order dependence.

## Definition of done
`make test` and `make lint` pass, and `ROADMAP.md`, the README and docs are updated when relevant.
