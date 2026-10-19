package konnect

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestCreateInputs(t *testing.T) {
	t.Run("EventGatewayStaticKey", func(t *testing.T) {
		testCreateInputs(t, &configurationv1alpha1.EventGatewayStaticKey{
			APIVersion: configurationv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayStaticKey",
			Spec: configurationv1alpha1.EventGatewayStaticKeySpec{
				APISpec: configurationv1alpha1.EventGatewayStaticKeyAPISpec{Name: "key"},
			},
		}, func(e *configurationv1alpha1.EventGatewayStaticKey, value string, secret bool) {
			e.Spec.APISpec.Value = configurationv1alpha1.SensitiveDataSource{
				Type: configurationv1alpha1.SensitiveDataSourceTypeInline, Value: new(value),
			}
			if secret {
				e.Spec.APISpec.Value = configurationv1alpha1.SensitiveDataSource{
					Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: "inputs", Key: "value"},
				}
			}
		}, func(e *configurationv1alpha1.EventGatewayStaticKey) { e.Spec.APISpec.Description = "changed" })
	})
	t.Run("AIGatewayDataPlaneCertificate", func(t *testing.T) {
		testCreateInputs(t, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			Kind:       "AIGatewayDataPlaneCertificate",
			Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
				APISpec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{Title: "certificate"},
			},
		}, func(e *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate, value string, secret bool) {
			e.Spec.APISpec.Cert = aiCreateInputsSource(value, secret)
		}, func(e *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate) { e.Spec.APISpec.Description = "changed" })
	})
	t.Run("AIGatewayConsumerCredential", func(t *testing.T) {
		testCreateInputs(t, &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			Kind:       "AIGatewayConsumerCredential",
			Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
				APISpec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialAPISpec{
					Name: "credential", DisplayName: "credential", Type: "api-key",
				},
			},
		}, func(e *aiconfigurationv1alpha1.AIGatewayConsumerCredential, value string, secret bool) {
			e.Spec.APISpec.APIKey = aiCreateInputsSource(value, secret)
		}, func(e *aiconfigurationv1alpha1.AIGatewayConsumerCredential) { e.Spec.APISpec.Ttl = 3600 })
	})
}

func aiCreateInputsSource(value string, secret bool) aiconfigurationv1alpha1.SensitiveDataSource {
	if secret {
		return aiconfigurationv1alpha1.SensitiveDataSource{
			Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
			SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: "inputs", Key: "value"},
		}
	}
	return aiconfigurationv1alpha1.SensitiveDataSource{Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeInline, Value: new(value)}
}

