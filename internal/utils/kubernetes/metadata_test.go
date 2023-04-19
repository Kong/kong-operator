package kubernetes

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnsureFinalizerInMetadata(t *testing.T) {
	testCases := []struct {
		name               string
		metadata           *metav1.ObjectMeta
		finalizers         []string
		changed            bool
		expectedFinalizers []string
	}{
		{
			name:               "empty finalizers,should append",
			metadata:           &metav1.ObjectMeta{},
			finalizers:         []string{"finalizer1"},
			changed:            true,
			expectedFinalizers: []string{"finalizer1"},
		},
		{
			name:               "empty finalizers,should append multiple finalizers",
			metadata:           &metav1.ObjectMeta{},
			finalizers:         []string{"finalizer1", "finalizer2"},
			changed:            true,
			expectedFinalizers: []string{"finalizer1", "finalizer2"},
		},
		{
			name: "only one finalizer does not exist, should append",
			metadata: &metav1.ObjectMeta{
				Finalizers: []string{
					"finalizer0",
				},
			},
			finalizers:         []string{"finalizer0", "finalizer1"},
			changed:            true,
			expectedFinalizers: []string{"finalizer0", "finalizer1"},
		},
		{
			name: "finalizer does not exist, should append",
			metadata: &metav1.ObjectMeta{
				Finalizers: []string{
					"finalizer0",
				},
			},
			finalizers:         []string{"finalizer1"},
			changed:            true,
			expectedFinalizers: []string{"finalizer0", "finalizer1"},
		},
		{
			name: "finalizer exists, should not change",
			metadata: &metav1.ObjectMeta{
				Finalizers: []string{
					"finalizer1",
					"finalizer2",
				},
			},
			finalizers:         []string{"finalizer1"},
			changed:            false,
			expectedFinalizers: []string{"finalizer1", "finalizer2"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			changed := EnsureFinalizersInMetadata(tc.metadata, tc.finalizers...)
			assert.Equal(t, tc.changed, changed)
			assert.Equal(t, tc.metadata.Finalizers, tc.expectedFinalizers)
		})
	}
}

func TestRemoveFinalizerInMetadata(t *testing.T) {
	testCases := []struct {
		name            string
		metadata        *metav1.ObjectMeta
		finalizer       string
		changed         bool
		finalizerLength int
	}{
		{
			name: "finalizer does not exist, should not change",
			metadata: &metav1.ObjectMeta{
				Finalizers: []string{
					"finalizer0",
				},
			},
			finalizer:       "finalizer1",
			changed:         false,
			finalizerLength: 1,
		},
		{
			name: "finalizer exists, should remove",
			metadata: &metav1.ObjectMeta{
				Finalizers: []string{
					"finalizer0",
					"finalizer1",
					"finalizer2",
				},
			},
			finalizer:       "finalizer1",
			changed:         true,
			finalizerLength: 2,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			changed := RemoveFinalizerInMetadata(tc.metadata, tc.finalizer)
			assert.NotContains(t, tc.metadata.Finalizers, tc.finalizer)
			assert.Equal(t, tc.changed, changed)
			assert.Len(t, tc.metadata.Finalizers, tc.finalizerLength)
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
						Controller: lo.ToPtr(true),
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
						Controller: lo.ToPtr(true),
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
						Controller: lo.ToPtr(true),
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
						Controller: lo.ToPtr(false),
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
						Controller: lo.ToPtr(true),
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
						Controller: lo.ToPtr(true),
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			toUpdate, ResultingMeta := EnsureObjectMetaIsUpdated(tc.existingObjMeta, tc.generatedObjMeta, tc.options...)
			assert.Equal(t, tc.toUpdate, toUpdate)
			assert.Equal(t, tc.resultingObjMeta, ResultingMeta)
		})
	}
}
