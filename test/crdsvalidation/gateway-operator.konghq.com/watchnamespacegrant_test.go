package crdsvalidation_test

import (
	"testing"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestWatchNamespaceGrant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("basic", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1alpha1.WatchNamespaceGrant]{
			{
				Name: "empty from is invalid",
				TestObject: &operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{},
					},
				},
				ExpectedErrorMessage: new("spec.from in body should have at least 1 items"),
			},
			{
				Name: "valid",
				TestObject: &operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
				ExpectedErrorMessage: new("spec.from[0].group: Unsupported value: \"invalid.group\": supported values: \"gateway-operator.konghq.com\""),
			},
			{
				Name: "unsupported kind",
				TestObject: &operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
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
				ExpectedErrorMessage: new("spec.from[0].kind: Unsupported value: \"invalid.kind\": supported values: \"ControlPlane\""),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
