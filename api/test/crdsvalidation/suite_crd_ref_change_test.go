package crdsvalidation_test

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

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

type Scope byte

const (
	ScopeCluster Scope = iota
	ScopeNamespace
)

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

type SupportedByKicT bool

const (
	SupportedByKIC    SupportedByKicT = true
	NotSupportedByKIC SupportedByKicT = false
)

func NewCRDValidationTestCasesGroupCPRefChange[
	T interface {
		client.Object
		DeepCopy() T
		SetConditions([]metav1.Condition)
		SetControlPlaneRef(*configurationv1alpha1.ControlPlaneRef)
		GetControlPlaneRef() *configurationv1alpha1.ControlPlaneRef
	},
](
	t *testing.T,
	obj T,
	supportedByKIC SupportedByKicT,
) crdsvalidation.TestCasesGroup[T] {
	var (
		ret = crdsvalidation.TestCasesGroup[T]{}

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
			ret = append(ret, crdsvalidation.TestCase[T]{
				Name:       "no cpRef is valid",
				TestObject: obj,
			})
		}
	}
	{
		if objScope == meta.RESTScopeNameNamespace {
			obj := obj.DeepCopy()
			obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name:      "test-konnect-control-plane",
					Namespace: "another-namespace",
				},
			})
			ret = append(ret, crdsvalidation.TestCase[T]{
				Name:                 "cpRef (type=konnectNamespacedRef) cannot have namespace",
				TestObject:           obj,
				ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
			})
		}
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
			Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectID: lo.ToPtr("123456"),
		})
		ret = append(ret, crdsvalidation.TestCase[T]{
			Name:                 "providing konnectID when type is konnectNamespacedRef yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectID,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		ret = append(ret, crdsvalidation.TestCase[T]{
			Name:                 "providing konnectNamespacedRef when type is konnectID yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
			Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
			KonnectID: lo.ToPtr("123456"),
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		ret = append(ret, crdsvalidation.TestCase[T]{
			Name:                 "providing konnectNamespacedRef and konnectID when type is konnectID yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectNamespacedRef must not be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
			Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectID: lo.ToPtr("123456"),
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		ret = append(ret, crdsvalidation.TestCase[T]{
			Name:                 "providing konnectID and konnectNamespacedRef when type is konnectNamespacedRef yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectID must not be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
			Type:      configurationv1alpha1.ControlPlaneRefKIC,
			KonnectID: lo.ToPtr("123456"),
		})
		ret = append(ret, crdsvalidation.TestCase[T]{
			Name:                 "providing konnectID when type is kic yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is kic, konnectID must not be set"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKIC,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		ret = append(ret, crdsvalidation.TestCase[T]{
			Name:                 "providing konnectNamespaceRef when type is kic yields an error",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("when type is kic, konnectNamespacedRef must not be set"),
		})
	}
	{
		if supportedByKIC == SupportedByKIC {
			obj := obj.DeepCopy()
			obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKIC,
			})
			ret = append(ret, crdsvalidation.TestCase[T]{
				Name:       "kic control plane ref is allowed",
				TestObject: obj,
			})
		}
	}

	// Updates: KonnectNamespacedRef
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		obj.SetConditions([]metav1.Condition{programmedConditionTrue})
		ret = append(ret, crdsvalidation.TestCase[T]{
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
		obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
		})
		obj.SetConditions([]metav1.Condition{programmedConditionFalse})
		ret = append(ret, crdsvalidation.TestCase[T]{
			Name:       "cpRef change (type=konnectNamespacedRef) is allowed when object is Programmed=False",
			TestObject: obj,
			Update: func(obj T) {
				cpRef := &configurationv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
			obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKIC,
			})
			obj.SetConditions([]metav1.Condition{programmedConditionTrue})
			ret = append(ret, crdsvalidation.TestCase[T]{
				Name:       "cpRef change (type=kic) is not allowed for Programmed=True",
				TestObject: obj,
				Update: func(obj T) {
					cpRef := &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
			obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKIC,
			})
			obj.SetConditions([]metav1.Condition{programmedConditionFalse})
			ret = append(ret, crdsvalidation.TestCase[T]{
				Name:       "cpRef change (type=kic) is allowed when object is not Programmed=True",
				TestObject: obj,
				Update: func(obj T) {
					cpRef := &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
			ret = append(ret, crdsvalidation.TestCase[T]{
				Name:       "cpRef change (type=<unset>) is allowed when object is Programmed=False",
				TestObject: obj,
				Update: func(obj T) {
					cpRef := &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
			obj.SetControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{})
			obj.SetConditions([]metav1.Condition{programmedConditionTrue})
			ret = append(ret, crdsvalidation.TestCase[T]{
				Name:       "cpRef change (type=<unset>) is not allowed for Programmed=True",
				TestObject: obj,
				Update: func(obj T) {
					cpRef := &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "new-konnect-control-plane",
						},
					}
					obj.SetControlPlaneRef(cpRef)
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			})
		}
	}

	return ret
}
