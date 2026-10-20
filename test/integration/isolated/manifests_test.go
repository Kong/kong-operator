package isolated

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
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

// adjustManifestObject walks the object and adapts it to the test environment, in place:
//   - `controllerName` of the default operator is replaced with the one of the operator under test,
//   - Kong Gateway images (`image: kong/kong-gateway:*`) are replaced with the one used in tests (see dataPlaneImage),
//   - `namespace: default` (metadata and cross-namespace references alike) is replaced with the test's namespace,
//   - the `kong` ingress class (ingressClassName, ingressClass, controllerName and the
//     kubernetes.io/ingress.class annotation) is replaced with the test's ingress class.
func adjustManifestObject(node any, namespace, ingressClass string) {
	const (
		defaultControllerName = "konghq.com/gateway-operator"
		kongGatewayImagePref  = "kong/kong-gateway:"
	)
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			str, ok := v.(string)
			if !ok {
				adjustManifestObject(v, namespace, ingressClass)
				continue
			}
			switch {
			case k == "controllerName" && str == defaultControllerName:
				n[k] = vars.ControllerName()
			case k == "image" && strings.HasPrefix(str, kongGatewayImagePref):
				n[k] = dataPlaneImage()
			case k == "namespace" && str == "default":
				n[k] = namespace
			case str == "kong" && slices.Contains([]string{"ingressClassName", "ingressClass", "controllerName", "kubernetes.io/ingress.class"}, k):
				n[k] = ingressClass
			}
		}
	case []any:
		for _, v := range n {
			adjustManifestObject(v, namespace, ingressClass)
		}
	}
}

// applyManifest applies the manifest from the given path to the test's namespace and registers
// its objects in the test's cleaner. Before applying, the objects are adjusted to the test environment
// (see adjustManifestObject), and namespaced objects without a namespace are created in the test's namespace.
// Optional replacements are applied to the raw manifest first (as in [strings.NewReplacer]: old1, new1, old2, new2, ...).
func applyManifest(ctx context.Context, t *testing.T, manifestPath string, replacements ...string) {
	t.Helper()

	namespace := GetNamespaceForT(ctx, t)
	cleaner := GetFromCtxForT[*clusters.Cleaner](ctx, t)
	ingressClass := GetIngressClassFromCtx(ctx)

	b, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	manifest := string(b)
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

		adjustManifestObject(obj.Object, namespace, ingressClass)

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
