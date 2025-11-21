package helpers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/modules/manager/scheme"
)

// TODO https://github.com/Kong/kubernetes-testing-framework/issues/302
// Extract this into KTF to be shared across tests and different repos.

// SetupTestEnv is a helper function for tests which conveniently creates a cluster
// cleaner (to clean up test resources automatically after the test finishes)
// and creates a new namespace for the test to use.
// The namespace is being automatically deleted during the test teardown using t.Cleanup().
func SetupTestEnv(t *testing.T, ctx context.Context, env environments.Environment) (*corev1.Namespace, *clusters.Cleaner) {
	t.Helper()

	t.Log("performing test setup")
	cleaner := clusters.NewCleaner(env.Cluster(), scheme.Get())
	t.Cleanup(func() {
		t.Helper()

		t.Log("performing test cleanup")
		// Use a longer timeout for cleanup to avoid flakiness when namespaces
		// take longer to terminate due to finalizers on CRDs (e.g., Konnect resources).
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		assert.NoError(t, cleaner.Cleanup(ctx))
	})

	t.Log("creating a testing namespace")
	namespace, err := clusters.GenerateNamespace(ctx, env.Cluster(), labelValueForTest(t))
	require.NoError(t, err)
	t.Logf("using test namespace: %s", namespace.Name)
	cleaner.AddNamespace(namespace)

	t.Cleanup(func() {
		if t.Failed() {
			output, err := env.Cluster().DumpDiagnostics(context.Background(), t.Name())
			require.NoError(t, err)
			t.Logf("%s failed, dumped diagnostics to %s", t.Name(), output)
		}
	})

	return namespace, cleaner
}

// labelValueForTest returns a sanitized test name that can be used as kubernetes
// label value.
func labelValueForTest(t *testing.T) string {
	s := strings.ReplaceAll(t.Name(), "/", ".")
	// Trim to adhere to k8s label requirements:
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set
	if len(s) > 63 {
		return s[:63]
	}
	return s
}
