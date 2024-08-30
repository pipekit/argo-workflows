package telemetry

import (
	"time"

	metricsdk "go.opentelemetry.io/otel/sdk/metric"
)

type Config struct {
	Enabled      bool
	Path         string
	Port         int
	TTL          time.Duration
	IgnoreErrors bool
	Secure       bool
	Modifiers    map[string]Modifier
	Temporality  metricsdk.TemporalitySelector
}
