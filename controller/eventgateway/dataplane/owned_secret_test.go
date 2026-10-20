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
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	pkgconsts "github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// automaticFallbackUnexpected returns a resolveAutomatic fallback that fails
// the test if called.
func automaticFallbackUnexpected(t *testing.T) func(context.Context, *eventgatewayv1alpha1.KegDataPlane) (op.Result, *corev1.Secret, error) {
	return func(context.Context, *eventgatewayv1alpha1.KegDataPlane) (op.Result, *corev1.Secret, error) {
		t.Fatal("automatic provisioning must not be used")
		return op.Noop, nil, nil
	}
}

func Test_resolveCertificateSecret(t *testing.T) {
	scheme := managerscheme.Get()

	t.Run("Manual: returns the referenced Secret as-is", func(t *testing.T) {
		egdp := newReconcileEGDPManualCert()
		secret := manualCertSecret(true)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(egdp, secret).Build()

		res, got, err := resolveCertificateSecret(t.Context(), cl, egdp, shareddataplane.ResolvedControlPlane{}, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		require.NotNil(t, got)
		assert.Equal(t, manualCertSecretName, got.Name)
		assertCondition(t, egdp,
			eventgatewayv1alpha1.CertificateProvisionedType,
			metav1.ConditionTrue,
			eventgatewayv1alpha1.CertificateProvisionedReason,
		)
	})

	for name, cs := range map[string]*eventgatewayv1alpha1.CertificateSecret{
		"unset":     nil,
		"Automatic": {Provisioning: new(eventgatewayv1alpha1.AutomaticCertificateProvisioning)},
	} {
		t.Run(name+": falls back to automatic provisioning", func(t *testing.T) {
			egdp := newReconcileEGDP()
			egdp.Spec.CertificateSecret = cs
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(egdp).Build()

			called := false
			_, _, err := resolveCertificateSecret(t.Context(), cl, egdp, shareddataplane.ResolvedControlPlane{},
				func(context.Context, *eventgatewayv1alpha1.KegDataPlane) (op.Result, *corev1.Secret, error) {
					called = true
					return op.Noop, nil, nil
				})
			require.NoError(t, err)
			assert.True(t, called)
		})
	}
}

func Test_cleanupStaleCertificates(t *testing.T) {
	scheme := managerscheme.Get()
	const checksum = "abcdef1234567890"

	ownedCert := func(name string, owner *eventgatewayv1alpha1.KegDataPlane) *configurationv1alpha1.EventGatewayDataPlaneCertificate {
		cert := &configurationv1alpha1.EventGatewayDataPlaneCertificate{Namespace: owner.Namespace, Name: name}
		k8sutils.SetOwnerForObject(cert, owner)
		return cert
	}
	automaticSecret := func(owner *eventgatewayv1alpha1.KegDataPlane) *corev1.Secret {
		s := &corev1.Secret{
			Namespace: owner.Namespace, Name: "automatic-cert",
			Labels: map[string]string{
				pkgconsts.SecretProvisioningLabelKey:         pkgconsts.SecretProvisioningAutomaticLabelValue,
				pkgconsts.SecretKEGDataPlaneCertificateLabel: "true",
			},
		}
		k8sutils.SetOwnerForObject(s, owner)
		return s
	}
	exists := func(t *testing.T, cl client.Client, obj client.Object, name string) bool {
		t.Helper()
		err := cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: name}, obj)
		if err != nil {
			require.True(t, apierrors.IsNotFound(err))
		}
		return err == nil
	}

	t.Run("Automatic: removes stale certificates, keeps the current one and the automatic Secret", func(t *testing.T) {
		egdp := newReconcileEGDP()
		current := shareddataplane.CertEntityName(egdp.Name, checksum)
		other := newReconcileEGDP()
		other.Name, other.UID = "other-dp", "other-uid"
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			egdp,
			ownedCert(current, egdp),
			ownedCert(egdp.Name, egdp), // legacy, fixed-name certificate
			ownedCert("other-dp-cert", other),
			automaticSecret(egdp),
		).Build()

		require.NoError(t, cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, shareddataplane.ResolvedControlPlane{}, checksum))

		assert.True(t, exists(t, cl, &configurationv1alpha1.EventGatewayDataPlaneCertificate{}, current))
		assert.False(t, exists(t, cl, &configurationv1alpha1.EventGatewayDataPlaneCertificate{}, egdp.Name))
		assert.True(t, exists(t, cl, &configurationv1alpha1.EventGatewayDataPlaneCertificate{}, "other-dp-cert"),
			"certificates owned by another KegDataPlane must be left alone")
		assert.True(t, exists(t, cl, &corev1.Secret{}, "automatic-cert"))
	})

	t.Run("Manual: also removes the operator-provisioned automatic Secret", func(t *testing.T) {
		egdp := newReconcileEGDPManualCert()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			egdp,
			manualCertSecret(true),
			automaticSecret(egdp),
		).Build()

		require.NoError(t, cleanupStaleCertificates(t.Context(), cl, logr.Discard(), egdp, shareddataplane.ResolvedControlPlane{}, checksum))

		assert.False(t, exists(t, cl, &corev1.Secret{}, "automatic-cert"))
		assert.True(t, exists(t, cl, &corev1.Secret{}, manualCertSecretName), "the user-owned Secret must never be touched")
	})
}
