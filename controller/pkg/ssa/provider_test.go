/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ssa

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	aexbuilder "k8s.io/apiextensions-apiserver/pkg/controller/openapi/builder"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/rest"
	kubespec3 "k8s.io/kube-openapi/pkg/spec3"
	validationspec "k8s.io/kube-openapi/pkg/validation/spec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// buildOpenAPIV3ForTest builds the OpenAPI v3 spec for a CRD's sole version,
// mirroring what Rebuild does internally, for tests that exercise
// mergeApplyModels directly.
func buildOpenAPIV3ForTest(t *testing.T, crd *apiextensionsv1.CustomResourceDefinition) (*kubespec3.OpenAPI, error) {
	t.Helper()
	return aexbuilder.BuildOpenAPIV3(crd, crd.Spec.Versions[0].Name, aexbuilder.Options{})
}

// testCRD builds a minimal, structurally-valid CRD for a single version so
// that aexbuilder.BuildOpenAPIV3 can build a schema from it.
func testCRD(group, kind, version, resourceVersion string) *apiextensionsv1.CustomResourceDefinition {
	plural := fmt.Sprintf("%ss", kind)
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s.%s", plural, group),
			ResourceVersion: resourceVersion,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     kind,
				ListKind: kind + "List",
				Plural:   plural,
				Singular: kind,
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    version,
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
					},
				},
			},
		},
	}
}

func Test_isCRDGroupRelevant(t *testing.T) {
	groups := map[string]struct{}{"eventgateway.konghq.com": {}}

	assert.True(t, isCRDGroupRelevant(groups, "eventgateway.konghq.com"))
	assert.False(t, isCRDGroupRelevant(groups, "other.konghq.com"))
	assert.False(t, isCRDGroupRelevant(nil, "eventgateway.konghq.com"))
}

func Test_TypeConverterProvider_IsCRDGroupRelevant_and_CRDGroups(t *testing.T) {
	groups := map[string]struct{}{"eventgateway.konghq.com": {}, "konnect.konghq.com": {}}
	p := &TypeConverterProvider{crdGroups: groups}

	assert.True(t, p.IsCRDGroupRelevant("eventgateway.konghq.com"))
	assert.False(t, p.IsCRDGroupRelevant("configuration.konghq.com"))
	assert.Equal(t, groups, p.CRDGroups())
}

func Test_TypeConverterProvider_Ready(t *testing.T) {
	p := &TypeConverterProvider{}
	require.Error(t, p.Ready(&http.Request{}))

	tc := managedfields.NewDeducedTypeConverter()
	p.current.Store(&tc)
	require.NoError(t, p.Ready(&http.Request{}))
}

// Test_TypeConverterProvider_ObjectToTyped_TypedToObject verifies that
// ObjectToTyped/TypedToObject simply forward to whatever converter get()
// returns, without needing real CRD/builtin schemas.
func Test_TypeConverterProvider_ObjectToTyped_TypedToObject(t *testing.T) {
	deduced := managedfields.NewDeducedTypeConverter()
	p := &TypeConverterProvider{}
	p.current.Store(&deduced)

	obj := svcWithPort(80)

	wantTyped, wantErr := deduced.ObjectToTyped(obj)
	gotTyped, gotErr := p.ObjectToTyped(obj)
	require.Equal(t, wantErr, gotErr)
	require.NotNil(t, gotTyped)
	assert.Equal(t, wantTyped, gotTyped)

	wantObj, wantErr := deduced.TypedToObject(wantTyped)
	gotObj, gotErr := p.TypedToObject(gotTyped)
	require.Equal(t, wantErr, gotErr)
	assert.Equal(t, wantObj, gotObj)
}

