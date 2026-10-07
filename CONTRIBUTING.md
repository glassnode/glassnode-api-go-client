# Contributing

Thanks for taking the time to contribute. This document explains how to get a
change into the project.

## Reporting bugs and requesting features

Open an [issue](https://github.com/glassnode/glassnode-api-go-client/issues).
For bugs, include the client version, the Go version, the metric path or
endpoint, and the error message; the client never puts credentials into error
messages, but check your own logs before pasting them. For a response that the
client decodes wrongly, the raw JSON of a few points helps most.

Security issues go through [SECURITY.md](SECURITY.md), not the issue tracker.

## Development setup

You need Go 1.24 or later and [golangci-lint](https://golangci-lint.run) v2.
The client has no dependencies outside the standard library, so there is
nothing else to install.

```sh
git clone https://github.com/glassnode/glassnode-api-go-client
cd glassnode-api-go-client
go test -race ./...
golangci-lint run ./...
```

Tests run against local HTTP servers and the recorded responses in
[`testdata/`](testdata), so they need no API key and make no network requests.
The programs in [`examples/`](examples) do call the API; see their
[README](examples/README.md).

## Making a change

1. Fork the repository and create a branch from `main`.
2. Make the change, with tests. A behaviour change needs a test that fails
   without it; a new response shape needs a recorded response in
   `testdata/contract/` (see its [README](testdata/contract/README.md)).
3. Run `go test -race ./...` and `golangci-lint run ./...` in the repository
   root, and `go build ./... && golangci-lint run ./...` in `examples/`.
4. Add a line to the `Unreleased` section of [CHANGELOG.md](CHANGELOG.md) if
   the change is visible to users.
5. Open a pull request. Describe what changes for a user of the client and
   why; a link to the relevant part of the
   [API documentation](https://docs.glassnode.com) helps the review.

CI runs the tests on the oldest supported and the latest Go release, the
linter and `govulncheck`. A maintainer listed in [CODEOWNERS](CODEOWNERS)
reviews every pull request.

## Guidelines

- **Keep the standard library as the only dependency.** This is a deliberate
  property of the client; see [docs/design.md](docs/design.md) for the
  reasoning behind this and other decisions.
- **Follow the API documentation.** Field names, parameter names and response
  shapes come from the API; the client does not rename or reinterpret them.
- **Don't break callers.** Exported names, struct fields and behaviour are the
  public API. Additions are fine; changes and removals need a discussion in an
  issue first and a major version bump once the client is at 1.0.
- **Write Go that looks like the surrounding code.** `gofmt`, short names where
  the scope is short, comments on every exported identifier, errors wrapped so
  `errors.Is` and `errors.As` keep working.

## Commit messages

Use the imperative mood and a `type(scope): summary` first line, for example
`fix(client): drain error bodies so connections are reused`. Types are `feat`,
`fix`, `docs`, `test`, `refactor`, `ci` and `chore`. Explain the why in the
body when the summary is not enough.

## Releases

Maintainers release by tagging `main` with `vX.Y.Z`. The tag must have a
matching section in `CHANGELOG.md`; CI then runs the tests and publishes a
GitHub release with that section as its notes. Until 1.0, minor versions may
change behaviour and say so in the changelog.

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE) that covers the project.
