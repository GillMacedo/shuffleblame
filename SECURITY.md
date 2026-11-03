# Security

Report vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/GillMacedo/shuffleblame/security/advisories/new), not in a public issue.

`shuffleblame` builds and runs the tests of the package you point it at, the same as `go test`, so only run it on code you trust. A wrong blame or a missed failure is a normal bug. A case where it reads a log or runs something you didn't ask for is a security issue, so report that privately. It doesn't use the network itself; your tests might.
