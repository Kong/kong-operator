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

package gwadminsync

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
)

func TestAPIPodsRunning(t *testing.T) {
	key := client.ObjectKey{Namespace: ns, Name: gwadminsynctest.GWAdminName}

	f := gwadminsynctest.NewGWAdmin(t, ns, gwadminsynctest.Endpoint("gw-admin-0", "10.0.0.1", true))
	running, err := apiPodsRunning(t.Context(), newFakeClient(t, interceptor.Funcs{}, f.EndpointSlice), key)
	require.NoError(t, err)
	assert.True(t, running)

	f = gwadminsynctest.NewGWAdmin(t, ns)
	running, err = apiPodsRunning(t.Context(), newFakeClient(t, interceptor.Funcs{}, f.EndpointSlice), key)
	require.NoError(t, err)
	assert.False(t, running, "an EndpointSlice without endpoints publishes no pod")

	f = gwadminsynctest.NewGWAdmin(t, ns, gwadminsynctest.Endpoint("gw-admin-0", "10.0.0.1", false))
	delete(f.EndpointSlice.Labels, "gateway-operator.konghq.com/gwadmin-service-role")
	running, err = apiPodsRunning(t.Context(), newFakeClient(t, interceptor.Funcs{}, f.EndpointSlice), key)
	require.NoError(t, err)
	assert.False(t, running, "only the EndpointSlices labelled as those of the API Service of the GWAdmin count")

	_, err = apiPodsRunning(t.Context(), newFakeClient(t, interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return errors.New("unavailable")
		},
	}), key)
	require.ErrorContains(t, err, "unavailable")
}

func TestGWAdminPods(t *testing.T) {
	slice := func(name string, addressType discoveryv1.AddressType, portName string, endpoints ...discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
		return &discoveryv1.EndpointSlice{
			Name: name, Namespace: ns, Labels: gwadminsynctest.EndpointSliceLabels(gwadminsynctest.APIServiceName),
			AddressType: addressType,
			Ports:       []discoveryv1.EndpointPort{{Name: new(portName), Port: new(int32(gwadminsynctest.APIPort))}},
			Endpoints:   endpoints,
		}
	}
	endpoint := gwadminsynctest.Endpoint
	// Ready, as the API Service publishes the pods not Ready, but not
	// serving.
	notReady := gwadminsynctest.NotServing(endpoint("gw-admin-1", "10.0.0.2", false))
	notReady.TargetRef.UID = "uid-1"
	notServing := endpoint("gw-admin-7", "10.0.0.8", true)
	notServing.Conditions.Serving = new(false)
	notAPod := discoveryv1.Endpoint{Addresses: []string{"10.0.0.9"}}
	notManaged := slice("gw-admin-api-manual", discoveryv1.AddressTypeIPv4, "api", endpoint("gw-admin-4", "10.0.0.5", false))
	delete(notManaged.Labels, discoveryv1.LabelManagedBy)
	otherService := slice("gw-admin-cp-abcde", discoveryv1.AddressTypeIPv4, "api", endpoint("gw-admin-5", "10.0.0.6", false))
	otherService.Labels = gwadminsynctest.EndpointSliceLabels("gw-admin-cp")
	gw := gwadminsynctest.NewGWAdmin(t, ns).GWAdmin
	cl := newFakeClient(t, interceptor.Funcs{},
		// Sorted before the IPv4 one by name: the IPv4 address is still used.
		slice("gw-admin-api-a6", discoveryv1.AddressTypeIPv6, "api", endpoint("gw-admin-0", "fd00::1", false), endpoint("gw-admin-6", "fd00::7", false)),
		slice("gw-admin-api-v4", discoveryv1.AddressTypeIPv4, "api",
			notReady, endpoint("gw-admin-0", "10.0.0.1", false),
			endpoint("gw-admin-2", "10.0.0.3", true), notServing, notAPod, discoveryv1.Endpoint{}),
		slice("gw-admin-api-other", discoveryv1.AddressTypeIPv4, "other", endpoint("gw-admin-3", "10.0.0.4", false)),
		notManaged, otherService,
	)

	pods, terminating, err := gwAdminPods(t.Context(), cl, gw)
	require.NoError(t, err)
	assert.Equal(t, []Pod{
		{Name: "gw-admin-0", UID: "gw-admin-0-uid", Address: "10.0.0.1:8083", Serving: true},
		{Name: "gw-admin-1", UID: "uid-1", Address: "10.0.0.2:8083"},
		{Name: "gw-admin-6", UID: "gw-admin-6-uid", Address: "[fd00::7]:8083", Serving: true},
	}, pods, "a pod of a dual-stack Service is returned once, on its IPv4 address; a pod shutting down, of another port, "+
		"of another Service or of an EndpointSlice the EndpointSlice controller doesn't manage isn't, and neither is an endpoint that isn't a pod; "+
		"a pod not serving is, with its readiness")
	assert.Equal(t, []Pod{{Name: "gw-admin-2", UID: "gw-admin-2-uid", Address: "10.0.0.3:8083", Serving: true}}, terminating,
		"a terminating pod is returned apart while it serves")

	unknown := gwadminsynctest.Endpoint("gw-admin-0", "10.0.0.1", false)
	unknown.Conditions.Serving = nil
	unknownTerminating := gwadminsynctest.Endpoint("gw-admin-1", "10.0.0.2", true)
	unknownTerminating.Conditions.Serving = nil
	unknownNotReady := gwadminsynctest.Endpoint("gw-admin-2", "10.0.0.3", false)
	unknownNotReady.Conditions.Serving, unknownNotReady.Conditions.Ready = nil, new(false)
	pods, terminating, err = gwAdminPods(t.Context(), newFakeClient(t, interceptor.Funcs{},
		slice("gw-admin-api-v4", discoveryv1.AddressTypeIPv4, "api", unknown, unknownTerminating, unknownNotReady)), gw)
	require.NoError(t, err)
	require.Len(t, pods, 2)
	assert.True(t, pods[0].Serving, "an unknown serving falls back to ready")
	assert.False(t, pods[1].Serving, "an unknown serving falls back to ready")
	assert.Len(t, terminating, 1, "a terminating pod whose serving is unknown falls back to ready")
}
