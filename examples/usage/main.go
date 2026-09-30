package main

import (
	"context"
	"fmt"

	glassnode "github.com/glassnode/glassnode-api-go-client"
	"github.com/glassnode/glassnode-api-go-client/examples/internal/example"
)

func main() {
	example.Run(run)
}

func run(ctx context.Context, client *glassnode.Client, params *glassnode.MetricParams) error {
	usage, err := client.GetAPIUsage(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Credits used: %d\nMonthly allowance: %d\n", usage.CreditsUsed, usage.CreditsPerMonth())
	return nil
}
