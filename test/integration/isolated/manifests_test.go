package isolated

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/pkg/vars"
	"github.com/kong/kong-operator/v2/test/integration/kic/consts"
)

// samplesManifestPath returns the path of a manifest from config/samples.
func samplesManifestPath(manifestName string) string {
	return filepath.Join("..", "..", "..", "config", "samples", manifestName)
}

var (
	gatewayClassControllerNameRegex = regexp.MustCompile(`(?m)^(\s*)controllerName: konghq\.com/gateway-operator\s*$`)
	kongGatewayImageRegex           = regexp.MustCompile(`(?m)^(\s*)image: kong/kong-gateway:\S+\s*$`)
	defaultNamespaceRegex           = regexp.MustCompile(`(?m)^(\s*)namespace: default\s*$`)
)

// applyManifest applies the manifest from the given path to the test's namespace and registers
// its objects in the test's cleaner. Before applying, the manifest is adjusted to the test environment:
//   - GatewayClasses are handed over to the operator under test (its controller name differs from the default one),
//   - Kong Gateway images are replaced with the one used in tests (see dataPlaneImage),
//   - namespaced objects without a namespace are created in the test's namespace (samples must not set
//     metadata.namespace, so that they can be applied to a temporary namespace created per test),
//   - `namespace: default` is replaced with the test's namespace in cross-namespace references that cannot be omitted
//     (e.g. GatewayClass' parametersRef),
//   - replacements are applied (as in [strings.NewReplacer]: old1, new1, old2, new2, ...).
func applyManifest(ctx context.Context, t *testing.T, manifestPath string, replacements ...string) {
	t.Helper()

	namespace := GetNamespaceForT(ctx, t)
	cleaner := GetFromCtxForT[*clusters.Cleaner](ctx, t)

	b, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	manifest := string(b)
	manifest = gatewayClassControllerNameRegex.ReplaceAllString(manifest, "${1}controllerName: "+vars.ControllerName())
	manifest = kongGatewayImageRegex.ReplaceAllString(manifest, "${1}image: "+dataPlaneImage())
	manifest = defaultNamespaceRegex.ReplaceAllString(manifest, "${1}namespace: "+namespace)
	if len(replacements) > 0 {
		manifest = strings.NewReplacer(replacements...).Replace(manifest)
	}

	cl, err := client.New(GetClusterFromCtx(ctx).Config(), client.Options{})
	require.NoError(t, err)

	t.Logf("applying yaml manifest %s to namespace %s", manifestPath, namespace)
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(manifest)), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(&obj.Object); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
		}
		if len(obj.Object) == 0 {
			continue
		}

		namespaced, err := cl.IsObjectNamespaced(obj)
		require.NoError(t, err)
		if namespaced && obj.GetNamespace() == "" {
			obj.SetNamespace(namespace)
		}

		// Retry as e.g. the validating webhook might reject objects referring to the ones not yet cached.
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.NoError(c, client.IgnoreAlreadyExists(cl.Create(ctx, obj.DeepCopy())))
		}, consts.StatusWait, consts.WaitTick, "failed to create %s %s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
		cleaner.Add(obj)
	}
}
