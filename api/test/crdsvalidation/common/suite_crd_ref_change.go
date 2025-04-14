package common

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// Scope represents the scope of the object
type Scope byte

const (
	// ScopeCluster represents the cluster scope
	ScopeCluster Scope = iota
	// ScopeNamespace represents the namespace scope
	ScopeNamespace
)

// GetGroupKindScope returns the scope of the object
func getGroupKindScope(t *testing.T, obj client.Object) meta.RESTScopeName {
	config, err := config.GetConfig()
	require.NoError(t, err)

	dc := discovery.NewDiscoveryClientForConfigOrDie(config)
	groupResources, err := restmapper.GetAPIGroupResources(dc)
	require.NoError(t, err)

	gk := obj.GetObjectKind().GroupVersionKind().GroupKind()
	r, err := restmapper.NewDiscoveryRESTMapper(groupResources).RESTMapping(gk)
	require.NoError(t, err)
	return r.Scope.Name()
}

// SupportedByKicT is a type to specify whether an object is supported by KIC or not
type SupportedByKicT bool

const (
	// SupportedByKIC represents that the object is supported by KIC
	SupportedByKIC SupportedByKicT = true
	// NotSupportedByKIC represents that the object is not supported by KIC
	NotSupportedByKIC SupportedByKicT = false
)

// ControlPlaneRefRequiredT is a type to specify whether control plane ref is required or not
type ControlPlaneRefRequiredT bool

const (
	// ControlPlaneRefRequired represents that control plane ref is required
	ControlPlaneRefRequired ControlPlaneRefRequiredT = true
	// ControlPlaneRefNotRequired represents that control plane ref is not required
	ControlPlaneRefNotRequired ControlPlaneRefRequiredT = false
)

