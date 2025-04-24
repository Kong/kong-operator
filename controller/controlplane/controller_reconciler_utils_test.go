package controlplane

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	admregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"

	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func Test_ensureValidatingWebhookConfiguration(t *testing.T) {
	const enforceConfig = true

	webhookSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "webhook-svc",
		},
	}

	testCases := []struct {
		name    string
		cp      *operatorv1beta1.ControlPlane
		webhook *admregv1.ValidatingWebhookConfiguration

		testBody func(*testing.T, *Reconciler, *operatorv1beta1.ControlPlane)
	}{
		{
			name: "creating validating webhook configuration",
			cp: &operatorv1beta1.ControlPlane{
				TypeMeta: metav1.TypeMeta{
					Kind: "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "cp",
				},
				Spec: operatorv1beta1.ControlPlaneSpec{
					ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
						Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
							Replicas: lo.ToPtr(int32(1)),
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										func() corev1.Container {
											c := k8sresources.GenerateControlPlaneContainer(
												k8sresources.GenerateContainerForControlPlaneParams{
													Image:                          consts.DefaultControlPlaneImage,
													AdmissionWebhookCertSecretName: lo.ToPtr("cert-secret"),
												})
											// Envs are set elsewhere so fill in the CONTROLLER_ADMISSION_WEBHOOK_LISTEN
											// here so that the webhook is enabled.
											c.Env = append(c.Env, corev1.EnvVar{
												Name:  "CONTROLLER_ADMISSION_WEBHOOK_LISTEN",
												Value: "0.0.0.0:8080",
											})
											return c
										}(),
									},
								},
							},
						},
					},
				},
			},
			testBody: func(t *testing.T, r *Reconciler, cp *operatorv1beta1.ControlPlane) {
				var (
					ctx      = t.Context()
					webhooks admregv1.ValidatingWebhookConfigurationList
				)
				require.NoError(t, r.List(ctx, &webhooks))
				require.Empty(t, webhooks.Items)

				certSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cert-secret",
					},
					Data: map[string][]byte{
						"ca.crt": []byte("ca"), // dummy
					},
				}

				res, err := r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc, enforceConfig)
				require.NoError(t, err)
				require.Equal(t, op.Created, res)

				require.NoError(t, r.List(ctx, &webhooks))
				require.Len(t, webhooks.Items, 1)

				res, err = r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc, enforceConfig)
				require.NoError(t, err)
				require.Equal(t, op.Noop, res)
			},
		},
		{
			name: "updating validating webhook configuration enforces ObjectMeta",
			cp: &operatorv1beta1.ControlPlane{
				TypeMeta: metav1.TypeMeta{
					Kind: "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "cp",
				},
				Spec: operatorv1beta1.ControlPlaneSpec{
					ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
						Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
							Replicas: lo.ToPtr(int32(1)),
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										func() corev1.Container {
											c := k8sresources.GenerateControlPlaneContainer(
												k8sresources.GenerateContainerForControlPlaneParams{
													Image:                          consts.DefaultControlPlaneImage,
													AdmissionWebhookCertSecretName: lo.ToPtr("cert-secret"),
												})
											// Envs are set elsewhere so fill in the CONTROLLER_ADMISSION_WEBHOOK_LISTEN
											// here so that the webhook is enabled.
											c.Env = append(c.Env, corev1.EnvVar{
												Name:  "CONTROLLER_ADMISSION_WEBHOOK_LISTEN",
												Value: "0.0.0.0:8080",
											})
											return c
										}(),
									},
								},
							},
						},
					},
				},
			},
			testBody: func(t *testing.T, r *Reconciler, cp *operatorv1beta1.ControlPlane) {
				var (
					ctx      = t.Context()
					webhooks admregv1.ValidatingWebhookConfigurationList
				)
				require.NoError(t, r.List(ctx, &webhooks))
				require.Empty(t, webhooks.Items)

				certSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cert-secret",
					},
					Data: map[string][]byte{
						"ca.crt": []byte("ca"), // dummy
					},
				}

				res, err := r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc, enforceConfig)
				require.NoError(t, err)
				require.Equal(t, op.Created, res)

				require.NoError(t, r.List(ctx, &webhooks))
				require.Len(t, webhooks.Items, 1, "webhook configuration should be created")

				res, err = r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc, enforceConfig)
				require.NoError(t, err)
				require.Equal(t, op.Noop, res)

				t.Log("updating webhook configuration outside of the controller")
				{
					w := webhooks.Items[0]
					w.Labels["foo"] = "bar"
					require.NoError(t, r.Update(ctx, &w))
				}

				t.Log("running ensureValidatingWebhookConfiguration to enforce ObjectMeta")
				res, err = r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc, enforceConfig)
				require.NoError(t, err)
				require.Equal(t, op.Updated, res)

				require.NoError(t, r.List(ctx, &webhooks))
				require.Len(t, webhooks.Items, 1)
				require.NotContains(t, webhooks.Items[0].Labels, "foo",
					"labels should be updated by the controller so that changes applied by 3rd parties are overwritten",
				)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.cp).
				Build()

			r := &Reconciler{
				Client: fakeClient,
			}

			tc.testBody(t, r, tc.cp)
		})
	}
}

func TestEnsureReferenceGrantsForNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		cp        *operatorv1beta1.ControlPlane
		grants    []client.Object
		wantErr   bool
	}{
		{
			name:      "watch namespace grant exists and matches",
			namespace: "test-ns",
			cp: &operatorv1beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			grants: []client.Object{
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-grant",
						Namespace: "test-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     "gateway-operator.konghq.com",
								Kind:      "ControlPlane",
								Namespace: "cp-ns",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "watch namespace grant doesn't exist",
			namespace: "test-ns",
			cp: &operatorv1beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			grants:  []client.Object{},
			wantErr: true,
		},
		{
			name:      "watch namespace grant exists but from namespace doesn't match",
			namespace: "test-ns",
			cp: &operatorv1beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			grants: []client.Object{
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "invalid-from-namespace",
						Namespace: "test-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     "gateway-operator.konghq.com",
								Kind:      "ControlPlane",
								Namespace: "wrong-namespace",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name:      "multiple reference grants with only one valid",
			namespace: "test-ns",
			cp: &operatorv1beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			grants: []client.Object{
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "invalid-grant",
						Namespace: "test-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     "wrong.group",
								Kind:      "WrongKind",
								Namespace: "wrong-ns",
							},
						},
					},
				},
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-grant",
						Namespace: "test-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     "gateway-operator.konghq.com",
								Kind:      "ControlPlane",
								Namespace: "cp-ns",
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tt.grants...).
				Build()

			err := ensureWatchNamespaceGrantsForNamespace(t.Context(), fakeClient, tt.cp, tt.namespace)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestProcessClusterRole(t *testing.T) {
	testCases := []struct {
		name                 string
		rules                []rbacv1.PolicyRule
		gvl                  map[schema.GroupVersion]*metav1.APIResourceList
		expectedRoleRules    []rbacv1.PolicyRule
		expectedClusterRules []rbacv1.PolicyRule
	}{
		{
			name: "namespaced resource",
			rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"get", "list"},
				},
			},
			gvl: map[schema.GroupVersion]*metav1.APIResourceList{
				{Group: "", Version: "v1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "configmaps",
							Group:      "",
							Namespaced: true,
						},
					},
				},
			},
			expectedRoleRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"get", "list"},
				},
			},
			expectedClusterRules: nil,
		},
		{
			name: "cluster-scoped resource",
			rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"namespaces"},
					Verbs:     []string{"get", "list"},
				},
			},
			gvl: map[schema.GroupVersion]*metav1.APIResourceList{
				{Group: "", Version: "v1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "namespaces",
							Group:      "",
							Namespaced: false,
						},
					},
				},
			},
			expectedRoleRules: nil,
			expectedClusterRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"namespaces"},
					Verbs:     []string{"get", "list"},
				},
			},
		},
		{
			name: "mixed resources",
			rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps", "namespaces"},
					Verbs:     []string{"get", "list"},
				},
			},
			gvl: map[schema.GroupVersion]*metav1.APIResourceList{
				{Group: "", Version: "v1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "configmaps",
							Group:      "",
							Namespaced: true,
						},
						{
							Name:       "namespaces",
							Group:      "",
							Namespaced: false,
						},
					},
				},
			},
			expectedRoleRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"get", "list"},
				},
			},
			expectedClusterRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"namespaces"},
					Verbs:     []string{"get", "list"},
				},
			},
		},
		{
			name: "unknown resource falls back to cluster role",
			rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"unknown"},
					Resources: []string{"custom-resource"},
					Verbs:     []string{"get", "list"},
				},
			},
			gvl: map[schema.GroupVersion]*metav1.APIResourceList{
				{Group: "", Version: "v1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "configmaps",
							Group:      "",
							Namespaced: true,
						},
					},
				},
			},
			expectedRoleRules: nil,
			expectedClusterRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"unknown"},
					Resources: []string{"custom-resource"},
					Verbs:     []string{"get", "list"},
				},
			},
		},
		{
			name: "multiple group versions in gvl",
			rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"apps"},
					Resources: []string{"deployments"},
					Verbs:     []string{"get", "list"},
				},
			},
			gvl: map[schema.GroupVersion]*metav1.APIResourceList{
				{Group: "apps", Version: "v1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "deployments",
							Group:      "apps",
							Namespaced: true,
						},
					},
				},
				{Group: "apps", Version: "v1beta1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "deployments",
							Group:      "apps",
							Namespaced: true,
						},
					},
				},
			},
			expectedRoleRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"apps"},
					Resources: []string{"deployments"},
					Verbs:     []string{"get", "list"},
				},
			},
			expectedClusterRules: nil,
		},
		{
			name: "resource exists in multiple API groups",
			rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"networking.k8s.io"},
					Resources: []string{"ingresses"},
					Verbs:     []string{"get", "list"},
				},
			},
			gvl: map[schema.GroupVersion]*metav1.APIResourceList{
				{Group: "networking.k8s.io", Version: "v1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "ingresses",
							Group:      "networking.k8s.io",
							Namespaced: true,
						},
					},
				},
				{Group: "extensions", Version: "v1beta1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "ingresses",
							Group:      "extensions",
							Namespaced: true,
						},
					},
				},
			},
			expectedRoleRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"networking.k8s.io"},
					Resources: []string{"ingresses"},
					Verbs:     []string{"get", "list"},
				},
			},
			expectedClusterRules: nil,
		},
		{
			name: "multiple resources with different scopes",
			rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"roles", "clusterroles"},
					Verbs:     []string{"get", "list"},
				},
			},
			gvl: map[schema.GroupVersion]*metav1.APIResourceList{
				{Group: "rbac.authorization.k8s.io", Version: "v1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "roles",
							Group:      "rbac.authorization.k8s.io",
							Namespaced: true,
						},
						{
							Name:       "clusterroles",
							Group:      "rbac.authorization.k8s.io",
							Namespaced: false,
						},
					},
				},
			},
			expectedRoleRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"roles"},
					Verbs:     []string{"get", "list"},
				},
			},
			expectedClusterRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"clusterroles"},
					Verbs:     []string{"get", "list"},
				},
			},
		},
		{
			name: "not found in gvl",
			rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"clusterroles"},
					Verbs:     []string{"get", "list"},
				},
			},
			gvl: map[schema.GroupVersion]*metav1.APIResourceList{
				{Group: "rbac.authorization.k8s.io", Version: "v1"}: {
					APIResources: []metav1.APIResource{
						{
							Name:       "roles",
							Group:      "rbac.authorization.k8s.io",
							Namespaced: true,
						},
					},
				},
			},
			expectedClusterRules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"clusterroles"},
					Verbs:     []string{"get", "list"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clusterRole := &rbacv1.ClusterRole{
				Rules: tc.rules,
			}
			roleRules, clusterRules := processClusterRole(clusterRole, tc.gvl)

			// Compare role rules
			if len(tc.expectedRoleRules) == 0 {
				require.Empty(t, roleRules)
			} else {
				require.Equal(t, tc.expectedRoleRules, roleRules)
			}

			// Compare cluster rules
			if len(tc.expectedClusterRules) == 0 {
				require.Empty(t, clusterRules)
			} else {
				require.Equal(t, tc.expectedClusterRules, clusterRules)
			}
		})
	}
}
