package v1alpha1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-workflows/v3/util/logging"
)

func TestArtifactPluginName(t *testing.T) {
	pluginName := ArtifactPluginName("my-plugin")

	t.Run("SocketDir", func(t *testing.T) {
		expected := "/artifact-plugins/my-plugin"
		assert.Equal(t, expected, pluginName.SocketDir())
	})

	t.Run("SocketPath", func(t *testing.T) {
		expected := "/artifact-plugins/my-plugin/socket"
		assert.Equal(t, expected, pluginName.SocketPath())
	})

	t.Run("VolumeMount", func(t *testing.T) {
		volumeMount := pluginName.VolumeMount()
		expected := apiv1.VolumeMount{
			Name:      "artifact-plugin-my-plugin",
			MountPath: "/artifact-plugins/my-plugin",
		}
		assert.Equal(t, expected, volumeMount)
	})

	t.Run("Volume", func(t *testing.T) {
		volume := pluginName.Volume()
		expected := apiv1.Volume{
			Name: "artifact-plugin-my-plugin",
			VolumeSource: apiv1.VolumeSource{
				EmptyDir: &apiv1.EmptyDirVolumeSource{},
			},
		}
		assert.Equal(t, expected, volume)
	})

	t.Run("EmptyPluginName", func(t *testing.T) {
		emptyPlugin := ArtifactPluginName("")
		assert.Equal(t, "/artifact-plugins/", emptyPlugin.SocketDir())
		assert.Equal(t, "/artifact-plugins//socket", emptyPlugin.SocketPath())
	})

	t.Run("PluginNameWithSpecialChars", func(t *testing.T) {
		specialPlugin := ArtifactPluginName("my-plugin-v1.2.3")
		assert.Equal(t, "/artifact-plugins/my-plugin-v1.2.3", specialPlugin.SocketDir())
		assert.Equal(t, "artifact-plugin-my-plugin-v1.2.3", specialPlugin.Volume().Name)
	})
}

func TestPluginArtifact(t *testing.T) {
	t.Run("GetKey", func(t *testing.T) {
		plugin := &PluginArtifact{
			Name:          "test-plugin",
			Configuration: `{"bucket": "my-bucket"}`,
			Key:           "path/to/artifact",
		}
		key, err := plugin.GetKey()
		assert.NoError(t, err)
		assert.Equal(t, "path/to/artifact", key)
	})

	t.Run("SetKey", func(t *testing.T) {
		plugin := &PluginArtifact{
			Name:          "test-plugin",
			Configuration: `{"bucket": "my-bucket"}`,
			Key:           "old/path",
		}
		err := plugin.SetKey("new/path/to/artifact")
		assert.NoError(t, err)
		assert.Equal(t, "new/path/to/artifact", plugin.Key)
	})

	t.Run("HasLocation_Complete", func(t *testing.T) {
		plugin := &PluginArtifact{
			Name:          "test-plugin",
			Configuration: `{"bucket": "my-bucket"}`,
			Key:           "path/to/artifact",
		}
		assert.True(t, plugin.HasLocation())
	})

	t.Run("HasLocation_MissingName", func(t *testing.T) {
		plugin := &PluginArtifact{
			Name:          "",
			Configuration: `{"bucket": "my-bucket"}`,
			Key:           "path/to/artifact",
		}
		assert.False(t, plugin.HasLocation())
	})

	t.Run("HasLocation_MissingConfiguration", func(t *testing.T) {
		plugin := &PluginArtifact{
			Name:          "test-plugin",
			Configuration: "",
			Key:           "path/to/artifact",
		}
		assert.False(t, plugin.HasLocation())
	})

	t.Run("HasLocation_MissingKey", func(t *testing.T) {
		plugin := &PluginArtifact{
			Name:          "test-plugin",
			Configuration: `{"bucket": "my-bucket"}`,
			Key:           "",
		}
		assert.False(t, plugin.HasLocation())
	})

	t.Run("HasLocation_Nil", func(t *testing.T) {
		var plugin *PluginArtifact
		assert.False(t, plugin.HasLocation())
	})

	t.Run("ConnectionTimeoutSeconds", func(t *testing.T) {
		plugin := &PluginArtifact{
			Name:                     "test-plugin",
			Configuration:            `{"bucket": "my-bucket"}`,
			Key:                      "path/to/artifact",
			ConnectionTimeoutSeconds: 30,
		}
		assert.Equal(t, int32(30), plugin.ConnectionTimeoutSeconds)
		assert.True(t, plugin.HasLocation())
	})
}

