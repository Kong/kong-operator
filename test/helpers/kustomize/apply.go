package kustomize

import (
	"context"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers/apply"
)

// Kustomization applies a kustomization to the cluster using the given rest.Config.
func Kustomization(ctx context.Context, t *testing.T, cfg *rest.Config, dir string) {
	t.Helper()

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fSys := filesys.MakeFsOnDisk()
	resmap, err := k.Run(fSys, path.Join(test.ProjectRootPath(), dir))
	require.NoError(t, err)

	b, err := resmap.AsYaml()
	require.NoError(t, err)

	res, err := apply.Apply(ctx, cfg, b)
	require.NoError(t, err)
	for _, r := range res {
		t.Logf("Result: %s", r)
	}
}
