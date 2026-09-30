package glassnode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// TimeSeriesPoint is a scalar metric point. Value is nil for a gap (JSON null).
// Timestamp is Unix seconds, not milliseconds.
type TimeSeriesPoint struct {
	Timestamp int64    `json:"t"`
	Value     *float64 `json:"v"`
}

// UnmarshalJSON requires the timestamp and scalar value, tolerating new fields.
func (p *TimeSeriesPoint) UnmarshalJSON(data []byte) error {
	var wire struct {
		Timestamp *int64          `json:"t"`
		Value     json.RawMessage `json:"v"`
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
	return nil
}

// ObjectTimeSeriesPoint is an object metric point (OHLC or a breakdown).
// Object values are nullable; nested shapes can be decoded with GetMetric.
type ObjectTimeSeriesPoint struct {
	Timestamp int64               `json:"t"`
	Object    map[string]*float64 `json:"o"`
}

func (p *ObjectTimeSeriesPoint) UnmarshalJSON(data []byte) error {
	var wire struct {
		Timestamp *int64              `json:"t"`
		Object    map[string]*float64 `json:"o"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Timestamp == nil || wire.Object == nil {
		return errors.New("object metric point requires t and o")
	}
	p.Timestamp = *wire.Timestamp
	p.Object = wire.Object
	return nil
}

// BulkEntry is one asset's nullable scalar value, optionally scoped to a network.
type BulkEntry struct {
	Asset   string   `json:"a"`
	Value   *float64 `json:"v"`
	Network string   `json:"network,omitempty"`
}

func (p *BulkEntry) UnmarshalJSON(data []byte) error {
	var wire struct {
		Asset   string          `json:"a"`
		Value   json.RawMessage `json:"v"`
		Network string          `json:"network"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Asset == "" || len(wire.Value) == 0 {
		return errors.New("bulk entry requires a and v")
	}
	var value *float64
	if err := json.Unmarshal(wire.Value, &value); err != nil {
		return err
	}
	p.Asset = wire.Asset
	p.Value = value
	p.Network = wire.Network
	return nil
}

// BulkDataPoint groups asset values at a Unix timestamp in seconds.
type BulkDataPoint struct {
	Timestamp int64       `json:"t"`
	Bulk      []BulkEntry `json:"bulk"`
}

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

// GetBulkMetric fetches values for several assets and unwraps the data envelope.
// Pass the base metric path, without a /bulk suffix.
func (c *Client) GetBulkMetric(ctx context.Context, path string, params *MetricParams) ([]BulkDataPoint, error) {
	path, q, err := metricQuery(path, params, false)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, "/bulk") {
		return nil, &InputError{"metric path", "pass the base path without /bulk"}
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
