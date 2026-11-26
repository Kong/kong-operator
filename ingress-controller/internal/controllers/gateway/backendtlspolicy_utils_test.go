package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestGetBackendTLSPolicyAncestors(t *testing.T) {
	tests := []struct {
		name        string
		policy      gwtypes.BackendTLSPolicy
		objects     []client.Object
		expected    []k8stypes.NamespacedName
		expectError bool
	}{
		{
			name: "target ref not a service",
			policy: gwtypes.BackendTLSPolicy{
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: gwtypes.Group("other-group"),
							Kind:  gwtypes.Kind("other-kind"),
							Name:  "example-service",
						},
					},
				},
			},
		},
		{
			name: "valid target ref, httproute with resolved refs",
			policy: gwtypes.BackendTLSPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
				},
			},
			objects: []client.Object{
				&gwtypes.HTTPRoute{
					Namespace: "default",
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										Group: new(gwtypes.Group("core")),
										Kind:  new(gwtypes.Kind("Service")),
										Name:  "example-service",
									},
								},
							},
						},
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  "example-gateway",
								},
							},
						},
					},
					Status: gwtypes.HTTPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ControllerName: GetControllerName(),
									ParentRef: gwtypes.ParentReference{
										Group:     new(gwtypes.Group("gateway.networking.k8s.io")),
										Kind:      new(gwtypes.Kind("Gateway")),
										Name:      "example-gateway",
										Namespace: new(gwtypes.Namespace("default")),
									},
									Conditions: []metav1.Condition{
										{
											Type:   string(gwtypes.RouteConditionResolvedRefs),
											Status: metav1.ConditionTrue,
										},
									},
								},
							},
						},
					},
				},
				&gwtypes.Gateway{
					Namespace: "default",
					Name:      "example-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "example-gateway-class",
					},
				},
			},
			expected: []k8stypes.NamespacedName{
				{
					Namespace: "default",
					Name:      "example-gateway",
				},
			},
		},
		{
			name: "valid target ref, httproute without resolved refs",
			policy: gwtypes.BackendTLSPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
				},
			},
			objects: []client.Object{
				&gwtypes.HTTPRoute{
					Namespace: "default",
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										Group: new(gwtypes.Group("core")),
										Kind:  new(gwtypes.Kind("Service")),
										Name:  "example-service",
									},
								},
							},
						},
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  "example-gateway",
								},
							},
						},
					},
					Status: gwtypes.HTTPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ControllerName: GetControllerName(),
									ParentRef: gwtypes.ParentReference{
										Group:     new(gwtypes.Group("gateway.networking.k8s.io")),
										Kind:      new(gwtypes.Kind("Gateway")),
										Name:      "example-gateway",
										Namespace: new(gwtypes.Namespace("default")),
									},
									Conditions: []metav1.Condition{
										{
											Type:   string(gwtypes.RouteConditionResolvedRefs),
											Status: metav1.ConditionFalse,
										},
									},
								},
							},
						},
					},
				},
				&gwtypes.Gateway{
					Namespace: "default",
					Name:      "example-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "example-gateway-class",
					},
				},
			},
		},
		{
			name: "multiple gateways belonging to multiple controllers",
			policy: gwtypes.BackendTLSPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
				},
			},
			objects: []client.Object{
				&gwtypes.HTTPRoute{
					Namespace: "default",
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										Group: new(gwtypes.Group("core")),
										Kind:  new(gwtypes.Kind("Service")),
										Name:  "example-service",
									},
								},
							},
						},
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  "other-gateway",
								},
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  new(gwtypes.Kind("Gateway")),
									Name:  "example-gateway",
								},
							},
						},
					},
					Status: gwtypes.HTTPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ControllerName: GetControllerName(),
									ParentRef: gwtypes.ParentReference{
										Group:     new(gwtypes.Group("gateway.networking.k8s.io")),
										Kind:      new(gwtypes.Kind("Gateway")),
										Name:      "example-gateway",
										Namespace: new(gwtypes.Namespace("default")),
									},
									Conditions: []metav1.Condition{
										{
											Type:   string(gwtypes.RouteConditionResolvedRefs),
											Status: metav1.ConditionTrue,
										},
									},
								},
								{
									ControllerName: "other-controller",
									ParentRef: gwtypes.ParentReference{
										Group:     new(gwtypes.Group("gateway.networking.k8s.io")),
										Kind:      new(gwtypes.Kind("Gateway")),
										Name:      "other-gateway",
										Namespace: new(gwtypes.Namespace("default")),
									},
									Conditions: []metav1.Condition{
										{
											Type:   string(gwtypes.RouteConditionResolvedRefs),
											Status: metav1.ConditionTrue,
										},
									},
								},
							},
						},
					},
				},
				&gwtypes.Gateway{
					Namespace: "default",
					Name:      "example-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "example-gateway-class",
					},
				},
			},
			expected: []k8stypes.NamespacedName{
				{
					Namespace: "default",
					Name:      "example-gateway",
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, gatewayv1.Install(scheme))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				WithIndex(
					&gwtypes.HTTPRoute{},
					httpRouteBackendRefIndexKey,
					indexHTTPRouteOnBackendRef,
				).
				Build()

			r := &BackendTLSPolicyReconciler{
				Client: cl,
			}

			gateways, err := r.getBackendTLSPolicyAncestors(t.Context(), tt.policy)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				for _, gateway := range gateways {
					key := client.ObjectKeyFromObject(&gateway)
					assert.Contains(t, tt.expected, key)
				}
			}
		})
	}
}

