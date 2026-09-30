package glassnode

// Asset describes a supported blockchain asset or token.
type Asset struct {
	ID             string            `json:"id"`
	ExternalIDs    map[string]string `json:"external_ids,omitempty"`
	Symbol         string            `json:"symbol"`
	Name           string            `json:"name"`
	AssetType      string            `json:"asset_type"`
	Blockchains    []Blockchain      `json:"blockchains"`
	Categories     []string          `json:"categories,omitempty"`
	LogoURL        string            `json:"logo_url,omitempty"`
	SemanticTags   []string          `json:"semantic_tags,omitempty"`
	DefaultNetwork string            `json:"default_network,omitempty"`
}

// Blockchain describes a token deployment and on-chain support.
type Blockchain struct {
	Blockchain     string `json:"blockchain"`
	Address        string `json:"address,omitempty"`
	Decimals       int    `json:"decimals,omitempty"`
	OnChainSupport bool   `json:"on_chain_support"`
}

// AssetsResponse is the wire envelope for asset metadata.
type AssetsResponse struct {
	Data []Asset `json:"data"`
}

// NamesResponse is the response of the metadata list endpoints
// (/v1/metadata/tags, /v1/metadata/assets/tags, ...): {"data":[{"name":"..."},...]}.
type NamesResponse struct {
	Data []NameEntry `json:"data"`
}

// NameEntry is a single named value in a names response.
type NameEntry struct {
	Name string `json:"name"`
}

// MetricMetadata describes a metric, its selectors and available variants.
type MetricMetadata struct {
	Path          string              `json:"path,omitempty"`
	Tier          int                 `json:"tier,omitempty"`
	IsPIT         bool                `json:"is_pit,omitempty"`
	Parameters    map[string][]string `json:"parameters"`
	Queried       map[string]string   `json:"queried,omitempty"`
	Refs          Refs                `json:"refs,omitempty"`
	BulkSupported bool                `json:"bulk_supported"`
	TimeRange     *TimeRange          `json:"timerange,omitempty"`
	Modified      int64               `json:"modified,omitempty"`
	Descriptors   *MetricDescriptors  `json:"descriptors,omitempty"`
}

// MetricVariant links the base, bulk and point-in-time metric paths.
type MetricVariant struct {
	Base *string `json:"base,omitempty"`
	Bulk *string `json:"bulk,omitempty"`
	PIT  *string `json:"pit,omitempty"`
}

// MetricDescriptors contains human-readable metric descriptions.
type MetricDescriptors struct {
	Name             string            `json:"name,omitempty"`
	ShortName        string            `json:"short_name,omitempty"`
	Group            string            `json:"group,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	Description      map[string]string `json:"description,omitempty"`
	DataSharingGroup string            `json:"data_sharing_group,omitempty"`
}

// TimeRange bounds available data in Unix seconds.
type TimeRange struct {
	Min int64 `json:"min,omitempty"`
	Max int64 `json:"max,omitempty"`
}

// Refs links API documentation, Studio and related metric variants.
type Refs struct {
	Docs          string         `json:"docs,omitempty"`
	Studio        string         `json:"studio,omitempty"`
	MetricVariant *MetricVariant `json:"metric_variant,omitempty"`
}
