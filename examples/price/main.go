package main

import (
	"context"
	"fmt"
	glassnode "github.com/glassnode/glassnode-api-go-client"
	"github.com/glassnode/glassnode-api-go-client/examples/internal/example"
	"time"
)

func main() {
	example.Run(run)
}

func run(ctx context.Context, client *glassnode.Client, params *glassnode.MetricParams) error {
	points, err := client.GetTimeSeries(ctx, "market/price_usd_close", params)
	if err != nil {
		return err
	}
	for _, point := range points {
		if point.Value == nil {
			fmt.Printf("%s: missing\n", time.Unix(point.Timestamp, 0).UTC().Format(time.DateOnly))
			continue
		}
		fmt.Printf("%s: %.2f USD\n", time.Unix(point.Timestamp, 0).UTC().Format(time.DateOnly), *point.Value)
	}
	return nil
}