func TestPluginArtifactRepository(t *testing.T) {
	t.Run("IntoArtifactLocation_WithKeyFormat", func(t *testing.T) {
		repo := &PluginArtifactRepository{
			Name:          "my-plugin",
			KeyFormat:     "custom/{{workflow.name}}/{{pod.name}}/{{artifact.name}}",
			Configuration: `{"endpoint": "https://my-storage.com"}`,
		}

		location := &ArtifactLocation{}
		repo.IntoArtifactLocation(location)

		require.NotNil(t, location.Plugin)
		assert.Equal(t, ArtifactPluginName("my-plugin"), location.Plugin.Name)
		assert.Equal(t, `{"endpoint": "https://my-storage.com"}`, location.Plugin.Configuration)
		assert.Equal(t, "custom/{{workflow.name}}/{{pod.name}}/{{artifact.name}}", location.Plugin.Key)
	})

	t.Run("IntoArtifactLocation_WithoutKeyFormat", func(t *testing.T) {
		repo := &PluginArtifactRepository{
			Name:          "my-plugin",
			Configuration: `{"endpoint": "https://my-storage.com"}`,
		}

		location := &ArtifactLocation{}
		repo.IntoArtifactLocation(location)

		require.NotNil(t, location.Plugin)
		assert.Equal(t, ArtifactPluginName("my-plugin"), location.Plugin.Name)
		assert.Equal(t, `{"endpoint": "https://my-storage.com"}`, location.Plugin.Configuration)
		assert.Equal(t, DefaultArchivePattern, location.Plugin.Key)
	})

	t.Run("IntoArtifactLocation_EmptyKeyFormat", func(t *testing.T) {
		repo := &PluginArtifactRepository{
			Name:          "my-plugin",
			KeyFormat:     "",
			Configuration: `{"endpoint": "https://my-storage.com"}`,
		}

		location := &ArtifactLocation{}
		repo.IntoArtifactLocation(location)

		require.NotNil(t, location.Plugin)
		assert.Equal(t, DefaultArchivePattern, location.Plugin.Key)
	})
}

func TestArtifactLocation_Plugin(t *testing.T) {
	t.Run("Get_Plugin", func(t *testing.T) {
		location := &ArtifactLocation{
			Plugin: &PluginArtifact{
				Name:          "test-plugin",
				Configuration: `{"bucket": "my-bucket"}`,
				Key:           "path/to/artifact",
			},
		}

		artifact, err := location.Get()
		assert.NoError(t, err)
		assert.IsType(t, &PluginArtifact{}, artifact)

		pluginArtifact := artifact.(*PluginArtifact)
		assert.Equal(t, ArtifactPluginName("test-plugin"), pluginArtifact.Name)
		assert.Equal(t, `{"bucket": "my-bucket"}`, pluginArtifact.Configuration)
		assert.Equal(t, "path/to/artifact", pluginArtifact.Key)
	})

	t.Run("SetType_Plugin", func(t *testing.T) {
		location := &ArtifactLocation{}
		pluginArtifact := &PluginArtifact{
			Name:          "test-plugin",
			Configuration: `{"bucket": "my-bucket"}`,
			Key:           "path/to/artifact",
		}

		err := location.SetType(pluginArtifact)
		assert.NoError(t, err)
		assert.NotNil(t, location.Plugin)
		// Note: SetType creates a new empty instance, not copying the values
		assert.Equal(t, ArtifactPluginName(""), location.Plugin.Name)
	})

	t.Run("HasLocation_Plugin", func(t *testing.T) {
		location := &ArtifactLocation{
			Plugin: &PluginArtifact{
				Name:          "test-plugin",
				Configuration: `{"bucket": "my-bucket"}`,
				Key:           "path/to/artifact",
			},
		}
		assert.True(t, location.HasLocation())
	})

	t.Run("HasLocation_PluginIncomplete", func(t *testing.T) {
		location := &ArtifactLocation{
			Plugin: &PluginArtifact{
				Name:          "test-plugin",
				Configuration: "",
				Key:           "path/to/artifact",
			},
		}
		assert.False(t, location.HasLocation())
	})

	t.Run("GetKey_Plugin", func(t *testing.T) {
		location := &ArtifactLocation{
			Plugin: &PluginArtifact{
				Name:          "test-plugin",
				Configuration: `{"bucket": "my-bucket"}`,
				Key:           "path/to/artifact",
			},
		}

		key, err := location.GetKey()
		assert.NoError(t, err)
		assert.Equal(t, "path/to/artifact", key)
	})

	t.Run("SetKey_Plugin", func(t *testing.T) {
		location := &ArtifactLocation{
			Plugin: &PluginArtifact{
				Name:          "test-plugin",
				Configuration: `{"bucket": "my-bucket"}`,
				Key:           "old/path",
			},
		}

		err := location.SetKey("new/path/to/artifact")
		assert.NoError(t, err)
		assert.Equal(t, "new/path/to/artifact", location.Plugin.Key)
	})
}

