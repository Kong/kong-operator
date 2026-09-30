package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestHandleConfigMapRef(t *testing.T) {
	policy := func(handler aiconfigurationv1alpha1.ConfigMapDataSource) *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
		return &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			Kind:       "AIGatewayCustomPolicy",
			Name:       "policy", Namespace: "default",
			Spec: aiconfigurationv1alpha1.AIGatewayCustomPolicySpec{
				APISpec: aiconfigurationv1alpha1.AIGatewayCustomPolicyAPISpec{
					AIGatewayCustomPolicyConfig: &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
						Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeStreaming,
						Streaming: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyStreamingRequest{
							Name:        "my-streaming-policy",
							DisplayName: "My streaming policy",
							Schema: aiconfigurationv1alpha1.ConfigMapDataSource{
								Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
								Value: new("return {}"),
							},
							Handler: handler,
						},
					},
				},
			},
		}
	}
	fromConfigMap := func(key string) aiconfigurationv1alpha1.ConfigMapDataSource {
		return aiconfigurationv1alpha1.ConfigMapDataSource{
			Type:         aiconfigurationv1alpha1.ConfigMapDataSourceTypeConfigMapRef,
			ConfigMapRef: &aiconfigurationv1alpha1.ConfigMapDataSourceRef{Name: "lua", Key: key},
		}
	}
	luaConfigMap := &corev1.ConfigMap{
		Name: "lua", Namespace: "default",
		Data:       map[string]string{"handler.lua": "return {}"},
		BinaryData: map[string][]byte{"binary.lua": []byte("return {}")},
	}

	staleInvalid := policy(aiconfigurationv1alpha1.ConfigMapDataSource{
		Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
		Value: new("return {}"),
	})
	staleInvalid.Status.Conditions = []metav1.Condition{{
		Type:               konnectv1alpha1.ConfigMapRefValidConditionType,
		Status:             metav1.ConditionFalse,
		Reason:             konnectv1alpha1.ConfigMapRefReasonInvalid,
		Message:            "ConfigMap default/lua not found",
		LastTransitionTime: metav1.Now(),
	}}

	tests := []struct {
		name          string
		obj           *aiconfigurationv1alpha1.AIGatewayCustomPolicy
		objs          []client.Object
		wantStop      bool
		wantCondition *metav1.ConditionStatus
		wantMessage   string
	}{
		{
			name: "no ConfigMap references sets no condition",
			obj: policy(aiconfigurationv1alpha1.ConfigMapDataSource{
				Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
				Value: new("return {}"),
			}),
		},
		{
			name: "switching to inline removes a stale ConfigMapRefValid condition",
			obj:  staleInvalid,
		},
		{
			name:          "key in data is valid",
			obj:           policy(fromConfigMap("handler.lua")),
			objs:          []client.Object{luaConfigMap},
			wantCondition: new(metav1.ConditionTrue),
		},
		{
			name:          "key in binaryData is valid",
			obj:           policy(fromConfigMap("binary.lua")),
			objs:          []client.Object{luaConfigMap},
			wantCondition: new(metav1.ConditionTrue),
		},
		{
			name:          "missing ConfigMap stops and hints at the label selector",
			obj:           policy(fromConfigMap("handler.lua")),
			wantStop:      true,
			wantCondition: new(metav1.ConditionFalse),
			wantMessage:   "ConfigMap default/lua not found: if it exists, it is not matched by --config-map-label-selector (konghq.com/configmap=true by default)",
		},
		{
			name:          "missing key stops",
			obj:           policy(fromConfigMap("missing.lua")),
			objs:          []client.Object{luaConfigMap},
			wantStop:      true,
			wantCondition: new(metav1.ConditionFalse),
			wantMessage:   `ConfigMap default/lua is missing key "missing.lua"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(append([]client.Object{tc.obj}, tc.objs...)...).
				WithStatusSubresource(tc.obj).
				Build()

			res, stop, err := handleConfigMapRef(t.Context(), cl, tc.obj)
			require.NoError(t, err)
			assert.True(t, res.IsZero())
			assert.Equal(t, tc.wantStop, stop)

			cond := meta.FindStatusCondition(tc.obj.Status.Conditions, konnectv1alpha1.ConfigMapRefValidConditionType)
			if tc.wantCondition == nil {
				assert.Nil(t, cond)
				return
			}
			require.NotNil(t, cond)
			assert.Equal(t, *tc.wantCondition, cond.Status)
			if tc.wantMessage != "" {
				assert.Equal(t, tc.wantMessage, cond.Message)
			}
		})
	}
}
