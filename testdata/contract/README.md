# Recorded API responses

Real responses from `https://api.glassnode.com`, used by `contract_test.go` to
check that the client decodes the production format. Running the tests sends
no requests and needs no credentials.

The files come from the TypeScript client's
[contract fixtures](https://github.com/glassnode/glassnode-api-ts-client/tree/main/test/fixtures/contract)
(Apache License 2.0), recorded on 2026-09-23 with its
`scripts/record-fixtures.mjs` and client version 0.27.0. They are unchanged,
except that `asset-metadata.json` is cut down to its first three assets.
