# Roadmap

## Next
- Run several packages in one call (`./...`), building each test binary once
- Point at the state the culprit changes: package variables it assigns, `os.Setenv`, `flag` calls, found by reading its source
- A Markdown output for pull request comments

## Later
- Examples and fuzz seed corpora as victims and culprits
- Read `gotestsum --jsonfile` output and GitHub Actions log downloads directly
- Test on Windows and macOS hosts and in GitHub Actions
