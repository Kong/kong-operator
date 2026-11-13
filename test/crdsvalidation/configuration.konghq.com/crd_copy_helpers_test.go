package configuration_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
)

// crdCopy is a copy of an operator CRD in an API group of its own, installed
// first without some of its validation rules. It lets a test store objects
// the installed CRD rejects, as if stored before the rules existed, and then
// add the rules, without changing the CRDs the other tests use in parallel.
type crdCopy struct {
	// Client can read and write the CRD and its objects, as unstructured.
	Client client.Client
	// APIVersion and Kind are the ones of the objects of the copy.
	APIVersion, Kind string

	withRules apiextensionsv1.CustomResourceDefinition
}

// installCRDCopy installs a copy of the operator CRD in file, in group,
// without the validation rules of its root schema for which drop returns
// true, and waits for it to be established. It fails the test if drop matches
// no rule. The copy is removed when the test ends.
//
// group must be unique to the test: the API server is shared by the package.
func installCRDCopy(
	t *testing.T,
	cfg *rest.Config,
	file, group string,
	drop func(apiextensionsv1.ValidationRule) bool,
) *crdCopy {
	t.Helper()
	ctx := t.Context()

	s := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(s))
	cl, err := client.New(cfg, client.Options{Scheme: s})
	require.NoError(t, err)

	manifest, err := os.ReadFile(filepath.Join(kcfg.KongOperatorCRDsPath(), file))
	require.NoError(t, err)
	var withRules apiextensionsv1.CustomResourceDefinition
	require.NoError(t, yaml.Unmarshal(manifest, &withRules))
	withRules.Spec.Group = group
	withRules.Name = withRules.Spec.Names.Plural + "." + group
	withRules.Annotations = nil

	withoutRules := withRules.DeepCopy()
	removed := 0
	for i := range withoutRules.Spec.Versions {
		schema := withoutRules.Spec.Versions[i].Schema.OpenAPIV3Schema
		before := len(schema.XValidations)
		schema.XValidations = slices.DeleteFunc(schema.XValidations, drop)
		removed += before - len(schema.XValidations)
	}
	require.Positive(t, removed, "no validation rule of %s matches", file)

	require.NoError(t, cl.Create(ctx, withoutRules))
	t.Cleanup(func() {
		// The test context is canceled before the cleanups run.
		assert.NoError(t, client.IgnoreNotFound(cl.Delete(context.Background(), withoutRules))) //nolint:usetesting
	})
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var crd apiextensionsv1.CustomResourceDefinition
		if !assert.NoError(c, cl.Get(ctx, client.ObjectKeyFromObject(withoutRules), &crd)) {
			return
		}
		established := slices.ContainsFunc(crd.Status.Conditions, func(cond apiextensionsv1.CustomResourceDefinitionCondition) bool {
			return cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue
		})
		assert.True(c, established, "CRD not established yet")
	}, time.Minute, 100*time.Millisecond)

	version := withRules.Spec.Versions[0].Name
	for _, v := range withRules.Spec.Versions {
		if v.Storage {
			version = v.Name
		}
	}
	return &crdCopy{
		Client:     cl,
		APIVersion: group + "/" + version,
		Kind:       withRules.Spec.Names.Kind,
		withRules:  withRules,
	}
}

// AddRules adds the dropped rules back to the copy. The API server may take
// a moment to enforce them: callers wait for it with a probe of their own.
func (c *crdCopy) AddRules(t *testing.T) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var crd apiextensionsv1.CustomResourceDefinition
		if !assert.NoError(ct, c.Client.Get(t.Context(), client.ObjectKeyFromObject(&c.withRules), &crd)) {
			return
		}
		crd.Spec = c.withRules.Spec
		assert.NoError(ct, c.Client.Update(t.Context(), &crd))
	}, time.Minute, 100*time.Millisecond)
}
