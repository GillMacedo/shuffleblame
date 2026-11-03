# 0001: One standard-library Go file, run as a skill

**Decision:** all logic lives in `plugin/scripts/shuffleblame.go`, using only the standard library, and the skill runs it with `go run`. No MCP server.

**Why:** anyone with this problem already has Go installed. A skill with a script needs no install step, no binaries per platform and no npm package. The agent only needs to run one command and read its output, which a skill covers.

**Cost:** the first `go run` compiles the tool, which takes a second or two.
