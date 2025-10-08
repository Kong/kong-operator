package route

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	routeconst "github.com/kong/kong-operator/controller/hybridgateway/const/route"
)

func TestGetProgrammedConditionForGVK(t *testing.T) {
	tests := []struct {
		name       string
		gvk        schema.GroupVersionKind
		programmed bool
		expects    metav1.Condition
	}{
		{
			name:       "KongRoute programmed true",
			gvk:        schema.GroupVersionKind{Kind: "KongRoute"},
			programmed: true,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongRouteProgrammed,
				Status:  metav1.ConditionTrue,
				Reason:  routeconst.ConditionReasonKongRouteProgrammed,
				Message: "Resource is programmed",
			},
		},
		{
			name:       "KongRoute programmed false",
			gvk:        schema.GroupVersionKind{Kind: "KongRoute"},
			programmed: false,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongRouteProgrammed,
				Status:  metav1.ConditionFalse,
				Reason:  routeconst.ConditionReasonKongRouteNotProgrammed,
				Message: "Resource is not programmed",
			},
		},
		{
			name:       "KongService programmed true",
			gvk:        schema.GroupVersionKind{Kind: "KongService"},
			programmed: true,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongServiceProgrammed,
				Status:  metav1.ConditionTrue,
				Reason:  routeconst.ConditionReasonKongServiceProgrammed,
				Message: "Resource is programmed",
			},
		},
		{
			name:       "KongService programmed false",
			gvk:        schema.GroupVersionKind{Kind: "KongService"},
			programmed: false,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongServiceProgrammed,
				Status:  metav1.ConditionFalse,
				Reason:  routeconst.ConditionReasonKongServiceNotProgrammed,
				Message: "Resource is not programmed",
			},
		},
		{
			name:       "KongTarget programmed true",
			gvk:        schema.GroupVersionKind{Kind: "KongTarget"},
			programmed: true,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongTargetProgrammed,
				Status:  metav1.ConditionTrue,
				Reason:  routeconst.ConditionReasonKongTargetProgrammed,
				Message: "Resource is programmed",
			},
		},
		{
			name:       "KongTarget programmed false",
			gvk:        schema.GroupVersionKind{Kind: "KongTarget"},
			programmed: false,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongTargetProgrammed,
				Status:  metav1.ConditionFalse,
				Reason:  routeconst.ConditionReasonKongTargetNotProgrammed,
				Message: "Resource is not programmed",
			},
		},
		{
			name:       "KongUpstream programmed true",
			gvk:        schema.GroupVersionKind{Kind: "KongUpstream"},
			programmed: true,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongUpstreamProgrammed,
				Status:  metav1.ConditionTrue,
				Reason:  routeconst.ConditionReasonKongUpstreamProgrammed,
				Message: "Resource is programmed",
			},
		},
		{
			name:       "KongUpstream programmed false",
			gvk:        schema.GroupVersionKind{Kind: "KongUpstream"},
			programmed: false,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongUpstreamProgrammed,
				Status:  metav1.ConditionFalse,
				Reason:  routeconst.ConditionReasonKongUpstreamNotProgrammed,
				Message: "Resource is not programmed",
			},
		},
		{
			name:       "KongPluginBinding programmed true",
			gvk:        schema.GroupVersionKind{Kind: "KongPluginBinding"},
			programmed: true,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongPluginBindingProgrammed,
				Status:  metav1.ConditionTrue,
				Reason:  routeconst.ConditionReasonKongPluginBindingProgrammed,
				Message: "Resource is programmed",
			},
		},
		{
			name:       "KongPluginBinding programmed false",
			gvk:        schema.GroupVersionKind{Kind: "KongPluginBinding"},
			programmed: false,
			expects: metav1.Condition{
				Type:    routeconst.ConditionTypeKongPluginBindingProgrammed,
				Status:  metav1.ConditionFalse,
				Reason:  routeconst.ConditionReasonKongPluginBindingNotProgrammed,
				Message: "Resource is not programmed",
			},
		},
		{
			name:       "Unknown kind returns empty",
			gvk:        schema.GroupVersionKind{Kind: "UnknownKind"},
			programmed: true,
			expects:    metav1.Condition{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetProgrammedConditionForGVK(tt.gvk, tt.programmed)
			if !reflect.DeepEqual(got, tt.expects) {
				t.Errorf("unexpected condition: got %+v, want %+v", got, tt.expects)
			}
		})
	}
}

func TestDeduplicateConditionsByType(t *testing.T) {
	tests := []struct {
		name    string
		input   []metav1.Condition
		expects []metav1.Condition
	}{
		{
			name: "unique types kept",
			input: []metav1.Condition{
				{Type: "A", Status: metav1.ConditionTrue},
				{Type: "B", Status: metav1.ConditionFalse},
			},
			expects: []metav1.Condition{
				{Type: "A", Status: metav1.ConditionTrue},
				{Type: "B", Status: metav1.ConditionFalse},
			},
		},
		{
			name: "most severe kept (False > Unknown > True)",
			input: []metav1.Condition{
				{Type: "A", Status: metav1.ConditionFalse},
				{Type: "A", Status: metav1.ConditionUnknown},
				{Type: "A", Status: metav1.ConditionTrue},
			},
			expects: []metav1.Condition{
				{Type: "A", Status: metav1.ConditionFalse},
			},
		},
		{
			name: "multiple types, mixed severities",
			input: []metav1.Condition{
				{Type: "A", Status: metav1.ConditionFalse},
				{Type: "B", Status: metav1.ConditionUnknown},
				{Type: "A", Status: metav1.ConditionUnknown},
				{Type: "B", Status: metav1.ConditionTrue},
			},
			expects: []metav1.Condition{
				{Type: "A", Status: metav1.ConditionFalse},
				{Type: "B", Status: metav1.ConditionUnknown},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeduplicateConditionsByType(tt.input)
			// Compare by type and status only.
			if !conditionsEqualByTypeStatus(got, tt.expects) {
				t.Errorf("unexpected result: got %+v, want %+v", got, tt.expects)
			}
		})
	}
}

func conditionsEqualByTypeStatus(a, b []metav1.Condition) bool {
	if len(a) != len(b) {
		return false
	}
	amap := make(map[string]metav1.ConditionStatus)
	bmap := make(map[string]metav1.ConditionStatus)
	for _, c := range a {
		amap[c.Type] = c.Status
	}
	for _, c := range b {
		bmap[c.Type] = c.Status
	}
	return reflect.DeepEqual(amap, bmap)
}

func TestConditionSeverity(t *testing.T) {
	tests := []struct {
		name  string
		input metav1.ConditionStatus
		want  int
	}{
		{"True", metav1.ConditionTrue, 2},
		{"Unknown", metav1.ConditionUnknown, 1},
		{"False", metav1.ConditionFalse, 0},
		{"Other", "other", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callConditionSeverity(tt.input)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// callConditionSeverity is a helper to access the unexported conditionSeverity function from the route package.
func callConditionSeverity(status metav1.ConditionStatus) int {
	return conditionSeverity(status)
}
