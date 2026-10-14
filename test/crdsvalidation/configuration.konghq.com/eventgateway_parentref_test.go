package configuration_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestEventGatewayParentRef covers spec.parentRef of the entities directly
// under an Event Gateway, and how it relates to the deprecated
// spec.gatewayRef.
func TestEventGatewayParentRef(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())

	t.Run("EventGatewayBackendCluster", func(t *testing.T) {
		runGatewayParentRefTestCases(t, cfg, validBackendCluster(ns.Name),
			func(obj *configurationv1alpha1.EventGatewayBackendCluster) (**commonv1alpha1.GatewayRef, *commonv1alpha1.ObjectRef) {
				return &obj.Spec.ParentRef, &obj.Spec.GatewayRef
			})
	})
	t.Run("EventGatewayDataPlaneCertificate", func(t *testing.T) {
		runGatewayParentRefTestCases(t, cfg, validDataPlaneCertificate(ns.Name),
			func(obj *configurationv1alpha1.EventGatewayDataPlaneCertificate) (**commonv1alpha1.GatewayRef, *commonv1alpha1.ObjectRef) {
				return &obj.Spec.ParentRef, &obj.Spec.GatewayRef
			})
	})
	t.Run("EventGatewayListener", func(t *testing.T) {
		runGatewayParentRefTestCases(t, cfg, validListener(ns.Name, intstr.FromInt32(9092)),
			func(obj *configurationv1alpha1.EventGatewayListener) (**commonv1alpha1.GatewayRef, *commonv1alpha1.ObjectRef) {
				return &obj.Spec.ParentRef, &obj.Spec.GatewayRef
			})
	})
	t.Run("EventGatewaySchemaRegistry", func(t *testing.T) {
		runGatewayParentRefTestCases(t, cfg, validSchemaRegistry(ns.Name),
			func(obj *configurationv1alpha1.EventGatewaySchemaRegistry) (**commonv1alpha1.GatewayRef, *commonv1alpha1.ObjectRef) {
				return &obj.Spec.ParentRef, &obj.Spec.GatewayRef
			})
	})
	t.Run("EventGatewayStaticKey", func(t *testing.T) {
		runGatewayParentRefTestCases(t, cfg, validStaticKey(ns.Name),
			func(obj *configurationv1alpha1.EventGatewayStaticKey) (**commonv1alpha1.GatewayRef, *commonv1alpha1.ObjectRef) {
				return &obj.Spec.ParentRef, &obj.Spec.GatewayRef
			})
	})
	t.Run("EventGatewayTLSTrustBundle", func(t *testing.T) {
		runGatewayParentRefTestCases(t, cfg, validTLSTrustBundle(ns.Name),
			func(obj *configurationv1alpha1.EventGatewayTLSTrustBundle) (**commonv1alpha1.GatewayRef, *commonv1alpha1.ObjectRef) {
				return &obj.Spec.ParentRef, &obj.Spec.GatewayRef
			})
	})
}

