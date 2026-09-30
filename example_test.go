package glassnode_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	glassnode "github.com/glassnode/glassnode-api-go-client"
)

func ExampleClient_GetTimeSeries() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"t":1700000000,"v":42000},{"t":1700086400,"v":null}]`)
	}))
	defer server.Close()
	client, err := glassnode.NewClient("example-key", glassnode.WithBaseURL(server.URL))
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	points, err := client.GetTimeSeries(ctx, "market/price_usd_close", &glassnode.MetricParams{Asset: "BTC"})
	if err != nil {
		panic(err)
	}
	for _, point := range points {
		if point.Value == nil {
			fmt.Println("gap")
		} else {
			fmt.Printf("%.0f\n", *point.Value)
		}
	}
	// Output:
	// 42000
	// gap
}
