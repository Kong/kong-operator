package kubernetes

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
			FieldsV1:    &metav1.FieldsV1{Raw: []byte(`{}`)},
		}
	}
	updateEntry := func(manager string) metav1.ManagedFieldsEntry {
		return metav1.ManagedFieldsEntry{
			Manager:   manager,
			Operation: metav1.ManagedFieldsOperationUpdate,
			FieldsV1:  &metav1.FieldsV1{Raw: []byte(`{}`)},
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

func TestEnsureObjectMetaIsUpdated(t *testing.T) {
	testCases := []struct {
		name             string
		existingObjMeta  metav1.ObjectMeta
		generatedObjMeta metav1.ObjectMeta
		options          []func(existingMeta, generatedMeta metav1.ObjectMeta) (bool, metav1.ObjectMeta)
		toUpdate         bool
		resultingObjMeta metav1.ObjectMeta
	}{
		{
			name: "meta with labels and owner references not to update",
			existingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name:       "ownRef test",
						Controller: new(true),
					},
				},
			},
			generatedObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name:       "ownRef test",
						Controller: new(true),
					},
				},
			},
			toUpdate: false,
			resultingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name:       "ownRef test",
						Controller: new(true),
					},
				},
			},
		},
		{
			name: "meta with labels and annotations not to update",
			existingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				Annotations: map[string]string{
					"test-key": "test-value",
				},
			},
			generatedObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			toUpdate: false,
			resultingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				Annotations: map[string]string{
					"test-key": "test-value",
				},
			},
		},
		{
			name: "meta to update because of different labels",
			existingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: "ownRef test",
					},
				},
			},
			generatedObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo-2": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: "ownRef test",
					},
				},
			},
			toUpdate: true,
			resultingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo-2": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: "ownRef test",
					},
				},
			},
		},
		{
			name: "meta to update because of different owner references",
			existingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name:       "ownRef test",
						Controller: new(false),
					},
				},
			},
			generatedObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name:       "ownRef test",
						Controller: new(true),
					},
				},
			},
			toUpdate: true,
			resultingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name:       "ownRef test",
						Controller: new(true),
					},
				},
			},
		},
		{
			name: "meta to update because of different annotations",
			existingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: "ownRef test",
					},
				},
			},
			generatedObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: "ownRef test",
					},
				},
				Annotations: map[string]string{
					"test-ann-key": "test-ann-value",
				},
			},
			options: []func(existingMeta metav1.ObjectMeta, generatedMeta metav1.ObjectMeta) (bool, metav1.ObjectMeta){
				func(existingMeta metav1.ObjectMeta, generatedMeta metav1.ObjectMeta) (bool, metav1.ObjectMeta) {
					var metaToUpdate bool
					if existingMeta.Annotations == nil && generatedMeta.Annotations != nil {
						existingMeta.Annotations = map[string]string{}
					}
					for k, v := range generatedMeta.Annotations {
						if existingMeta.Annotations[k] != v {
							existingMeta.Annotations[k] = v
							metaToUpdate = true
						}
					}
					return metaToUpdate, existingMeta
				},
			},
			toUpdate: true,
			resultingObjMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						Name: "ownRef test",
					},
				},
				Annotations: map[string]string{
					"test-ann-key": "test-ann-value",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			toUpdate, ResultingMeta := EnsureObjectMetaIsUpdated(tc.existingObjMeta, tc.generatedObjMeta, tc.options...)
			assert.Equal(t, tc.toUpdate, toUpdate)
			assert.Equal(t, tc.resultingObjMeta, ResultingMeta)
		})
	}
}
