package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// updatesNotAllowedForStatusTCs are test cases checking if updates to cpRef
// are indeed blocked for some status conditions.
var updatesNotAllowedForStatusTCs = testCasesGroup{
	Name: "updates not allowed for status conditions",
	TestCases: []testCase{
		{
			Name: "cpRef change is not allowed for Programmed=True",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "test-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-service",
							Kind:  "Service",
							Group: "core",
						},
					},
				},
			},
			KongPluginBindingStatus: &configurationv1alpha1.KongPluginBindingStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{},
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						Reason:             "Valid",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(c *configurationv1alpha1.KongPluginBinding) {
				c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when entity is already Programmed"),
		},
		{
			Name: "cpRef change is allowed when cp is not Programmed=True nor APIAuthValid=True",
			KongPluginBinding: configurationv1alpha1.KongPluginBinding{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
					PluginReference: configurationv1alpha1.PluginRef{
						Kind: lo.ToPtr("KongPlugin"),
						Name: "test-plugin",
					},
					Targets: configurationv1alpha1.KongPluginBindingTargets{
						ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
							Name:  "test-service",
							Kind:  "Service",
							Group: "core",
						},
					},
				},
			},
			KongPluginBindingStatus: &configurationv1alpha1.KongPluginBindingStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{},
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionFalse,
						Reason:             "NotProgrammed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(c *configurationv1alpha1.KongPluginBinding) {
				c.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
			},
		},
	},
}
