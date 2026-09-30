# Recorded API contract fixtures

Copied from `glassnode/glassnode-api-ts-client/test/fixtures/contract` (Apache
License 2.0), captured on 2026-09-23 by its `scripts/record-fixtures.mjs` using
client version 0.27.0 against `https://api.glassnode.com`.

Response bodies are unchanged except `asset-metadata.json`, reduced to its first
three assets to keep this SDK's source archive small. No requests or credentials
are needed to run the tests. The exchange metadata fixture specifically guards
`parameters_defaults`, which was previously discarded by the Go model.