func testCreateInputs[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	t *testing.T, fixture TEnt, setSource func(TEnt, string, bool), changeSpec func(TEnt),
) {
	t.Helper()
	for _, tc := range []struct {
		name       string
		secret     bool
		baseline   bool
		change     func(*testing.T, TEnt, client.Client, *corev1.Secret)
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{name: "unchanged", wantStatus: metav1.ConditionTrue, wantReason: konnectv1alpha1.CreateInputsReasonUnchanged},
		{name: "legacy baseline", baseline: true, wantStatus: metav1.ConditionTrue, wantReason: konnectv1alpha1.CreateInputsReasonBaselineEstablished},
		{name: "inline change", change: func(_ *testing.T, e TEnt, _ client.Client, _ *corev1.Secret) { setSource(e, "rotated-value", false) },
			wantStatus: metav1.ConditionFalse, wantReason: konnectv1alpha1.CreateInputsReasonValueChanged},
		{name: "other spec change", change: func(_ *testing.T, e TEnt, _ client.Client, _ *corev1.Secret) { changeSpec(e) },
			wantStatus: metav1.ConditionFalse, wantReason: konnectv1alpha1.CreateInputsReasonValueChanged},
		{name: "Secret change", secret: true, change: func(t *testing.T, _ TEnt, cl client.Client, s *corev1.Secret) {
			s.Data["value"] = []byte("rotated-value")
			require.NoError(t, cl.Update(t.Context(), s))
		}, wantStatus: metav1.ConditionFalse, wantReason: konnectv1alpha1.CreateInputsReasonValueChanged},
		{name: "unrelated Secret key", secret: true, change: func(t *testing.T, _ TEnt, cl client.Client, s *corev1.Secret) {
			s.Data["unrelated"] = []byte("unrelated-value")
			require.NoError(t, cl.Update(t.Context(), s))
		}, wantStatus: metav1.ConditionTrue, wantReason: konnectv1alpha1.CreateInputsReasonUnchanged},
		{name: "same value from inline", secret: true, change: func(_ *testing.T, e TEnt, _ client.Client, _ *corev1.Secret) { setSource(e, "original-value", false) },
			wantStatus: metav1.ConditionTrue, wantReason: konnectv1alpha1.CreateInputsReasonUnchanged},
		{name: "operator metadata", change: func(_ *testing.T, e TEnt, _ client.Client, _ *corev1.Secret) {
			e.SetLabels(map[string]string{"new": "label"})
		},
			wantStatus: metav1.ConditionTrue, wantReason: konnectv1alpha1.CreateInputsReasonUnchanged},
		{name: "missing Secret", secret: true, change: func(t *testing.T, _ TEnt, cl client.Client, s *corev1.Secret) {
			require.NoError(t, cl.Delete(t.Context(), s))
		}, wantStatus: metav1.ConditionUnknown, wantReason: konnectv1alpha1.CreateInputsReasonUnavailable},
		{name: "missing key", secret: true, change: func(t *testing.T, _ TEnt, cl client.Client, s *corev1.Secret) {
			delete(s.Data, "value")
			require.NoError(t, cl.Update(t.Context(), s))
		}, wantStatus: metav1.ConditionUnknown, wantReason: konnectv1alpha1.CreateInputsReasonUnavailable},
		{name: "legacy missing Secret", secret: true, baseline: true, change: func(t *testing.T, _ TEnt, cl client.Client, s *corev1.Secret) {
			require.NoError(t, cl.Delete(t.Context(), s))
		}, wantStatus: metav1.ConditionUnknown, wantReason: konnectv1alpha1.CreateInputsReasonUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			ent := fixture.DeepCopyObject().(TEnt)
			ent.SetName("entity")
			ent.SetNamespace("default")
			ent.SetGeneration(2)
			ent.SetKonnectID("konnect-id")
			ops.SetKonnectEntityProgrammedConditionTrue(ent)
			setSource(ent, "original-value", tc.secret)
			secret := &corev1.Secret{Name: "inputs", Namespace: "default", Data: map[string][]byte{"value": []byte("original-value")}}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(ent, secret).WithStatusSubresource(ent).Build()
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(ent), ent))
			inputs := any(ent).(createInputsAware)
			hash, err := inputs.CalculateCreateInputsHash(ctx, cl)
			require.NoError(t, err)
			if !tc.baseline {
				inputs.SetCreateInputsHash(hash)
				require.NoError(t, cl.Status().Update(ctx, ent))
			}
			if tc.change != nil {
				tc.change(t, ent, cl, secret)
			}
			res, stop, err := handleSecretRef(ctx, cl, ent)
			require.NoError(t, err)
			require.False(t, stop)
			require.True(t, res.IsZero())
			res, err = handleCreateInputs(ctx, cl, ent)
			require.NoError(t, err)
			require.True(t, res.IsZero())
			cond, ok := k8sutils.GetCondition(konnectv1alpha1.CreateInputsAppliedConditionType, ent)
			require.True(t, ok)
			assert.Equal(t, tc.wantStatus, cond.Status)
			assert.Equal(t, tc.wantReason, cond.Reason)
			assert.Equal(t, ent.GetGeneration(), cond.ObservedGeneration)
			assert.NotContains(t, cond.Message, "original-value")
			assert.NotContains(t, cond.Message, "rotated-value")
			if tc.baseline && tc.wantStatus == metav1.ConditionUnknown {
				assert.Empty(t, inputs.GetCreateInputsHash())
			} else {
				assert.Equal(t, hash, inputs.GetCreateInputsHash(), "drift must not replace the creation fingerprint")
			}
			programmed, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, ent)
			require.True(t, ok)
			assert.Equal(t, metav1.ConditionTrue, programmed.Status)
			if tc.wantStatus == metav1.ConditionFalse {
				if _, ok := any(ent).(*configurationv1alpha1.EventGatewayStaticKey); ok {
					assert.Contains(t, cond.Message, "Create a new EventGatewayStaticKey")
				} else {
					assert.Contains(t, cond.Message, "Recreate the Kubernetes object")
				}
				setSource(ent, "original-value", false)
				if tc.name == "other spec change" {
					return
				}
				_, err := handleCreateInputs(ctx, cl, ent)
				require.NoError(t, err)
				cond, _ := k8sutils.GetCondition(konnectv1alpha1.CreateInputsAppliedConditionType, ent)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
			}
		})
	}
}

