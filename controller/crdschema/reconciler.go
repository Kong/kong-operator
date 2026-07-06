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

// Package crdschema contains a controller that rebuilds the shared SSA
// TypeConverterProvider whenever a relevant CustomResourceDefinition changes.
package crdschema

import (
	"context"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	crtbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch

// Provider is the subset of *ssa.TypeConverterProvider that Reconciler
// depends on. Narrowing to an interface keeps this package decoupled from
// the ssa package's internals and lets tests inject a fake.
type Provider interface {
	CRDGroups() map[string]struct{}
	IsCRDGroupRelevant(group string) bool
	Rebuild(ctx context.Context, logger logr.Logger, crds []*apiextensionsv1.CustomResourceDefinition) error
}

// Reconciler watches CustomResourceDefinitions and rebuilds the shared
// TypeConverterProvider whenever a relevant CRD changes.
type Reconciler struct {
	client.Client

	LoggingMode logging.Mode
	Provider    Provider
}

// isRelevantCRD reports whether o is a CustomResourceDefinition belonging to
// a group managed by r.Provider. Used as the watch predicate.
func (r *Reconciler) isRelevantCRD(o client.Object) bool {
	crd, ok := o.(*apiextensionsv1.CustomResourceDefinition)
	return ok && r.Provider.IsCRDGroupRelevant(crd.Spec.Group)
}

// SetupWithManager registers the reconciler with the manager.
// The spec.group field index this relies on (see index.OptionsForCRDSchema)
// is registered centrally by modules/manager.SetupCacheIndexes.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&apiextensionsv1.CustomResourceDefinition{},
			crtbuilder.WithPredicates(predicate.NewPredicateFuncs(r.isRelevantCRD)),
		).
		Complete(r)
}

// Reconcile is called whenever a relevant CRD is added, updated, or deleted.
// It lists only CRDs in the configured groups from the cache (via field index)
// and rebuilds the provider's TypeConverter in-process.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "ssa-crd-schema", r.LoggingMode)
	log.Trace(logger, "rebuilding SSA TypeConverter", "trigger", req.Name)

	var relevant []*apiextensionsv1.CustomResourceDefinition
	for group := range r.Provider.CRDGroups() {
		list := &apiextensionsv1.CustomResourceDefinitionList{}
		if err := r.List(ctx, list, client.MatchingFields{index.IndexFieldCRDOnGroup: group}); err != nil {
			return ctrl.Result{}, err
		}
		for i := range list.Items {
			relevant = append(relevant, &list.Items[i])
		}
	}

	if err := r.Provider.Rebuild(ctx, logger, relevant); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
