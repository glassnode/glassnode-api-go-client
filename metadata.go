package glassnode

import (
	"context"
	"errors"
	"net/url"
)

// ListAssets lists asset metadata, optionally scoped by a CEL filter.
func (c *Client) ListAssets(ctx context.Context, filter string) ([]Asset, error) {
	q := url.Values{}
	if filter != "" {
		q.Set("filter", filter)
	}
	var wire AssetsResponse
	if err := c.Get(ctx, "/v1/metadata/assets", q, &wire); err != nil {
		return nil, err
	}
	if wire.Data == nil {
		return nil, &DecodeError{"/v1/metadata/assets", errors.New("expected data array")}
	}
	for _, asset := range wire.Data {
		if asset.ID == "" {
			return nil, &DecodeError{"/v1/metadata/assets", errors.New("expected asset ID")}
		}
	}
	return wire.Data, nil
}

// ListMetrics lists metric paths, optionally filtered by asset and other selectors.
func (c *Client) ListMetrics(ctx context.Context, params *MetricParams) ([]string, error) {
	q, err := params.Values()
	if err != nil {
		return nil, err
	}
	var paths []string
	if err := c.Get(ctx, "/v1/metadata/metrics", q, &paths); err != nil {
		return nil, err
	}
	if paths == nil {
		return nil, &DecodeError{"/v1/metadata/metrics", errors.New("expected an array")}
	}
	for _, path := range paths {
		if err := validateEndpoint(path); err != nil {
			return nil, &DecodeError{"/v1/metadata/metrics", errors.New("expected metric paths")}
		}
	}
	return paths, nil
}

// GetMetricMetadata describes a metric's selectors, variants and availability.
func (c *Client) GetMetricMetadata(ctx context.Context, path string, params *MetricParams) (*MetricMetadata, error) {
	_, q, err := metricQuery(path, params, true)
	if err != nil {
		return nil, err
	}
	var meta MetricMetadata
	if err := c.Get(ctx, "/v1/metadata/metric", q, &meta); err != nil {
		return nil, err
	}
	if meta.Parameters == nil {
		return nil, &DecodeError{"/v1/metadata/metric", errors.New("expected metric parameters")}
	}
	return &meta, nil
}

// LagPercentiles are durations in the enclosing MetricLagEntry.Unit. Pointers
// distinguish missing percentiles from measurements of zero.
type LagPercentiles struct {
	P50 *float64 `json:"p50,omitempty"`
	P90 *float64 `json:"p90,omitempty"`
	P95 *float64 `json:"p95,omitempty"`
	P99 *float64 `json:"p99,omitempty"`
}

// MetricLagEntry contains data lag over a measurement window by resolution.
type MetricLagEntry struct {
	Unit       string                    `json:"unit"`
	Window     string                    `json:"window"`
	Resolution map[string]LagPercentiles `json:"resolution"`
}

// MetricStats contains the API's lag statistics. Lag values are durations,
// not timestamps; use each entry's Unit when interpreting them.
type MetricStats struct {
	Lag []MetricLagEntry `json:"lag"`
}

// GetMetricStats fetches metric data-lag percentiles.
func (c *Client) GetMetricStats(ctx context.Context, path string, params *MetricParams) (*MetricStats, error) {
	_, q, err := metricQuery(path, params, true)
	if err != nil {
		return nil, err
	}
	var stats MetricStats
	if err := c.Get(ctx, "/v1/metadata/metric/stats", q, &stats); err != nil {
		return nil, err
	}
	if stats.Lag == nil {
		return nil, &DecodeError{"/v1/metadata/metric/stats", errors.New("expected lag array")}
	}
	for _, entry := range stats.Lag {
		if entry.Unit == "" || entry.Resolution == nil {
			return nil, &DecodeError{"/v1/metadata/metric/stats", errors.New("expected lag unit and resolution")}
		}
	}
	return &stats, nil
}

// ListNames lists a named metadata endpoint, optionally filtered by CEL.
func (c *Client) ListNames(ctx context.Context, endpoint, filter string) ([]string, error) {
	q := url.Values{}
	if filter != "" {
		q.Set("filter", filter)
	}
	var wire NamesResponse
	if err := c.Get(ctx, endpoint, q, &wire); err != nil {
		return nil, err
	}
	if wire.Data == nil {
		return nil, &DecodeError{endpoint, errors.New("expected data array")}
	}
	names := make([]string, 0, len(wire.Data))
	for _, entry := range wire.Data {
		if entry.Name == "" {
			return nil, &DecodeError{endpoint, errors.New("expected metadata name")}
		}
		names = append(names, entry.Name)
	}
	return names, nil
}

// ListMetricTags lists the available metric tags.
func (c *Client) ListMetricTags(ctx context.Context) ([]string, error) {
	return c.ListNames(ctx, "/v1/metadata/tags", "")
}

// ListAssetTags lists asset tags, optionally scoped by a CEL filter.
func (c *Client) ListAssetTags(ctx context.Context, filter string) ([]string, error) {
	return c.ListNames(ctx, "/v1/metadata/assets/tags", filter)
}

// ListAssetCategories lists asset categories, optionally scoped by a CEL filter.
func (c *Client) ListAssetCategories(ctx context.Context, filter string) ([]string, error) {
	return c.ListNames(ctx, "/v1/metadata/assets/categories", filter)
}

// ListAssetBlockchains lists asset blockchains, optionally scoped by a CEL filter.
func (c *Client) ListAssetBlockchains(ctx context.Context, filter string) ([]string, error) {
	return c.ListNames(ctx, "/v1/metadata/assets/blockchains", filter)
}