func TestCreateInputsConditionDoesNotBlockProgrammed(t *testing.T) {
	ent := &configurationv1alpha1.EventGatewayStaticKey{Name: "key", Namespace: "default"}
	ops.SetKonnectEntityProgrammedConditionTrue(ent)
	k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(
		konnectv1alpha1.CreateInputsAppliedConditionType, metav1.ConditionFalse,
		konnectv1alpha1.CreateInputsReasonValueChanged, "changed", 0,
	), ent)
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(ent).WithStatusSubresource(ent).Build()
	_, err := patchWithProgrammedStatusConditionBasedOnOtherConditions(t.Context(), cl, ent)
	require.NoError(t, err)
	programmed, _ := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, ent)
	assert.Equal(t, metav1.ConditionTrue, programmed.Status)
}

func TestRecoverPendingCreateInputsHash(t *testing.T) {
	for _, hash := range []string{"sha256:actual-request", ""} {
		for _, id := range []string{"", "old-id", "new-id"} {
			t.Run("hash "+hash+"/previous ID "+id, func(t *testing.T) {
				ctx := t.Context()
				ent := &configurationv1alpha1.EventGatewayStaticKey{Name: "key", Namespace: "default"}
				ent.Spec.APISpec.Name = "key"
				ent.Spec.APISpec.Value = configurationv1alpha1.SensitiveDataSource{
					Type: configurationv1alpha1.SensitiveDataSourceTypeInline, Value: new("current-value"),
				}
				ent.SetKonnectID(id)
				ent.SetCreateInputsHash("sha256:previous-request")
				ops.SetKonnectEntityCreateInputsAppliedConditionTrue(ent)
				failedWrites := 2
				cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(ent).WithStatusSubresource(ent).
					WithInterceptorFuncs(interceptor.Funcs{
						SubResourcePatch: func(ctx context.Context, cl client.Client, subResource string, obj client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
							if failedWrites > 0 {
								failedWrites--
								return errors.New("status write failed")
							}
							return cl.SubResource(subResource).Patch(ctx, obj, p, opts...)
						},
					}).Build()
				r := NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayStaticKey](nil, logging.DevelopmentMode, cl)
				key := client.ObjectKeyFromObject(ent)
				r.pendingKonnectIDs.Store(key, pendingKonnectEntityStatus{ID: "new-id", CreateInputsHash: hash})
				for attempt := range 3 {
					require.NoError(t, cl.Get(ctx, key, ent))
					_, stop, err := r.reconcilePendingKonnectID(ctx, ent)
					require.True(t, stop)
					if attempt < 2 {
						require.ErrorContains(t, err, "status write failed")
					} else {
						require.NoError(t, err)
					}
					_, pending := r.pendingKonnectIDs.Get(key)
					assert.True(t, pending, "recovery must survive repeated status write failures")
				}
				require.NoError(t, cl.Get(ctx, key, ent))
				assert.Equal(t, "new-id", ent.GetKonnectID())
				assert.Equal(t, hash, ent.GetCreateInputsHash())
				_, hasCondition := k8sutils.GetCondition(konnectv1alpha1.CreateInputsAppliedConditionType, ent)
				assert.Equal(t, hash != "", hasCondition)

				for _, staleID := range []string{id, "old-id"} {
					stale := ent.DeepCopy()
					stale.SetKonnectID(staleID)
					r.restorePendingKonnectIDForDeletion(stale)
					assert.Equal(t, "new-id", stale.GetKonnectID(), "deletion must target the recreated entity before its status is persisted")
				}

				_, stop, err := r.reconcilePendingKonnectID(ctx, ent)
				require.NoError(t, err)
				require.False(t, stop)
				_, pending := r.pendingKonnectIDs.Get(key)
				assert.False(t, pending, "recovery must finish before establishing a baseline")
				if hash == "" {
					_, err := handleCreateInputs(ctx, cl, ent)
					require.NoError(t, err)
					assert.NotEmpty(t, ent.GetCreateInputsHash())
					condition, ok := k8sutils.GetCondition(konnectv1alpha1.CreateInputsAppliedConditionType, ent)
					require.True(t, ok)
					assert.Equal(t, konnectv1alpha1.CreateInputsReasonBaselineEstablished, condition.Reason)
					baseline := ent.GetCreateInputsHash()
					_, stop, err := r.reconcilePendingKonnectID(ctx, ent)
					require.NoError(t, err)
					assert.False(t, stop)
					assert.Equal(t, baseline, ent.GetCreateInputsHash(), "pending recovery must not reset the new baseline")
				}
			})
		}
	}
}
