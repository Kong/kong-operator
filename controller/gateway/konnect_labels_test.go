package gateway

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/metadata"
)

func TestParseLabelsAnnotationValue(t *testing.T) {
	testcases := []struct {
		name      string
		value     string
		expected  map[string]string
		expectErr bool
	}{
		{
			name:     "empty value",
			value:    "",
			expected: nil,
		},
		{
			name:     "single label",
			value:    "key1=value1",
			expected: map[string]string{"key1": "value1"},
		},
		{
			name:  "multiple labels",
			value: "key1=value1,key2=value2",
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name:  "duplicate key - last one wins",
			value: "key1=value1,key1=value2",
			expected: map[string]string{
				"key1": "value2",
			},
		},
		{
			name:      "malformed - missing equals",
			value:     "key1",
			expectErr: true,
		},
		{
			name:      "malformed - empty key",
			value:     "=value1",
			expectErr: true,
		},
		{
			name:      "malformed - trailing comma",
			value:     "key1=value1,",
			expectErr: true,
		},
		{
			name:      "malformed - value contains equals",
			value:     "key1=value1=extra",
			expectErr: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLabelsAnnotationValue(tc.value)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestMergeLabelsWithCap(t *testing.T) {
	testcases := []struct {
		name      string
		base      map[string]string
		override  map[string]string
		maxItems  int
		expected  map[string]string
		expectErr bool
	}{
		{
			name:     "both empty",
			maxItems: 5,
			expected: map[string]string{},
		},
		{
			name:     "override only",
			override: map[string]string{"a": "1"},
			maxItems: 5,
			expected: map[string]string{"a": "1"},
		},
		{
			name:     "base only",
			base:     map[string]string{"a": "1"},
			maxItems: 5,
			expected: map[string]string{"a": "1"},
		},
		{
			name:     "override wins on conflict",
			base:     map[string]string{"a": "from-base"},
			override: map[string]string{"a": "from-override"},
			maxItems: 5,
			expected: map[string]string{"a": "from-override"},
		},
		{
			name: "no conflict, both kept",
			base: map[string]string{"a": "1"},
			override: map[string]string{
				"b": "2",
			},
			maxItems: 5,
			expected: map[string]string{"a": "1", "b": "2"},
		},
		{
			name: "over cap - base-only entries dropped in ascending key order",
			base: map[string]string{
				"z-base": "1",
				"a-base": "2",
				"m-base": "3",
			},
			override: map[string]string{
				"o1": "1",
				"o2": "2",
				"o3": "3",
			},
			maxItems: 5,
			// 3 override + 3 base = 6, exceeds the maxItems of 5, so an error should be returned.
			expectErr: true,
		},
		{
			name: "override alone exceeds cap",
			override: map[string]string{
				"o1": "1",
				"o2": "2",
				"o3": "3",
				"o4": "4",
				"o5": "5",
				"o6": "6",
			},
			maxItems:  5,
			expectErr: true,
		},
		{
			name: "base alone exceeds cap",
			base: map[string]string{
				"o1": "1",
				"o2": "2",
				"o3": "3",
				"o4": "4",
				"o5": "5",
				"o6": "6",
			},
			maxItems:  5,
			expectErr: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mergeLabelsWithCap(tc.base, tc.override, tc.maxItems)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
			assert.LessOrEqual(t, len(got), tc.maxItems)
		})
	}
}

func TestValidateDPLabels(t *testing.T) {
	testcases := []struct {
		name      string
		labels    map[string]string
		expectErr bool
	}{
		{
			name:   "nil labels",
			labels: nil,
		},
		{
			name:   "valid labels",
			labels: map[string]string{"team": "payments", "env": "prod"},
		},
		{
			name: "too many labels",
			labels: map[string]string{
				"a": "1", "b": "2", "c": "3", "d": "4", "e": "5", "f": "6",
			},
			expectErr: true,
		},
		{
			name:      "key too long",
			labels:    map[string]string{strings.Repeat("k", 64): "value"},
			expectErr: true,
		},
		{
			name:      "value too long",
			labels:    map[string]string{"key": strings.Repeat("v", 64)},
			expectErr: true,
		},
		{
			name:      "key does not match pattern",
			labels:    map[string]string{"-bad-key": "value"},
			expectErr: true,
		},
		{
			name:      "value does not match pattern",
			labels:    map[string]string{"key": "bad value with spaces"},
			expectErr: true,
		},
		{
			name:      "reserved key prefix kong",
			labels:    map[string]string{"kong-internal": "value"},
			expectErr: true,
		},
		{
			name:      "reserved key prefix konnect",
			labels:    map[string]string{"konnect-internal": "value"},
			expectErr: true,
		},
		{
			name:   "k8s prefix is allowed for dataplane labels",
			labels: map[string]string{"k8s-team": "payments"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDPLabels(tc.labels)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateCPLabels(t *testing.T) {
	testcases := []struct {
		name      string
		labels    map[string]string
		expectErr bool
	}{
		{
			name:   "nil labels",
			labels: nil,
		},
		{
			name:   "valid labels",
			labels: map[string]string{"team": "payments", "env": "prod"},
		},
		{
			name: "too many labels",
			labels: map[string]string{
				"a": "1", "b": "2", "c": "3", "d": "4", "e": "5", "f": "6",
			},
			expectErr: true,
		},
		{
			name:      "key too long",
			labels:    map[string]string{strings.Repeat("k", 64): "value"},
			expectErr: true,
		},
		{
			name:      "value too long",
			labels:    map[string]string{"key": strings.Repeat("v", 64)},
			expectErr: true,
		},
		{
			name:      "reserved key prefix k8s",
			labels:    map[string]string{"k8s-team": "payments"},
			expectErr: true,
		},
		{
			name:      "reserved key prefix kong",
			labels:    map[string]string{"kong-internal": "value"},
			expectErr: true,
		},
		{
			name:   "value with spaces is allowed for control plane labels",
			labels: map[string]string{"key": "value with spaces"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCPLabels(tc.labels)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestResolveKonnectLabels(t *testing.T) {
	const annotationKey = metadata.AnnotationKeyKonnectCPLabels

	testcases := []struct {
		name         string
		gateway      *gwtypes.Gateway
		gatewayClass *gatewayv1.GatewayClass
		expected     map[string]string
		expectErr    bool
	}{
		{
			name: "no annotations anywhere",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expected: map[string]string{},
		},
		{
			name: "labels from gateway only",
			gateway: &gwtypes.Gateway{
				Annotations: map[string]string{annotationKey: "team=payments"},
			},
			expected: map[string]string{"team": "payments"},
		},
		{
			name: "labels from gatewayclass only",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				Annotations: map[string]string{annotationKey: "team=platform"},
			},
			expected: map[string]string{"team": "platform"},
		},
		{
			name: "gateway overrides gatewayclass on conflicting key",
			gateway: &gwtypes.Gateway{
				Annotations: map[string]string{annotationKey: "team=payments"},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				Annotations: map[string]string{annotationKey: "team=platform,env=prod"},
			},
			expected: map[string]string{"team": "payments", "env": "prod"},
		},
		{
			name: "malformed gateway annotation errors",
			gateway: &gwtypes.Gateway{
				Annotations: map[string]string{annotationKey: "bad"},
			},
			expectErr: true,
		},
		{
			name: "malformed gatewayclass annotation errors",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				Annotations: map[string]string{annotationKey: "bad"},
			},
			expectErr: true,
		},
		{
			name: "gateway annotation alone exceeds cap",
			gateway: &gwtypes.Gateway{
				Annotations: map[string]string{annotationKey: "a=1,b=2,c=3,d=4,e=5,f=6"},
			},
			expectErr: true,
		},
		{
			name: "gatewayclass annotation alone exceeds cap",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				Annotations: map[string]string{annotationKey: "a=1,b=2,c=3,d=4,e=5,f=6"},
			},
			expectErr: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveKonnectLabels(tc.gateway, tc.gatewayClass, annotationKey)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}
