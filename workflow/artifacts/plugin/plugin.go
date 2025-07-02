package plugin

import (
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/argoproj/argo-workflows/v3/pkg/apiclient/artifact"
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

const defaultConnectionTimeoutSeconds = int32(5)

// Driver implements the ArtifactDriver interface by making gRPC calls to a plugin service
type Driver struct {
	pluginName wfv1.ArtifactPluginName
	conn       *grpc.ClientConn
	client     artifact.ArtifactServiceClient
}

// NewDriver creates a new plugin artifact driver
func NewDriver(pluginName wfv1.ArtifactPluginName, socketPath string, connectionTimeoutSeconds int32) (*Driver, error) {
	// Connect to the plugin via Unix socket
	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to plugin %s at %s: %w", pluginName, socketPath, err)
	}

	driver := &Driver{
		pluginName: pluginName,
		conn:       conn,
		client:     artifact.NewArtifactServiceClient(conn),
	}

	// Verify the connection by checking the connection state
	if connectionTimeoutSeconds == 0 {
		connectionTimeoutSeconds = defaultConnectionTimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(connectionTimeoutSeconds)*time.Second)
	defer cancel()

	// Wait for the connection to be ready
	if !conn.WaitForStateChange(ctx, connectivity.Idle) {
		return nil, fmt.Errorf("failed to connect to plugin %s at %s: connection timeout", pluginName, socketPath)
	}

	state := conn.GetState()
	if state != connectivity.Ready {
		return nil, fmt.Errorf("failed to connect to plugin %s at %s: connection not ready (state: %s)", pluginName, socketPath, state)
	}

	return driver, nil
}

// Close closes the gRPC connection
func (d *Driver) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

// Load implements ArtifactDriver.Load by calling the plugin service
func (d *Driver) Load(inputArtifact *wfv1.Artifact, path string) error {
	grpcArtifact := convertToGRPC(inputArtifact)
	resp, err := d.client.Load(context.Background(), &artifact.LoadArtifactRequest{
		InputArtifact: grpcArtifact,
		Path:          path,
	})
	if err != nil {
		return fmt.Errorf("plugin %s load failed: %w", d.pluginName, err)
	}
	if !resp.Success {
		return fmt.Errorf("plugin %s load failed: %s", d.pluginName, resp.Error)
	}
	return nil
}

// OpenStream implements ArtifactDriver.OpenStream by calling the plugin service
func (d *Driver) OpenStream(a *wfv1.Artifact) (io.ReadCloser, error) {
	grpcArtifact := convertToGRPC(a)
	stream, err := d.client.OpenStream(context.Background(), &artifact.OpenStreamRequest{
		Artifact: grpcArtifact,
	})
	if err != nil {
		return nil, fmt.Errorf("plugin %s open stream failed: %w", d.pluginName, err)
	}

	reader, writer := io.Pipe()

	go func() {
		defer writer.Close()
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				writer.CloseWithError(fmt.Errorf("plugin %s stream receive failed: %w", d.pluginName, err))
				return
			}
			if resp.Error != "" {
				writer.CloseWithError(fmt.Errorf("plugin %s stream error: %s", d.pluginName, resp.Error))
				return
			}
			if resp.IsEnd {
				break
			}
			if len(resp.Data) > 0 {
				if _, writeErr := writer.Write(resp.Data); writeErr != nil {
					writer.CloseWithError(fmt.Errorf("plugin %s stream write failed: %w", d.pluginName, writeErr))
					return
				}
			}
		}
	}()

	return reader, nil
}

// Save implements ArtifactDriver.Save by calling the plugin service
func (d *Driver) Save(path string, outputArtifact *wfv1.Artifact) error {
	grpcArtifact := convertToGRPC(outputArtifact)
	resp, err := d.client.Save(context.Background(), &artifact.SaveArtifactRequest{
		Path:           path,
		OutputArtifact: grpcArtifact,
	})
	if err != nil {
		return fmt.Errorf("plugin %s save failed: %w", d.pluginName, err)
	}
	if !resp.Success {
		return fmt.Errorf("plugin %s save failed: %s", d.pluginName, resp.Error)
	}
	return nil
}

// Delete implements ArtifactDriver.Delete by calling the plugin service
func (d *Driver) Delete(artifactRef *wfv1.Artifact) error {
	grpcArtifact := convertToGRPC(artifactRef)
	resp, err := d.client.Delete(context.Background(), &artifact.DeleteArtifactRequest{
		Artifact: grpcArtifact,
	})
	if err != nil {
		return fmt.Errorf("plugin %s delete failed: %w", d.pluginName, err)
	}
	if !resp.Success {
		return fmt.Errorf("plugin %s delete failed: %s", d.pluginName, resp.Error)
	}
	return nil
}

// ListObjects implements ArtifactDriver.ListObjects by calling the plugin service
func (d *Driver) ListObjects(artifactRef *wfv1.Artifact) ([]string, error) {
	grpcArtifact := convertToGRPC(artifactRef)
	resp, err := d.client.ListObjects(context.Background(), &artifact.ListObjectsRequest{
		Artifact: grpcArtifact,
	})
	if err != nil {
		return nil, fmt.Errorf("plugin %s list objects failed: %w", d.pluginName, err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("plugin %s list objects failed: %s", d.pluginName, resp.Error)
	}
	return resp.Objects, nil
}

// IsDirectory implements ArtifactDriver.IsDirectory by calling the plugin service
func (d *Driver) IsDirectory(artifactRef *wfv1.Artifact) (bool, error) {
	grpcArtifact := convertToGRPC(artifactRef)
	resp, err := d.client.IsDirectory(context.Background(), &artifact.IsDirectoryRequest{
		Artifact: grpcArtifact,
	})
	if err != nil {
		return false, fmt.Errorf("plugin %s is directory check failed: %w", d.pluginName, err)
	}
	if resp.Error != "" {
		return false, fmt.Errorf("plugin %s is directory check failed: %s", d.pluginName, resp.Error)
	}
	return resp.IsDirectory, nil
}

// convertToGRPC converts v1alpha1.Artifact to gRPC Artifact
func convertToGRPC(a *wfv1.Artifact) *artifact.Artifact {
	if a == nil {
		return nil
	}

	grpcArtifact := &artifact.Artifact{
		Name: a.Name,
		Path: a.Path,
	}

	// Convert plugin-specific configuration to options
	if a.Plugin != nil {
		grpcArtifact.Options = map[string]string{
			"plugin_name":   string(a.Plugin.Name),
			"plugin_config": a.Plugin.Configuration,
			"plugin_key":    a.Plugin.Key,
		}
	}

	return grpcArtifact
}
