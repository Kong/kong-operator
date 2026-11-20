package namegen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/dump"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
)

// TestControlPlaneRefForHashKeepsGeneratedNames pins the text a ControlPlaneRef
// is hashed from. Generated Kong entity names depend on it: if it changes,
// every generated entity gets a new name and is recreated on upgrade.
func TestControlPlaneRefForHashKeepsGeneratedNames(t *testing.T) {
	cp := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "cp1",
		},
	}

	assert.Equal(t,
		"(*v1alpha1.ControlPlaneRef){Type:(string)konnectNamespacedRef KonnectNamespacedRef:(*v1alpha1.KonnectNamespacedRef){Name:(string)cp1 Namespace:(string)}}",
		dump.ForHash(controlPlaneRefForHash(cp)),
	)
	assert.Equal(t, "(*v1alpha1.ControlPlaneRef)<nil>", dump.ForHash(controlPlaneRefForHash(nil)))

	withWorkspace := cp.DeepCopy()
	withWorkspace.Workspace = "team-a"
	assert.Equal(t, utils.Hash32(controlPlaneRefForHash(cp)), utils.Hash32(controlPlaneRefForHash(withWorkspace)),
		"fields added to ControlPlaneRef must not change generated names")
}
