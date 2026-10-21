package crdsvalidation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

type createInputsObject interface {
	client.Object
	GetCreateInputsHash() string
	SetCreateInputsHash(string)
}

func TestAIGatewayCreateInputsHash(t *testing.T) {
	t.Parallel()

	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, t.Context(), scheme)
	for name, fixture := range map[string]createInputsObject{
		"AIGatewayDataPlaneCertificate": &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
				AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gateway"},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
					Title: "certificate",
					Cert: aiconfigurationv1alpha1.SensitiveDataSource{
						Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeInline, Value: new("certificate"),
					},
				},
			},
		},
		"AIGatewayConsumerCredential": &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
				AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "consumer"},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialAPISpec{
					Name: "credential", DisplayName: "credential", Type: "api-key",
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			const validHash = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
			withHash := fixture.DeepCopyObject().(createInputsObject)
			withHash.SetCreateInputsHash(validHash)
			common.TestCasesGroup[createInputsObject]{
				{
					Name:       "valid fingerprint is persisted",
					TestObject: withHash,
					Assert: func(t *testing.T, obj createInputsObject) {
						assert.Equal(t, validHash, obj.GetCreateInputsHash())
					},
				},
				{
					Name:       "legacy status without fingerprint is accepted",
					TestObject: fixture.DeepCopyObject().(createInputsObject),
				},
				{
					Name:       "invalid fingerprint is rejected",
					TestObject: fixture.DeepCopyObject().(createInputsObject),
					StatusUpdate: func(obj createInputsObject) {
						obj.SetCreateInputsHash("sha256:" + strings.Repeat("a", 63))
					},
					ExpectedStatusUpdateErrorMessage: new("createInputsHash in body should match"),
				},
			}.RunWithConfig(t, cfg, scheme)
		})
	}
}
