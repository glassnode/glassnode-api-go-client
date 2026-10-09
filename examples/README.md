# Examples

Small programs that call the Glassnode API with this client. They build against
the client in this repository checkout, so you do not need to install the
module first.

```sh
cd examples
export GLASSNODE_API_KEY=your-api-key
go run ./price
```

| Program | What it shows |
| --- | --- |
| `price` | Daily BTC closing prices with `GetTimeSeries`, including gaps |
| `ohlc` | Daily OHLC prices with `GetObjectTimeSeries` |
| `bulk` | Market cap of BTC and ETH in one request with `GetBulkMetric` |
| `metadata` | Metadata of the closing-price metric with `GetMetricMetadata` |
| `usage` | The account's daily or monthly allowance and what is left of it, with `GetAPIUsage` |
| `custom` | Decoding into your own types with `GetMetric`, keeping full numeric precision |

Every request uses API credits from your account, and the metrics must be
included in your plan.

## Options

All programs accept the same flags:

| Flag | Default | Used by |
| --- | --- | --- |
| `-asset` | `BTC` | `price`, `ohlc`, `metadata`, `custom` |
| `-days` | `30` | `price`, `ohlc`, `bulk`, `custom` |
| `-timeout` | `30s` | all |

```sh
go run ./price -asset ETH -days 7
```

Credentials and the API address come from environment variables:

| Variable | Purpose |
| --- | --- |
| `GLASSNODE_API_KEY` | Your API key |
| `GLASSNODE_BASE_URL` | A different API address, for example a local mock server |

The API key is sent to `GLASSNODE_BASE_URL` when it is set, so only point it
at a server you trust.

The shared setup code is in [internal/example](internal/example/example.go).

## How this directory is built

The directory is its own Go module. Its `go.mod` uses a `replace` directive to
build against the client in the parent directory, and Go leaves it out of the
client's module archive. `go test ./...` in the repository root does not
include it, so check it separately:

```sh
go build ./...
golangci-lint run ./...
```
