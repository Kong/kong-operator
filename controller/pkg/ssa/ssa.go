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

// Package ssa provides helpers for Server-Side Apply (SSA) workflows,
// including TypeConverter initialisation, diff-before-apply, and
// structured-merge-diff-based object merging.
package ssa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/openapi"
	"k8s.io/kube-openapi/pkg/spec3"
	"k8s.io/kube-openapi/pkg/validation/spec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	"github.com/kong/kong-operator/v2/controller/pkg/op"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// NewTypeConverter builds a TypeConverter from the API server's live OpenAPI v3
// schemas. Only schemas for the specified GroupVersions are fetched, reducing
// startup latency compared to downloading the full schema set.
//
// groupVersions should list every GV whose objects will be passed to MergeObjects
// or ApplyIfChanged. For core types use schema.GroupVersion{Group: "", Version: "v1"}.
// Call this once during SetupWithManager.
func NewTypeConverter(mgr ctrl.Manager, groupVersions []schema.GroupVersion) (managedfields.TypeConverter, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client for OpenAPI: %w", err)
	}

	openAPIClient := openapi.NewClient(discoveryClient.RESTClient())
	paths, err := openAPIClient.Paths()
	if err != nil {
		return nil, fmt.Errorf("failed to list OpenAPI paths: %w", err)
	}

	// Build a set of path keys we want, e.g. "api/v1" or "apis/apps/v1".
	wanted := make(map[string]struct{}, len(groupVersions))
	for _, gv := range groupVersions {
		wanted[gvToPathKey(gv)] = struct{}{}
	}

	schemas := map[string]*spec.Schema{}
	for pathKey, gv := range paths {
		if _, ok := wanted[pathKey]; !ok {
			continue
		}
		s, err := gv.Schema("application/json")
		if err != nil {
			return nil, fmt.Errorf("failed to download schema for %s: %w", pathKey, err)
		}
		var oa spec3.OpenAPI
		if err := json.Unmarshal(s, &oa); err != nil {
			return nil, fmt.Errorf("failed to parse schema for %s: %w", pathKey, err)
		}
		maps.Copy(schemas, oa.Components.Schemas)
	}

	tc, err := managedfields.NewTypeConverter(schemas, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create TypeConverter: %w", err)
	}
	return tc, nil
}

// gvToPathKey converts a GroupVersion to the path key used by the OpenAPI v3
// discovery endpoint, e.g. schema.GroupVersion{Group:"apps",Version:"v1"} → "apis/apps/v1".
func gvToPathKey(gv schema.GroupVersion) string {
	if gv.Group == "" {
		return "api/" + gv.Version
	}
	return "apis/" + gv.Group + "/" + gv.Version
}

// MergeObjects merges userOverlay into base using structured-merge-diff.
// User-provided values in userOverlay win on conflicts; the base supplies
// default values for fields the user has not specified. Returns an
// *unstructured.Unstructured so the caller can use the result directly with
// ApplyIfChanged or convert it to a typed struct.
func MergeObjects(tc managedfields.TypeConverter, base, userOverlay runtime.Object) (*unstructured.Unstructured, error) {
	baseTyped, err := tc.ObjectToTyped(base)
	if err != nil {
		return nil, fmt.Errorf("failed to convert base to TypedValue: %w", err)
	}
	userTyped, err := tc.ObjectToTyped(userOverlay)
	if err != nil {
		return nil, fmt.Errorf("failed to convert user overlay to TypedValue: %w", err)
	}

	// base.Merge(userOverlay): RHS (userOverlay) wins on conflicts.
	merged, err := baseTyped.Merge(userTyped)
	if err != nil {
		return nil, fmt.Errorf("failed to merge objects: %w", err)
	}

	result, err := tc.TypedToObject(merged)
	if err != nil {
		return nil, fmt.Errorf("failed to convert merged TypedValue to object: %w", err)
	}

	// The OpenAPI-based TypeConverter always returns *unstructured.Unstructured.
	unstr, ok := result.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("merged result is not Unstructured: %T", result)
	}

	return unstr, nil
}