func Test_TypeConverterProvider_Rebuild_CachesUnchangedCRDVersions(t *testing.T) {
	p := &TypeConverterProvider{
		crdGroups: map[string]struct{}{"example.konghq.com": {}},
		specCache: make(map[string]crdSpecCache),
	}

	crd := testCRD("example.konghq.com", "Widget", "v1", "1")
	require.NoError(t, p.Rebuild(t.Context(), logr.Discard(), []*apiextensionsv1.CustomResourceDefinition{crd}))

	key := crd.Name + "/v1"
	firstSpec := p.specCache[key].spec
	require.NotNil(t, firstSpec)

	// Rebuilding with the same resourceVersion must reuse the cached spec
	// (same pointer), not call BuildOpenAPIV3 again.
	require.NoError(t, p.Rebuild(t.Context(), logr.Discard(), []*apiextensionsv1.CustomResourceDefinition{crd}))
	assert.Same(t, firstSpec, p.specCache[key].spec)

	// Bumping the resourceVersion must trigger a fresh build.
	crd.ResourceVersion = "2"
	require.NoError(t, p.Rebuild(t.Context(), logr.Discard(), []*apiextensionsv1.CustomResourceDefinition{crd}))
	assert.NotSame(t, firstSpec, p.specCache[key].spec)
	assert.Equal(t, "2", p.specCache[key].rv)
}

func Test_TypeConverterProvider_Rebuild_ConcurrentCallsDoNotRace(t *testing.T) {
	p := &TypeConverterProvider{
		crdGroups: map[string]struct{}{"example.konghq.com": {}},
		specCache: make(map[string]crdSpecCache),
	}
	crd := testCRD("example.konghq.com", "Widget", "v1", "1")

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(rv string) {
			defer wg.Done()
			c := testCRD("example.konghq.com", "Widget", "v1", rv)
			assert.NoError(t, p.Rebuild(t.Context(), logr.Discard(), []*apiextensionsv1.CustomResourceDefinition{c}))
		}(fmt.Sprintf("%d", i))
	}
	wg.Wait()

	require.NoError(t, p.Rebuild(t.Context(), logr.Discard(), []*apiextensionsv1.CustomResourceDefinition{crd}))
	require.NotNil(t, p.current.Load())
}

func Test_mergeApplyModels(t *testing.T) {
	crd := testCRD("example.konghq.com", "Widget", "v1", "1")
	spec, err := buildOpenAPIV3ForTest(t, crd)
	require.NoError(t, err)

	builtins := map[string]*validationspec.Schema{
		// Shares a definition name with the CRD spec to verify builtins win.
		"io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": {},
		"io.k8s.api.core.v1.Service":                      {},
	}

	merged, err := mergeApplyModels(builtins, []*kubespec3.OpenAPI{spec})
	require.NoError(t, err)

	// Builtin-only schema must be present.
	assert.Contains(t, merged, "io.k8s.api.core.v1.Service")
	// Shared key must resolve to the builtin's (empty) schema, not the CRD's.
	assert.Equal(t, builtins["io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"], merged["io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"])
}

func Test_listRelevantCRDs(t *testing.T) {
	ctx := t.Context()
	scheme := managerscheme.Get()

	relevant := testCRD("eventgateway.konghq.com", "KegDataPlane", "v1alpha1", "1")
	irrelevant := testCRD("unrelated.example.com", "Widget", "v1", "1")

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(relevant, irrelevant).Build()
	crds, err := listRelevantCRDs(ctx, cl, map[string]struct{}{"eventgateway.konghq.com": {}})
	require.NoError(t, err)
	require.Len(t, crds, 1)
	assert.Equal(t, relevant.Name, crds[0].Name)
}

func Test_NewTypeConverterProvider_error(t *testing.T) {
	mgr, err := ctrl.NewManager(&rest.Config{Host: "http://127.0.0.1:0"}, ctrlmgr.Options{Scheme: managerscheme.Get()})
	require.NoError(t, err)

	_, err = NewTypeConverterProvider(t.Context(), logr.Discard(), mgr, map[string]struct{}{"eventgateway.konghq.com": {}})
	require.Error(t, err)
}
