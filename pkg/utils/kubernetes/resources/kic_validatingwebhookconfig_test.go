package resources_test

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admregv1 "k8s.io/api/admissionregistration/v1"

	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
)

func TestGenerateValidatingWebhookConfigurationForControlPlane(t *testing.T) {
	testCases := []struct {
		image         string
		expectedError error
		devMode       bool
	}{
		{
			image: "kong/kubernetes-ingress-controller:3.2.0",
		},
		{
			image: "kong/kubernetes-ingress-controller:3.1.2",
		},
		{
			image:         "kong/kubernetes-ingress-controller:3.0.0",
			expectedError: k8sresources.ErrControlPlaneVersionNotSupported,
		},
		{
			image:   "kong/kubernetes-ingress-controller:febecdfe",
			devMode: true,
		},
		{
			image:   "kong/nightly-ingress-controller:nightly",
			devMode: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.image, func(t *testing.T) {
			cfg, err := k8sresources.GenerateValidatingWebhookConfigurationForControlPlane(
				"webhook",
				tc.image,
				tc.devMode,
				admregv1.WebhookClientConfig{
					Service: &admregv1.ServiceReference{
						Name:      "svc",
						Namespace: "ns",
					},
				},
			)
			if tc.expectedError != nil {
				require.Equal(t, tc.expectedError, err)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, "webhook", cfg.Name)
			for _, wh := range cfg.Webhooks {
				assert.Equal(t, "ns", wh.ClientConfig.Service.Namespace, "each webhook should have service namespace set")
				assert.Equal(t, "svc", wh.ClientConfig.Service.Name, "each webhook should have service name set")
				assert.Equal(t, lo.ToPtr(admregv1.Ignore), wh.FailurePolicy, "each webhook should have failure policy set")

				assert.NotEmpty(t, wh.Rules, "each webhook should have rules set")

				for _, rule := range wh.Rules {
					assert.NotEmpty(t, rule.APIGroups, "each rule should have APIGroups set")
					assert.NotEmpty(t, rule.APIVersions, "each rule should have APIVersions set")
					assert.NotEmpty(t, rule.Resources, "each rule should have Resources set")
					assert.NotEmpty(t, rule.Operations, "each rule should have Operations set")
				}
			}
		})
	}
}
