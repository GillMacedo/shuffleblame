# shuffleblame plugin

This plugin adds the `finding-order-dependent-go-tests` skill to Claude Code. When a Go test passes alone but fails with the package, under `go test -shuffle`, or only in CI, Claude runs the bundled `scripts/shuffleblame.go` with `go run`. It replays the failing order from a log, a seed or a search, bisects the tests that ran first, confirms the culprit, and prints a two-test command that reproduces the failure. It needs Go 1.22 or newer and runs the package's tests. See the repository README for examples and limits.

Install it with `claude plugin marketplace add GillMacedo/shuffleblame` and then `claude plugin install shuffleblame@shuffleblame-marketplace`.
