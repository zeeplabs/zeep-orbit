# Contributing to zeep-orbit

## Prerequisites

- Go 1.26+
- PostgreSQL 14+ (for integration tests)
- Docker + Docker Compose (optional)

## Development setup

```bash
git clone https://github.com/zeeplabs/zeep-orbit
cd zeep-orbit
go mod download
make build
```

## Running tests

Unit tests (no database required):

```bash
make test
```

Integration tests:

```bash
TEST_DATABASE_URL=postgres://user:pass@localhost/testdb go test -p 1 -parallel 4 ./...
```

Integration tests skip automatically when `TEST_DATABASE_URL` is not set. They create isolated schemas with unique names and clean up after themselves.

`-p 1 -parallel 4` matters once `TEST_DATABASE_URL` is set: `internal/dashboard` and `internal/mcpserver` both run integration tests against the same database and step on each other's shared rows under Go's default cross-package parallelism (deadlocks / spurious FK violations, not a real bug). This is what CI runs (`.github/workflows/reusable-ci.yml`) — match it locally or a plain `go test ./...` can fail for reasons that have nothing to do with your change.

## Making changes

1. Fork the repository
2. Create a branch: `git checkout -b feat/my-change`
3. Make your changes
4. Run `go vet ./...` and `go test -p 1 -parallel 4 ./...`
5. Commit with a clear message (see style below)
6. Open a pull request against `main`

## Commit style

```
type: short description

Longer explanation if needed.
```

Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`

## What to work on

Check open issues labeled `good first issue` or `help wanted`.

## Security

Do not open public issues for security vulnerabilities. Email the maintainers directly.

## License

By contributing you agree your code will be licensed under the [MIT License](LICENSE).
