package glassnode

import (
	"context"
	"encoding/json"
	"errors"
)

// APIAddon is an API credit allowance returned by the usage endpoint.
type APIAddon struct {
	Value int `json:"value"`
}

// APIUsage reports credits consumed and subscribed API allowances.
type APIUsage struct {
	CreditsUsed int        `json:"creditsUsed"`
	APIAddons   []APIAddon `json:"apiAddons"`
}

func (u *APIUsage) UnmarshalJSON(data []byte) error {
	var wire struct {
		CreditsUsed *int       `json:"creditsUsed"`
		APIAddons   []APIAddon `json:"apiAddons"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.CreditsUsed == nil {
		return errors.New("usage requires creditsUsed")
	}
	u.CreditsUsed = *wire.CreditsUsed
	// Accounts without add-ons may report null or omit the field entirely.
	u.APIAddons = wire.APIAddons
	if u.APIAddons == nil {
		u.APIAddons = []APIAddon{}
	}
	return nil
}

// CreditsPerMonth returns the largest subscribed credit allowance, or zero.
func (u *APIUsage) CreditsPerMonth() int {
	var max int
	for _, addon := range u.APIAddons {
		if addon.Value > max {
			max = addon.Value
		}
	}
	return max
}

// GetAPIUsage fetches the current authenticated user's API credit usage.
func (c *Client) GetAPIUsage(ctx context.Context) (*APIUsage, error) {
	var usage APIUsage
	if err := c.Get(ctx, "/v1/user/api_usage", nil, &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}