// runGatewayParentRefTestCases runs the spec.parentRef test cases against
// valid, a valid object referencing its gateway through the deprecated
// spec.gatewayRef by name only. refs returns the object's spec.parentRef and
// spec.gatewayRef.
func runGatewayParentRefTestCases[
	T interface {
		client.Object
		DeepCopy() T
		SetConditions([]metav1.Condition)
	},
](
	t *testing.T,
	cfg *rest.Config,
	valid T,
	refs func(T) (**commonv1alpha1.GatewayRef, *commonv1alpha1.ObjectRef),
) {
	t.Helper()

	const (
		konnectGroup = "konnect.konghq.com"
		konnectKind  = "KonnectEventGateway"
	)
	_, validGatewayRef := refs(valid)
	gatewayName := validGatewayRef.NamespacedRef.Name

	// withRefs returns a copy of the valid object with the given references.
	withRefs := func(parentRef *commonv1alpha1.GatewayRef, gatewayRef commonv1alpha1.ObjectRef) T {
		obj := valid.DeepCopy()
		p, g := refs(obj)
		*p, *g = parentRef, gatewayRef
		return obj
	}
	setRefs := func(parentRef *commonv1alpha1.GatewayRef, gatewayRef commonv1alpha1.ObjectRef) func(T) {
		return func(obj T) {
			p, g := refs(obj)
			*p, *g = parentRef, gatewayRef
		}
	}
	konnectRef := func(name string) *commonv1alpha1.GatewayRef {
		return &commonv1alpha1.GatewayRef{Group: konnectGroup, Kind: konnectKind, Name: name}
	}
	withParentRef := func(group, kind, name string) T {
		return withRefs(&commonv1alpha1.GatewayRef{Group: group, Kind: kind, Name: name}, commonv1alpha1.ObjectRef{})
	}
	programmed := func(obj T) T {
		obj.SetConditions([]metav1.Condition{{
			Type:               "Programmed",
			Status:             metav1.ConditionTrue,
			Reason:             "Valid",
			LastTransitionTime: metav1.Now(),
		}})
		return obj
	}
	gatewayRefInNamespace := func(namespace string) commonv1alpha1.ObjectRef {
		return commonv1alpha1.ObjectRef{
			Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gatewayName, Namespace: &namespace},
		}
	}
	const sameGatewayOnly = "spec.gatewayRef can only be replaced by a spec.parentRef referencing the same gateway"

	common.TestCasesGroup[T]{
		{
			Name:       "KonnectEventGateway is accepted",
			TestObject: withParentRef(konnectGroup, konnectKind, gatewayName),
		},
		{
			Name: "namespace is accepted",
			TestObject: func() T {
				ref := konnectRef(gatewayName)
				ref.Namespace = new("other")
				return withRefs(ref, commonv1alpha1.ObjectRef{})
			}(),
		},
		{
			Name:                 "OnPremEventGateway is not supported yet",
			TestObject:           withParentRef("eventgateway.konghq.com", "OnPremEventGateway", gatewayName),
			ExpectedErrorMessage: new("parentRef must reference one of: konnect.konghq.com/KonnectEventGateway"),
		},
		{
			Name:                 "kind of another group is rejected",
			TestObject:           withParentRef("eventgateway.konghq.com", konnectKind, gatewayName),
			ExpectedErrorMessage: new("parentRef must reference one of"),
		},
		{
			Name:                 "unsupported kind is rejected",
			TestObject:           withParentRef(konnectGroup, "KonnectGatewayControlPlane", gatewayName),
			ExpectedErrorMessage: new("parentRef must reference one of"),
		},
		{
			Name:                 "group is required",
			TestObject:           withParentRef("", konnectKind, gatewayName),
			ExpectedErrorMessage: new("spec.parentRef.group: Required value"),
		},
		{
			Name:                 "kind is required",
			TestObject:           withParentRef(konnectGroup, "", gatewayName),
			ExpectedErrorMessage: new("spec.parentRef.kind: Required value"),
		},
		{
			Name:                 "name is required",
			TestObject:           withParentRef(konnectGroup, konnectKind, ""),
			ExpectedErrorMessage: new("spec.parentRef.name: Required value"),
		},
		{
			Name:                 "gatewayRef and parentRef together are rejected",
			TestObject:           withRefs(konnectRef(gatewayName), *validGatewayRef),
			ExpectedErrorMessage: new("exactly one of spec.gatewayRef and spec.parentRef must be set"),
		},
		{
			Name:                 "neither gatewayRef nor parentRef is rejected",
			TestObject:           withRefs(nil, commonv1alpha1.ObjectRef{}),
			ExpectedErrorMessage: new("exactly one of spec.gatewayRef and spec.parentRef must be set"),
		},
		{
			Name:       "parentRef name can change when not programmed",
			TestObject: withParentRef(konnectGroup, konnectKind, gatewayName),
			Update:     setRefs(konnectRef("different"), commonv1alpha1.ObjectRef{}),
		},
		{
			Name:                       "parentRef name cannot change when programmed",
			TestObject:                 programmed(withParentRef(konnectGroup, konnectKind, gatewayName)),
			Update:                     setRefs(konnectRef("different"), commonv1alpha1.ObjectRef{}),
			ExpectedUpdateErrorMessage: new("spec.parentRef is immutable when an entity is already Programmed"),
		},
		{
			Name:       "gatewayRef can be replaced by a parentRef to the same gateway when programmed",
			TestObject: programmed(valid.DeepCopy()),
			Update:     setRefs(konnectRef(gatewayName), commonv1alpha1.ObjectRef{}),
		},
		{
			Name:                       "gatewayRef cannot be replaced by a parentRef to another gateway when programmed",
			TestObject:                 programmed(valid.DeepCopy()),
			Update:                     setRefs(konnectRef("different"), commonv1alpha1.ObjectRef{}),
			ExpectedUpdateErrorMessage: new(sameGatewayOnly),
		},
		{
			Name:       "gatewayRef cannot be replaced by a parentRef in another namespace when programmed",
			TestObject: programmed(valid.DeepCopy()),
			Update: func(obj T) {
				ref := konnectRef(gatewayName)
				ref.Namespace = new("other")
				setRefs(ref, commonv1alpha1.ObjectRef{})(obj)
			},
			ExpectedUpdateErrorMessage: new(sameGatewayOnly),
		},
		{
			Name:       "gatewayRef with a namespace can be replaced by a parentRef with the same namespace when programmed",
			TestObject: programmed(withRefs(nil, gatewayRefInNamespace("other"))),
			Update: func(obj T) {
				ref := konnectRef(gatewayName)
				ref.Namespace = new("other")
				setRefs(ref, commonv1alpha1.ObjectRef{})(obj)
			},
		},
		{
			Name:                       "gatewayRef with a namespace cannot be replaced by a parentRef without one when programmed",
			TestObject:                 programmed(withRefs(nil, gatewayRefInNamespace("other"))),
			Update:                     setRefs(konnectRef(gatewayName), commonv1alpha1.ObjectRef{}),
			ExpectedUpdateErrorMessage: new(sameGatewayOnly + ", with the same name and namespace as spec.gatewayRef, once an entity is already Programmed"),
		},
		{
			// An empty namespace means the object's own namespace, like an
			// unset one; parentRef can't hold an empty namespace.
			Name:       "gatewayRef with an empty namespace can be replaced by a parentRef without one when programmed",
			TestObject: programmed(withRefs(nil, gatewayRefInNamespace(""))),
			Update:     setRefs(konnectRef(gatewayName), commonv1alpha1.ObjectRef{}),
		},
		{
			Name:                       "gatewayRef with an empty namespace cannot be replaced by a parentRef in another namespace when programmed",
			TestObject:                 programmed(withRefs(nil, gatewayRefInNamespace(""))),
			Update: func(obj T) {
				ref := konnectRef(gatewayName)
				ref.Namespace = new("other")
				setRefs(ref, commonv1alpha1.ObjectRef{})(obj)
			},
			ExpectedUpdateErrorMessage: new(sameGatewayOnly),
		},
		{
			// A konnectID names no object a parentRef could be compared with.
			Name: "gatewayRef with a konnectID cannot be replaced by a parentRef when programmed",
			TestObject: programmed(withRefs(nil, commonv1alpha1.ObjectRef{
				Type:      commonv1alpha1.ObjectRefTypeKonnectID,
				KonnectID: new("8ae65120-cdec-4310-84c1-4b19caf67967"),
			})),
			Update:                     setRefs(konnectRef(gatewayName), commonv1alpha1.ObjectRef{}),
			ExpectedUpdateErrorMessage: new(sameGatewayOnly),
		},
		{
			Name:       "gatewayRef can be replaced by a parentRef to another gateway when not programmed",
			TestObject: valid.DeepCopy(),
			Update:     setRefs(konnectRef("different"), commonv1alpha1.ObjectRef{}),
		},
		{
			Name:                       "parentRef cannot be replaced by gatewayRef",
			TestObject:                 withParentRef(konnectGroup, konnectKind, gatewayName),
			Update:                     setRefs(nil, *validGatewayRef),
			ExpectedUpdateErrorMessage: new("spec.parentRef cannot be replaced by the deprecated spec.gatewayRef"),
		},
		{
			Name:                       "parentRef cannot be replaced by gatewayRef when programmed",
			TestObject:                 programmed(withParentRef(konnectGroup, konnectKind, gatewayName)),
			Update:                     setRefs(nil, *validGatewayRef),
			ExpectedUpdateErrorMessage: new("spec.parentRef cannot be replaced by the deprecated spec.gatewayRef"),
		},
	}.RunWithConfig(t, cfg, scheme.Get())
}
