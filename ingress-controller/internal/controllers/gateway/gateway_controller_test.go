package gateway

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/scheme"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	referencegranthelpers "github.com/kong/kong-operator/v2/test/helpers/referencegrant"
)

// testReferenceGrantVersion is the version used by tests that are not sensitive to
// which one the cluster serves; see referencegranthelpers.V1. Tests that depend on a
// grant being found run against referencegranthelpers.Versions instead.
// In production it is resolved once at startup and injected into the reconcilers;
// these tests construct reconcilers directly, so they must set it themselves.
var testReferenceGrantVersion = referencegranthelpers.V1()

func TestReadyConditionExistsForObservedGeneration(t *testing.T) {
	t.Log("checking programmed condition for currently ready gateway")
	currentlyProgrammedGateway := &gwtypes.Gateway{
		Generation: 1,
		Status: gwtypes.GatewayStatus{
			Conditions: []metav1.Condition{{
				Type:               string(gwtypes.GatewayConditionProgrammed),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				LastTransitionTime: metav1.Now(),
				Reason:             string(gwtypes.GatewayReasonProgrammed),
			}},
		},
	}
	assert.True(t, isGatewayProgrammed(currentlyProgrammedGateway))

	t.Log("checking programmed condition for previously programmed gateway that has since been updated")
	previouslyProgrammedGateway := &gwtypes.Gateway{
		Generation: 2,
		Status: gwtypes.GatewayStatus{
			Conditions: []metav1.Condition{{
				Type:               string(gwtypes.GatewayConditionProgrammed),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				LastTransitionTime: metav1.Now(),
				Reason:             string(gwtypes.GatewayReasonProgrammed),
			}},
		},
	}
	assert.False(t, isGatewayProgrammed(previouslyProgrammedGateway))

	t.Log("checking programmed condition for a gateway which has never been ready")
	neverBeenProgrammedGateway := &gwtypes.Gateway{
		Generation: 10,
		Status:     gwtypes.GatewayStatus{},
	}
	assert.False(t, isGatewayProgrammed(neverBeenProgrammedGateway))
}

