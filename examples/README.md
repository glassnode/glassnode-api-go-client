# Runnable Go SDK examples

Requires Go 1.24+ and a repository checkout. All programs use the local SDK via
`replace ... => ..`; there is no need to download the private module.

```sh
cd examples
export GLASSNODE_API_KEY='your-api-key'
go run ./price
```

Set exactly one of `GLASSNODE_API_KEY` or `GLASSNODE_ACCESS_TOKEN`. The latter
accepts an existing OAuth bearer token; these programs do not perform login or
refresh. Credentials are read only by example setup, never implicitly by the SDK.
Requests use your account's API credits and require access to the selected metric.

| Command | Demonstrates | Output |
| --- | --- | --- |
| `go run ./price` | Nullable scalar BTC closing prices | Date and USD price, including gaps |
| `go run ./ohlc` | Object time series | OHLC points as JSON |
| `go run ./bulk` | Repeated asset selectors for BTC and ETH | Unwrapped bulk market-cap points as JSON |
| `go run ./metadata` | Typed metric metadata | Closing-price metadata as JSON |
| `go run ./usage` | Account credit usage | Used credits and monthly allowance |
| `go run ./custom` | Caller-owned types and `json.Number` | Prices as JSON with numeric precision preserved |

The metric examples default to 30 days of daily data. Change the asset, range or
whole-operation deadline with flags:

```sh
go run ./price -asset ETH -days 7 -timeout 15s
go run ./price -h
```

Bulk always selects BTC and ETH. Metadata uses only the asset flag; usage ignores
asset and days. Shared [setup](internal/example/example.go) handles configuration
and errors so each program focuses on its SDK operation.

`GLASSNODE_BASE_URL` can point at a local test server. Only use a server you trust:
the example sends your configured credential there. No production requests are
needed to compile these programs:

```sh
go vet ./...
go build ./...
```

This directory is a nested Go module, so Go excludes it from the parent SDK's
module archive. Root `go test ./...` does not traverse it; CI vets and builds this
module separately. These programs are repository examples, not a published module.