func TestSortGateways(t *testing.T) {
	tests := []struct {
		name              string
		gateways          []gwtypes.Gateway
		expected          []gwtypes.Gateway
		existingAncestors []gwtypes.PolicyAncestorStatus
		policyNamespace   string
	}{
		{
			name: "different namespaces, no existing ancestors",
			gateways: []gwtypes.Gateway{
				{
					Namespace: "namespace-2",
					Name:      "gateway-1",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-1",
				},
			},
			expected: []gwtypes.Gateway{
				{
					Namespace: "namespace-1",
					Name:      "gateway-1",
				},
				{
					Namespace: "namespace-2",
					Name:      "gateway-1",
				},
			},
		},
		{
			name: "same namespace, no existing ancestors",
			gateways: []gwtypes.Gateway{
				{
					Namespace: "namespace-1",
					Name:      "gateway-2",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-1",
				},
			},
			expected: []gwtypes.Gateway{
				{
					Namespace: "namespace-1",
					Name:      "gateway-1",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-2",
				},
			},
		},
		{
			name: "multiple combinations, no existing ancestors",
			gateways: []gwtypes.Gateway{
				{
					Namespace: "namespace-2",
					Name:      "gateway-1",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-2",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-1",
				},
			},
			expected: []gwtypes.Gateway{
				{
					Namespace: "namespace-1",
					Name:      "gateway-1",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-2",
				},
				{
					Namespace: "namespace-2",
					Name:      "gateway-1",
				},
			},
		},
		{
			name: "multiple combinations, with existing ancestors",
			gateways: []gwtypes.Gateway{
				{
					Namespace: "namespace-2",
					Name:      "gateway-1",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-2",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-1",
				},
				{
					Namespace: "namespace-2",
					Name:      "gateway-2",
				},
			},
			expected: []gwtypes.Gateway{
				{
					Namespace: "namespace-2",
					Name:      "gateway-1",
				},
				{
					Namespace: "namespace-2",
					Name:      "gateway-2",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-1",
				},
				{
					Namespace: "namespace-1",
					Name:      "gateway-2",
				},
			},
			existingAncestors: []gwtypes.PolicyAncestorStatus{
				{
					AncestorRef: gwtypes.ParentReference{
						Namespace: new(gwtypes.Namespace("namespace-2")),
						Name:      "gateway-2",
					},
				},
				{
					AncestorRef: gwtypes.ParentReference{
						Name: "gateway-1",
					},
				},
			},
			policyNamespace: "namespace-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortGateways(tt.gateways, tt.existingAncestors, tt.policyNamespace)
			assert.Equal(t, tt.expected, tt.gateways)
		})
	}
}

