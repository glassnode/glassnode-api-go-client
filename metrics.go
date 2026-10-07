package glassnode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// TimeSeriesPoint is a scalar metric point. Value is nil for a gap (JSON null).
// Timestamp is Unix seconds, not milliseconds. ComputedAt is the Unix time at
// which a point-in-time metric computed the value; it is nil for other metrics.
type TimeSeriesPoint struct {
	Timestamp  int64    `json:"t"`
	Value      *float64 `json:"v"`
	ComputedAt *int64   `json:"computed_at,omitempty"`
}

// UnmarshalJSON requires the timestamp and scalar value, tolerating new fields.
func (p *TimeSeriesPoint) UnmarshalJSON(data []byte) error {
	var wire struct {
		Timestamp  *int64          `json:"t"`
		Value      json.RawMessage `json:"v"`
		ComputedAt *int64          `json:"computed_at"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Timestamp == nil || len(wire.Value) == 0 {
		return errors.New("metric point requires t and v")
	}
	var value *float64
	if err := json.Unmarshal(wire.Value, &value); err != nil {
		return err
	}
	p.Timestamp = *wire.Timestamp
	p.Value = value
	p.ComputedAt = wire.ComputedAt
	return nil
}

// ObjectTimeSeriesPoint is an object metric point (OHLC or a breakdown).
// Object values are nullable; nested shapes can be decoded with GetMetric.
// ComputedAt is set for point-in-time metrics, as in TimeSeriesPoint.
type ObjectTimeSeriesPoint struct {
	Timestamp  int64               `json:"t"`
	Object     map[string]*float64 `json:"o"`
	ComputedAt *int64              `json:"computed_at,omitempty"`
}

// UnmarshalJSON requires the timestamp and object, tolerating new fields.
func (p *ObjectTimeSeriesPoint) UnmarshalJSON(data []byte) error {
	var wire struct {
		Timestamp  *int64              `json:"t"`
		Object     map[string]*float64 `json:"o"`
		ComputedAt *int64              `json:"computed_at"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Timestamp == nil || wire.Object == nil {
		return errors.New("object metric point requires t and o")
	}
	p.Timestamp = *wire.Timestamp
	p.Object = wire.Object
	p.ComputedAt = wire.ComputedAt
	return nil
}

// BulkEntry is one value of a bulk response. Params holds every selector that
// identifies the entry, such as "a", "e", "network" or, for object metrics,
// "category". Asset and Network repeat Params["a"] and Params["network"].
type BulkEntry struct {
	Asset   string
	Value   *float64
	Network string
	Params  map[string]string
}

// UnmarshalJSON requires v and keeps every other field as a selector.
func (p *BulkEntry) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	raw, ok := fields["v"]
	if !ok {
		return errors.New("bulk entry requires v")
	}
	var value *float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	params := make(map[string]string, len(fields)-1)
	for key, raw := range fields {
		if key == "v" {
			continue
		}
		var param string
		if err := json.Unmarshal(raw, &param); err != nil {
			param = string(raw)
		}
		params[key] = param
	}
	*p = BulkEntry{Asset: params["a"], Value: value, Network: params["network"], Params: params}
	return nil
}

// MarshalJSON encodes the entry in the API's wire format, with the selectors
// next to "v".
func (p BulkEntry) MarshalJSON() ([]byte, error) {
	fields := make(map[string]any, len(p.Params)+3)
	for key, param := range p.Params {
		fields[key] = param
	}
	if p.Asset != "" {
		fields["a"] = p.Asset
	}
	if p.Network != "" {
		fields["network"] = p.Network
	}
	fields["v"] = p.Value
	return json.Marshal(fields)
}

// BulkDataPoint groups asset values at a Unix timestamp in seconds.
type BulkDataPoint struct {
	Timestamp int64       `json:"t"`
	Bulk      []BulkEntry `json:"bulk"`
}

// UnmarshalJSON requires the timestamp and bulk entries.
func (p *BulkDataPoint) UnmarshalJSON(data []byte) error {
	var wire struct {
		Timestamp *int64      `json:"t"`
		Bulk      []BulkEntry `json:"bulk"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Timestamp == nil || wire.Bulk == nil {
		return errors.New("bulk point requires t and bulk")
	}
	p.Timestamp = *wire.Timestamp
	p.Bulk = wire.Bulk
	return nil
}

// GetMetric decodes a metric response into dst. Use this for custom/nested
// shapes or lossless json.RawMessage; common shapes have convenience methods.
func (c *Client) GetMetric(ctx context.Context, path string, params *MetricParams, dst any) error {
	path, q, err := metricQuery(path, params, false)
	if err != nil {
		return err
	}
	return c.Get(ctx, "/v1/metrics"+path, q, dst)
}

// GetTimeSeries fetches a scalar metric with nullable values.
func (c *Client) GetTimeSeries(ctx context.Context, path string, params *MetricParams) ([]TimeSeriesPoint, error) {
	var points []TimeSeriesPoint
	if err := c.GetMetric(ctx, path, params, &points); err != nil {
		return nil, err
	}
	if points == nil {
		return nil, &DecodeError{"/v1/metrics/" + strings.TrimPrefix(path, "/"), errors.New("expected an array")}
	}
	return points, nil
}

// GetObjectTimeSeries fetches a metric whose o field is a numeric breakdown.
func (c *Client) GetObjectTimeSeries(ctx context.Context, path string, params *MetricParams) ([]ObjectTimeSeriesPoint, error) {
	var points []ObjectTimeSeriesPoint
	if err := c.GetMetric(ctx, path, params, &points); err != nil {
		return nil, err
	}
	if points == nil {
		return nil, &DecodeError{"/v1/metrics/" + strings.TrimPrefix(path, "/"), errors.New("expected an array")}
	}
	return points, nil
}

// GetBulkMetric fetches the bulk variant of a metric and unwraps the data
// envelope. Pass the base metric path, without a /bulk suffix. The API requires
// Since and limits the time range per request (31 days at 24h resolution).
// Each asset and selector combination is billed like a separate request, and
// omitting Assets selects every asset, so set the selectors explicitly.
func (c *Client) GetBulkMetric(ctx context.Context, path string, params *MetricParams) ([]BulkDataPoint, error) {
	path, q, err := metricQuery(path, params, false)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, "/bulk") {
		return nil, &InputError{"metric path", "pass the base path without /bulk"}
	}
	if !q.Has("s") {
		return nil, &InputError{"s", "bulk metrics require Since"}
	}
	endpoint := "/v1/metrics" + path + "/bulk"
	var wire struct {
		Data []BulkDataPoint `json:"data"`
	}
	if err := c.Get(ctx, endpoint, q, &wire); err != nil {
		return nil, err
	}
	if wire.Data == nil {
		return nil, &DecodeError{endpoint, errors.New("expected data array")}
	}
	return wire.Data, nil
}
