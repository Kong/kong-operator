package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
