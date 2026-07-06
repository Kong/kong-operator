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

package crdschema

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kong/kong-operator/v2/internal/utils/index"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// fakeProvider is a test double for Provider that records Rebuild calls and
// lets tests force errors from List (via the client) or Rebuild.
type fakeProvider struct {
	groups map[string]struct{}

	rebuildErr   error
	rebuildCalls int
	lastCRDs     []*apiextensionsv1.CustomResourceDefinition
}

func (f *fakeProvider) CRDGroups() map[string]struct{} {
	return f.groups
}

func (f *fakeProvider) IsCRDGroupRelevant(group string) bool {
	_, ok := f.groups[group]
	return ok
}

func (f *fakeProvider) Rebuild(_ context.Context, _ logr.Logger, crds []*apiextensionsv1.CustomResourceDefinition) error {
	f.rebuildCalls++
	f.lastCRDs = crds
	return f.rebuildErr
}

func crdWithGroup(name, group string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
		},
	}
}

func crdNames(crds []*apiextensionsv1.CustomResourceDefinition) []string {
	names := make([]string, len(crds))
	for i, c := range crds {
		names[i] = c.Name
	}
	return names
}

func Test_isRelevantCRD(t *testing.T) {
	r := &Reconciler{Provider: &fakeProvider{groups: map[string]struct{}{"relevant.example.com": {}}}}

	assert.True(t, r.isRelevantCRD(crdWithGroup("a.relevant.example.com", "relevant.example.com")))
	assert.False(t, r.isRelevantCRD(crdWithGroup("b.irrelevant.example.com", "irrelevant.example.com")))
	assert.False(t, r.isRelevantCRD(&corev1.Pod{}))
}

// Test_SetupWithManager_schemeError exercises SetupWithManager's error
// return without a live envtest manager: a scheme that doesn't know about
// CustomResourceDefinition makes the controller builder's GVK lookup fail
// immediately, with no network access required. The happy path (which does
// need a real API server for discovery) is covered by TestCRDSchemaReconciler
// in test/envtest.
func Test_SetupWithManager_schemeError(t *testing.T) {
	mgr, err := ctrl.NewManager(&rest.Config{Host: "http://127.0.0.1:0"}, ctrlmgr.Options{Scheme: runtime.NewScheme()})
	require.NoError(t, err)

	r := &Reconciler{Provider: &fakeProvider{}}
	err = r.SetupWithManager(t.Context(), mgr)
	require.Error(t, err)
}

func Test_Reconcile(t *testing.T) {
	scheme := managerscheme.Get()

	crdG1 := crdWithGroup("widgets.g1.example.com", "g1.example.com")
	crdG2 := crdWithGroup("gadgets.g2.example.com", "g2.example.com")
	crdG3 := crdWithGroup("gizmos.g3.example.com", "g3.example.com")

	tests := []struct {
		name          string
		provider      *fakeProvider
		buildClient   func(base client.WithWatch) client.Client
		wantErr       bool
		wantRebuilds  int
		wantCRDNames  []string
		wantNoRebuild bool
	}{
		{
			name:         "lists only the configured group",
			provider:     &fakeProvider{groups: map[string]struct{}{"g1.example.com": {}}},
			buildClient:  func(base client.WithWatch) client.Client { return base },
			wantRebuilds: 1,
			wantCRDNames: []string{crdG1.Name},
		},
		{
			name:         "aggregates matches across multiple configured groups",
			provider:     &fakeProvider{groups: map[string]struct{}{"g1.example.com": {}, "g2.example.com": {}}},
			buildClient:  func(base client.WithWatch) client.Client { return base },
			wantRebuilds: 1,
			wantCRDNames: []string{crdG1.Name, crdG2.Name},
		},
		{
			name:         "no CRDs match the configured group",
			provider:     &fakeProvider{groups: map[string]struct{}{"unrelated.example.com": {}}},
			buildClient:  func(base client.WithWatch) client.Client { return base },
			wantRebuilds: 1,
			wantCRDNames: nil,
		},
		{
			name:     "List error is propagated and Rebuild is not called",
			provider: &fakeProvider{groups: map[string]struct{}{"g1.example.com": {}}},
			buildClient: func(base client.WithWatch) client.Client {
				return interceptor.NewClient(base, interceptor.Funcs{
					List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						return assert.AnError
					},
				})
			},
			wantErr:       true,
			wantNoRebuild: true,
		},
		{
			name:         "Rebuild error is propagated",
			provider:     &fakeProvider{groups: map[string]struct{}{"g1.example.com": {}}, rebuildErr: assert.AnError},
			buildClient:  func(base client.WithWatch) client.Client { return base },
			wantErr:      true,
			wantRebuilds: 1,
			wantCRDNames: []string{crdG1.Name},
		},
	}

	for _, tcse := range tests {
		t.Run(tcse.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(crdG1, crdG2, crdG3)
			for _, opt := range index.OptionsForCRDSchema() {
				builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
			}
			base := builder.Build()

			r := &Reconciler{
				Client:   tcse.buildClient(base),
				Provider: tcse.provider,
			}

			res, err := r.Reconcile(t.Context(), ctrl.Request{})

			if tcse.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, ctrl.Result{}, res)

			if tcse.wantNoRebuild {
				assert.Equal(t, 0, tcse.provider.rebuildCalls)
				return
			}
			assert.Equal(t, tcse.wantRebuilds, tcse.provider.rebuildCalls)
			assert.ElementsMatch(t, tcse.wantCRDNames, crdNames(tcse.provider.lastCRDs))
		})
	}
}
