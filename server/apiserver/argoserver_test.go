package apiserver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/argoproj/argo-workflows/v3/config"
	"github.com/argoproj/argo-workflows/v3/server/types"
)

func TestValidateArtifactDriverImages(t *testing.T) {
	tests := []struct {
		name           string
		config         *config.Config
		pod            *corev1.Pod
		expectedError  bool
		expectedErrMsg string
	}{
		{
			name: "No artifact drivers configured - should skip validation",
			config: &config.Config{
				ArtifactDrivers: []config.ArtifactDriver{},
			},
			expectedError: false,
		},
		{
			name: "All artifact driver images present in pod - should pass",
			config: &config.Config{
				ArtifactDrivers: []config.ArtifactDriver{
					{
						Name:  "my-driver",
						Image: "my-driver:latest",
					},
					{
						Name:  "another-driver",
						Image: "another-driver:v1.0",
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "argo-server",
							Image: "quay.io/argoproj/argocli:latest",
						},
						{
							Name:  "my-driver",
							Image: "my-driver:latest",
						},
						{
							Name:  "another-driver",
							Image: "another-driver:v1.0",
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "Missing artifact driver image in pod - should fail",
			config: &config.Config{
				ArtifactDrivers: []config.ArtifactDriver{
					{
						Name:  "my-driver",
						Image: "my-driver:latest",
					},
					{
						Name:  "missing-driver",
						Image: "missing-driver:v1.0",
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "argo-server",
							Image: "quay.io/argoproj/argocli:latest",
						},
						{
							Name:  "my-driver",
							Image: "my-driver:latest",
						},
					},
				},
			},
			expectedError:  true,
			expectedErrMsg: "Artifact driver validation failed: The following artifact driver images are not present in the server pod: [missing-driver:v1.0]",
		},
		{
			name: "Artifact driver image in init container - should pass",
			config: &config.Config{
				ArtifactDrivers: []config.ArtifactDriver{
					{
						Name:  "init-driver",
						Image: "init-driver:latest",
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "argo-server",
							Image: "quay.io/argoproj/argocli:latest",
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:  "init-driver",
							Image: "init-driver:latest",
						},
					},
				},
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake Kubernetes client
			fakeClient := fake.NewSimpleClientset()

			// Create the argoServer instance
			as := &argoServer{
				clients: &types.Clients{
					Kubernetes: fakeClient,
				},
				namespace: "argo",
			}

			// Set up the test data
			if tt.pod != nil {
				_, err := fakeClient.CoreV1().Pods("argo").Create(context.Background(), tt.pod, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			// Set HOSTNAME environment variable
			t.Setenv("HOSTNAME", "test-pod")

			// Run the validation
			err := as.validateArtifactDriverImages(context.Background(), tt.config)

			// Check results
			if tt.expectedError {
				require.Error(t, err)
				if tt.expectedErrMsg != "" {
					require.Contains(t, err.Error(), tt.expectedErrMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
