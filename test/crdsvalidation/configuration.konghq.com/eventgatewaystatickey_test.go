package configuration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validStaticKey(ns string) *configurationv1alpha1.EventGatewayStaticKey {
	return &configurationv1alpha1.EventGatewayStaticKey{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: configurationv1alpha1.EventGatewayStaticKeySpec{
			GatewayRef: generatedParentRef(),
			APISpec: configurationv1alpha1.EventGatewayStaticKeyAPISpec{
				Name:  "encryption-key",
				Value: secretRefSDS("my-key-secret", "key"),
			},
		},
	}
}

func TestEventGatewayStaticKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("status.createInputsHash validation", func(t *testing.T) {
		const validHash = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		obj := validStaticKey(ns.Name)
		obj.Status.CreateInputsHash = validHash
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayStaticKey]{
			{
				Name:       "valid fingerprint is persisted",
				TestObject: obj,
				Assert: func(t *testing.T, obj *configurationv1alpha1.EventGatewayStaticKey) {
					assert.Equal(t, validHash, obj.Status.CreateInputsHash)
				},
			},
			{
				Name:       "legacy status without fingerprint is accepted",
				TestObject: validStaticKey(ns.Name),
			},
			{
				Name:       "invalid fingerprint is rejected",
				TestObject: validStaticKey(ns.Name),
				StatusUpdate: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Status.CreateInputsHash = "sha256:" + strings.Repeat("a", 63)
				},
				ExpectedStatusUpdateErrorMessage: new("createInputsHash in body should match"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("value SensitiveDataSource validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayStaticKey]{
			{
				Name:       "secretRef type with secretRef passes",
				TestObject: validStaticKey(ns.Name),
			},
			{
				Name: "inline type with value passes",
				TestObject: func() *configurationv1alpha1.EventGatewayStaticKey {
					obj := validStaticKey(ns.Name)
					obj.Spec.APISpec.Value = inlineSDS("key-value")
					return obj
				}(),
			},
			{
				Name: "secretRef type without secretRef fails",
				TestObject: func() *configurationv1alpha1.EventGatewayStaticKey {
					obj := validStaticKey(ns.Name)
					obj.Spec.APISpec.Value = configurationv1alpha1.SensitiveDataSource{
						Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; secretRef required when type=secretRef"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	// Konnect's naming rule, not declared in its OpenAPI spec.
	t.Run("name validation", func(t *testing.T) {
		withName := func(name string) *configurationv1alpha1.EventGatewayStaticKey {
			obj := validStaticKey(ns.Name)
			obj.Spec.APISpec.Name = name
			return obj
		}
		const invalid = "spec.apiSpec.name in body should match"
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayStaticKey]{
			{Name: "letters, digits, spaces and allowed punctuation pass", TestObject: withName("Key 1_a.b:c/d+e'f-g")},
			{Name: "non-ASCII letters pass", TestObject: withName("clé")},
			{Name: "two characters pass", TestObject: withName("k1")},
			{Name: "a single character fails", TestObject: withName("k"), ExpectedErrorMessage: new(invalid)},
			{Name: "a leading underscore fails", TestObject: withName("_key"), ExpectedErrorMessage: new(invalid)},
			{Name: "a trailing space fails", TestObject: withName("key "), ExpectedErrorMessage: new(invalid)},
			{Name: "other punctuation fails", TestObject: withName("a@b"), ExpectedErrorMessage: new(invalid)},
		}.RunWithConfig(t, cfg, scheme)
	})

	// On-prem, status.id is never set: the inline value is immutable once the
	// OnPremEventGateway controller recorded the fingerprint of the value it
	// pushed. The rest of apiSpec is updated in place, and a value read from a
	// Secret is checked by the controller.
	t.Run("value immutability on-prem", func(t *testing.T) {
		const immutable = "the inline value is immutable once the static key has been pushed: records encrypted with it would no longer be decrypted"
		const key1 = "Y2hhaW5zYXctc3RhdGljLWtleS0zMi1ieXRlcy1vayE="
		const key2 = "Y2hhaW5zYXctc3RhdGljLWtleS0zMi1ieXRlcy12MiE="
		onPrem := func(hash string, value configurationv1alpha1.SensitiveDataSource) *configurationv1alpha1.EventGatewayStaticKey {
			obj := validStaticKey(ns.Name)
			obj.Spec.GatewayRef = commonv1alpha1.ObjectRef{}
			obj.Spec.ParentRef = &commonv1alpha1.GatewayRef{Group: "eventgateway.konghq.com", Kind: "OnPremEventGateway", Name: "gateway"}
			obj.Spec.APISpec.Value = value
			obj.Status.CreateInputsHash = hash
			return obj
		}
		const pushed = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayStaticKey]{
			{
				Name:       "changing the inline value before it is pushed passes",
				TestObject: onPrem("", inlineSDS(key1)),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Value = inlineSDS(key2)
				},
			},
			{
				Name:       "changing the inline value once pushed fails",
				TestObject: onPrem(pushed, inlineSDS(key1)),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Value = inlineSDS(key2)
				},
				ExpectedUpdateErrorMessage: new(immutable),
			},
			{
				Name:       "switching from an inline value to a Secret once pushed passes: the controller checks the value",
				TestObject: onPrem(pushed, inlineSDS(key1)),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Value = secretRefSDS("another-secret", "key")
				},
			},
			{
				Name:       "switching to another Secret once pushed passes: the controller checks the value",
				TestObject: onPrem(pushed, secretRefSDS("my-key-secret", "key")),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Value = secretRefSDS("another-secret", "key")
				},
			},
			{
				Name: "changing the inline value of a Konnect key with a fingerprint but no id passes",
				TestObject: func() *configurationv1alpha1.EventGatewayStaticKey {
					obj := validStaticKey(ns.Name)
					obj.Spec.APISpec.Value = inlineSDS(key1)
					obj.Status.CreateInputsHash = pushed
					return obj
				}(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Value = inlineSDS(key2)
				},
			},
			{
				Name:       "changing the gateway before it is pushed passes",
				TestObject: onPrem("", inlineSDS(key1)),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.ParentRef.Name = "another-gateway"
				},
			},
			{
				Name:       "changing the gateway once pushed fails",
				TestObject: onPrem(pushed, inlineSDS(key1)),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.ParentRef.Name = "another-gateway"
				},
				ExpectedUpdateErrorMessage: new("parentRef is immutable once the static key has been pushed"),
			},
			{
				Name:       "changing the name, description and labels once pushed passes",
				TestObject: onPrem(pushed, inlineSDS(key1)),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Name = "renamed-key"
					obj.Spec.APISpec.Description = "rotated yearly"
					obj.Spec.APISpec.Labels = configurationv1alpha1.Labels{"team": "a"}
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	// Konnect has no update API for static keys: apiSpec is immutable once the
	// static key exists in Konnect (status.id set), and editable before.
	t.Run("apiSpec immutability", func(t *testing.T) {
		// The field path prefix of the error differs between Kubernetes
		// versions (e.g. `spec.apiSpec: Invalid value: "object": ` on 1.31):
		// match the message only.
		const immutable = "apiSpec is immutable once the static key has been created in Konnect: Konnect static keys can't be updated, create a new EventGatewayStaticKey instead"
		notInKonnect := func() *configurationv1alpha1.EventGatewayStaticKey {
			obj := validStaticKey(ns.Name)
			obj.Status.Conditions = []metav1.Condition{{
				Type:               "Programmed",
				Status:             metav1.ConditionFalse,
				Reason:             "Pending",
				LastTransitionTime: metav1.Now(),
			}}
			return obj
		}
		inKonnect := func() *configurationv1alpha1.EventGatewayStaticKey {
			obj := validStaticKey(ns.Name)
			obj.SetKonnectID("static-key-id")
			return obj
		}
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayStaticKey]{
			{
				Name:       "changing the name once in Konnect fails",
				TestObject: inKonnect(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Name = "renamed-key"
				},
				ExpectedUpdateErrorMessage: new(immutable),
			},
			{
				Name:       "changing the value source once in Konnect fails",
				TestObject: inKonnect(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Value = secretRefSDS("another-secret", "key")
				},
				ExpectedUpdateErrorMessage: new(immutable),
			},
			{
				Name:       "changing the labels once in Konnect fails",
				TestObject: inKonnect(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Labels = configurationv1alpha1.Labels{"team": "a"}
				},
				ExpectedUpdateErrorMessage: new(immutable),
			},
			{
				Name:       "changing the object's metadata once in Konnect passes",
				TestObject: inKonnect(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Labels = map[string]string{"team": "a"}
				},
			},
			{
				Name:       "changing the gateway once in Konnect fails, even when not Programmed",
				TestObject: inKonnect(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.GatewayRef.NamespacedRef.Name = "another-gateway"
				},
				ExpectedUpdateErrorMessage: new("gatewayRef is immutable once the static key has been created in Konnect"),
			},
			{
				Name: "changing the parentRef gateway once in Konnect fails, even when not Programmed",
				TestObject: func() *configurationv1alpha1.EventGatewayStaticKey {
					obj := inKonnect()
					obj.Spec.GatewayRef = commonv1alpha1.ObjectRef{}
					obj.Spec.ParentRef = &commonv1alpha1.GatewayRef{Group: "konnect.konghq.com", Kind: "KonnectEventGateway", Name: "my-event-gateway"}
					return obj
				}(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.ParentRef.Name = "another-gateway"
				},
				ExpectedUpdateErrorMessage: new("parentRef is immutable once the static key has been created in Konnect"),
			},
			{
				Name:       "replacing gatewayRef by a parentRef to the same gateway once in Konnect passes",
				TestObject: inKonnect(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.ParentRef = &commonv1alpha1.GatewayRef{Group: "konnect.konghq.com", Kind: "KonnectEventGateway", Name: obj.Spec.GatewayRef.NamespacedRef.Name}
					obj.Spec.GatewayRef = commonv1alpha1.ObjectRef{}
				},
			},
			{
				Name:       "replacing gatewayRef by a parentRef to another gateway once in Konnect fails, even when not Programmed",
				TestObject: inKonnect(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.GatewayRef = commonv1alpha1.ObjectRef{}
					obj.Spec.ParentRef = &commonv1alpha1.GatewayRef{Group: "konnect.konghq.com", Kind: "KonnectEventGateway", Name: "another-gateway"}
				},
				ExpectedUpdateErrorMessage: new("gatewayRef can only be replaced by a parentRef referencing the same gateway, with the same name and namespace as gatewayRef, once the static key has been created in Konnect"),
			},
			{
				Name: "replacing a gatewayRef with an empty namespace by a parentRef without one once in Konnect passes",
				TestObject: func() *configurationv1alpha1.EventGatewayStaticKey {
					obj := inKonnect()
					obj.Spec.GatewayRef.NamespacedRef.Namespace = new("")
					return obj
				}(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.ParentRef = &commonv1alpha1.GatewayRef{Group: "konnect.konghq.com", Kind: "KonnectEventGateway", Name: obj.Spec.GatewayRef.NamespacedRef.Name}
					obj.Spec.GatewayRef = commonv1alpha1.ObjectRef{}
				},
			},
			{
				// The generic gatewayRef rule needs a status, which the
				// operator writes before creating the key in Konnect.
				Name:       "changing the gateway before creation in Konnect passes",
				TestObject: notInKonnect(),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.GatewayRef.NamespacedRef.Name = "another-gateway"
				},
			},
			{
				Name:       "changing the value source before creation in Konnect passes",
				TestObject: validStaticKey(ns.Name),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Value = secretRefSDS("another-secret", "key")
				},
			},
			{
				Name:       "changing the name before creation in Konnect passes",
				TestObject: validStaticKey(ns.Name),
				Update: func(obj *configurationv1alpha1.EventGatewayStaticKey) {
					obj.Spec.APISpec.Name = "renamed-key"
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