// NewCRDValidationTestCasesGroupCPRefChange creates a test cases group for control plane ref change
func NewCRDValidationTestCasesGroupCPRefChange[
	T interface {
		client.Object
		DeepCopy() T
		SetConditions([]metav1.Condition)
		SetControlPlaneRef(*commonv1alpha1.ControlPlaneRef)
		GetControlPlaneRef() *commonv1alpha1.ControlPlaneRef
	},
](
	t *testing.T,
	obj T,
	supportedByKIC SupportedByKicT,
	controlPlaneRefRequired ControlPlaneRefRequiredT,
) TestCasesGroup[T] {
	var (
		ret = TestCasesGroup[T]{}

		programmedConditionTrue = metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionTrue,
			Reason:             "Valid",
			LastTransitionTime: metav1.Now(),
		}
		programmedConditionFalse = metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionFalse,
			Reason:             "NotProgrammed",
			LastTransitionTime: metav1.Now(),
		}
		objScope = getGroupKindScope(t, obj)
	)

	{
		if supportedByKIC == SupportedByKIC {
			// Since objects managed by KIC do not require spec.controlPlane,
			// object without spec.controlPlaneRef should be allowed.
			obj := obj.DeepCopy()
			obj.SetControlPlaneRef(nil)
			ret = append(ret, TestCase[T]{
				Name:       "no cpRef is valid",
				TestObject: obj,
			})
		}
	}
	{
		if objScope == meta.RESTScopeNameNamespace {
			obj := obj.DeepCopy()
			obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name:      "test-konnect-control-plane",
					Namespace: "another-namespace",
				},
			})
			ret = append(ret, TestCase[T]{
				Name:                 "cpRef (type=konnectNamespacedRef) cannot have namespace",
				TestObject:           obj,
				ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
			})
		}
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectID: lo.ToPtr(commonv1alpha1.KonnectIDType("123456")),
		})
		ret = append(ret, TestCase[T]{
			Name:                 "providing konnectID when type is konnectNamespacedRef yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectID,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		ret = append(ret, TestCase[T]{
			Name:                 "providing konnectNamespacedRef when type is konnectID yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type:      commonv1alpha1.ControlPlaneRefKonnectID,
			KonnectID: lo.ToPtr(commonv1alpha1.KonnectIDType("123456")),
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		ret = append(ret, TestCase[T]{
			Name:                 "providing konnectNamespacedRef and konnectID when type is konnectID yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectNamespacedRef must not be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectID: lo.ToPtr(commonv1alpha1.KonnectIDType("123456")),
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		ret = append(ret, TestCase[T]{
			Name:                 "providing konnectID and konnectNamespacedRef when type is konnectNamespacedRef yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectID must not be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type:      commonv1alpha1.ControlPlaneRefKIC,
			KonnectID: lo.ToPtr(commonv1alpha1.KonnectIDType("123456")),
		})
		ret = append(ret, TestCase[T]{
			Name:                 "providing konnectID when type is kic yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is kic, konnectID must not be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKIC,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		ret = append(ret, TestCase[T]{
			Name:                 "providing konnectNamespaceRef when type is kic yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is kic, konnectNamespacedRef must not be set"),
		})
	}
	{
		if supportedByKIC == SupportedByKIC {
			obj := obj.DeepCopy()
			obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKIC,
			})
			ret = append(ret, TestCase[T]{
				Name:       "kic control plane ref is allowed",
				TestObject: obj,
			})
		}
	}

	// Updates: KonnectNamespacedRef
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		obj.SetConditions([]metav1.Condition{programmedConditionTrue})
		ret = append(ret, TestCase[T]{
			Name:       "cpRef change (type=konnectNamespacedRef) is not allowed for Programmed=True",
			TestObject: obj,
			Update: func(obj T) {
				cpRef := obj.GetControlPlaneRef()
				cpRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
				obj.SetControlPlaneRef(cpRef)
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		obj.SetConditions([]metav1.Condition{programmedConditionFalse})
		ret = append(ret, TestCase[T]{
			Name:       "cpRef change (type=konnectNamespacedRef) is allowed when object is Programmed=False",
			TestObject: obj,
			Update: func(obj T) {
				cpRef := &commonv1alpha1.ControlPlaneRef{
					Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
						Name: "new-konnect-control-plane",
					},
				}
				obj.SetControlPlaneRef(cpRef)
			},
		})
	}

	// Updates: KIC
	{
		if supportedByKIC == SupportedByKIC {
			obj := obj.DeepCopy()
			obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKIC,
			})
			obj.SetConditions([]metav1.Condition{programmedConditionTrue})
			ret = append(ret, TestCase[T]{
				Name:       "cpRef change (type=kic) is not allowed for Programmed=True",
				TestObject: obj,
				Update: func(obj T) {
					cpRef := &commonv1alpha1.ControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "new-konnect-control-plane",
						},
					}
					obj.SetControlPlaneRef(cpRef)
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			})
		}
	}
	{
		if supportedByKIC == SupportedByKIC {
			obj := obj.DeepCopy()
			obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKIC,
			})
			obj.SetConditions([]metav1.Condition{programmedConditionFalse})
			ret = append(ret, TestCase[T]{
				Name:       "cpRef change (type=kic) is allowed when object is not Programmed=True",
				TestObject: obj,
				Update: func(obj T) {
					cpRef := &commonv1alpha1.ControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "new-konnect-control-plane",
						},
					}
					obj.SetControlPlaneRef(cpRef)
				},
			})
		}
	}

	// Updates: ControlPlane ref is unset
	{
		if supportedByKIC == SupportedByKIC {
			obj := obj.DeepCopy()
			obj.SetConditions([]metav1.Condition{programmedConditionFalse})
			ret = append(ret, TestCase[T]{
				Name:       "cpRef change (type=<unset>) is allowed when object is Programmed=False",
				TestObject: obj,
				Update: func(obj T) {
					cpRef := &commonv1alpha1.ControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "new-konnect-control-plane",
						},
					}
					obj.SetControlPlaneRef(cpRef)
				},
			})
		}
	}
	{
		if supportedByKIC == SupportedByKIC {
			obj := obj.DeepCopy()
			obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{})
			obj.SetConditions([]metav1.Condition{programmedConditionTrue})
			ret = append(ret, TestCase[T]{
				Name:       "cpRef change (type=<unset>) is not allowed for Programmed=True",
				TestObject: obj,
				Update: func(obj T) {
					cpRef := &commonv1alpha1.ControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "new-konnect-control-plane",
						},
					}
					obj.SetControlPlaneRef(cpRef)
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			})
		}
	}
	{
		if controlPlaneRefRequired == ControlPlaneRefRequired {
			obj := obj.DeepCopy()
			obj.SetControlPlaneRef(nil)
			ret = append(ret, TestCase[T]{
				Name:                 "cpRef is required",
				TestObject:           obj,
				ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef: Required value"),
			})
		}
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type:      commonv1alpha1.ControlPlaneRefKonnectID,
			KonnectID: lo.ToPtr(commonv1alpha1.KonnectIDType("123456")),
		})
		ret = append(ret, TestCase[T]{
			Name:                 "cpRef (type=konnectID) is not allowed",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("'konnectID' type is not supported"),
		})
	}

	return ret
}
