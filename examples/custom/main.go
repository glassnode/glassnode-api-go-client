package main

import (
	"context"
	"encoding/json"
	glassnode "github.com/glassnode/glassnode-api-go-client"
	"github.com/glassnode/glassnode-api-go-client/examples/internal/example"
)

func main() {
	example.Run(run)
}

func run(ctx context.Context, client *glassnode.Client, params *glassnode.MetricParams) error {
	// Keep numbers lossless and choose your own response types.
	var points []struct {
		Timestamp int64        `json:"t"`
		Value     *json.Number `json:"v"`
	}
	if err := client.GetMetric(ctx, "market/price_usd_close", params, &points); err != nil {
		return err
	}
	return example.PrintJSON(points)
}
