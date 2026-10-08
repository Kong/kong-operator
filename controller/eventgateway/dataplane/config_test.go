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