func TestArtifacts_GetPluginNames(t *testing.T) {
	ctx := logging.WithLogger(context.Background(), logging.NewTestLogger(logging.Info, logging.JSON))

	t.Run("NoPlugins", func(t *testing.T) {
		artifacts := Artifacts{
			{
				Name: "regular-artifact",
				ArtifactLocation: ArtifactLocation{
					S3: &S3Artifact{
						S3Bucket: S3Bucket{Bucket: "my-bucket"},
						Key:      "path/to/artifact",
					},
				},
			},
		}

		pluginNames := artifacts.GetPluginNames(ctx, nil, ExcludeLogs)
		assert.Empty(t, pluginNames)
	})

	t.Run("SinglePlugin", func(t *testing.T) {
		artifacts := Artifacts{
			{
				Name: "plugin-artifact",
				ArtifactLocation: ArtifactLocation{
					Plugin: &PluginArtifact{
						Name:          "my-plugin",
						Configuration: `{"bucket": "my-bucket"}`,
						Key:           "path/to/artifact",
					},
				},
			},
		}

		pluginNames := artifacts.GetPluginNames(ctx, nil, ExcludeLogs)
		assert.Len(t, pluginNames, 1)
		assert.Contains(t, pluginNames, ArtifactPluginName("my-plugin"))
	})

	t.Run("MultiplePlugins", func(t *testing.T) {
		artifacts := Artifacts{
			{
				Name: "plugin-artifact-1",
				ArtifactLocation: ArtifactLocation{
					Plugin: &PluginArtifact{
						Name:          "plugin-1",
						Configuration: `{"bucket": "bucket-1"}`,
						Key:           "path/to/artifact1",
					},
				},
			},
			{
				Name: "plugin-artifact-2",
				ArtifactLocation: ArtifactLocation{
					Plugin: &PluginArtifact{
						Name:          "plugin-2",
						Configuration: `{"bucket": "bucket-2"}`,
						Key:           "path/to/artifact2",
					},
				},
			},
		}

		pluginNames := artifacts.GetPluginNames(ctx, nil, ExcludeLogs)
		assert.Len(t, pluginNames, 2)
		assert.Contains(t, pluginNames, ArtifactPluginName("plugin-1"))
		assert.Contains(t, pluginNames, ArtifactPluginName("plugin-2"))
	})

	t.Run("DuplicatePlugins", func(t *testing.T) {
		artifacts := Artifacts{
			{
				Name: "plugin-artifact-1",
				ArtifactLocation: ArtifactLocation{
					Plugin: &PluginArtifact{
						Name:          "my-plugin",
						Configuration: `{"bucket": "bucket-1"}`,
						Key:           "path/to/artifact1",
					},
				},
			},
			{
				Name: "plugin-artifact-2",
				ArtifactLocation: ArtifactLocation{
					Plugin: &PluginArtifact{
						Name:          "my-plugin",
						Configuration: `{"bucket": "bucket-2"}`,
						Key:           "path/to/artifact2",
					},
				},
			},
		}

		pluginNames := artifacts.GetPluginNames(ctx, nil, ExcludeLogs)
		assert.Len(t, pluginNames, 1)
		assert.Contains(t, pluginNames, ArtifactPluginName("my-plugin"))
	})

	t.Run("WithDefaultRepo", func(t *testing.T) {
		artifacts := Artifacts{
			{
				Name:             "artifact-without-plugin",
				ArtifactLocation: ArtifactLocation{
					// No plugin specified, should use default repo
				},
			},
		}

		defaultRepo := &ArtifactRepository{
			Plugin: &PluginArtifactRepository{
				Name:          "default-plugin",
				Configuration: `{"bucket": "default-bucket"}`,
			},
		}

		pluginNames := artifacts.GetPluginNames(ctx, defaultRepo, ExcludeLogs)
		assert.Len(t, pluginNames, 1)
		assert.Contains(t, pluginNames, ArtifactPluginName("default-plugin"))
	})

	t.Run("IncludeLogs", func(t *testing.T) {
		artifacts := Artifacts{
			{
				Name: "regular-artifact",
				ArtifactLocation: ArtifactLocation{
					S3: &S3Artifact{
						S3Bucket: S3Bucket{Bucket: "my-bucket"},
						Key:      "path/to/artifact",
					},
				},
			},
		}

		defaultRepo := &ArtifactRepository{
			Plugin: &PluginArtifactRepository{
				Name:          "log-plugin",
				Configuration: `{"bucket": "log-bucket"}`,
			},
			ArchiveLogs: ptr.To(true),
		}

		pluginNames := artifacts.GetPluginNames(ctx, defaultRepo, IncludeLogs)
		assert.Len(t, pluginNames, 1)
		assert.Contains(t, pluginNames, ArtifactPluginName("log-plugin"))
	})

	t.Run("ExcludeLogs", func(t *testing.T) {
		artifacts := Artifacts{
			{
				Name: "regular-artifact",
				ArtifactLocation: ArtifactLocation{
					S3: &S3Artifact{
						S3Bucket: S3Bucket{Bucket: "my-bucket"},
						Key:      "path/to/artifact",
					},
				},
			},
		}

		defaultRepo := &ArtifactRepository{
			S3: &S3ArtifactRepository{
				S3Bucket: S3Bucket{Bucket: "log-bucket"},
			},
			ArchiveLogs: ptr.To(true),
		}

		pluginNames := artifacts.GetPluginNames(ctx, defaultRepo, ExcludeLogs)
		// When ExcludeLogs is used and there are no plugin artifacts, should be empty
		assert.Empty(t, pluginNames)
	})

	t.Run("MixedArtifacts", func(t *testing.T) {
		artifacts := Artifacts{
			{
				Name: "s3-artifact",
				ArtifactLocation: ArtifactLocation{
					S3: &S3Artifact{
						S3Bucket: S3Bucket{Bucket: "s3-bucket"},
						Key:      "path/to/s3-artifact",
					},
				},
			},
			{
				Name: "plugin-artifact",
				ArtifactLocation: ArtifactLocation{
					Plugin: &PluginArtifact{
						Name:          "my-plugin",
						Configuration: `{"bucket": "plugin-bucket"}`,
						Key:           "path/to/plugin-artifact",
					},
				},
			},
			{
				Name:             "default-artifact",
				ArtifactLocation: ArtifactLocation{
					// No specific location, should use default
				},
			},
		}

		defaultRepo := &ArtifactRepository{
			Plugin: &PluginArtifactRepository{
				Name:          "default-plugin",
				Configuration: `{"bucket": "default-bucket"}`,
			},
		}

		pluginNames := artifacts.GetPluginNames(ctx, defaultRepo, ExcludeLogs)
		assert.Len(t, pluginNames, 2)
		assert.Contains(t, pluginNames, ArtifactPluginName("my-plugin"))
		assert.Contains(t, pluginNames, ArtifactPluginName("default-plugin"))
	})
}