func TestSetGatewayCondtion(t *testing.T) {
	testCases := []struct {
		name            string
		gw              *gwtypes.Gateway
		condition       metav1.Condition
		conditionLength int
	}{
		{
			name: "no_such_condition_should_append_one",
			gw:   &gwtypes.Gateway{},
			condition: metav1.Condition{
				Type:               "fake1",
				Status:             metav1.ConditionTrue,
				Reason:             "fake1",
				ObservedGeneration: 1,
				LastTransitionTime: metav1.Now(),
			},
			conditionLength: 1,
		},
		{
			name: "have_condition_with_type_should_replace",
			gw: &gwtypes.Gateway{
				Status: gwtypes.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "fake1",
							Status:             metav1.ConditionFalse,
							Reason:             "fake1",
							ObservedGeneration: 1,
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			condition: metav1.Condition{
				Type:               "fake1",
				Status:             metav1.ConditionTrue,
				Reason:             "fake1",
				ObservedGeneration: 2,
				LastTransitionTime: metav1.Now(),
			},
			conditionLength: 1,
		},
		{
			name: "multiple_conditions_with_type_should_preserve_one",
			gw: &gwtypes.Gateway{
				Status: gwtypes.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "fake1",
							Status:             metav1.ConditionFalse,
							Reason:             "fake1",
							ObservedGeneration: 1,
							LastTransitionTime: metav1.Now(),
						},
						{
							Type:               "fake1",
							Status:             metav1.ConditionTrue,
							Reason:             "fake2",
							ObservedGeneration: 2,
							LastTransitionTime: metav1.Now(),
						},
						{
							Type:               "fake2",
							Status:             metav1.ConditionTrue,
							Reason:             "fake2",
							ObservedGeneration: 2,
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			condition: metav1.Condition{
				Type:               "fake1",
				Status:             metav1.ConditionTrue,
				Reason:             "fake1",
				ObservedGeneration: 3,
				LastTransitionTime: metav1.Now(),
			},
			conditionLength: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setGatewayCondition(tc.gw, tc.condition)
			t.Logf("checking conditions of gateway after setting")
			assert.Len(t, tc.gw.Status.Conditions, tc.conditionLength)

			conditionNum := 0
			var observedCondition metav1.Condition
			for _, condition := range tc.gw.Status.Conditions {
				if condition.Type == tc.condition.Type {
					conditionNum++
					observedCondition = condition
				}
			}
			assert.Equal(t, 1, conditionNum)
			assert.Equal(t, tc.condition, observedCondition)
		})
	}
}

func TestIsGatewayMarkedAsAccepted(t *testing.T) {
	t.Log("verifying scheduled check for gateway object which has been accepted")
	scheduledGateway := &gwtypes.Gateway{
		Generation: 1,
		Status: gwtypes.GatewayStatus{
			Conditions: []metav1.Condition{{
				Type:               string(gwtypes.GatewayConditionAccepted),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				LastTransitionTime: metav1.Now(),
				Reason:             string(gwtypes.GatewayReasonAccepted),
			}},
		},
	}
	assert.True(t, isGatewayAccepted(scheduledGateway))

	t.Log("verifying scheduled check for gateway object which has not been scheduled")
	unscheduledGateway := &gwtypes.Gateway{}
	assert.False(t, isGatewayAccepted(unscheduledGateway))
}

func TestPruneStatusConditions(t *testing.T) {
	t.Log("verifying that a gateway with minimal status conditions is not pruned")
	gateway := &gwtypes.Gateway{}
	for i := range 4 {
		gateway.Status.Conditions = append(gateway.Status.Conditions, metav1.Condition{Type: "fake", ObservedGeneration: int64(i)})
	}
	assert.Len(t, pruneGatewayStatusConds(gateway).Status.Conditions, 4)
	assert.Len(t, gateway.Status.Conditions, 4)

	t.Log("verifying that a gateway with the maximum allowed number of conditions is note pruned")
	for i := range 4 {
		gateway.Status.Conditions = append(gateway.Status.Conditions, metav1.Condition{Type: "fake", ObservedGeneration: int64(i) + 4})
	}
	assert.Len(t, pruneGatewayStatusConds(gateway).Status.Conditions, 8)
	assert.Len(t, gateway.Status.Conditions, 8)

	t.Log("verifying that a gateway with too many status conditions is pruned")
	for i := range 4 {
		gateway.Status.Conditions = append(gateway.Status.Conditions, metav1.Condition{Type: "fake", ObservedGeneration: int64(i) + 8})
	}
	assert.Len(t, pruneGatewayStatusConds(gateway).Status.Conditions, 8)
	assert.Len(t, gateway.Status.Conditions, 8)

	t.Log("verifying that the more recent 8 conditions were retained after the pruning")
	assert.Equal(t, int64(4), gateway.Status.Conditions[0].ObservedGeneration)
	assert.Equal(t, int64(11), gateway.Status.Conditions[7].ObservedGeneration)
}

func TestReconcileGatewaysIfClassMatches(t *testing.T) {
	t.Log("generating a gatewayclass to test reconciliation filters")
	gatewayClass := &gwtypes.GatewayClass{
		Name: "us",
		Spec: gwtypes.GatewayClassSpec{
			ControllerName: GetControllerName(),
		},
	}

	t.Log("generating a list of matching controllers")
	matching := []gwtypes.Gateway{
		{
			Name:      "sanfrancisco",
			Namespace: "california",
			Spec: gwtypes.GatewaySpec{
				GatewayClassName: gwtypes.ObjectName(gatewayClass.Name),
			},
		},
		{
			Name:      "sandiego",
			Namespace: "california",
			Spec: gwtypes.GatewaySpec{
				GatewayClassName: gwtypes.ObjectName(gatewayClass.Name),
			},
		},
		{
			Name:      "losangelos",
			Namespace: "california",
			Spec: gwtypes.GatewaySpec{
				GatewayClassName: gwtypes.ObjectName(gatewayClass.Name),
			},
		},
	}

	t.Log("generating a list of non-matching controllers")
	nonmatching := []gwtypes.Gateway{
		{
			Name:      "hamburg",
			Namespace: "germany",
			Spec: gwtypes.GatewaySpec{
				GatewayClassName: gwtypes.ObjectName("eu"),
			},
		},
		{
			Name:      "paris",
			Namespace: "france",
			Spec: gwtypes.GatewaySpec{
				GatewayClassName: gwtypes.ObjectName("eu"),
			},
		},
	}

	t.Log("verifying reconciliation counts")
	assert.Len(t, reconcileGatewaysIfClassMatches(gatewayClass, append(matching, nonmatching...)), len(matching))
	assert.Len(t, reconcileGatewaysIfClassMatches(gatewayClass, matching), len(matching))
	assert.Empty(t, reconcileGatewaysIfClassMatches(gatewayClass, nonmatching))
	assert.Empty(t, reconcileGatewaysIfClassMatches(gatewayClass, nil))

	t.Log("verifying reconciliation results")
	expected := []reconcile.Request{
		{
			Name:      "sanfrancisco",
			Namespace: "california",
		},
		{
			Name:      "sandiego",
			Namespace: "california",
		},
		{
			Name:      "losangelos",
			Namespace: "california",
		},
	}
	assert.Equal(t, expected, reconcileGatewaysIfClassMatches(gatewayClass, append(matching, nonmatching...)))
	assert.Equal(t, expected, reconcileGatewaysIfClassMatches(gatewayClass, matching))
}

func TestIsGatewayControlled(t *testing.T) {
	var testControllerName gwtypes.GatewayController = "acme.io/gateway-controller"

	testCases := []struct {
		name           string
		GatewayClass   *gwtypes.GatewayClass
		expectedResult bool
	}{
		{
			name: "uncontrolled GatewayClass",
			GatewayClass: &gwtypes.GatewayClass{
				Name: "uncontrolled",
				Spec: gwtypes.GatewayClassSpec{
					ControllerName: testControllerName,
				},
			},
			expectedResult: false,
		},
		{
			name: "controlled GatewayClass",
			GatewayClass: &gwtypes.GatewayClass{
				Name: "controlled",
				Spec: gwtypes.GatewayClassSpec{
					ControllerName: GetControllerName(),
				},
			},
			expectedResult: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expectedResult, isGatewayClassControlled(tc.GatewayClass))
		})
	}
}