// ApplyIfChanged performs a diff-before-apply using structured-merge-diff:
//  1. Fetch the existing object from the API server.
//  2. Convert both existing and desired to typed.TypedValue via TypeConverter.
//  3. Compare using TypedValue.Compare().
//  4. Issue a Server-Side Apply only when a difference is detected.
//
// This prevents infinite reconcile loops on API server versions that incorrectly
// bump resourceVersion on no-op SSA patches.
func ApplyIfChanged(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	tc managedfields.TypeConverter,
	desired client.Object,
	fieldManager string,
) (op.Result, error) {
	applyOpts := []client.ApplyOption{client.FieldOwner(fieldManager), client.ForceOwnership}

	// Build a clean unstructured representation: strip .status because this
	// function operates on the main resource, not the status subresource.
	// This object is used for both comparison and the apply payload.
	desiredU, err := toUnstructuredWithoutStatus(desired)
	if err != nil {
		return op.Noop, err
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(desired.GetObjectKind().GroupVersionKind())
	err = cl.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		ac := client.ApplyConfigurationFromUnstructured(desiredU)
		return op.Created, cl.Apply(ctx, ac, applyOpts...)
	}
	if err != nil {
		return op.Noop, err
	}

	existingTyped, err := tc.ObjectToTyped(existing)
	if err != nil {
		return op.Noop, fmt.Errorf("failed to convert existing object to TypedValue: %w", err)
	}
	desiredTyped, err := tc.ObjectToTyped(desiredU)
	if err != nil {
		return op.Noop, fmt.Errorf("failed to convert desired object to TypedValue: %w", err)
	}

	// Build the comparison scope as the union of:
	// - Fields currently owned by this manager (ownedSet): detects removals
	//   when a field we used to apply is no longer in the desired object.
	// - Fields present in the desired object (desiredFieldSet): covers identity
	//   fields (.apiVersion, .kind, .metadata.name/namespace) and any new
	//   fields we want to add.
	//
	// Projecting existing through this scope ensures that server-defaulted
	// and foreign-manager fields do not cause spurious diffs, while identity
	// fields (present on both sides with equal values) are compared correctly
	// and never appear as "Added".
	ownedSet, err := ownedFieldSetForSubresource(existing, fieldManager, "")
	if err != nil {
		return op.Noop, fmt.Errorf("failed to extract owned field set: %w", err)
	}
	desiredFieldSet, err := desiredTyped.ToFieldSet()
	if err != nil {
		return op.Noop, fmt.Errorf("failed to compute desired field set: %w", err)
	}
	compareSet := ownedSet.Union(desiredFieldSet)

	// ExtractItems is a value projection: it walks existingTyped's in-memory
	// tree and copies only entries whose paths appear in compareSet. It does
	// not fabricate values, if a path is in compareSet but has no data in
	// existingTyped, the extracted result simply omits it.
	//
	// This means:
	//  - Identity fields (.apiVersion, .kind, .metadata.name/namespace) exist
	//    in both existingTyped and desiredTyped with equal values → no diff.
	//  - Genuinely new fields (in desiredFieldSet, absent on the server) have
	//    no value to extract → absent on the left, present on the right →
	//    correctly reported as Added.
	//  - Fields we used to own but dropped from desired (in ownedSet only)
	//    are extracted from existing but absent in desired → Removed.
	//  - Foreign-manager / server-defaulted fields outside compareSet are
	//    invisible to the diff.
	comparison, err := existingTyped.ExtractItems(compareSet).Compare(desiredTyped)
	if err != nil {
		return op.Noop, fmt.Errorf("failed to compare existing and desired objects: %w", err)
	}

	if comparison.IsSame() {
		logger.V(3).Info("no changes detected",
			"object", client.ObjectKeyFromObject(desired),
			"kind", desired.GetObjectKind().GroupVersionKind().Kind,
		)
		return op.Noop, nil
	}

	logger.V(3).Info("applying changes",
		"object", client.ObjectKeyFromObject(desired),
		"kind", desired.GetObjectKind().GroupVersionKind().Kind,
		"changes", comparison.String(),
	)

	ac := client.ApplyConfigurationFromUnstructured(desiredU)
	return op.Updated, cl.Apply(ctx, ac, applyOpts...)
}

