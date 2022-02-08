package multicluster

import (
	"os"
)

// IsEnabled is a flag for whether the new multi-cluster feature is
// enabled
func IsEnabled() bool {
	return os.Getenv("ARGO_MULTICLUSTER_ENABLED") == "true"
}
