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

package onprem

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestMapAIGatewayDataPlaneToOnPremAIGateway(t *testing.T) {
	dpWithRef := func(
		refType aigatewayv1alpha1.ControlPlaneRefType,
		ref *aigatewayv1alpha1.NamespacedRef,
	) *aigatewayv1alpha1.AIGatewayDataPlane {
		return &aigatewayv1alpha1.AIGatewayDataPlane{
			Namespace: "default",
			Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
				ControlPlaneRef: &aigatewayv1alpha1.ControlPlaneRef{
					Type:                refType,
					OnPremNamespacedRef: ref,
				},
			},
		}
	}
	tests := []struct {
		name string
		dp   *aigatewayv1alpha1.AIGatewayDataPlane
		want []reconcile.Request
	}{
		{
			name: "nil controlPlaneRef",
			dp:   &aigatewayv1alpha1.AIGatewayDataPlane{},
			want: nil,
		},
		{
			name: "wrong ref type",
			dp:   dpWithRef(aigatewayv1alpha1.ControlPlaneRefType("konnectNamespacedRef"), nil),
			want: nil},
		{
			name: "missing onprem ref",
			dp:   dpWithRef(aigatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef, nil),
			want: nil,
		},
		{
			name: "valid ref",
			dp:   dpWithRef(aigatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef, &aigatewayv1alpha1.NamespacedRef{Name: "cp"}),
			want: []reconcile.Request{
				{Namespace: "default", Name: "cp"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapAIGatewayDataPlaneToOnPremAIGateway(context.Background(), tt.dp)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestClientCertSecretPredicate(t *testing.T) {
	clientCertSecret := func(labels map[string]string) *corev1.Secret {
		return &corev1.Secret{
			Namespace: "default",
			Name:      "cert",
			Labels:    labels,
		}
	}

	tests := []struct {
		name   string
		secret *corev1.Secret
		want   bool
	}{
		{
			name: "client certificate Secret matches",
			secret: clientCertSecret(map[string]string{
				consts.SecretOnPremAIGatewayAdminClientCertificateLabel: "true",
			}),
			want: true,
		},
		{
			name:   "secret without the client certificate label does not match",
			secret: clientCertSecret(map[string]string{"konghq.com/secret": "true"}),
			want:   false,
		},
		{
			name:   "secret without labels does not match",
			secret: clientCertSecret(nil),
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := clientCertSecretPredicate()
			require.Equal(t, tt.want, p.Create(event.TypedCreateEvent[client.Object]{Object: tt.secret}))
			require.Equal(t, tt.want, p.Update(event.TypedUpdateEvent[client.Object]{ObjectNew: tt.secret}))
			require.Equal(t, tt.want, p.Delete(event.TypedDeleteEvent[client.Object]{Object: tt.secret}))
		})
	}
}
