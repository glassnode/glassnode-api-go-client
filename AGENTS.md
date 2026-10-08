# glassnode-api-go-client: agent instructions

Follow the Glassnode engineering handbook in `glassnode/tech`
(https://gitlab.com/glassnode/tech, Glassnode engineers only). Start with its
`handbook/README.md`.

## Contents

- [What this repo is](#what-this-repo-is)
- [Commands](#commands)
- [Layout](#layout)
- [Conventions](#conventions)
- [Don't](#dont)

## What this repo is

The public Go client for the Glassnode API. Details: [README.md](README.md).

## Commands

- Lint: `golangci-lint run ./...` (v2), and the same in `examples/`
- Test: `go test -race ./...`
- Build examples: `cd examples && go build ./...`
- Module files: `go mod tidy -diff` in the root and in `examples/`

Run lint and tests before opening a PR.

## Layout

- `*.go` at the root: the client, one package `glassnode`.
- `testdata/`: recorded responses for unit tests; `testdata/contract/`: real API responses
  for `contract_test.go` (see its README).
- `examples/`: runnable programs, a separate module that calls the live API.
- `docs/design.md`: design decisions.

## Conventions

- Commit messages: `type(scope): summary` (see [CONTRIBUTING.md](CONTRIBUTING.md)).
- Field and parameter names follow the API documentation; don't rename them.
- User-visible changes add a line to the `Unreleased` section of `CHANGELOG.md`.

## Don't

- Add dependencies outside the standard library.
- Change or remove exported names, struct fields or behaviour without an issue first.
- Edit files in `testdata/contract/` by hand.
- Put API keys in code, tests or fixtures; tests need none.
