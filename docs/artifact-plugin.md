# Artifact Driver/Plugin Development

This document provides guidance for developers who want to create custom artifact drivers/plugins for Argo Workflows.

## Overview

Artifact drivers/plugins allow you to extend Argo Workflows with alternative storage solutions or artifact handling logic.
By implementing a plugin, you can integrate with proprietary storage systems, add custom processing logic, or support specialized artifact formats.

## High-Level Requirements

To create an artifact plugin, you need to:

### 1. Create and Distribute a Docker Image

Your plugin must be packaged as a Docker image that contains:
- Your plugin implementation
- All necessary dependencies and runtime requirements
- The GRPC server that implements the artifact interface

The Docker image will be deployed alongside workflow pods as sidecars and init containers.

### 2. Implement a GRPC Server

Your plugin's entrypoint must run a GRPC server that:
- Listens on the socket path provided as the first and only command-line parameter
- Implements the artifact service interface
- Handles artifact operations (load, save, delete, etc.)

The GRPC interface is defined in **[`artifact.proto`](https://github.com/argoproj/argo-workflows/blob/main/pkg/apiclient/artifact/artifact.proto)**.
This contains the main `ArtifactService` interface and all request/response message types your plugin must implement.

### 3. Language Flexibility

You can implement your plugin in any programming language that supports GRPC, including:
- Go
- Python
- Java
- Rust
- Node.JS
- C#
- [and others](https://grpc.io/docs/languages/)

Choose the language that best fits your team's expertise and your storage backend's SDK requirements.

## Implementation Steps

Follow these steps to implement an artifact plugin in your chosen language:

### 1. Download Protocol Definition

Download the [`artifact.proto`](https://github.com/argoproj/argo-workflows/blob/main/pkg/apiclient/artifact/artifact.proto) file to your project.
You can add this to your build process (such as a Makefile) to automatically fetch the latest version:

```makefile
# Example Makefile target
artifact.proto:
	curl -o artifact.proto https://raw.githubusercontent.com/argoproj/argo-workflows/main/pkg/apiclient/artifact/artifact.proto
```

### 2. Generate GRPC Server Code

Use your language's GRPC tooling to generate server stubs from the protocol definition:

#### Go
```bash
# Install protoc-gen-go and protoc-gen-go-grpc
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate Go code
protoc --go_out=. --go-grpc_out=. artifact.proto
```

#### Python
```bash
# Install grpcio-tools
pip install grpcio-tools

# Generate Python code
python -m grpc_tools.protoc --python_out=. --grpc_python_out=. artifact.proto
```

#### Java
```bash
# Using protoc with Java plugin
protoc --java_out=src/main/java --grpc-java_out=src/main/java artifact.proto
```

#### Rust
```rust
// Add to Cargo.toml
[build-dependencies]
tonic-build = "0.10"

// In build.rs
fn main() {
    tonic_build::compile_protos("artifact.proto").unwrap();
}
```

#### Node.JS
```bash
# Install grpc-tools
npm install grpc-tools

# Generate JavaScript code
grpc_tools_node_protoc --js_out=import_style=commonjs:. --grpc_out=grpc_js:. artifact.proto
```

#### C#
```bash
# Install Grpc.Tools package, then use protoc
protoc --csharp_out=. --grpc_out=. --plugin=protoc-gen-grpc=grpc_csharp_plugin artifact.proto
```

### 3. Implement Required Methods

Your GRPC server must implement these six methods from the `ArtifactService` interface:

#### Required Methods

1. **`Load(LoadArtifactRequest)` → `LoadArtifactResponse`**
   - Downloads an artifact from your storage system to the specified local path
   - Called during workflow execution to retrieve input artifacts

2. **`Save(SaveArtifactRequest)` → `SaveArtifactResponse`**  
   - Uploads an artifact from a local path to your storage system
   - Called to store output artifacts after step completion

3. **`Delete(DeleteArtifactRequest)` → `DeleteArtifactResponse`**
   - Removes an artifact from your storage system
   - Used for artifact garbage collection

4. **`ListObjects(ListObjectsRequest)` → `ListObjectsResponse`**
   - Lists objects/files within an artifact location
   - Used by the UI and CLI for artifact browsing

5. **`IsDirectory(IsDirectoryRequest)` → `IsDirectoryResponse`**
   - Determines if an artifact location represents a directory or file
   - Used to handle directory artifacts appropriately

6. **`OpenStream(OpenStreamRequest)` → `stream OpenStreamResponse`**
   - Streams artifact content directly to clients
   - Used for efficient artifact downloads in the UI
   - Could be implemented as Load() and then streaming the data from the local file, which is what some built-in drivers do, but this is not recommended as it is not efficient.

#### Implementation Notes

- Parse the plugin configuration from `artifact.configuration` field in each request
- Use the `artifact.key` field to identify the specific artifact location in your storage
- Handle errors gracefully and return appropriate error messages
- Support both file and directory artifacts where applicable
- Consider implementing timeouts and retry logic for storage operations

### 4. Build and Package

Package your plugin as a Docker image with your GRPC server as the entrypoint.
The server should accept a single command-line argument specifying the Unix socket path to listen on.

### 5. Local Testing

For faster development iteration, test your plugin locally using a simple GRPC client:

#### Start Your Plugin Server

Run your plugin binary directly, providing a Unix socket path:

```bash
# Start your plugin server listening on a Unix socket
./your-plugin-binary /tmp/plugin.sock
```

or in a container, using the socket path as the command and exposing the socket path as a volume:
```bash
docker run -v /tmp/plugin.sock:/tmp/plugin.sock your-plugin-image /tmp/plugin.sock
```

#### Possibly test with `grpcurl`

Use [`grpcurl`](https://github.com/fullstorydev/grpcurl) for quick testing without writing client code.
Your server would need to be running and listening on the socket path for this to work.
Your server must also have [reflection enabled](https://grpc.io/docs/guides/reflection/), which is not the default.
Although `grpcurl` is written in Go, it can be used to test plugins written in any language.

```bash
# Install grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# Test Load method (requires reflection enabled in your server)
grpcurl -plaintext -unix /tmp/plugin.sock \
  -d '{
    "input_artifact": {
      "name": "test-artifact",
      "artifact_location": {
        "plugin": {
          "name": "my-plugin",
          "configuration": "{\"bucket\": \"test-bucket\"}",
          "key": "test/file.txt"
        }
      }
    },
    "path": "/tmp/test-download.txt"
  }' \
  artifact.ArtifactService/Load
```

#### Create Test Clients

Create simple GRPC clients to test each method:

##### Go Test Client Example
```go
package main

import (
    "context"
    "log"
    "net"
    "google.golang.org/grpc"
    pb "path/to/your/generated/artifact"
)

func main() {
    // Connect to Unix socket
    conn, err := grpc.Dial("unix:///tmp/plugin.sock", grpc.WithInsecure())
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()
    
    client := pb.NewArtifactServiceClient(conn)
    
    // Test Load method
    loadReq := &pb.LoadArtifactRequest{
        InputArtifact: &pb.Artifact{
            Name: "test-artifact",
            ArtifactLocation: &pb.ArtifactLocation{
                Plugin: &pb.PluginArtifact{
                    Name: "my-plugin",
                    Configuration: `{"bucket": "test-bucket", "endpoint": "localhost:9000"}`,
                    Key: "test/file.txt",
                },
            },
        },
        Path: "/tmp/downloaded-file.txt",
    }
    
    loadResp, err := client.Load(context.Background(), loadReq)
    if err != nil {
        log.Printf("Load failed: %v", err)
    } else {
        log.Printf("Load success: %v", loadResp.Success)
    }
    
    // Test other methods similarly...
}
```

##### Python Test Client Example
```python
import grpc
import artifact_pb2
import artifact_pb2_grpc

def test_plugin():
    # Connect to Unix socket
    channel = grpc.insecure_channel('unix:///tmp/plugin.sock')
    stub = artifact_pb2_grpc.ArtifactServiceStub(channel)
    
    # Test Load method
    artifact = artifact_pb2.Artifact(
        name="test-artifact",
        artifact_location=artifact_pb2.ArtifactLocation(
            plugin=artifact_pb2.PluginArtifact(
                name="my-plugin",
                configuration='{"bucket": "test-bucket", "endpoint": "localhost:9000"}',
                key="test/file.txt"
            )
        )
    )
    
    request = artifact_pb2.LoadArtifactRequest(
        input_artifact=artifact,
        path="/tmp/downloaded-file.txt"
    )
    
    try:
        response = stub.Load(request)
        print(f"Load success: {response.success}")
    except grpc.RpcError as e:
        print(f"Load failed: {e}")

if __name__ == "__main__":
    test_plugin()
```

### 6. Integration Testing and Deployment

Once local testing passes, test with the full workflow controller:

1. Build and push your Docker image
2. Configure it in the workflow controller ConfigMap
3. Create workflows that use your plugin for artifacts
4. Verify all artifact operations work correctly by using artifacts as inputs, outputs and performing garbage collection
