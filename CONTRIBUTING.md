# Contributing to zeep-core

## Prerequisites

- Go 1.26+
- PostgreSQL 14+ (for integration tests)
- Docker + Docker Compose (optional)

## Development setup

```bash
git clone https://github.com/zeep-tecnologia/zeep-core
cd zeep-core
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
TEST_DATABASE_URL=postgres://user:pass@localhost/testdb go test ./...
```

Integration tests skip automatically when `TEST_DATABASE_URL` is not set. They create isolated schemas with unique names and clean up after themselves.

## Making changes

1. Fork the repository
2. Create a branch: `git checkout -b feat/my-change`
3. Make your changes
4. Run `go vet ./...` and `go test ./...`
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