func TestIsGatewayUnmanaged(t *testing.T) {
	testCases := []struct {
		name                    string
		GatewayClassAnnotations map[string]string
		expectedResult          bool
	}{
		{
			name: "unmanaged GatewayClass",
			GatewayClassAnnotations: map[string]string{
				annotations.AnnotationPrefix + annotations.GatewayClassUnmanagedKey: annotations.GatewayClassUnmanagedAnnotationValuePlaceholder,
			},
			expectedResult: true,
		},
		{
			name:                    "managed GatewayClass",
			GatewayClassAnnotations: map[string]string{},
			expectedResult:          false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expectedResult, isGatewayClassUnmanaged(tc.GatewayClassAnnotations))
		})
	}
}

func TestGetReferenceGrantConditionReason(t *testing.T) {
	testCases := []struct {
		name             string
		gatewayNamespace string
		certRef          gwtypes.SecretObjectReference
		referenceGrants  []*gwtypes.ReferenceGrant
		expectedReason   string
	}{
		{
			name:           "empty reference",
			certRef:        gwtypes.SecretObjectReference{},
			expectedReason: string(gwtypes.ListenerReasonResolvedRefs),
		},
		{
			name:             "no need for reference",
			gatewayNamespace: "test",
			certRef: gwtypes.SecretObjectReference{
				Kind: util.StringToGatewayAPIKindPtr("Secret"),
				Name: "testSecret",
			},
			expectedReason: string(gwtypes.ListenerReasonResolvedRefs),
		},
		{
			name:             "reference not granted - secret name not matching",
			gatewayNamespace: "test",
			certRef: gwtypes.SecretObjectReference{
				Kind:      util.StringToGatewayAPIKindPtr("Secret"),
				Name:      "testSecret",
				Namespace: new(gwtypes.Namespace("otherNamespace")),
			},
			referenceGrants: []*gwtypes.ReferenceGrant{
				{
					Namespace: "otherNamespace",
					Spec: gwtypes.ReferenceGrantSpec{
						From: []gwtypes.ReferenceGrantFrom{
							{
								Group:     gwtypes.V1Group,
								Kind:      "Gateway",
								Namespace: "test",
							},
						},
						To: []gwtypes.ReferenceGrantTo{
							{
								Group: "",
								Kind:  "Secret",
								Name:  new(gwtypes.ObjectName("anotherSecret")),
							},
						},
					},
				},
			},
			expectedReason: string(gwtypes.ListenerReasonRefNotPermitted),
		},
		{
			name:             "reference not granted - no grants specified",
			gatewayNamespace: "test",
			certRef: gwtypes.SecretObjectReference{
				Kind:      util.StringToGatewayAPIKindPtr("Secret"),
				Name:      "testSecret",
				Namespace: new(gwtypes.Namespace("otherNamespace")),
			},
			expectedReason: string(gwtypes.ListenerReasonRefNotPermitted),
		},
		{
			name:             "reference granted, secret name not specified",
			gatewayNamespace: "test",
			certRef: gwtypes.SecretObjectReference{
				Kind:      util.StringToGatewayAPIKindPtr("Secret"),
				Name:      "testSecret",
				Namespace: new(gwtypes.Namespace("otherNamespace")),
			},
			referenceGrants: []*gwtypes.ReferenceGrant{
				{
					Namespace: "otherNamespace",
					Spec: gwtypes.ReferenceGrantSpec{
						From: []gwtypes.ReferenceGrantFrom{
							// useless entry, just to furtherly test the function
							{
								Group:     "otherGroup",
								Kind:      "otherKind",
								Namespace: "test",
							},
							// good entry
							{
								Group:     gwtypes.V1Group,
								Kind:      "Gateway",
								Namespace: "test",
							},
						},
						To: []gwtypes.ReferenceGrantTo{
							{
								Group: "",
								Kind:  "Secret",
							},
						},
					},
				},
			},
			expectedReason: string(gwtypes.ListenerReasonResolvedRefs),
		},
		{
			name:             "reference granted, secret name specified",
			gatewayNamespace: "test",
			certRef: gwtypes.SecretObjectReference{
				Kind:      util.StringToGatewayAPIKindPtr("Secret"),
				Name:      "testSecret",
				Namespace: new(gwtypes.Namespace("otherNamespace")),
			},
			referenceGrants: []*gwtypes.ReferenceGrant{
				{
					Namespace: "otherNamespace",
					Spec: gwtypes.ReferenceGrantSpec{
						From: []gwtypes.ReferenceGrantFrom{
							{
								Group:     gwtypes.V1Group,
								Kind:      "Gateway",
								Namespace: "test",
							},
						},
						To: []gwtypes.ReferenceGrantTo{
							{
								Group: "",
								Kind:  "Secret",
								Name:  new(gwtypes.ObjectName("testSecret")),
							},
						},
					},
				},
			},
			expectedReason: string(gwtypes.ListenerReasonResolvedRefs),
		},
	}

	for _, tc := range testCases {
		assert.Equal(t, tc.expectedReason, getReferenceGrantConditionReason(
			tc.gatewayNamespace,
			tc.certRef,
			tc.referenceGrants,
		))
	}
}

