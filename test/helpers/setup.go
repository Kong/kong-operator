package helpers

import (
	"context"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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
	cleaner := clusters.NewCleaner(env.Cluster())
	t.Cleanup(func() {
		assert.NoError(t, cleaner.Cleanup(context.Background()))
	})

	t.Log("creating a testing namespace")
	namespace, err := clusters.GenerateNamespace(ctx, env.Cluster(), t.Name())
	require.NoError(t, err)
	t.Logf("using test namespace: %s", namespace.Name)
	cleaner.AddNamespace(namespace)

	t.Cleanup(func() {
		if t.Failed() {
			output, err := env.Cluster().DumpDiagnostics(context.Background(), t.Name())
			assert.NoError(t, err)
			t.Logf("%s failed, dumped diagnostics to %s", t.Name(), output)
		}
	})

	return namespace, cleaner
}
