package application

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sharesub/sharesub/backend/internal/domain"
)

type rateWindow struct {
	usedPercent float64
	resetAfter  int64
	minutes     int64
}

func ParseCodexQuotaHeaders(headers http.Header, observedAt time.Time) []domain.QuotaSignal {
	windows := []rateWindow{
		parseWindow(headers, "x-codex-primary"),
		parseWindow(headers, "x-codex-secondary"),
	}
	out := make([]domain.QuotaSignal, 0, 2)
	for _, window := range windows {
		var kind string
		switch window.minutes {
		case 300:
			kind = domain.Window5H
		case 10080:
			kind = domain.Window7D
		default:
			continue
		}
		if window.usedPercent < 0 || window.usedPercent > 100 || window.resetAfter <= 0 {
			continue
		}
		resetAt := observedAt.Truncate(time.Second).Add(time.Duration(window.resetAfter) * time.Second)
		out = append(out, domain.QuotaSignal{
			WindowType:        kind,
			WindowStart:       resetAt.Add(-time.Duration(window.minutes) * time.Minute),
			ResetAt:           resetAt,
			AccountUsedMicros: int64(window.usedPercent * domain.PercentMicros),
		})
	}
	return out
}

func parseWindow(headers http.Header, prefix string) rateWindow {
	used, usedErr := strconv.ParseFloat(strings.TrimSpace(headers.Get(prefix+"-used-percent")), 64)
	reset, resetErr := strconv.ParseInt(strings.TrimSpace(headers.Get(prefix+"-reset-after-seconds")), 10, 64)
	minutes, minutesErr := strconv.ParseInt(strings.TrimSpace(headers.Get(prefix+"-window-minutes")), 10, 64)
	if usedErr != nil || resetErr != nil || minutesErr != nil {
		return rateWindow{minutes: -1}
	}
	return rateWindow{usedPercent: used, resetAfter: reset, minutes: minutes}
}
