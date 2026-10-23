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

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestCheckKonnectAIGatewayEndpoints(t *testing.T) {
	const (
		// Public region: the server URL carries no path, the endpoints are bare hosts.
		publicServerURL = "https://us.api.konghq.tech"
		// Private region: the server URL carries a path, so do the endpoints.
		privateServerURL = "https://us.svc.konghq.tech/api"
		// Private region hosts without a path, and the same hosts with one.
		privateNoPath = "https://us.svc.konghq.tech"
		privateCPPath = "https://us.svc.konghq.tech/cp/2cf352e764"
		privateTPPath = "https://us.svc.konghq.tech/tp/2cf352e764"
		// Public endpoints: bare hosts.
		publicCP = "https://2cf352e764.us.cp.konghq.tech"
		publicTP = "https://2cf352e764.us.tp.konghq.tech:443"
	)

	tests := []struct {
		name            string
		serverURL       string
		configuration   string
		telemetry       string
		noEndpoints     bool
		wantErr         bool
		wantErrContains string
	}{
		{
			name:          "private server URL, private endpoints without a path",
			serverURL:     privateNoPath,
			configuration: privateNoPath,
			telemetry:     privateNoPath,
		},
		{
			name:          "private server URL, private endpoints with a path",
			serverURL:     privateNoPath,
			configuration: privateCPPath,
			telemetry:     privateTPPath,
			wantErr:       true,
			// The private rendering of the AI Gateway is not implemented yet
			// (#6105): a path in the endpoint would be rendered into an
			// address Kong rejects.
			wantErrContains: privateCPPath,
		},
		{
			name:            "private server URL, public endpoints",
			serverURL:       privateServerURL,
			configuration:   publicCP,
			telemetry:       publicTP,
			wantErr:         true,
			wantErrContains: publicCP,
		},
		{
			name:            "public server URL, private endpoints with a path",
			serverURL:       publicServerURL,
			configuration:   privateCPPath,
			telemetry:       privateTPPath,
			wantErr:         true,
			wantErrContains: privateCPPath,
		},
		{
			name:          "public server URL, private endpoints without a path",
			serverURL:     publicServerURL,
			configuration: privateNoPath,
			telemetry:     privateNoPath,
		},
		{
			name:        "endpoints nil",
			serverURL:   privateServerURL,
			noEndpoints: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			aigw := &konnectv1alpha1.KonnectAIGateway{}
			aigw.Status.ServerURL = tc.serverURL
			if !tc.noEndpoints {
				aigw.Status.Endpoints = &konnectv1alpha1.KonnectAIGatewayEndpoints{
					Configuration: tc.configuration,
					Telemetry:     tc.telemetry,
				}
			}

			err := checkKonnectAIGatewayEndpoints(aigw)
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErrContains)
		})
	}
}

// TestCheckKonnectAIGatewayEndpointsUnexpectedObject asserts the check fails closed
// on a control plane object of another kind instead of reporting usable endpoints.
func TestCheckKonnectAIGatewayEndpointsUnexpectedObject(t *testing.T) {
	err := checkKonnectAIGatewayEndpoints(&aigatewayv1alpha1.OnPremAIGateway{})
	require.ErrorContains(t, err, "unexpected control plane object")
}
