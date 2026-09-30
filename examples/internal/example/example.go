// Package example provides setup for the runnable programs, not for SDK consumers.
package example

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	glassnode "github.com/glassnode/glassnode-api-go-client"
)

// Run configures credentials, a deadline and common metric flags.
func Run(run func(context.Context, *glassnode.Client, *glassnode.MetricParams) error) {
	asset := flag.String("asset", "BTC", "asset symbol (ignored by bulk and usage)")
	days := flag.Int("days", 30, "days of history (metric examples only)")
	timeout := flag.Duration("timeout", 30*time.Second, "deadline for the entire operation")
	flag.Parse()
	if *days <= 0 || *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "days and timeout must be positive")
		os.Exit(2)
	}
	key, token := os.Getenv("GLASSNODE_API_KEY"), os.Getenv("GLASSNODE_ACCESS_TOKEN")
	if (key == "") == (token == "") {
		fmt.Fprintln(os.Stderr, "set exactly one of GLASSNODE_API_KEY or GLASSNODE_ACCESS_TOKEN")
		os.Exit(2)
	}
	var options []glassnode.Option
	if token != "" {
		options = append(options, glassnode.WithBearerToken(token))
	}
	if baseURL := os.Getenv("GLASSNODE_BASE_URL"); baseURL != "" {
		options = append(options, glassnode.WithBaseURL(baseURL))
	}
	client, err := glassnode.NewClient(key, options...)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		err = run(ctx, client, &glassnode.MetricParams{Asset: *asset, Since: time.Now().AddDate(0, 0, -*days), Interval: "24h"})
		cancel()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// PrintJSON prints nullable and structured values without discarding fields.
func PrintJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
