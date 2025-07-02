package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

func TestConvertToGRPC(t *testing.T) {
	tests := []struct {
		name     string
		artifact *wfv1.Artifact
		expected map[string]string
	}{
		{
			name:     "nil artifact",
			artifact: nil,
			expected: nil,
		},
		{
			name: "basic artifact",
			artifact: &wfv1.Artifact{
				Name: "test-artifact",
				Path: "/tmp/test",
			},
			expected: nil,
		},
		{
			name: "plugin artifact",
			artifact: &wfv1.Artifact{
				Name: "test-artifact",
				Path: "/tmp/test",
				ArtifactLocation: wfv1.ArtifactLocation{
					Plugin: &wfv1.PluginArtifact{
						Name:          "test-plugin",
						Configuration: "config: value",
						Key:           "test-key",
					},
				},
			},
			expected: map[string]string{
				"plugin_name":   "test-plugin",
				"plugin_config": "config: value",
				"plugin_key":    "test-key",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToGRPC(tt.artifact)

			if tt.artifact == nil {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			assert.Equal(t, tt.artifact.Name, result.Name)
			assert.Equal(t, tt.artifact.Path, result.Path)

			if tt.expected != nil {
				assert.Equal(t, tt.expected, result.Options)
			} else {
				assert.Nil(t, result.Options)
			}
		})
	}
}

func TestNewDriver(t *testing.T) {
	// This test would require a mock gRPC server to be running
	// For now, we'll just test that the function returns an error for invalid socket paths
	_, err := NewDriver("test-plugin", "/nonexistent/socket", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to plugin test-plugin")
}