func TestTemplate_PluginType(t *testing.T) {
	t.Run("GetType_Plugin", func(t *testing.T) {
		tmpl := &Template{
			Plugin: &Plugin{
				Object: Object{
					Value: []byte(`{"my-plugin": {"config": "value"}}`),
				},
			},
		}
		assert.Equal(t, TemplateTypePlugin, tmpl.GetType())
	})

	t.Run("GetNodeType_Plugin", func(t *testing.T) {
		tmpl := &Template{
			Plugin: &Plugin{
				Object: Object{
					Value: []byte(`{"my-plugin": {"config": "value"}}`),
				},
			},
		}
		assert.Equal(t, NodeTypePlugin, tmpl.GetNodeType())
	})

	t.Run("IsLeaf_Plugin", func(t *testing.T) {
		tmpl := &Template{
			Plugin: &Plugin{
				Object: Object{
					Value: []byte(`{"my-plugin": {"config": "value"}}`),
				},
			},
		}
		assert.True(t, tmpl.IsLeaf())
	})

	t.Run("IsPodType_Plugin", func(t *testing.T) {
		tmpl := &Template{
			Plugin: &Plugin{
				Object: Object{
					Value: []byte(`{"my-plugin": {"config": "value"}}`),
				},
			},
		}
		assert.False(t, tmpl.IsPodType())
	})

	t.Run("HasOutput_Plugin", func(t *testing.T) {
		tmpl := &Template{
			Plugin: &Plugin{
				Object: Object{
					Value: []byte(`{"my-plugin": {"config": "value"}}`),
				},
			},
		}
		assert.True(t, tmpl.HasOutput())
	})
}

