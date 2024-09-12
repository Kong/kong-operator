package testcases

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

var controlPlaneRef = testCasesGroup{
	Name: "controlPlaneRef",
	TestCases: []testCase{
		{
			Name: "no control plane ref to have control plane ref in valid",
			KongTarget: configurationv1alpha1.KongTarget{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: configurationv1alpha1.TargetRef{
						Name: "upstream",
					},
					KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
						Target: "example.com",
						Weight: 100,
					},
				},
			},
			Update: func(kt *configurationv1alpha1.KongTarget) {
				kt.Spec.ControlPlaneRef = &configurationv1alpha1.ControlPlaneRef{
					Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
					KonnectID: lo.ToPtr("konnect-1"),
				}
			},
		},
		{
			Name: "have control plane to no control plane is invalid",
			KongTarget: configurationv1alpha1.KongTarget{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongTargetSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectID: lo.ToPtr("konnect-1"),
					},
					UpstreamRef: configurationv1alpha1.TargetRef{
						Name: "upstream",
					},
					KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
						Target: "example.com",
						Weight: 100,
					},
				},
			},
			Update: func(kt *configurationv1alpha1.KongTarget) {
				kt.Spec.ControlPlaneRef = nil
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("controlPlaneRef is required once set"),
		},
		{
			Name: "control plane is immutable once programmed",
			KongTarget: configurationv1alpha1.KongTarget{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongTargetSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectID: lo.ToPtr("konnect-1"),
					},
					UpstreamRef: configurationv1alpha1.TargetRef{
						Name: "upstream",
					},
					KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
						Target: "example.com",
						Weight: 100,
					},
				},
			},
			KongTargetStatus: &configurationv1alpha1.KongTargetStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
					ControlPlaneID: "konnect-1",
				},
				Conditions: []metav1.Condition{
					{
						Type:               "Programmed",
						Status:             metav1.ConditionTrue,
						ObservedGeneration: 1,
						Reason:             "Programmed",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			Update: func(kt *configurationv1alpha1.KongTarget) {
				kt.Spec.ControlPlaneRef = &configurationv1alpha1.ControlPlaneRef{
					Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
					KonnectID: lo.ToPtr("konnect-2"),
				}
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		},
	},
}
