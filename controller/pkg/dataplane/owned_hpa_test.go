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

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
)

// newHPAAIGWDP returns an AIGatewayDataPlane with horizontal scaling configured.
func newHPAAIGWDP() *aigatewayv1alpha1.AIGatewayDataPlane {
	dp := newReconcileAIGWDP()
	dp.Spec.Deployment = &aigatewayv1alpha1.DeploymentOptions{
		Scaling: &aigatewayv1alpha1.Scaling{
			HorizontalScaling: &aigatewayv1alpha1.HorizontalScaling{
				MaxReplicas: 5,
			},
		},
	}
	return dp
}

func TestGenerateBaseDeployment_ReplicaGuard(t *testing.T) {
	aigatewaycp := testKonnectAIGateway()

	t.Run("sets replicas when HPA is not configured", func(t *testing.T) {
		replicas := int32(3)
		aigwdp := newReconcileAIGWDP()
		aigwdp.Spec.Deployment = &aigatewayv1alpha1.DeploymentOptions{Replicas: &replicas}

		d, err := GenerateBaseDeployment(logr.Discard(), aigwdp, resolvedTestCP(aigatewaycp), "kong/aigw:latest", "cert-secret", "", "", testConfig.Deployment, testConfig.Certificate.ChecksumAnnotation)
		require.NoError(t, err)
		require.NotNil(t, d.Spec.Replicas)
		assert.Equal(t, replicas, *d.Spec.Replicas)
	})

	t.Run("seeds HPA default of 1 when HPA is active without minReplicas", func(t *testing.T) {
		aigwdp := newHPAAIGWDP()
		aigwdp.Spec.Deployment.Replicas = new(int32(3))

		d, err := GenerateBaseDeployment(logr.Discard(), aigwdp, resolvedTestCP(aigatewaycp), "kong/aigw:latest", "cert-secret", "", "", testConfig.Deployment, testConfig.Certificate.ChecksumAnnotation)
		require.NoError(t, err)
		require.NotNil(t, d.Spec.Replicas)
		assert.Equal(t, int32(1), *d.Spec.Replicas)
	})

	t.Run("seeds replicas from minReplicas when HPA is active and minReplicas is set", func(t *testing.T) {
		aigwdp := newHPAAIGWDP()
		aigwdp.Spec.Deployment.Scaling.HorizontalScaling.MinReplicas = new(int32(2))

		d, err := GenerateBaseDeployment(logr.Discard(), aigwdp, resolvedTestCP(aigatewaycp), "kong/aigw:latest", "cert-secret", "", "", testConfig.Deployment, testConfig.Certificate.ChecksumAnnotation)
		require.NoError(t, err)
		require.NotNil(t, d.Spec.Replicas)
		assert.Equal(t, int32(2), *d.Spec.Replicas)
	})
}
