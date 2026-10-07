// Package glassnode is a client for the Glassnode API
// (https://docs.glassnode.com).
//
// Create a Client with NewClient and reuse it; it is safe for concurrent use.
// Every method takes a context.Context, which controls cancellation and the
// overall deadline of the call, including retries:
//
//	client, err := glassnode.NewClient(apiKey)
//	if err != nil {
//		return err
//	}
//	points, err := client.GetTimeSeries(ctx, "market/price_usd_close", &glassnode.MetricParams{
//		Asset:    "BTC",
//		Interval: "24h",
//	})
//
// Timestamps are Unix seconds, and missing values are nil pointers. Methods
// such as GetTimeSeries, GetObjectTimeSeries and GetBulkMetric cover the common
// response shapes; GetMetric, Get and Raw handle everything else.
//
// The package reads no environment variables or files: all configuration,
// including credentials, is passed to NewClient as options. Errors can be
// inspected with errors.As for APIError, InputError, TransportError,
// DecodeError and AuthError.
package glassnode
