Add a test for this input that shuffleblame must refuse: $ARGUMENTS

1. Make the input from a real command run, not by hand: a log saved from `go test` goes in `tests/testdata/logs/`, other output in `tests/testdata/bad/`. A broken package goes in `badPackages` in `tests/integration_test.go`.
2. Add it to `TestBadLogs` in `plugin/scripts/shuffleblame_test.go` and to `TestBadLogsAgainstRealPackage`, or it is covered by `TestBadPackages`.
3. Run `make test`. It must exit 2 with nothing on standard output, with and without `-fail`. If it doesn't, fix the tool, and describe the input in `examples/README.md`.
