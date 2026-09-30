package glassnode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordedAPIContracts(t *testing.T) {
	for _, tt := range []struct {
		file string
		call func(*Client) error
	}{
		{"asset-metadata.json", func(c *Client) error { _, err := c.ListAssets(context.Background(), ""); return err }},
		{"metric-list.json", func(c *Client) error { _, err := c.ListMetrics(context.Background(), nil); return err }},
		{"metric-metadata-price-usd-close.json", func(c *Client) error {
			_, err := c.GetMetricMetadata(context.Background(), "market/price_usd_close", nil)
			return err
		}},
		{"metric-metadata-us-spot-etf-balances-all.json", func(c *Client) error {
			_, err := c.GetMetricMetadata(context.Background(), "institutions/us_spot_etf_balances_all", nil)
			return err
		}},
		{"metric-metadata-balance-exchanges-btc.json", func(c *Client) error {
			metadata, err := c.GetMetricMetadata(context.Background(), "distribution/balance_exchanges", &MetricParams{Asset: "BTC"})
			if err != nil {
				return err
			}
			defaults := metadata.ParametersDefaults["e"]
			if len(defaults) != 1 || defaults[0] != "aggregated" {
				return fmt.Errorf("lost recorded exchange defaults: %v", defaults)
			}
			return nil
		}},
		{"metric-stats-active-count-btc.json", func(c *Client) error {
			_, err := c.GetMetricStats(context.Background(), "addresses/active_count", &MetricParams{Asset: "BTC"})
			return err
		}},
		{"metric-stats-us-spot-etf-balances-all.json", func(c *Client) error {
			_, err := c.GetMetricStats(context.Background(), "institutions/us_spot_etf_balances_all", nil)
			return err
		}},
		{"timeseries-price-usd-close-btc.json", func(c *Client) error {
			_, err := c.GetTimeSeries(context.Background(), "market/price_usd_close", nil)
			return err
		}},
		{"timeseries-price-usd-ohlc-btc.json", func(c *Client) error {
			_, err := c.GetObjectTimeSeries(context.Background(), "market/price_usd_ohlc", nil)
			return err
		}},
		{"bulk-marketcap-usd.json", func(c *Client) error {
			_, err := c.GetBulkMetric(context.Background(), "market/marketcap_usd", nil)
			return err
		}},
	} {
		t.Run(tt.file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", "contract", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
			if err := tt.call(client); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMalformedMetadataAndUsage(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		call       func(*Client) error
	}{
		{"missing asset ID", `{"data":[{"name":"Bitcoin"}]}`, func(c *Client) error { _, err := c.ListAssets(context.Background(), ""); return err }},
		{"invalid metric path", `["/../escape"]`, func(c *Client) error { _, err := c.ListMetrics(context.Background(), nil); return err }},
		{"missing lag unit", `{"lag":[{"resolution":{}}]}`, func(c *Client) error {
			_, err := c.GetMetricStats(context.Background(), "market/price", nil)
			return err
		}},
		{"missing credits", `{"apiAddons":[]}`, func(c *Client) error { _, err := c.GetAPIUsage(context.Background()); return err }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(tt.body)) })
			var decode *DecodeError
			if err := tt.call(client); !errors.As(err, &decode) {
				t.Fatalf("expected DecodeError, got %v", err)
			}
		})
	}
}
