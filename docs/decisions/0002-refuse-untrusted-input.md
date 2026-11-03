# 0002: Refuse anything that could produce a false all clear

**Decision:** a log that is cut off, from another package, from a failed build or a passing run, or that fails without naming a test, exits 2. So does a package that doesn't build, has no tests, skips every test, or whose test binary's exit code doesn't match its output. Exit code 2 never comes with a report.

**Why:** earlier tools in this family passed their own tests and still reported a failed build or an unreadable report as all clear. Here, a quiet search on a package whose `TestMain` returns before `m.Run` would look exactly like a healthy package.

**Cost:** a log must be saved whole, up to the package's result line.

**Alternatives:** using the failing test names from any text that has them. A cut log can lose the seed or some failing tests, and the result would look complete.
