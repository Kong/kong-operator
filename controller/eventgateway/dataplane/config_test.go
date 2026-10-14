/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataplane

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
)

func TestControlPlaneRef(t *testing.T) {
	t.Run("konnectNamespacedRef set", func(t *testing.T) {
		assert.Equal(t, shareddataplane.ControlPlaneRef{
			Kind: konnectEventGatewayKind.Kind,
			Name: reconcileTestKEGName,
		}, config.ControlPlaneRef(newReconcileEGDP()))
	})

	t.Run("konnectNamespacedRef missing: empty ref instead of a panic", func(t *testing.T) {
		egdp := newReconcileEGDP()
		egdp.Spec.ControlPlaneRef.KonnectNamespacedRef = nil
		assert.Equal(t, shareddataplane.ControlPlaneRef{}, config.ControlPlaneRef(egdp))
	})
}

func TestBuildContainerWithoutControlPlane(t *testing.T) {
	_, _, err := buildContainer(newReconcileEGDP(), shareddataplane.ResolvedControlPlane{}, "image", "cert", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no resolved KonnectEventGateway")
}

func TestBuildEventGatewayDataPlaneCertificate(t *testing.T) {
	egdp := newReconcileEGDP()
	cert := buildEventGatewayDataPlaneCertificate(
		egdp,
		shareddataplane.ResolvedControlPlane{Kind: konnectEventGatewayKind.Kind, IsKonnect: true, Object: newProgrammedKEG()},
		"cert-secret", "checksum",
	)

	// The gateway is referenced through spec.parentRef only: the deprecated
	// spec.gatewayRef must stay unset, as exactly one of the two is allowed.
	assert.Equal(t, &commonv1alpha1.GatewayRef{
		Group: "konnect.konghq.com",
		Kind:  "KonnectEventGateway",
		Name:  reconcileTestKEGName,
	}, cert.Spec.ParentRef)
	assert.Zero(t, cert.Spec.GatewayRef)

	assert.Equal(t, egdp.Namespace, cert.Namespace)
	assert.Equal(t, shareddataplane.CertEntityName(egdp.Name, "checksum"), cert.Name)
	require.NotNil(t, cert.Spec.APISpec.Certificate.SecretRef)
	assert.Equal(t, "cert-secret", cert.Spec.APISpec.Certificate.SecretRef.Name)
}
