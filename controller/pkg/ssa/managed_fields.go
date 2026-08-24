package ssa

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FindManagedFieldsEntry returns the ManagedFieldsEntry for the given field manager
// and subresource (pass "" for the main resource). Only entries with Operation=Apply
// are considered, matching the Server-Side Apply ownership model.
// Returns false if no matching entry exists.
func FindManagedFieldsEntry(obj metav1.Object, fieldManager, subresource string) (metav1.ManagedFieldsEntry, bool) {
	for _, entry := range obj.GetManagedFields() {
		if entry.Manager == fieldManager &&
			entry.Operation == metav1.ManagedFieldsOperationApply &&
			entry.Subresource == subresource {
			return entry, true
		}
	}
	return metav1.ManagedFieldsEntry{}, false
}
