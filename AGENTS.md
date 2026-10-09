# glassnode-api-go-client: agent instructions

Read [CONTRIBUTING.md](CONTRIBUTING.md) first: setup, commands, commit format and
guidelines all live there. Before opening a PR, run the checks in its "Making a change"
section, in the root and in `examples/`.

## Don't

- Edit `testdata/contract/` by hand: it holds recorded real API responses
  (see its [README](testdata/contract/README.md)).
- Run the programs in `examples/` to test a change: they call the live API and need a key.
  `go build ./...` in `examples/` is enough.
- Add a dependency, or change an exported name, field or behaviour: see the guidelines in
  CONTRIBUTING.md.
