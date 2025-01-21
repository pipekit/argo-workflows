package pod

import (
	"fmt"
	"strings"
)

// Should I use "clean-up" or "cleanup"?
// * cleanup is a noun - e.g "The cleanup"
// * clean-up is a verb - e.g. "I clean-up"

type (
	podCleanupKey    = string // describes the pod to cleanup + the cleanup action to take
	podCleanupAction = string
)

const (
	noAction            podCleanupAction = ""
	deletePod           podCleanupAction = "deletePod"
	labelPodCompleted   podCleanupAction = "labelPodCompleted"
	terminateContainers podCleanupAction = "terminateContainers"
	killContainers      podCleanupAction = "killContainers"
	removeFinalizer     podCleanupAction = "removeFinalizer"
)

func newPodCleanupKey(namespace string, podName string, action podCleanupAction) podCleanupKey {
	val := fmt.Sprintf("%s/%s/%v", namespace, podName, action)
	fmt.Printf("newPodCleanupKey: %s\n", val)
	return val
}

func parsePodCleanupKey(k podCleanupKey) (namespace string, podName string, action podCleanupAction) {
	fmt.Printf("parsePodCleanupKey: %s\n", k)
	parts := strings.Split(k, "/")
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}
