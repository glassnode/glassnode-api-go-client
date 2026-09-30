package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	glassnode "github.com/glassnode/glassnode-api-go-client"
)

func main() {
	client, err := glassnode.NewClient(os.Getenv("GLASSNODE_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	points, err := client.GetTimeSeries(ctx, "market/price_usd_close", &glassnode.MetricParams{
		Asset: "BTC", Since: time.Now().AddDate(0, 0, -30), Interval: "24h",
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, point := range points {
		if point.Value != nil {
			fmt.Printf("%s: %.2f\n", time.Unix(point.Timestamp, 0).UTC(), *point.Value)
		}
	}
}
