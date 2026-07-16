package common

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// NewCRDValidationTestCasesGroupParentRefChange creates a test cases group for
// parent ref change.
func NewCRDValidationTestCasesGroupParentRefChange[
	T ObjectWithParentRef[T],
](
	t *testing.T,
	cfg *rest.Config,
	obj T,
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
	)

	{
		obj := obj.DeepCopy()
		obj.SetConditions([]metav1.Condition{programmedConditionTrue})
		ret = append(ret, TestCase[T]{
			Name:       "cannot change parent ref when programmed",
			TestObject: obj,
			Update: func(obj T) {
				obj.SetParentRef(commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "different",
					},
				})
			},
			ExpectedUpdateErrorMessage: new("can't update"),
		})
	}
	{
		obj := obj.DeepCopy()
		ret = append(ret, TestCase[T]{
			Name:       "can change parent ref when no status conditions",
			TestObject: obj,
			Update: func(obj T) {
				obj.SetParentRef(commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "different",
					},
				})
			},
		})
	}
	{
		obj := obj.DeepCopy()
		obj.SetConditions([]metav1.Condition{programmedConditionFalse})
		ret = append(ret, TestCase[T]{
			Name:       "can change parent ref when programmed condition is false",
			TestObject: obj,
			Update: func(obj T) {
				obj.SetParentRef(commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "different",
					},
				})
			},
		})
	}
	return ret
}
