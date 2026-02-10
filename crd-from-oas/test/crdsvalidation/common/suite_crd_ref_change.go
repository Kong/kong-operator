package common

import (
	"testing"

	"k8s.io/client-go/rest"
)

// Scope represents the scope of the object
type Scope byte

const (
	// ScopeCluster represents the cluster scope
	ScopeCluster Scope = iota
	// ScopeNamespace represents the namespace scope
	ScopeNamespace
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
	T ObjectWithControlPlaneRef[T],
](
	t *testing.T,
	cfg *rest.Config,
	obj T,
	controlPlaneRefRequired ControlPlaneRefRequiredT,
) TestCasesGroup[T] {
	var (
		ret = TestCasesGroup[T]{}
	)

	{
		// Since objects managed by KIC do not require spec.controlPlane,
		// object without spec.controlPlaneRef should be allowed.
		obj := obj.DeepCopy()
		ret = append(ret, TestCase[T]{
			Name:       "base",
			TestObject: obj,
		})
	}

	return ret
}
