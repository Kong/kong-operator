package common

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
)

// EmptyControlPlaneRefAllowedT is a type to specify whether an empty control plane ref is allowed or not
type EmptyControlPlaneRefAllowedT bool

const (
	// EmptyControlPlaneRefAllowed is a value to specify that an empty control plane ref is allowed
	EmptyControlPlaneRefAllowed EmptyControlPlaneRefAllowedT = true
	// EmptyControlPlaneRefNotAllowed is a value to specify that an empty control plane ref is not allowed
	EmptyControlPlaneRefNotAllowed EmptyControlPlaneRefAllowedT = false
)

// NewCRDValidationTestCasesGroupCPRefChangeKICUnsupportedTypes returns a test cases group for testing control plane ref change to KIC unsupported types
func NewCRDValidationTestCasesGroupCPRefChangeKICUnsupportedTypes[
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
	emptyControlPlaneRefAllowed EmptyControlPlaneRefAllowedT,
) TestCasesGroup[T] {
	ret := TestCasesGroup[T]{}

	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKIC,
		})
		ret = append(ret, TestCase[T]{
			Name:                 "kic control plane ref is not allowed",
			TestObject:           obj,
			ExpectedErrorMessage: lo.ToPtr("KIC is not supported as control plane"),
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(nil)
		switch emptyControlPlaneRefAllowed {
		case EmptyControlPlaneRefNotAllowed:
			ret = append(ret, TestCase[T]{
				Name:                 "<unset> control plane ref is not allowed",
				TestObject:           obj,
				ExpectedErrorMessage: lo.ToPtr("controlPlaneRef"),
			})
		case EmptyControlPlaneRefAllowed:
			ret = append(ret, TestCase[T]{
				Name:       "<unset> control plane ref is allowed",
				TestObject: obj,
			})
		}
	}

	return ret
}
