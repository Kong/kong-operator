package common

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// WorkspaceSupportT is a type to specify whether an object can be placed in a Konnect Workspace.
type WorkspaceSupportT bool

const (
	// WorkspaceSupported represents that the object can be placed in a Konnect Workspace.
	WorkspaceSupported WorkspaceSupportT = true
	// WorkspaceNotSupported represents that the object belongs to the ControlPlane, not to a Workspace.
	WorkspaceNotSupported WorkspaceSupportT = false
)

// NewCRDValidationTestCasesGroupCPRefWorkspace creates a test cases group for
// spec.controlPlaneRef.workspace.
// For objects that do not support Workspaces, notSupportedMessage is the expected
// validation error when a Workspace is set.
func NewCRDValidationTestCasesGroupCPRefWorkspace[
	T ObjectWithControlPlaneRef[T],
](
	t *testing.T,
	obj T,
	workspaceSupport WorkspaceSupportT,
	notSupportedMessage string,
) TestCasesGroup[T] {
	t.Helper()

	cpRefWithWorkspace := func(ws string) *commonv1alpha1.ControlPlaneRef {
		return &commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "test-konnect-control-plane",
			},
			Workspace: ws,
		}
	}

	ret := TestCasesGroup[T]{}

	if workspaceSupport == WorkspaceNotSupported {
		o := obj.DeepCopy()
		o.SetControlPlaneRef(cpRefWithWorkspace("team-a"))
		return append(ret, TestCase[T]{
			Name:                 "workspace is not supported",
			TestObject:           o,
			ExpectedErrorMessage: new(notSupportedMessage),
		})
	}

	{
		o := obj.DeepCopy()
		o.SetControlPlaneRef(cpRefWithWorkspace("team-a"))
		ret = append(ret, TestCase[T]{
			Name:       "workspace with type=konnectNamespacedRef is allowed",
			TestObject: o,
		})
	}
	{
		o := obj.DeepCopy()
		o.SetControlPlaneRef(cpRefWithWorkspace(""))
		ret = append(ret, TestCase[T]{
			Name:       "no workspace (default Workspace) is allowed",
			TestObject: o,
		})
	}
	{
		o := obj.DeepCopy()
		o.SetControlPlaneRef(&commonv1alpha1.ControlPlaneRef{
			Type:      commonv1alpha1.ControlPlaneRefKIC,
			Workspace: "team-a",
		})
		ret = append(ret, TestCase[T]{
			Name:                 "workspace with type=kic yields an error",
			TestObject:           o,
			ExpectedErrorMessage: new("workspace can only be set when type is konnectNamespacedRef"),
		})
	}
	{
		o := obj.DeepCopy()
		o.SetControlPlaneRef(cpRefWithWorkspace("team-a"))
		o.SetConditions([]metav1.Condition{{
			Type:               "Programmed",
			Status:             metav1.ConditionTrue,
			Reason:             "Valid",
			LastTransitionTime: metav1.Now(),
		}})
		ret = append(ret, TestCase[T]{
			Name:       "workspace change is not allowed for Programmed=True",
			TestObject: o,
			Update: func(obj T) {
				obj.SetControlPlaneRef(cpRefWithWorkspace("team-b"))
			},
			ExpectedUpdateErrorMessage: new("spec.controlPlaneRef is immutable when an entity is already Programmed"),
		})
	}
	{
		o := obj.DeepCopy()
		o.SetControlPlaneRef(cpRefWithWorkspace("team-a"))
		o.SetConditions([]metav1.Condition{{
			Type:               "Programmed",
			Status:             metav1.ConditionFalse,
			Reason:             "NotProgrammed",
			LastTransitionTime: metav1.Now(),
		}})
		ret = append(ret, TestCase[T]{
			Name:       "workspace change is allowed when object is Programmed=False",
			TestObject: o,
			Update: func(obj T) {
				obj.SetControlPlaneRef(cpRefWithWorkspace("team-b"))
			},
		})
	}
	return ret
}
