package glassnode

import (
	"context"
	"encoding/json"
	"errors"
)

// AllowancePeriod is the period an API allowance is granted over.
type AllowancePeriod string

// Allowance periods reported by the usage endpoint. An add-on without a period
// predates the field and is monthly.
const (
	AllowanceDaily   AllowancePeriod = "daily"
	AllowanceMonthly AllowancePeriod = "monthly"
)

// APIAddon is an API allowance returned by the usage endpoint: Value units per
// Period, at most RPM requests per minute when the API reports it.
type APIAddon struct {
	Value  int             `json:"value"`
	Period AllowancePeriod `json:"period,omitempty"`
	RPM    int             `json:"rpm,omitempty"`
}

// APIUsage reports usage and subscribed API allowances. CreditsUsed counts
// credits this month and DailyRequestsUsed counts requests today; each is the
// counter for an allowance of the matching period.
type APIUsage struct {
	CreditsUsed       int        `json:"creditsUsed"`
	DailyRequestsUsed int        `json:"dailyRequestsUsed"`
	APIAddons         []APIAddon `json:"apiAddons"`
}

// UnmarshalJSON requires creditsUsed and tolerates missing add-ons.
func (u *APIUsage) UnmarshalJSON(data []byte) error {
	var wire struct {
		CreditsUsed       *int       `json:"creditsUsed"`
		DailyRequestsUsed int        `json:"dailyRequestsUsed"`
		APIAddons         []APIAddon `json:"apiAddons"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.CreditsUsed == nil {
		return errors.New("usage requires creditsUsed")
	}
	u.CreditsUsed = *wire.CreditsUsed
	u.DailyRequestsUsed = wire.DailyRequestsUsed
	// Accounts without add-ons may report null or omit the field entirely.
	u.APIAddons = wire.APIAddons
	if u.APIAddons == nil {
		u.APIAddons = []APIAddon{}
	}
	return nil
}

// Allowance is an account's API allowance and the usage counted against it.
// On a daily allowance the unit is requests and Used is today's requests; on
// a monthly allowance the unit is credits and Used is this month's credits.
type Allowance struct {
	Limit  int
	Period AllowancePeriod
	Used   int
}

// Remaining returns how much of the allowance is left in the current period,
// never below zero.
func (a Allowance) Remaining() int {
	return max(a.Limit-a.Used, 0)
}

// Allowance returns the account's allowance with the usage of the matching
// period. ok is false when the account has no add-on, an add-on reports a
// period the client does not know, or add-ons of different periods are
// combined, since no single counter then applies; callers should not guess.
func (u *APIUsage) Allowance() (allowance Allowance, ok bool) {
	var period AllowancePeriod
	for i, addon := range u.APIAddons {
		p := addon.Period
		if p == "" {
			p = AllowanceMonthly
		}
		if p != AllowanceDaily && p != AllowanceMonthly {
			return Allowance{}, false
		}
		if i > 0 && p != period {
			return Allowance{}, false
		}
		period = p
		allowance.Limit = max(allowance.Limit, addon.Value)
	}
	if len(u.APIAddons) == 0 {
		return Allowance{}, false
	}
	allowance.Period = period
	allowance.Used = u.CreditsUsed
	if period == AllowanceDaily {
		allowance.Used = u.DailyRequestsUsed
	}
	return allowance, true
}

// CreditsPerMonth returns the largest subscribed allowance, or zero, whatever
// its period.
//
// Deprecated: an allowance may be daily; use Allowance, which reports the
// period and the usage counted against it.
func (u *APIUsage) CreditsPerMonth() int {
	var largest int
	for _, addon := range u.APIAddons {
		largest = max(largest, addon.Value)
	}
	return largest
}

// GetAPIUsage fetches the current authenticated user's API usage.
func (c *Client) GetAPIUsage(ctx context.Context) (*APIUsage, error) {
	var usage APIUsage
	if err := c.Get(ctx, "/v1/user/api_usage", nil, &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}
