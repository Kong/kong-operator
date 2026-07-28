package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

// fakeMirrorable is a minimal type implementing MirrorableEntity to exercise the
// interface-based fallback without depending on a specific generated CRD.
type fakeMirrorable struct {
	source *commonv1alpha1.EntitySource
	mirror *konnectv1alpha2.MirrorSpec
}

func (f *fakeMirrorable) GetSource() *commonv1alpha1.EntitySource { return f.source }
func (f *fakeMirrorable) GetMirror() *konnectv1alpha2.MirrorSpec  { return f.mirror }

func TestMirrorableEntity_InterfaceFallback(t *testing.T) {
	origin := new(commonv1alpha1.EntitySourceOrigin)
	mirror := new(commonv1alpha1.EntitySourceMirror)

	assert.True(t, isMirrorableViaInterface(&fakeMirrorable{source: origin}))
	assert.False(t, isMirrorEntityViaInterface(&fakeMirrorable{source: origin}))
	assert.True(t, isMirrorEntityViaInterface(&fakeMirrorable{source: mirror}))
}
