package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"

	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestWatchNamespaceGrant(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1alpha1.WatchNamespaceGrant]{
			{
				Name: "empty from is invalid",
				TestObject: &operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from in body should have at least 1 items"),
			},
			{
				Name: "valid",
				TestObject: &operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     operatorv1beta1.SchemeGroupVersion.Group,
								Kind:      "ControlPlane",
								Namespace: "test",
							},
						},
					},
				},
			},
			{
				Name: "unsupported group",
				TestObject: &operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     "invalid.group",
								Kind:      "ControlPlane",
								Namespace: "test",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from[0].group: Unsupported value: \"invalid.group\": supported values: \"gateway-operator.konghq.com\""),
			},
			{
				Name: "unsupported kind",
				TestObject: &operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: common.CommonObjectMeta,
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     operatorv1beta1.SchemeGroupVersion.Group,
								Kind:      "invalid.kind",
								Namespace: "test",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.from[0].kind: Unsupported value: \"invalid.kind\": supported values: \"ControlPlane\""),
			},
		}.Run(t)
	})
}