func TestValidateBackendTLSPolicy(t *testing.T) {
	tests := []struct {
		name        string
		policy      *gwtypes.BackendTLSPolicy
		objects     []client.Object
		expected    *metav1.Condition
		expectError bool
	}{
		{
			name: "valid policy with no conflicts",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
				},
			},
			expected: &metav1.Condition{
				Type:   string(gwtypes.PolicyConditionAccepted),
				Status: metav1.ConditionTrue,
				Reason: string(gwtypes.PolicyConditionAccepted),
			},
		},
		{
			name: "valid policy with multiple CACertificateRefs",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
					Validation: gwtypes.BackendTLSPolicyValidation{
						CACertificateRefs: []gwtypes.LocalObjectReference{
							{
								Group: "",
								Kind:  "ConfigMap",
								Name:  "example-configmap",
							},
							{
								Group: "",
								Kind:  "Secret",
								Name:  "example-secret",
							},
						},
					},
				},
			},
			objects: []client.Object{
				&corev1.Secret{
					Name:      "example-secret",
					Namespace: "default",
				},
				&corev1.ConfigMap{
					Name:      "example-configmap",
					Namespace: "default",
				},
			},
			expected: &metav1.Condition{
				Type:   string(gwtypes.PolicyConditionAccepted),
				Status: metav1.ConditionTrue,
				Reason: string(gwtypes.PolicyConditionAccepted),
			},
		},
		{
			name: "policy with conflicting target refs",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
				},
			},
			objects: []client.Object{
				&gwtypes.BackendTLSPolicy{
					Name:      "conflicting-policy",
					Namespace: "default",
					Spec: gwtypes.BackendTLSPolicySpec{
						TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
							{
								Group: "core",
								Kind:  "Service",
								Name:  "example-service",
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Type:    string(gwtypes.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gwtypes.PolicyReasonConflicted),
				Message: "Multiple BackendTLSPolicies target the same service",
			},
		},
		{
			name: "policy with unsupported CACertificateRefs",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
					Validation: gwtypes.BackendTLSPolicyValidation{
						CACertificateRefs: []gwtypes.LocalObjectReference{
							{
								Group: "core",
								Kind:  "WrongKind",
								Name:  "example-wrongKind",
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Type:    string(gwtypes.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gwtypes.PolicyReasonInvalid),
				Message: "CACertificateRefs must reference ConfigMaps or Secrets in the core group",
			},
		},
		{
			name: "policy with unsupported SubjectAltNames",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
					Validation: gwtypes.BackendTLSPolicyValidation{
						SubjectAltNames: []gwtypes.SubjectAltName{
							{
								Type:     gwtypes.HostnameSubjectAltNameType,
								Hostname: "example.com",
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Type:    string(gwtypes.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gwtypes.PolicyReasonInvalid),
				Message: "SubjectAltNames feature is not currently supported",
			},
		},
		{
			name: "policy with unsupported WellKnownCACertificates",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
					Validation: gwtypes.BackendTLSPolicyValidation{
						WellKnownCACertificates: new(gwtypes.WellKnownCACertificatesType("well-known-ca")),
					},
				},
			},
			expected: &metav1.Condition{
				Type:    string(gwtypes.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gwtypes.PolicyReasonInvalid),
				Message: "WellKnownCACertificates feature is not currently supported",
			},
		},
		{
			name: "policy with both unsupported features",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
					Validation: gwtypes.BackendTLSPolicyValidation{
						SubjectAltNames: []gwtypes.SubjectAltName{
							{
								Type:     gwtypes.HostnameSubjectAltNameType,
								Hostname: "example.com",
							},
						},
						WellKnownCACertificates: new(gwtypes.WellKnownCACertificatesType("well-known-ca")),
						CACertificateRefs: []gwtypes.LocalObjectReference{
							{
								Group: "core",
								Kind:  "WrongKind",
								Name:  "example-wrongkind",
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Type:    string(gwtypes.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gwtypes.PolicyReasonInvalid),
				Message: "CACertificateRefs must reference ConfigMaps or Secrets in the core group - SubjectAltNames feature is not currently supported - WellKnownCACertificates feature is not currently supported",
			},
		},
		{
			name: "valid policy referencing not existing CACert (ConfigMap)",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
					Validation: gwtypes.BackendTLSPolicyValidation{
						CACertificateRefs: []gwtypes.LocalObjectReference{
							{
								Group: "",
								Kind:  "ConfigMap",
								Name:  gwtypes.ObjectName("example-configmap"),
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Type:    string(gwtypes.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gwtypes.PolicyReasonInvalid),
				Message: "failed getting *v1.ConfigMap default/example-configmap set as CACertificateRef: configmaps \"example-configmap\" not found",
			},
		},
		{
			name: "valid policy referencing not existing CACert (Secret)",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
					Validation: gwtypes.BackendTLSPolicyValidation{
						CACertificateRefs: []gwtypes.LocalObjectReference{
							{
								Group: "",
								Kind:  "Secret",
								Name:  gwtypes.ObjectName("example-secret"),
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Type:    string(gwtypes.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gwtypes.PolicyReasonInvalid),
				Message: "failed getting *v1.Secret default/example-secret set as CACertificateRef: secrets \"example-secret\" not found",
			},
		},
		{
			name: "valid policy referencing not existing CACert (ConfigMap, group core)",
			policy: &gwtypes.BackendTLSPolicy{
				Name:      "test-policy",
				Namespace: "default",
				Spec: gwtypes.BackendTLSPolicySpec{
					TargetRefs: []gwtypes.LocalPolicyTargetReferenceWithSectionName{
						{
							Group: "core",
							Kind:  "Service",
							Name:  "example-service",
						},
					},
					Validation: gwtypes.BackendTLSPolicyValidation{
						CACertificateRefs: []gwtypes.LocalObjectReference{
							{
								Group: "core",
								Kind:  "ConfigMap",
								Name:  gwtypes.ObjectName("example-configmap"),
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Type:    string(gwtypes.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gwtypes.PolicyReasonInvalid),
				Message: "failed getting *v1.ConfigMap default/example-configmap set as CACertificateRef: configmaps \"example-configmap\" not found",
			},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, gatewayv1.Install(scheme))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []client.Object{
				tt.policy,
			}
			objects = append(objects, tt.objects...)
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithIndex(
					&gwtypes.BackendTLSPolicy{},
					backendTLSPolicyTargetRefIndexKey,
					indexBackendTLSPolicyOnTargetRef,
				).
				WithIndex(
					&gwtypes.BackendTLSPolicy{},
					backendTLSPolicyValidationCARefConfigMapIndexKey,
					indexBackendTLSPolicyOnValidationCACertificateConfigMapRef,
				).
				WithIndex(
					&gwtypes.BackendTLSPolicy{},
					backendTLSPolicyValidationCARefSecretIndexKey,
					indexBackendTLSPolicyOnValidationCACertificateSecretRef,
				).
				Build()

			r := &BackendTLSPolicyReconciler{
				Client: cl,
			}

			condition, err := r.validateBackendTLSPolicy(t.Context(), *tt.policy)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, condition)
			}
		})
	}
}
