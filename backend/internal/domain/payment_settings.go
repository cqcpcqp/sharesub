package domain

import "time"

type PaymentSettings struct {
	BaseURL       string     `json:"base_url"`
	PID           string     `json:"pid"`
	KeyCiphertext []byte     `json:"-"`
	Enabled       bool       `json:"enabled"`
	Revision      int64      `json:"revision"`
	StartedAt     *time.Time `json:"started_at"`
}
