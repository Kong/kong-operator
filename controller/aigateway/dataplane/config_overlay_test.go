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
	corev1 "k8s.io/api/core/v1"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// aigwdpWithOverlay returns an AIGatewayDataPlane whose pod template sets the
// given env vars on the gateway container.
func aigwdpWithOverlay(envVarNames []string) *aigatewayv1alpha1.AIGatewayDataPlane {
	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{}
	if len(envVarNames) == 0 {
		return aigwdp
	}
	env := make([]corev1.EnvVar, 0, len(envVarNames))
	for _, name := range envVarNames {
		env = append(env, corev1.EnvVar{Name: name, Value: "overlay"})
	}
	aigwdp.Spec.Deployment = &aigatewayv1alpha1.DeploymentOptions{
		PodTemplateSpec: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: consts.AIGatewayDataPlaneContainerName,
					Env:  env,
				}},
			},
		},
	}
	return aigwdp
}
