package kongstate

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeprecated_GetKongIngressForServices(t *testing.T) {
	t.Skip("KongIngress has been deprecated and removed")
}

func TestDeprecated_GetKongIngressFromObjectMeta(t *testing.T) {
	t.Skip("KongIngress has been deprecated and removed")
}

func TestPrettyPrintServiceList(t *testing.T) {
	for _, tt := range []struct {
		name     string
		services []*corev1.Service
		expected string
	}{
		{
			name:     "an empty list of services produces an empty string",
			expected: "",
		},
		{
			name: "a single service should just return the <namespace>/<name>",
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service1",
						Namespace: corev1.NamespaceDefault,
					},
				},
			},
			expected: "default/test-service1",
		},
		{
			name: "multiple services should be comma deliniated",
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service1",
						Namespace: corev1.NamespaceDefault,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service2",
						Namespace: corev1.NamespaceDefault,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service3",
						Namespace: corev1.NamespaceDefault,
					},
				},
			},
			expected: "default/test-service[0-9], default/test-service[0-9], default/test-service[0-9]",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			re := regexp.MustCompile(tt.expected)
			require.True(t, re.MatchString(prettyPrintServiceList(tt.services)))
		})
	}
}
