package glassnode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestExtractedEndpointFixtures(t *testing.T) {
	ctx := context.Background()
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fixtures := map[string]string{
			"/v1/metadata/assets": "assets.json", "/v1/metadata/metrics": "metrics_list.json",
			"/v1/metadata/metric": "metric_describe.json", "/v1/metrics/market/price_usd_close": "metric_points.json",
			"/v1/metrics/market/marketcap_usd/bulk": "metric_bulk.json", "/v1/user/api_usage": "api_usage.json",
		}
		if file := fixtures[r.URL.Path]; file != "" {
			data, err := os.ReadFile("testdata/" + file)
			if err != nil {
				t.Error(err)
			}
			w.Write(data)
			return
		}
		if r.URL.Path == "/v1/metadata/metric/stats" {
			if r.URL.Query().Get("path") != "/market/price_usd_close" || r.URL.Query().Get("a") != "BTC" {
				t.Errorf("stats query %v", r.URL)
			}
			fmt.Fprint(w, `{"lag":[{"unit":"seconds","window":"30d","resolution":{"24h":{"p50":0,"p99":30}}}]}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"name":"one"},{"name":"two"}]}`)
	})
	assets, err := client.ListAssets(ctx, "")
	if err != nil || len(assets) == 0 || assets[0].ID == "" {
		t.Fatalf("assets %v %v", assets, err)
	}
	metrics, err := client.ListMetrics(ctx, nil)
	if err != nil || len(metrics) == 0 {
		t.Fatalf("metrics %v %v", metrics, err)
	}
	meta, err := client.GetMetricMetadata(ctx, "market/price_usd_close", nil)
	if err != nil || meta.Parameters == nil {
		t.Fatalf("meta %v %v", meta, err)
	}
	series, err := client.GetTimeSeries(ctx, "market/price_usd_close", nil)
	if err != nil || len(series) != 3 {
		t.Fatalf("series %v %v", series, err)
	}
	bulk, err := client.GetBulkMetric(ctx, "market/marketcap_usd", &MetricParams{Assets: []string{"BTC", "ETH"}, Since: time.Unix(1770076800, 0)})
	if err != nil || len(bulk) != 31 || bulk[0].Bulk[0].Asset() != "BTC" {
		t.Fatalf("bulk %v %v", bulk, err)
	}
	usage, err := client.GetAPIUsage(ctx)
	if err != nil || usage.CreditsUsed != 6 || usage.CreditsPerMonth() != 1500000 {
		t.Fatalf("usage %v %v", usage, err)
	}
	stats, err := client.GetMetricStats(ctx, "market/price_usd_close", &MetricParams{Asset: "BTC"})
	if err != nil {
		t.Fatal(err)
	}
	percentiles := stats.Lag[0].Resolution["24h"]
	if percentiles.P50 == nil || *percentiles.P50 != 0 || percentiles.P90 != nil {
		t.Fatalf("stats %v", percentiles)
	}
	for _, call := range []func() ([]string, error){
		func() ([]string, error) { return client.ListMetricTags(ctx) }, func() ([]string, error) { return client.ListAssetTags(ctx, "") },
		func() ([]string, error) { return client.ListAssetCategories(ctx, "") }, func() ([]string, error) { return client.ListAssetBlockchains(ctx, "") },
		func() ([]string, error) { return client.ListExchanges(ctx) }, func() ([]string, error) { return client.ListNetworks(ctx) },
		func() ([]string, error) { return client.ListMiners(ctx) },
	} {
		names, err := call()
		if err != nil || len(names) != 2 || names[0] != "one" {
			t.Fatalf("names %v %v", names, err)
		}
	}
}

func TestUsageWithoutAddons(t *testing.T) {
	for _, body := range []string{`{"creditsUsed":6,"apiAddons":null}`, `{"creditsUsed":6}`} {
		client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		usage, err := client.GetAPIUsage(context.Background())
		if err != nil || usage.CreditsUsed != 6 || usage.CreditsPerMonth() != 0 || usage.APIAddons == nil {
			t.Fatalf("%s: %+v %v", body, usage, err)
		}
	}
}

