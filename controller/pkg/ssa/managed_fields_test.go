package ssa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFindManagedFieldsEntry(t *testing.T) {
	applyEntry := func(manager, subresource string) metav1.ManagedFieldsEntry {
		return metav1.ManagedFieldsEntry{
			Manager:     manager,
			Operation:   metav1.ManagedFieldsOperationApply,
			Subresource: subresource,
			FieldsV1:    FieldsWithRawBytes([]byte(`{}`)),
		}
	}
	updateEntry := func(manager string) metav1.ManagedFieldsEntry {
		return metav1.ManagedFieldsEntry{
			Manager:   manager,
			Operation: metav1.ManagedFieldsOperationUpdate,
			FieldsV1:  FieldsWithRawBytes([]byte(`{}`)),
		}
	}

	tests := []struct {
		name          string
		managedFields []metav1.ManagedFieldsEntry
		manager       string
		subresource   string
		wantFound     bool
		wantManager   string
	}{
		{
			name: "matching apply entry found",
			managedFields: []metav1.ManagedFieldsEntry{
				applyEntry("gateway-operator", ""),
			},
			manager:     "gateway-operator",
			subresource: "",
			wantFound:   true,
			wantManager: "gateway-operator",
		},
		{
			name: "matching apply entry for subresource found",
			managedFields: []metav1.ManagedFieldsEntry{
				applyEntry("gateway-operator", "status"),
			},
			manager:     "gateway-operator",
			subresource: "status",
			wantFound:   true,
			wantManager: "gateway-operator",
		},
		{
			name: "update entry with same manager is ignored",
			managedFields: []metav1.ManagedFieldsEntry{
				updateEntry("gateway-operator"),
			},
			manager:     "gateway-operator",
			subresource: "",
			wantFound:   false,
		},
		{
			name: "different manager not returned",
			managedFields: []metav1.ManagedFieldsEntry{
				applyEntry("kubectl-client-side-apply", ""),
				applyEntry("gateway-operator", ""),
			},
			manager:     "gateway-operator",
			subresource: "",
			wantFound:   true,
			wantManager: "gateway-operator",
		},
		{
			name: "subresource mismatch not returned",
			managedFields: []metav1.ManagedFieldsEntry{
				applyEntry("gateway-operator", "status"),
			},
			manager:     "gateway-operator",
			subresource: "",
			wantFound:   false,
		},
		{
			name:          "empty managed fields returns not found",
			managedFields: nil,
			manager:       "gateway-operator",
			subresource:   "",
			wantFound:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &metav1.ObjectMeta{ManagedFields: tt.managedFields}
			entry, found := FindManagedFieldsEntry(obj, tt.manager, tt.subresource)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, tt.wantManager, entry.Manager)
				assert.Equal(t, metav1.ManagedFieldsOperationApply, entry.Operation)
			}
		})
	}
}
