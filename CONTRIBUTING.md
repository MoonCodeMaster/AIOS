# Contributing to AIOS

Thanks for your interest in AIOS. This document tells you how to set up a
development environment, what we expect in a pull request, and how to report
bugs or propose changes.

## Ways to contribute

- **Report a bug** — open an issue. Include the `aios --version`, Go version,
  OS, a minimal `.aios/config.toml`, and the exact command you ran.
- **Propose a feature** — open an issue first so we can align on scope before
  code lands.
- **Improve docs** — typo fixes, clearer examples, and translations are all
  welcome and can be sent as small standalone PRs.
- **Send a patch** — pick an issue labeled `good first issue` or `help wanted`,
  or discuss your idea in an issue before opening a pull request against
  non-trivial surface area.

## Development setup

Prerequisites:

- Go **1.26** or newer (matches `go.mod`)
- `git` 2.40+ (worktree operations)
- A POSIX shell (`sh`), required by the verifier

For the optional end-to-end suite you additionally need authenticated
`claude` and `codex` CLIs — see [`docs/e2e-setup.md`](docs/e2e-setup.md).

```bash
git clone https://github.com/Solaxis/aios.git
cd aios
make build        # produces ./bin/aios
make test         # unit + internal package tests
make int          # integration tests (no external CLIs required)
```

## Running locally

```bash
./bin/aios init
./bin/aios new "Add a /health endpoint with a unit test"
./bin/aios run
./bin/aios status
```

State lives under `.aios/` inside whichever repo you point AIOS at. Delete the
directory to reset.

## Pull request checklist

Before opening a PR, please confirm:

- [ ] `make fmt` produces no diff
- [ ] `make lint` (wraps `go vet ./...`) passes
- [ ] `make test` passes
- [ ] New behavior has a test (unit or integration); bug fixes include a
      regression test
- [ ] Exported identifiers have doc comments (`golint` style)
- [ ] If you changed flags, commands, config keys, or task frontmatter,
      `README.md` is updated

## Commit style

We follow a loose Conventional Commits style. Prefix the subject with one of:

```
feat:      a new user-visible capability
fix:       a bug fix
docs:      documentation only
refactor:  code change that is not a fix or feature
test:      test-only change
chore:     tooling, build, release plumbing
perf:      performance improvement
```

Subjects are imperative and ≤72 characters. Explain *why* in the body if the
diff isn't self-evident. Reference issues with `Fixes #N` / `Refs #N`.

## Code style

- Run `gofmt` / `goimports` on every change — CI will reject unformatted code.
- Keep `internal/` packages small and cohesive. If a file grows past ~500 lines
  or a package gains more than one responsibility, split it.
- Prefer table-driven tests (`[]struct{ name string; ... }`) for anything with
  more than two branches.
- Errors are values — wrap with `fmt.Errorf("context: %w", err)` at boundaries;
  do not swallow or re-stringify.
- No external service calls in unit tests. Integration tests may shell out but
  must not require network access or credentials.

## Reporting security issues

Do **not** open a public issue for security vulnerabilities. Email the
maintainer listed on the GitHub profile instead.

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE) that covers the project.
