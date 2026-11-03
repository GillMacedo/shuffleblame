# 0003: Replay orders with -shuffle and -run instead of reimplementing the shuffle

**Decision:** subsets are run with the original seed and a `-test.run` pattern that lists the tests by name. The tool never computes Go's shuffle order itself.

**Why:** Go shuffles the full test list before filtering, so a subset keeps its relative order for the same seed. Using the binary's own behavior keeps the tool correct if the shuffle algorithm changes, and the printed reproduce command is exactly what the tool ran.

**Cost:** a parallel victim gets every other test as a candidate, because the order of parallel tests isn't fixed by the seed.
