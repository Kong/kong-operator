/*
Copyright 2025 Kong, Inc.

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
	"context"
	"fmt"
	"maps"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	aexbuilder "k8s.io/apiextensions-apiserver/pkg/controller/openapi/builder"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/rest"
	kubespec3 "k8s.io/kube-openapi/pkg/spec3"
	validationspec "k8s.io/kube-openapi/pkg/validation/spec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v6/typed"
)

// builtinGroupVersions are the non-CRD GroupVersions whose schemas are fetched
// once from /openapi/v3 at startup (built-ins are published at apiserver start,
// not subject to the CRD OpenAPI publication debounce).
var builtinGroupVersions = []schema.GroupVersion{
	{Group: "", Version: "v1"},     // core: Service, Secret, ConfigMap
	{Group: "apps", Version: "v1"}, // Deployment
}

// crdSpecCache caches the OpenAPI spec built from a single CRD version.
// The rv field allows detecting whether the CRD has changed since the last build.
type crdSpecCache struct {
	rv   string // CRD resourceVersion at build time
	spec *kubespec3.OpenAPI
}

// TypeConverterProvider is a shared, always-current managedfields.TypeConverter.
// It implements the managedfields.TypeConverter interface directly, so it can
// be passed anywhere a managedfields.TypeConverter is expected (e.g.
// ApplyIfChanged, ApplyStatusIfChanged, MergeObjects).
//
// CRD schemas are built in-process from the live CRD objects (apiserver-style,
// zero debounce latency); built-in schemas (core/v1, apps/v1) are fetched once
// from /openapi/v3.  A dedicated CRD controller calls Rebuild whenever a relevant
// CRD changes, atomically swapping the live converter.
type TypeConverterProvider struct {
	// current holds the active TypeConverter. Lock-free read on the reconcile
	// hot path. Always non-nil once NewTypeConverterProvider has returned
	// successfully.
	current atomic.Pointer[managedfields.TypeConverter]

	// mu guards specCache.
	mu sync.Mutex

	// crdGroups is the set of API groups whose CRDs are built in-process
	// (apiserver-style) from the CRD objects rather than fetched from
	// /openapi/v3. Only groups whose types are actually passed to
	// ApplyIfChanged / ApplyStatusIfChanged / MergeObjects belong here.
	crdGroups map[string]struct{}

	// builtins is the cached set of built-in OpenAPI schemas, populated once
	// at construction and reused on every Rebuild.
	builtins map[string]*validationspec.Schema

	// specCache caches per-CRD-version OpenAPI specs keyed by "<crdName>/<version>".
	// BuildOpenAPIV3 is only re-called when a CRD's resourceVersion changes.
	specCache map[string]crdSpecCache

	// cfg is the REST config used to fetch built-in schemas.
	cfg *rest.Config
}

// NewTypeConverterProvider builds a TypeConverterProvider scoped to crdGroups:
// it fetches the built-in schemas and lists the matching CRDs concurrently
// (via mgr's uncached API reader, safe to call before mgr.Start), then builds
// the initial converter. The returned provider is immediately usable.
func NewTypeConverterProvider(ctx context.Context, logger logr.Logger, mgr ctrl.Manager, crdGroups map[string]struct{}) (*TypeConverterProvider, error) {
	p := &TypeConverterProvider{
		cfg:       mgr.GetConfig(),
		crdGroups: crdGroups,
		specCache: make(map[string]crdSpecCache),
	}

	var builtins map[string]*validationspec.Schema
	var crds []*apiextensionsv1.CustomResourceDefinition
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		builtins, err = fetchBuiltinSchemas(p.cfg, builtinGroupVersions)
		return err
	})
	g.Go(func() error {
		var err error
		crds, err = listRelevantCRDs(gctx, mgr.GetAPIReader(), crdGroups)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("ssa provider: failed to fetch initial schemas: %w", err)
	}
	p.builtins = builtins

	if err := p.Rebuild(ctx, logger, crds); err != nil {
		return nil, err
	}
	return p, nil
}

// ObjectToTyped implements managedfields.TypeConverter. Lock-free read.
func (p *TypeConverterProvider) ObjectToTyped(obj runtime.Object, opts ...typed.ValidationOptions) (*typed.TypedValue, error) {
	return p.get().ObjectToTyped(obj, opts...)
}

// TypedToObject implements managedfields.TypeConverter.
func (p *TypeConverterProvider) TypedToObject(v *typed.TypedValue) (runtime.Object, error) {
	return p.get().TypedToObject(v)
}

// get returns the current converter. Safe to call any time after
// NewTypeConverterProvider has returned, since construction always performs
// an initial Rebuild.
func (p *TypeConverterProvider) get() managedfields.TypeConverter {
	return *p.current.Load()
}

// IsCRDGroupRelevant reports whether the given group is managed by this provider.
func (p *TypeConverterProvider) IsCRDGroupRelevant(group string) bool {
	return isCRDGroupRelevant(p.crdGroups, group)
}

// CRDGroups returns the set of API groups managed by this provider.
func (p *TypeConverterProvider) CRDGroups() map[string]struct{} {
	return p.crdGroups
}

func isCRDGroupRelevant(crdGroups map[string]struct{}, group string) bool {
	_, ok := crdGroups[group]
	return ok
}

// Rebuild rebuilds the TypeConverter in-process from the provided CRD list and
// the cached built-in schemas, then atomically swaps the live converter.
// Concurrent callers (rare) coalesce: the last one to store wins, which is
// correct since each holds its own current CRD snapshot.
//
// BuildOpenAPIV3 is only called for CRD versions whose resourceVersion has
// changed since the last build; unchanged versions reuse their cached spec.
func (p *TypeConverterProvider) Rebuild(ctx context.Context, logger logr.Logger, crds []*apiextensionsv1.CustomResourceDefinition) error {
	p.mu.Lock()
	specCacheSnapshot := p.specCache
	p.mu.Unlock()

	var rebuilt, cached int
	newCache := make(map[string]crdSpecCache, len(specCacheSnapshot))
	crdSpecs := make([]*kubespec3.OpenAPI, 0, len(crds))
	for _, crd := range crds {
		for _, v := range crd.Spec.Versions {
			key := crd.Name + "/" + v.Name
			if entry, ok := specCacheSnapshot[key]; ok && entry.rv == crd.ResourceVersion {
				newCache[key] = entry
				crdSpecs = append(crdSpecs, entry.spec)
				cached++
				continue
			}
			s, err := aexbuilder.BuildOpenAPIV3(crd, v.Name, aexbuilder.Options{})
			if err != nil {
				return fmt.Errorf("ssa provider: build OpenAPI v3 for %s/%s: %w", crd.Name, v.Name, err)
			}
			newCache[key] = crdSpecCache{rv: crd.ResourceVersion, spec: s}
			crdSpecs = append(crdSpecs, s)
			rebuilt++
			logger.V(1).Info("rebuilt CRD schema", "crd", crd.Name, "version", v.Name)
		}
	}
	logger.V(1).Info("SSA TypeConverter spec cache", "rebuilt", rebuilt, "cached", cached)

	schemas, err := mergeApplyModels(p.builtins, crdSpecs)
	if err != nil {
		return fmt.Errorf("ssa provider: failed to merge apply models: %w", err)
	}
	tc, err := managedfields.NewTypeConverter(schemas, false)
	if err != nil {
		return fmt.Errorf("ssa provider: failed to create TypeConverter: %w", err)
	}

	p.mu.Lock()
	p.specCache = newCache
	p.mu.Unlock()
	p.current.Store(&tc)
	return nil
}

// Ready implements the healthz.Checker interface. Returns nil once the first
// build has completed so the manager's readyz check passes only after the
// converter is ready.
func (p *TypeConverterProvider) Ready(_ *http.Request) error {
	if p.current.Load() == nil {
		return fmt.Errorf("SSA TypeConverter not yet initialised")
	}
	return nil
}

// listRelevantCRDs lists all CRDs in the configured groups using the given reader.
func listRelevantCRDs(ctx context.Context, r client.Reader, crdGroups map[string]struct{}) ([]*apiextensionsv1.CustomResourceDefinition, error) {
	allCRDs := &apiextensionsv1.CustomResourceDefinitionList{}
	if err := r.List(ctx, allCRDs); err != nil {
		return nil, err
	}
	relevant := make([]*apiextensionsv1.CustomResourceDefinition, 0, len(allCRDs.Items))
	for i := range allCRDs.Items {
		if isCRDGroupRelevant(crdGroups, allCRDs.Items[i].Spec.Group) {
			relevant = append(relevant, &allCRDs.Items[i])
		}
	}
	return relevant, nil
}

// mergeApplyModels merges pre-built CRD OpenAPI specs with the cached built-in
// schemas into the flat map required by managedfields.NewTypeConverter.
//
// MergeSpecsV3 silently drops any spec whose Paths field is nil, so builtins
// (which have no Paths) must be combined via a plain map merge after the CRD
// specs are merged.  Builtins win on conflict so core types (apps/v1.Deployment
// etc.) are never shadowed by a CRD schema.
func mergeApplyModels(
	builtins map[string]*validationspec.Schema,
	crdSpecs []*kubespec3.OpenAPI,
) (map[string]*validationspec.Schema, error) {
	merged, err := aexbuilder.MergeSpecsV3(crdSpecs...)
	if err != nil {
		return nil, fmt.Errorf("merge CRD OpenAPI v3 specs: %w", err)
	}
	schemas := make(map[string]*validationspec.Schema, len(builtins))
	if merged.Components != nil {
		maps.Copy(schemas, merged.Components.Schemas)
	}
	maps.Copy(schemas, builtins)
	return schemas, nil
}
