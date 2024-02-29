package resources_test

import (
	"testing"

	"github.com/Masterminds/semver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admregv1 "k8s.io/api/admissionregistration/v1"

	"github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
)

func TestGenerateValidatingWebhookConfigurationForControlPlane(t *testing.T) {
	testCases := []struct {
		version       *semver.Version
		expectedError error
	}{
		{
			version: semver.MustParse("3.2.0"),
		},
		{
			version: semver.MustParse("3.1.0"),
		},
		{
			version:       semver.MustParse("3.0.0"),
			expectedError: resources.ErrControlPlaneVersionNotSupported,
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.version.String(), func(t *testing.T) {
			cfg, err := resources.GenerateValidatingWebhookConfigurationForControlPlane("webhook", tc.version, admregv1.WebhookClientConfig{
				Service: &admregv1.ServiceReference{
					Name:      "svc",
					Namespace: "ns",
				},
			})
			if tc.expectedError != nil {
				require.Equal(t, tc.expectedError, err)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, "webhook", cfg.Name)
			for _, wh := range cfg.Webhooks {
				assert.Equal(t, "ns", wh.ClientConfig.Service.Namespace, "each webhook should have service namespace set")
				assert.Equal(t, "svc", wh.ClientConfig.Service.Name, "each webhook should have service name set")

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
