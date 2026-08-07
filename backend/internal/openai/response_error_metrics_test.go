package openai

import (
	"testing"
	"time"
)

func TestProxyMetricsIncludesTerminalError(t *testing.T) {
	metrics := proxyMetrics(time.Now(), time.Time{}, time.Time{}, terminalResponse{Error: &responseError{
		Code: "server_error", Message: "upstream temporarily unavailable",
	}}, false)
	if metrics.ErrorCode != "server_error" || metrics.ErrorMessage != "upstream temporarily unavailable" || metrics.ErrorStatusCode != 502 {
		t.Fatalf("error metrics = %+v", metrics)
	}
}

func TestParseUpstreamErrorEnvelopeUsesFixedErrorObject(t *testing.T) {
	errorDetail := parseUpstreamErrorEnvelope([]byte(`{"error":{"type":"server_error","code":"upstream_error","message":"connection terminated"}}`))
	if errorDetail == nil || errorDetail.Type != "server_error" || errorDetail.Code != "upstream_error" || errorDetail.Message != "connection terminated" {
		t.Fatalf("error detail = %+v", errorDetail)
	}
}
