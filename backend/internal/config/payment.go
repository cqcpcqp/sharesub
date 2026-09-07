package config

import (
	"os"
	"strings"

	"github.com/sharesub/sharesub/backend/internal/payment"
)

func EasyPay() payment.Config {
	return payment.Config{BaseURL: strings.TrimSpace(os.Getenv("SHARESUB_EASYPAY_URL")), PID: strings.TrimSpace(os.Getenv("SHARESUB_EASYPAY_PID")), Key: strings.TrimSpace(os.Getenv("SHARESUB_EASYPAY_KEY"))}
}
