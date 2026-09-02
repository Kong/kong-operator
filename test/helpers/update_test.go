package helpers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeObj is a minimal stand-in for a client-go generated object, just enough to
// carry a name and a mutable field through Get/Update.
type fakeObj struct {
	name  string
	value int
}

// fakeGetUpdater mimics a real client-go generated clientset: on failure, Get and
// Update return a non-nil pointer to a *zero-valued* object alongside the error
// (client-go's gentype.Client does `result := c.newObject(); ...; return result, err`) --
// exactly the poisoned value that makes the naive Get-mutate-Update-reassign pattern
// self-poisoning, since an empty .name is then fed into the next Get.
type fakeGetUpdater struct {
	stored          fakeObj
	updateConflicts int // number of leading Update calls that fail with a conflict
	updateCalls     int
}

func (f *fakeGetUpdater) Get(_ context.Context, name string, _ metav1.GetOptions) (*fakeObj, error) {
	if name == "" {
		return &fakeObj{}, errors.New("resource name may not be empty")
	}
	if name != f.stored.name {
		return &fakeObj{}, apierrors.NewNotFound(schema.GroupResource{}, name)
	}
	got := f.stored
	return &got, nil
}

func (f *fakeGetUpdater) Update(_ context.Context, obj *fakeObj, _ metav1.UpdateOptions) (*fakeObj, error) {
	f.updateCalls++
	if f.updateCalls <= f.updateConflicts {
		return &fakeObj{}, apierrors.NewConflict(schema.GroupResource{}, obj.name, errors.New("conflict"))
	}
	f.stored = *obj
	updated := f.stored
	return &updated, nil
}

func TestUpdateWithRetry_RetriesOnConflict(t *testing.T) {
	ctx := t.Context()
	cl := &fakeGetUpdater{stored: fakeObj{name: "route-1", value: 1}, updateConflicts: 2}

	updated, err := UpdateWithRetry(ctx, cl, "route-1", func(o *fakeObj) {
		o.value = 42
	})

	require.NoError(t, err)
	assert.Equal(t, 42, updated.value)
	assert.Equal(t, "route-1", updated.name)
	assert.Equal(t, 3, cl.updateCalls, "expected two conflicting attempts then one success")
}

func TestUpdateWithRetry_ReturnsZeroValueOnPermanentFailure(t *testing.T) {
	ctx := t.Context()
	// updateConflicts higher than retry.DefaultRetry's step count so every attempt fails.
	cl := &fakeGetUpdater{stored: fakeObj{name: "route-1", value: 1}, updateConflicts: 1000}

	updated, err := UpdateWithRetry(ctx, cl, "route-1", func(o *fakeObj) {
		o.value = 42
	})

	require.Error(t, err)
	// This is the regression this helper guards against: the underlying fake returns a
	// non-nil poisoned object (empty .name) on every failed attempt, but the caller must
	// still get back the true zero value (nil, for a pointer T) -- never that half-written
	// object, which would otherwise poison a later retry's lookup key.
	assert.Nil(t, updated)
}

func TestUpdateWithRetry_MutatesFreshlyFetchedObject(t *testing.T) {
	ctx := t.Context()
	cl := &fakeGetUpdater{stored: fakeObj{name: "route-1", value: 1}}

	var seenDuringMutate fakeObj
	updated, err := UpdateWithRetry(ctx, cl, "route-1", func(o *fakeObj) {
		seenDuringMutate = *o
		o.value = 7
	})

	require.NoError(t, err)
	assert.Equal(t, fakeObj{name: "route-1", value: 1}, seenDuringMutate, "mutate must see the freshly-fetched object")
	assert.Equal(t, 7, updated.value)
}

func TestUpdateWithRetry_GetErrorIsNotRetriedForever(t *testing.T) {
	ctx := t.Context()
	cl := &fakeGetUpdater{stored: fakeObj{name: "route-1", value: 1}}

	// Simulate the poisoned-name failure mode directly: an empty name must fail fast
	// rather than loop, since it can never succeed.
	_, err := UpdateWithRetry(ctx, cl, "", func(*fakeObj) {})
	require.Error(t, err)
}
