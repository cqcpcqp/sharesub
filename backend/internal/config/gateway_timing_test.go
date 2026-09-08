package config

import (
	"testing"
	"time"
)

func TestGatewayTimingConfig(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("SHARESUB_GATEWAY_TIMING_ENABLED", "")
	t.Setenv("SHARESUB_GATEWAY_TIMING_SLOW_THRESHOLD", "")
	t.Setenv("SHARESUB_GATEWAY_TIMING_SAMPLE_EVERY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GatewayTiming != (GatewayTiming{Enabled: true, SlowThreshold: 30 * time.Second, SampleEvery: 100}) {
		t.Fatalf("defaults = %+v", cfg.GatewayTiming)
	}
	t.Setenv("SHARESUB_GATEWAY_TIMING_ENABLED", "true")
	t.Setenv("SHARESUB_GATEWAY_TIMING_SLOW_THRESHOLD", "5s")
	t.Setenv("SHARESUB_GATEWAY_TIMING_SAMPLE_EVERY", "0")
	cfg, err = Load()
	if err != nil || cfg.GatewayTiming != (GatewayTiming{Enabled: true, SlowThreshold: 5 * time.Second}) {
		t.Fatalf("overrides = %+v, err = %v", cfg.GatewayTiming, err)
	}
	t.Setenv("SHARESUB_GATEWAY_TIMING_ENABLED", "false")
	cfg, err = Load()
	if err != nil || cfg.GatewayTiming.Enabled {
		t.Fatalf("explicit disable = %+v, err = %v", cfg.GatewayTiming, err)
	}
}

func TestGatewayTimingInvalidConfig(t *testing.T) {
	for _, test := range []struct{ name, value string }{
		{"ENABLED", "maybe"}, {"SLOW_THRESHOLD", "0s"}, {"SLOW_THRESHOLD", "-1s"},
		{"SLOW_THRESHOLD", "bad"}, {"SAMPLE_EVERY", "-1"}, {"SAMPLE_EVERY", "0.1"},
		{"SAMPLE_EVERY", "9999999999999999999999999999"},
	} {
		t.Run(test.name+test.value, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("SHARESUB_GATEWAY_TIMING_"+test.name, test.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