func TestNodeStatus_PluginType(t *testing.T) {
	t.Run("IsTaskSetNode_Plugin", func(t *testing.T) {
		node := &NodeStatus{
			Type: NodeTypePlugin,
		}
		assert.True(t, node.IsTaskSetNode())
	})

	t.Run("IsTaskSetNode_HTTP", func(t *testing.T) {
		node := &NodeStatus{
			Type: NodeTypeHTTP,
		}
		assert.True(t, node.IsTaskSetNode())
	})

	t.Run("IsTaskSetNode_Pod", func(t *testing.T) {
		node := &NodeStatus{
			Type: NodeTypePod,
		}
		assert.False(t, node.IsTaskSetNode())
	})
}

func TestArtifactRepository_Plugin_Integration(t *testing.T) {
	t.Run("Get_Plugin", func(t *testing.T) {
		repo := &ArtifactRepository{
			Plugin: &PluginArtifactRepository{
				Name:          "test-plugin",
				KeyFormat:     "custom/{{workflow.name}}/{{pod.name}}",
				Configuration: `{"endpoint": "https://my-storage.com"}`,
			},
		}

		repoType := repo.Get()
		assert.IsType(t, &PluginArtifactRepository{}, repoType)

		pluginRepo := repoType.(*PluginArtifactRepository)
		assert.Equal(t, ArtifactPluginName("test-plugin"), pluginRepo.Name)
		assert.Equal(t, "custom/{{workflow.name}}/{{pod.name}}", pluginRepo.KeyFormat)
		assert.Equal(t, `{"endpoint": "https://my-storage.com"}`, pluginRepo.Configuration)
	})

	t.Run("ToArtifactLocation_Plugin", func(t *testing.T) {
		repo := &ArtifactRepository{
			Plugin: &PluginArtifactRepository{
				Name:          "test-plugin",
				KeyFormat:     "custom/{{workflow.name}}/{{pod.name}}",
				Configuration: `{"endpoint": "https://my-storage.com"}`,
			},
			ArchiveLogs: ptr.To(true),
		}

		location := repo.ToArtifactLocation()
		require.NotNil(t, location)
		assert.Equal(t, ptr.To(true), location.ArchiveLogs)
		require.NotNil(t, location.Plugin)
		assert.Equal(t, ArtifactPluginName("test-plugin"), location.Plugin.Name)
		assert.Equal(t, "custom/{{workflow.name}}/{{pod.name}}", location.Plugin.Key)
		assert.Equal(t, `{"endpoint": "https://my-storage.com"}`, location.Plugin.Configuration)
	})
}