func TestTimeSeriesPointWithoutComputedAt(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"t":1,"v":2},{"t":3,"v":4,"computed_at":5}]`)
	})
	points, err := client.GetTimeSeries(context.Background(), "market/price", nil)
	if err != nil || points[0].ComputedAt != nil || points[1].ComputedAt == nil || *points[1].ComputedAt != 5 {
		t.Fatalf("points %+v %v", points, err)
	}
	encoded, _ := json.Marshal(points[0])
	if string(encoded) != `{"t":1,"v":2}` {
		t.Errorf("omitempty lost: %s", encoded)
	}
}

func TestObjectAndNullableBulk(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics/market/ohlc" {
			fmt.Fprint(w, `[{"t":1,"o":{"o":10,"c":null,"new":15}}]`)
			return
		}
		fmt.Fprint(w, `{"data":[{"t":1,"bulk":[{"a":"BTC","v":null},{"a":"ETH","v":1,"network":"eth"}]}]}`)
	})
	points, err := client.GetObjectTimeSeries(context.Background(), "market/ohlc", nil)
	if err != nil || len(points) != 1 || points[0].Object["c"] != nil || *points[0].Object["new"] != 15 {
		t.Fatalf("object %v %v", points, err)
	}
	bulk, err := client.GetBulkMetric(context.Background(), "market/price", &MetricParams{Since: time.Unix(1, 0)})
	if err != nil || bulk[0].Bulk[0].Value != nil || bulk[0].Bulk[1].Network() != "eth" {
		t.Fatalf("bulk %v %v", bulk, err)
	}
}

func TestBulkEntryKeepsSelectors(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"t":1,"bulk":[
			{"a":"BTC","e":"binance","v":12},
			{"a":"BTC","e":"bitfinex","v":123},
			{"a":"ETH","category":"more_10y","v":null}]}]}`)
	})
	bulk, err := client.GetBulkMetric(context.Background(), "distribution/balance_exchanges", &MetricParams{Since: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	entries := bulk[0].Bulk
	if entries[0].Params["e"] != "binance" || entries[1].Params["e"] != "bitfinex" || entries[1].Asset() != "BTC" {
		t.Errorf("exchange selectors lost: %+v", entries)
	}
	if entries[2].Params["category"] != "more_10y" || entries[2].Value != nil {
		t.Errorf("category lost: %+v", entries[2])
	}
	encoded, err := json.Marshal(entries[0])
	if err != nil || string(encoded) != `{"a":"BTC","e":"binance","v":12}` {
		t.Errorf("marshal %s %v", encoded, err)
	}
	var missingValue BulkEntry
	if err := json.Unmarshal([]byte(`{"a":"BTC"}`), &missingValue); err == nil {
		t.Error("accepted bulk entry without v")
	}
	// A non-string selector is kept as its JSON text and written back as a string.
	var numeric BulkEntry
	if err := json.Unmarshal([]byte(`{"a":"BTC","bps":5,"v":1}`), &numeric); err != nil || numeric.Params["bps"] != "5" {
		t.Fatalf("numeric selector: %+v %v", numeric, err)
	}
	if encoded, _ := json.Marshal(numeric); string(encoded) != `{"a":"BTC","bps":"5","v":1}` {
		t.Errorf("numeric selector marshal: %s", encoded)
	}
}

func TestParametersDefaultsAcceptsStrings(t *testing.T) {
	var meta MetricMetadata
	if err := json.Unmarshal([]byte(`{"path":"/p","parameters":{},"parameters_defaults":{"e":"aggregated","network":["eth"]}}`), &meta); err != nil {
		t.Fatal(err)
	}
	if got := meta.ParametersDefaults; len(got["e"]) != 1 || got["e"][0] != "aggregated" || got["network"][0] != "eth" || meta.Path != "/p" {
		t.Fatalf("defaults %v", meta)
	}
	if err := json.Unmarshal([]byte(`{"parameters":{},"parameters_defaults":{"e":1}}`), &meta); err == nil {
		t.Error("accepted numeric default")
	}
}

func TestMetadataFilterAndRepeatedValues(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metadata/assets" {
			if r.URL.Query().Get("filter") != "asset.id=='BTC'" {
				t.Error("filter lost")
			}
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		if len(r.URL.Query()["a"]) != 2 || len(r.URL.Query()["e"]) != 2 || r.URL.Query().Get("quote_symbol") != "USDT" {
			t.Errorf("selectors %v", r.URL.Query())
		}
		fmt.Fprint(w, `[]`)
	})
	if _, err := client.ListAssets(context.Background(), "asset.id=='BTC'"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListMetrics(context.Background(), &MetricParams{Assets: []string{"BTC", "ETH"}, Exchanges: []string{"binance", "coinbase"}, Extra: url.Values{"quote_symbol": {"USDT"}}}); err != nil {
		t.Fatal(err)
	}
}
