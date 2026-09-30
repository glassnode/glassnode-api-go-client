package glassnode

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MetricParams selects data or filters metadata. Zero fields are omitted.
// Asset is a convenience for one asset; Assets and Exchanges encode repeated
// query parameters (a=BTC&a=ETH). Do not set both Asset and Assets.
// Extra accepts additional documented selectors such as network, quote_symbol,
// bps, side, size, miner and maturity; it must not duplicate a typed field.
type MetricParams struct {
	Asset     string
	Assets    []string
	Exchanges []string
	Since     time.Time
	Until     time.Time
	Interval  string
	Currency  string
	Extra     url.Values
}

// Values returns a fresh query map. Unix seconds may also be supplied through
// Extra["s"] and Extra["u"] when the corresponding time.Time field is zero.
func (p *MetricParams) Values() (url.Values, error) {
	if p == nil {
		return url.Values{}, nil
	}
	q, err := cloneQuery(p.Extra)
	if err != nil {
		return nil, err
	}
	set := func(key string, values []string) error {
		if len(values) == 0 {
			return nil
		}
		if _, exists := q[key]; exists {
			return &InputError{key, "set either the typed field or Extra, not both"}
		}
		q[key] = append([]string(nil), values...)
		return nil
	}
	if p.Asset != "" && len(p.Assets) > 0 {
		return nil, &InputError{"asset", "set Asset or Assets, not both"}
	}
	assets := p.Assets
	if p.Asset != "" {
		assets = []string{p.Asset}
	}
	if err := set("a", assets); err != nil {
		return nil, err
	}
	if err := set("e", p.Exchanges); err != nil {
		return nil, err
	}
	for _, pair := range []struct{ key, value string }{{"i", p.Interval}, {"c", p.Currency}} {
		if pair.value != "" {
			if err := set(pair.key, []string{pair.value}); err != nil {
				return nil, err
			}
		}
	}
	for _, pair := range []struct {
		key   string
		value time.Time
	}{{"s", p.Since}, {"u", p.Until}} {
		if !pair.value.IsZero() {
			if err := set(pair.key, []string{strconv.FormatInt(pair.value.Unix(), 10)}); err != nil {
				return nil, err
			}
		}
	}
	for _, key := range []string{"s", "u", "i", "c", "f"} {
		if len(q[key]) > 1 {
			return nil, &InputError{key, "accepts only one value"}
		}
	}
	var since, until *int64
	for _, key := range []string{"s", "u"} {
		if raw := q.Get(key); raw != "" {
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return nil, &InputError{key, "expected Unix seconds"}
			}
			if key == "s" {
				since = &v
			} else {
				until = &v
			}
		}
	}
	if since != nil && until != nil && *since > *until {
		return nil, &InputError{"time range", "Since must be <= Until"}
	}
	return cloneQuery(q)
}

func metricQuery(path string, params *MetricParams, metadata bool) (string, url.Values, error) {
	path = "/" + strings.TrimPrefix(path, "/")
	if err := validateEndpoint(path); err != nil {
		return "", nil, &InputError{"metric path", "expected a relative metric path without URL, query or traversal"}
	}
	q, err := params.Values()
	if err != nil {
		return "", nil, err
	}
	if metadata {
		if _, exists := q["path"]; exists {
			return "", nil, &InputError{"path", "use the metric path argument"}
		}
		q.Set("path", path)
	}
	q.Set("f", "json")
	return path, q, nil
}
