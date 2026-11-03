# Changelog

All notable changes are documented here, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-03
### Added
- Finds the earlier test that makes a Go test fail in another order, by replaying the order and bisecting the tests that ran before it, down to one test or the smallest set
- Three ways to get the order: a saved `go test` log (plain, `-v` or `-json`) with its `-test.shuffle` seed, a `-seed`, or a search of source order and shuffle seeds
- Confirms each blame with repeated runs alone and after the culprit, and prints a two-test `go test` command that reproduces it, with the culprit's file and line
- Reports tests that fail when run twice, fail on their own, or don't reproduce, and names skipped tests
- Refuses failed-build, cut-off, foreign and empty logs, and packages that don't build, have no tests, or fail in `TestMain` or `init`, with exit code 2
- `-fail`, `-json`, `-test`, `-race`, `-tags`, `-timeout`, `-repeat`, `-runs`, `-first-seed` and `-max` options
- Claude Code plugin with the `finding-order-dependent-go-tests` skill
