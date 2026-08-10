package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestHandleConfigStoreRef(t *testing.T) {
	configStoreRef := &configurationv1alpha1.KonnectConfigStoreRef{
		Name:      "tls-cert-keys",
		Namespace: "ns",
	}

	programmedConfigStore := func() *konnectv1alpha1.KonnectConfigStore {
		cs := &konnectv1alpha1.KonnectConfigStore{
			ObjectMeta: metav1.ObjectMeta{Name: "tls-cert-keys", Namespace: "ns"},
		}
		cs.Status.ID = "cs-id"
		return cs
	}
	notProgrammedConfigStore := &konnectv1alpha1.KonnectConfigStore{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-cert-keys", Namespace: "ns"},
	}

	// KongVault is cluster-scoped, hence the empty `from` namespace.
	grant := func(mutate ...func(*configurationv1alpha1.KongReferenceGrant)) *configurationv1alpha1.KongReferenceGrant {
		g := &configurationv1alpha1.KongReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{Name: "allow-kongvault", Namespace: "ns"},
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{{
					Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
					Kind:      configurationv1alpha1.Kind("KongVault"),
					Namespace: configurationv1alpha1.Namespace(""),
				}},
				To: []configurationv1alpha1.ReferenceGrantTo{{
					Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
					Kind:  configurationv1alpha1.Kind(configurationv1alpha1.KonnectConfigStoreKind),
				}},
			},
		}
		for _, m := range mutate {
			m(g)
		}
		return g
	}

	// KongVault is cluster-scoped, so it carries no namespace of its own.
	vault := func(mutate ...func(*configurationv1alpha1.KongVault)) *configurationv1alpha1.KongVault {
		v := &configurationv1alpha1.KongVault{
			ObjectMeta: metav1.ObjectMeta{Name: "certvault"},
			Spec: configurationv1alpha1.KongVaultSpec{
				Backend: "konnect",
				Prefix:  "certvault",
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
						Name:      "cp-1",
						Namespace: "ns",
					},
				},
				ConfigStoreRef: configStoreRef,
			},
			Status: configurationv1alpha1.KongVaultStatus{
				Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
					ControlPlaneID: "cp-id",
				},
			},
		}
		for _, m := range mutate {
			m(v)
		}
		return v
	}

	testCases := []struct {
		name                string
		vault               *configurationv1alpha1.KongVault
		objects             []client.Object
		expectStop          bool
		expectErrorContains string
		expectCondition     *metav1.Condition
	}{
		{
			name: "no configStoreRef leaves the entity untouched",
			vault: vault(func(v *configurationv1alpha1.KongVault) {
				v.Spec.ConfigStoreRef = nil
			}),
			expectStop: false,
		},
		{
			name:       "programmed config store sets the condition to True and continues",
			vault:      vault(),
			objects:    []client.Object{grant(), programmedConfigStore()},
			expectStop: false,
			expectCondition: &metav1.Condition{
				Type:   konnectv1alpha1.ConfigStoreRefValidConditionType,
				Status: metav1.ConditionTrue,
				Reason: konnectv1alpha1.ConfigStoreRefReasonValid,
			},
		},
		{
			name: "a programmed config store does not update status during deletion",
			vault: vault(func(v *configurationv1alpha1.KongVault) {
				v.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
				v.Finalizers = []string{KonnectCleanupFinalizer}
			}),
			objects:    []client.Object{programmedConfigStore()},
			expectStop: false,
		},
		{
			name:       "missing config store stops reconciliation with an Invalid condition",
			vault:      vault(),
			objects:    []client.Object{grant()},
			expectStop: true,
			expectCondition: &metav1.Condition{
				Type:   konnectv1alpha1.ConfigStoreRefValidConditionType,
				Status: metav1.ConditionFalse,
				Reason: konnectv1alpha1.ConfigStoreRefReasonInvalid,
			},
		},
		{
			name:       "not programmed config store stops reconciliation with a NotProgrammed condition",
			vault:      vault(),
			objects:    []client.Object{grant(), notProgrammedConfigStore},
			expectStop: true,
			expectCondition: &metav1.Condition{
				Type:   konnectv1alpha1.ConfigStoreRefValidConditionType,
				Status: metav1.ConditionFalse,
				Reason: konnectv1alpha1.ConfigStoreRefReasonNotProgrammed,
			},
		},
		{
			name: "configStoreRef conflicting with spec.config stops reconciliation as Invalid",
			vault: vault(func(v *configurationv1alpha1.KongVault) {
				v.Spec.Config = apiextensionsv1.JSON{Raw: []byte(`{"config_store_id":"manually-copied-id"}`)}
			}),
			objects:    []client.Object{grant(), programmedConfigStore()},
			expectStop: true,
			expectCondition: &metav1.Condition{
				Type:   konnectv1alpha1.ConfigStoreRefValidConditionType,
				Status: metav1.ConditionFalse,
				Reason: konnectv1alpha1.ConfigStoreRefReasonInvalid,
			},
		},
		{
			name:       "no KongReferenceGrant stops reconciliation with a RefNotPermitted condition",
			vault:      vault(),
			objects:    []client.Object{programmedConfigStore()},
			expectStop: true,
			expectCondition: &metav1.Condition{
				Type:   konnectv1alpha1.ConfigStoreRefValidConditionType,
				Status: metav1.ConditionFalse,
				Reason: konnectv1alpha1.ConfigStoreRefReasonRefNotPermitted,
			},
		},
		{
			name:  "a KongReferenceGrant in another namespace does not permit the reference",
			vault: vault(),
			objects: []client.Object{
				grant(func(g *configurationv1alpha1.KongReferenceGrant) {
					g.Namespace = "other-ns"
				}),
				programmedConfigStore(),
			},
			expectStop: true,
			expectCondition: &metav1.Condition{
				Type:   konnectv1alpha1.ConfigStoreRefValidConditionType,
				Status: metav1.ConditionFalse,
				Reason: konnectv1alpha1.ConfigStoreRefReasonRefNotPermitted,
			},
		},
		{
			name:  "a KongReferenceGrant naming another KonnectConfigStore does not permit the reference",
			vault: vault(),
			objects: []client.Object{
				grant(func(g *configurationv1alpha1.KongReferenceGrant) {
					g.Spec.To[0].Name = new(configurationv1alpha1.ObjectName("another-config-store"))
				}),
				programmedConfigStore(),
			},
			expectStop: true,
			expectCondition: &metav1.Condition{
				Type:   konnectv1alpha1.ConfigStoreRefValidConditionType,
				Status: metav1.ConditionFalse,
				Reason: konnectv1alpha1.ConfigStoreRefReasonRefNotPermitted,
			},
		},
		{
			name:  "a KongReferenceGrant naming this KonnectConfigStore permits the reference",
			vault: vault(),
			objects: []client.Object{
				grant(func(g *configurationv1alpha1.KongReferenceGrant) {
					g.Spec.To[0].Name = new(configurationv1alpha1.ObjectName(configStoreRef.Name))
				}),
				programmedConfigStore(),
			},
			expectStop: false,
			expectCondition: &metav1.Condition{
				Type:   konnectv1alpha1.ConfigStoreRefValidConditionType,
				Status: metav1.ConditionTrue,
				Reason: konnectv1alpha1.ConfigStoreRefReasonValid,
			},
		},
		{
			name: "an unresolvable ref does not block deletion",
			vault: vault(func(v *configurationv1alpha1.KongVault) {
				v.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
				v.Finalizers = []string{KonnectCleanupFinalizer}
			}),
			expectStop: false,
		},
		{
			name: "a removed KongReferenceGrant does not block deletion",
			vault: vault(func(v *configurationv1alpha1.KongVault) {
				v.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
				v.Finalizers = []string{KonnectCleanupFinalizer}
			}),
			objects:    []client.Object{programmedConfigStore()},
			expectStop: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
			require.NoError(t, konnectv1alpha1.AddToScheme(scheme))
			require.NoError(t, konnectv1alpha2.AddToScheme(scheme))
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.vault).
				WithObjects(tc.objects...).
				WithStatusSubresource(tc.vault).
				Build()
			require.NoError(t, fakeClient.Status().Update(t.Context(), tc.vault))

			// The KongReferenceGrant lookup matches on the referring object's GVK,
			// which the controller-runtime client populates but the fake one doesn't.
			tc.vault.GetObjectKind().SetGroupVersionKind(
				configurationv1alpha1.GroupVersion.WithKind("KongVault"),
			)

			res, stop, _, err := handleConfigStoreRef(t.Context(), fakeClient, tc.vault)

			if tc.expectErrorContains != "" {
				require.ErrorContains(t, err, tc.expectErrorContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{}, res)
			assert.Equal(t, tc.expectStop, stop)

			updated := &configurationv1alpha1.KongVault{}
			require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(tc.vault), updated))

			cond, found := k8sutils.GetCondition(konnectv1alpha1.ConfigStoreRefValidConditionType, updated)
			if tc.expectCondition == nil {
				assert.False(t, found, "no ConfigStoreRefValid condition expected, got %v", cond)
				return
			}
			require.True(t, found, "ConfigStoreRefValid condition not set")
			assert.Equal(t, tc.expectCondition.Status, cond.Status)
			assert.Equal(t, tc.expectCondition.Reason, cond.Reason)
			assert.NotEmpty(t, cond.Message, "the condition must explain itself to the user")
		})
	}
}
