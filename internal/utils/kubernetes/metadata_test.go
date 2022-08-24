package kubernetes

import (
	"testing"

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
