package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound               = errors.New("resource not found")
	ErrUnauthorized           = errors.New("authentication required")
	ErrForbidden              = errors.New("operation forbidden")
	ErrPasswordChangeRequired = errors.New("password change required")
	ErrConflict               = errors.New("resource conflict")
	ErrInvalidInput           = errors.New("invalid input")
	ErrShareExceeded          = errors.New("allocated shares exceed 100 percent")
	ErrQuotaExhausted         = errors.New("member quota exhausted")
	ErrAccountUnavailable     = errors.New("OpenAI account unavailable")
	ErrAccountTokenRefresh    = errors.New("OpenAI account token refresh failed")
	ErrNoRouteAvailable       = errors.New("no configured Plan has available quota")
	ErrPublicPlanFull         = errors.New("public Plan has no available seats")
	ErrAccountConcurrency     = errors.New("OpenAI account concurrency limit reached")
	ErrAccountRateLimited     = errors.New("OpenAI account RPM limit reached")
	ErrAccountAlreadyBound    = fmt.Errorf("OpenAI account is already bound to another Plan: %w", ErrConflict)
)
