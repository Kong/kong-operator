package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnsureFinalizerInMetadata(t *testing.T) {
	testCases := []struct {
		name            string
		metadata        *metav1.ObjectMeta
		finalizer       string
		changed         bool
		finalizerLength int
	}{
		{
			name:            "empty finalizers,should append",
			metadata:        &metav1.ObjectMeta{},
			finalizer:       "finalizer1",
			changed:         true,
			finalizerLength: 1,
		},
		{
			name: "finalizer does not exist, should append",
			metadata: &metav1.ObjectMeta{
				Finalizers: []string{
					"finalizer0",
				},
			},
			finalizer:       "finalizer1",
			changed:         true,
			finalizerLength: 2,
		},
		{
			name: "finalizer exists, should not change",
			metadata: &metav1.ObjectMeta{
				Finalizers: []string{
					"finalizer1",
					"finalizer2",
				},
			},
			finalizer:       "finalizer1",
			changed:         false,
			finalizerLength: 2,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			changed := EnsureFinalizerInMetadata(tc.metadata, tc.finalizer)
			assert.Contains(t, tc.metadata.Finalizers, tc.finalizer)
			assert.Equal(t, tc.changed, changed)
			assert.Len(t, tc.metadata.Finalizers, tc.finalizerLength)
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