func TestUpdateAddressesAndListenersStatus_UpdatesAddressesWhenProgrammed(t *testing.T) {
	ctx := context.Background()
	sch := scheme.Get()

	ipType := gwtypes.IPAddressType
	gateway := &gwtypes.Gateway{
		Name:       "gw",
		Namespace:  "default",
		Generation: 1,
		Status: gwtypes.GatewayStatus{
			Addresses: []gwtypes.GatewayStatusAddress{
				{Type: &ipType, Value: "10.0.0.1"},
			},
			Conditions: []metav1.Condition{
				{
					Type:               string(gwtypes.GatewayConditionProgrammed),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					Reason:             string(gwtypes.GatewayReasonProgrammed),
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	cl := fakeclient.NewClientBuilder().
		WithScheme(sch).
		WithObjects(gateway).
		WithStatusSubresource(gateway).
		Build()

	r := &GatewayReconciler{
		Client:                cl,
		Log:                   logr.Discard(),
		ReferenceGrantVersion: testReferenceGrantVersion,
	}

	listenerStatuses := []gwtypes.ListenerStatus{}
	newAddresses := []gwtypes.GatewayStatusAddress{
		{Type: &ipType, Value: "10.0.0.2"},
	}

	res, err := r.updateAddressesAndListenersStatus(ctx, logr.Discard(), gateway, listenerStatuses, newAddresses)
	require.NoError(t, err)
	require.True(t, res.IsZero())

	updated := &gwtypes.Gateway{}
	require.NoError(t, cl.Get(ctx, ctrlclient.ObjectKeyFromObject(gateway), updated))
	require.Equal(t, newAddresses, updated.Status.Addresses)
}
