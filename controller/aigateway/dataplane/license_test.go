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

	"github.com/kong/go-kong/kong"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
)

// getterFunc adapts a function to the shared LicenseGetter interface.
type getterFunc func() mo.Option[kong.License]

func (f getterFunc) GetLicense() mo.Option[kong.License] {
	return f()
}

func Test_withLicenseEnvVar(t *testing.T) {
	// base is the wrapped BuildContainer: it returns a container with one env
	// var and no volumes.
	base := func(
		_ *aigatewayv1alpha1.AIGatewayDataPlane,
		_ shareddataplane.ResolvedControlPlane,
		_, _, _ string,
	) (corev1.Container, []corev1.Volume, error) {
		return corev1.Container{
			Name: "proxy",
			Env:  []corev1.EnvVar{{Name: "KONG_DATABASE", Value: "off"}},
		}, nil, nil
	}

	tests := []struct {
		name        string
		getter      shareddataplane.LicenseGetter
		wantLicense bool
	}{
		{
			name:        "license present: KONG_LICENSE_DATA appended",
			getter:      getterFunc(func() mo.Option[kong.License] { return mo.Some(kong.License{Payload: new(`{"license":{}}`)}) }),
			wantLicense: true,
		},
		{
			name:        "no license: env unchanged",
			getter:      getterFunc(func() mo.Option[kong.License] { return mo.None[kong.License]() }),
			wantLicense: false,
		},
		{
			name:        "license without payload: env unchanged",
			getter:      getterFunc(func() mo.Option[kong.License] { return mo.Some(kong.License{}) }),
			wantLicense: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			container, _, err := withLicenseEnvVar(base, tc.getter)(nil, shareddataplane.ResolvedControlPlane{}, "", "", "")
			require.NoError(t, err)

			// The base env var must always be intact.
			assert.Contains(t, container.Env, corev1.EnvVar{Name: "KONG_DATABASE", Value: "off"})

			licenseEnv := -1
			for i, env := range container.Env {
				if env.Name == EnvKongLicenseData {
					licenseEnv = i
				}
			}
			if tc.wantLicense {
				require.NotEqual(t, -1, licenseEnv)
				assert.JSONEq(t, `{"license":{}}`, container.Env[licenseEnv].Value)
			} else {
				assert.Equal(t, -1, licenseEnv)
			}
		})
	}
}
