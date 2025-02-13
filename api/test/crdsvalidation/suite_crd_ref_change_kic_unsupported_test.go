package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

type EmptyControlPlaneRefAllowedT bool

const (
	EmptyControlPlaneRefAllowed    EmptyControlPlaneRefAllowedT = true
	EmptyControlPlaneRefNotAllowed EmptyControlPlaneRefAllowedT = false
)

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
) crdsvalidation.TestCasesGroup[T] {
	ret := crdsvalidation.TestCasesGroup[T]{}

	{
		obj := obj.DeepCopy()
		obj.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKIC,
		})
		ret = append(ret, crdsvalidation.TestCase[T]{
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
			ret = append(ret, crdsvalidation.TestCase[T]{
				Name:                 "<unset> control plane ref is not allowed",
				TestObject:           obj,
				ExpectedErrorMessage: lo.ToPtr("controlPlaneRef"),
			})
		case EmptyControlPlaneRefAllowed:
			ret = append(ret, crdsvalidation.TestCase[T]{
				Name:       "<unset> control plane ref is allowed",
				TestObject: obj,
			})
		}
	}

	return ret
}
