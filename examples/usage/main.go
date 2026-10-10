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
	allowance, ok := usage.Allowance()
	if !ok {
		fmt.Printf("No single API allowance found; %d credits used this month\n", usage.CreditsUsed)
		return nil
	}
	unit := "credits"
	if allowance.Period == glassnode.AllowanceDaily {
		unit = "requests"
	}
	fmt.Printf("%s allowance: %d %s, used %d, remaining %d\n",
		allowance.Period, allowance.Limit, unit, allowance.Used, allowance.Remaining())
	return nil
}
