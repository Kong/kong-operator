package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
)

type recordingForgetter struct {
	forgotten []types.UID
}

func (f *recordingForgetter) Forget(uid types.UID) {
	f.forgotten = append(f.forgotten, uid)
}

func TestForgetIfCleanupFinalizerRemoved(t *testing.T) {
	newService := func(finalizers ...string) *configurationv1alpha1.KongService {
		svc := &configurationv1alpha1.KongService{}
		svc.SetUID("svc-uid")
		svc.SetFinalizers(finalizers)
		return svc
	}

	t.Run("finalizer removed forgets the entity", func(t *testing.T) {
		f := &recordingForgetter{}
		forgetIfCleanupFinalizerRemoved(f, newService("other"))
		assert.Equal(t, []types.UID{"svc-uid"}, f.forgotten)
	})

	t.Run("finalizer present keeps the entity", func(t *testing.T) {
		f := &recordingForgetter{}
		forgetIfCleanupFinalizerRemoved(f, newService(KonnectCleanupFinalizer))
		assert.Empty(t, f.forgotten)
	})

	t.Run("nil tracker", func(t *testing.T) {
		var tracker *ops.ConfigVersionTracker
		assert.NotPanics(t, func() { forgetIfCleanupFinalizerRemoved(tracker, newService()) })
	})
}
