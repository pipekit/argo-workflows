package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

func TestConvertToGRPC(t *testing.T) {
	tests := []struct {
		name               string
		artifact           *wfv1.Artifact
		expectPlugin       bool
		expectedPluginName string
		expectedConfig     string
		expectedKey        string
	}{
		{
			name:         "nil artifact",
			artifact:     nil,
			expectPlugin: false,
		},
		{
			name: "basic artifact",
			artifact: &wfv1.Artifact{
				Name: "test-artifact",
				Path: "/tmp/test",
			},
			expectPlugin: false,
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
			expectPlugin:       true,
			expectedPluginName: "test-plugin",
			expectedConfig:     "config: value",
			expectedKey:        "test-key",
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

			if tt.expectPlugin {
				require.NotNil(t, result.ArtifactLocation)
				require.NotNil(t, result.ArtifactLocation.Plugin)
				assert.Equal(t, tt.expectedPluginName, result.ArtifactLocation.Plugin.Name)
				assert.Equal(t, tt.expectedConfig, result.ArtifactLocation.Plugin.Configuration)
				assert.Equal(t, tt.expectedKey, result.ArtifactLocation.Plugin.Key)
			} else {
				if result.ArtifactLocation != nil {
					assert.Nil(t, result.ArtifactLocation.Plugin)
				}
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
