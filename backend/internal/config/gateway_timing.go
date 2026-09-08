package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type GatewayTiming struct {
	Enabled       bool
	SlowThreshold time.Duration
	SampleEvery   int
}

func loadGatewayTiming() (GatewayTiming, error) {
	enabled, err := boolEnv("SHARESUB_GATEWAY_TIMING_ENABLED", false)
	if err != nil {
		return GatewayTiming{}, err
	}
	slow, err := positiveDurationEnv("SHARESUB_GATEWAY_TIMING_SLOW_THRESHOLD", 30*time.Second)
	if err != nil {
		return GatewayTiming{}, err
	}
	sampleEvery := 100
	if raw := os.Getenv("SHARESUB_GATEWAY_TIMING_SAMPLE_EVERY"); raw != "" {
		sampleEvery, err = strconv.Atoi(raw)
		if err != nil || sampleEvery < 0 {
			return GatewayTiming{}, fmt.Errorf("SHARESUB_GATEWAY_TIMING_SAMPLE_EVERY must be a non-negative integer")
		}
	}
	return GatewayTiming{Enabled: enabled, SlowThreshold: slow, SampleEvery: sampleEvery}, nil
}
