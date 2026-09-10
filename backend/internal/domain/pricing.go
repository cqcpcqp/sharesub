package domain

import "time"

type TokenPrices struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CacheRead   int64 `json:"cache_read"`
	CacheWrite  int64 `json:"cache_write"`
	ImageInput  int64 `json:"image_input"`
	ImageOutput int64 `json:"image_output"`
}

type ModelPrice struct {
	Model             string      `json:"model"`
	Standard          TokenPrices `json:"standard"`
	LongContextTokens int64       `json:"long_context_tokens"`
	LongInputBPS      int64       `json:"long_input_bps"`
	LongOutputBPS     int64       `json:"long_output_bps"`
}

type PricingConfig struct {
	Models          []ModelPrice `json:"models"`
	FastMultiplierBPS int64      `json:"fast_multiplier_bps"`
	FlexMultiplierBPS int64      `json:"flex_multiplier_bps"`
	WebSearchMicros int64        `json:"web_search_micros"`
	Image1KMicros   int64        `json:"image_1k_micros"`
	Image2KMicros   int64        `json:"image_2k_micros"`
	Image4KMicros   int64        `json:"image_4k_micros"`
}

type PricingVersionSummary struct {
	ID          int64     `json:"id"`
	PublishedAt time.Time `json:"published_at"`
	PublishedBy string    `json:"published_by"`
	Reason      string    `json:"reason"`
}

type PricingVersion struct {
	PricingVersionSummary
	Config PricingConfig `json:"config"`
}

type PublishPricingInput struct {
	BaseVersionID int64         `json:"base_version_id"`
	Reason        string        `json:"reason"`
	Config        PricingConfig `json:"config"`
}