// ApplyStatusIfChanged is like ApplyIfChanged but operates on the status
// subresource. It compares fields owned by fieldManager under the "status"
// subresource via SMD and issues a Status().Apply only when a difference
// is detected, preventing spurious status patches on every reconcile.
func ApplyStatusIfChanged(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	tc managedfields.TypeConverter,
	desired client.Object,
	fieldManager string,
) (op.Result, error) {
	applyOpts := []client.SubResourceApplyOption{client.FieldOwner(fieldManager), client.ForceOwnership}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(desired.GetObjectKind().GroupVersionKind())
	if err := cl.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		return op.Noop, err
	}

	gvk := desired.GetObjectKind().GroupVersionKind()

	// Convert desired to unstructured once; reused for comparison and the apply payload.
	desiredRaw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desired)
	if err != nil {
		return op.Noop, fmt.Errorf("failed to convert desired object to unstructured: %w", err)
	}
	desiredStatus, _, err := unstructured.NestedFieldNoCopy(desiredRaw, "status")
	if err != nil {
		return op.Noop, fmt.Errorf("failed to extract desired status: %w", err)
	}
	existingStatus, _, err := unstructured.NestedFieldNoCopy(existing.Object, "status")
	if err != nil {
		return op.Noop, fmt.Errorf("failed to extract existing status: %w", err)
	}

	// Build minimal objects containing only status for SMD comparison.
	// This scopes the diff strictly to status fields, avoiding noise from
	// spec or metadata differences between the in-memory desired and the
	// live object returned by Get.
	statusOnlyObj := func(statusVal any) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": gvk.GroupVersion().String(),
			"kind":       gvk.Kind,
			"metadata": map[string]any{
				"name":      desired.GetName(),
				"namespace": desired.GetNamespace(),
			},
			"status": statusVal,
		}}
	}

	existingTyped, err := tc.ObjectToTyped(statusOnlyObj(existingStatus))
	if err != nil {
		return op.Noop, fmt.Errorf("failed to convert existing status to TypedValue: %w", err)
	}
	desiredTyped, err := tc.ObjectToTyped(statusOnlyObj(desiredStatus))
	if err != nil {
		return op.Noop, fmt.Errorf("failed to convert desired status to TypedValue: %w", err)
	}

	ownedSet, err := ownedFieldSetForSubresource(existing, fieldManager, "status")
	if err != nil {
		return op.Noop, fmt.Errorf("failed to extract owned status field set: %w", err)
	}
	desiredFieldSet, err := desiredTyped.ToFieldSet()
	if err != nil {
		return op.Noop, fmt.Errorf("failed to compute desired status field set: %w", err)
	}
	compareSet := ownedSet.Union(desiredFieldSet)

	// See comment in ApplyIfChanged for a detailed explanation of why
	// ExtractItems(compareSet) is the correct projection: it extracts
	// values (not paths) from existingTyped, so fields absent on the
	// server produce no left-side entry and are correctly detected as Added.
	comparison, err := existingTyped.ExtractItems(compareSet).Compare(desiredTyped)
	if err != nil {
		return op.Noop, fmt.Errorf("failed to compare status objects: %w", err)
	}

	if comparison.IsSame() {
		logger.V(3).Info("no status changes detected",
			"object", client.ObjectKeyFromObject(desired),
			"kind", gvk.Kind,
		)
		return op.Noop, nil
	}

	logger.V(3).Info("applying status changes",
		"object", client.ObjectKeyFromObject(desired),
		"kind", gvk.Kind,
		"changes", comparison.String(),
	)

	ac := client.ApplyConfigurationFromUnstructured(statusOnlyObj(desiredStatus))
	return op.Updated, cl.Status().Apply(ctx, ac, applyOpts...)
}

// ownedFieldSetForSubresource returns the fieldpath.Set of fields currently
// owned by fieldManager for the given subresource ("" for the main resource,
// "status" for the status subresource). Returns an empty set if the manager
// has no entry yet (e.g. before the first apply).
func ownedFieldSetForSubresource(obj client.Object, fieldManager, subresource string) (*fieldpath.Set, error) {
	entry, ok := k8sutils.FindManagedFieldsEntry(obj, fieldManager, subresource)
	if !ok || entry.FieldsV1 == nil {
		return &fieldpath.Set{}, nil
	}
	set := &fieldpath.Set{}
	if err := set.FromJSON(bytes.NewReader(entry.FieldsV1.Raw)); err != nil {
		return nil, fmt.Errorf("failed to decode managed fields for manager %q subresource %q: %w", fieldManager, subresource, err)
	}
	return set, nil
}

// toUnstructuredWithoutStatus converts a client.Object to
// *unstructured.Unstructured with the .status field removed. This is suitable
// for main-resource SSA comparison and apply payloads where .status should not
// be included (status is managed via the status subresource separately).
func toUnstructuredWithoutStatus(obj client.Object) (*unstructured.Unstructured, error) {
	var u *unstructured.Unstructured
	if existing, ok := obj.(*unstructured.Unstructured); ok {
		u = existing.DeepCopy()
	} else {
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
		}
		u = &unstructured.Unstructured{Object: raw}
	}
	delete(u.Object, "status")
	return u, nil
}
