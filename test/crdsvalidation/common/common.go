package common

import (
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CommonObjectMeta is a common object meta used in tests.
var CommonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-",
	Namespace:    "default",
}

// WarningCollector implements the k8s client-go rest.WarningHandler interface
// and collects warning messages emitted by the API server.
//
// It is used in envtest-based CRD validation tests to assert that specific
// warnings were produced when creating or updating objects.
//
// The interface method signature is:
//
//	HandleWarningHeader(code int, agent, message string)
//
// We only need to collect the message content for assertions.
//
// Note: this lives in a test-only helper package, so thread-safety is kept simple
// with a mutex and a slice copy on read.
type WarningCollector struct {
	mu       sync.Mutex
	messages []string
}

// HandleWarningHeader records a warning message.
func (w *WarningCollector) HandleWarningHeader(_ int, _ string, message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = append(w.messages, message)
}

// Messages returns a snapshot copy of collected warning messages.
func (w *WarningCollector) Messages() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make([]string, len(w.messages))
	copy(cp, w.messages)
	return cp
}
