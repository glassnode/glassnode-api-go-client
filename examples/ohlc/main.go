package main

import (
	"context"

	glassnode "github.com/glassnode/glassnode-api-go-client"
	"github.com/glassnode/glassnode-api-go-client/examples/internal/example"
)

func main() {
	example.Run(run)
}

func run(ctx context.Context, client *glassnode.Client, params *glassnode.MetricParams) error {
	points, err := client.GetObjectTimeSeries(ctx, "market/price_usd_ohlc", params)
	if err != nil {
		return err
	}
	return example.PrintJSON(points)
}
