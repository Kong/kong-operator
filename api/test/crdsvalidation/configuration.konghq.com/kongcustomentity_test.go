package configuration_test

import (
	"testing"

	"github.com/samber/lo"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestKongCustomEntity(t *testing.T) {
	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongCustomEntity]{
			{
				Name: "basic allowed spec",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
						ParentRef: &configurationv1alpha1.ObjectReference{
							Kind: lo.ToPtr("KongPlugin"),
						},
					},
				},
			},
			{
				Name: "spec.fields is required",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec:       configurationv1alpha1.KongCustomEntitySpec{},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.fields: Required value"),
			},
			{
				Name: "spec.type cannot be known Kong entity type - services",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						EntityType: "services",
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("The type field cannot be one of the known Kong entity types"),
			},
			{
				Name: "spec.type cannot be known Kong entity type - routes",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						EntityType: "routes",
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("The type field cannot be one of the known Kong entity types"),
			},
			{
				Name: "spec.type cannot be known Kong entity type - upstreams",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						EntityType: "upstreams",
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("The type field cannot be one of the known Kong entity types"),
			},
			{
				Name: "spec.type cannot be known Kong entity type - targets",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						EntityType: "targets",
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("The type field cannot be one of the known Kong entity types"),
			},
			{
				Name: "spec.type cannot be known Kong entity type - plugins",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						EntityType: "plugins",
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("The type field cannot be one of the known Kong entity types"),
			},
			{
				Name: "spec.type cannot be known Kong entity type - consumers",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						EntityType: "consumers",
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("The type field cannot be one of the known Kong entity types"),
			},
			{
				Name: "spec.type cannot be known Kong entity type - consumer_groups",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						EntityType: "consumer_groups",
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("The type field cannot be one of the known Kong entity types"),
			},
			{
				Name: "spec.type can be set",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						EntityType: "dummy",
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
					},
				},
			},
			{
				Name: "spec.type cannot be changed",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						EntityType: "dummy",
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
					},
				},
				Update: func(kce *configurationv1alpha1.KongCustomEntity) {
					kce.Spec.EntityType = "new-dummy"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("The spec.type field is immutable"),
			},
			{
				Name: "spec.parentRef.kind KongPlugin is supported",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
						ParentRef: &configurationv1alpha1.ObjectReference{
							Kind: lo.ToPtr("KongPlugin"),
						},
					},
				},
			},
			{
				Name: "spec.parentRef.kind KongClusterPlugin is supported",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
						ParentRef: &configurationv1alpha1.ObjectReference{
							Kind: lo.ToPtr("KongClusterPlugin"),
						},
					},
				},
			},
			{
				Name: "other types for spec.parentRef.kind are not allowed",
				TestObject: &configurationv1alpha1.KongCustomEntity{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongCustomEntitySpec{
						Fields: apiextensionsv1.JSON{
							Raw: []byte(
								`{}`,
							),
						},
						ParentRef: &configurationv1alpha1.ObjectReference{
							Kind: lo.ToPtr("CustomKind"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.parentRef.kind: Unsupported value: \"CustomKind\": supported values: \"KongPlugin\", \"KongClusterPlugin\""),
			},
		}.Run(t)
	})
}
