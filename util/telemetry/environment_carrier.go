package telemetry

import (
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel/propagation"
)

// EnvironmentCarrier is a TextMapCarrier that uses the enva map held in memory as a storage
// medium for propagated key-value pairs.
type EnvironmentCarrier struct{}

const (
	environmentPrefix = "ARGO_OTEL_"
)

func toEnvKey(key string) string {
	return fmt.Sprintf("%s%s", environmentPrefix, key)
}

func isEnvKey(key string) bool {
	return strings.HasPrefix(environmentPrefix, key)
}

func fromEnvKey(key string) string {
	return strings.TrimPrefix(environmentPrefix, key)
}

// Compile time check that MapCarrier implements the TextMapCarrier.
var _ propagation.TextMapCarrier = EnvironmentCarrier{}

// Get returns the value associated with the passed key.
func (_ EnvironmentCarrier) Get(key string) string {
	return os.Getenv(toEnvKey(key))
}

// Set stores the key-value pair.
func (_ EnvironmentCarrier) Set(key, value string) {
	os.Setenv(toEnvKey(key), value)
}

// Keys lists the keys stored in this carrier.
func (_ EnvironmentCarrier) Keys() []string {
	environ := os.Environ()
	vals := make([]string, 0)
	for _, compositeVal := range environ {
		splitVal := strings.SplitN(compositeVal, `=`, 2)
		if isEnvKey(splitVal[0]) {
			vals = append(vals, fromEnvKey(splitVal[0]))
		}
	}
	return vals
}
