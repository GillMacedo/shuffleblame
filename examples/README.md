# Examples

## `shop/`
A small made-up Go module with two packages and no dependencies. `pricing` keeps a default currency and a rate cache in package variables; `TestEuroStore` and `TestCacheRate` change them and don't restore them, so `TestFormatDefault` and `TestRatesStartEmpty` fail after them. All tests pass in source order. `ledger` changes a package variable too, but restores it with `t.Cleanup`, so every order passes.

## `logs/`
Real `go test` output from `examples/shop` on Go 1.27.3, linux/arm64, saved unedited:

- `shuffle-4.txt`: `go test -count=1 -shuffle=4 ./...`
- `shuffle-2-verbose.txt`: `go test -count=1 -v -shuffle=2 ./pricing`
- `shuffle-2.json`: `go test -count=1 -json -shuffle=2 ./...`

## `real/`
Output of `shuffleblame -runs 10 .` on Go 1.27.3, linux/arm64, on 2026-11-03, in the root package of shallow clones of public projects:

- `urfave-cli-7dad5b6.txt`: `urfave/cli` (MIT) at commit 7dad5b6e43fe89c5659832f59b6eea6a98dd9711.
- `pflag-f8a9906.txt`: `spf13/pflag` (BSD-3-Clause) at commit f8a990678a6d44385313e9b19e40737f889f9b14.
- `viper-quiet.txt`: `spf13/viper` (MIT) at commit 528f7416c4b56a4948673984b190bf8713f0c3c4.
- `cobra-quiet.txt`: `spf13/cobra` (Apache-2.0) at commit adbc8813901bba65827259daa8e22ff94ec1f30e.
- `logrus-quiet.txt`: `sirupsen/logrus` (MIT) at commit 6ef0748d1177dadaeee9aa79ffef604570f487d7.

Each reproduce command in the cli and pflag files was run by hand and failed, and each failing test passed alone.

## `../tests/testdata/`
`logs/` holds real `go test` output from a temporary copy of `examples/shop` with one change each: a compile error added to `pricing.go` (`build-failed.txt`, `build-failed.json`), a `TestMain` that exits 1 before running tests (`testmain-exit.txt`, `.json`), a passing run (`passed.txt`, `passed-source-order-verbose.txt`), and an added test that panics after `TestEuroStore` (`panic-verbose.txt`; its stack trace has the temporary path). `bad/` holds input that isn't a usable log: empty and blank files, random bytes, prose, `shuffle-4.txt` converted to UTF-16 with `iconv`, real `go vet` output, a log line for another package, and the first 300 bytes of `shuffle-2.json`, ending mid-line, followed by the rest of the file from its second line.
