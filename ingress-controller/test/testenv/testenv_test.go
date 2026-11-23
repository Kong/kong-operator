package testenv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsKongEnterpriseImage(t *testing.T) {
	testCases := []struct {
		image    string
		expected bool
	}{
		{image: "kong/kong-gateway", expected: true},
		{image: "kong/kong-gateway-dev", expected: true},
		{image: "kong/kong-gateway-internal", expected: true},
		{image: "docker.io/kong/kong-gateway-dev", expected: true},
		{image: "localhost:5000/kong/kong-gateway", expected: true},
		{image: "localhost/kong/kong-gateway", expected: true},
		{image: "kong/kong-gateway:3.9.0", expected: true},
		{image: "kong/kong-gateway-dev:3.9.0", expected: true},
		{image: "docker.io/kong/kong-gateway:3.9.0", expected: true},
		{image: "localhost:5000/kong/kong-gateway:3.9.0", expected: true},
		{image: "kong/kong-gateway@sha256:abc123", expected: true},
		{image: "docker.io/kong/kong-gateway-dev@sha256:abc123", expected: true},
		{image: "localhost:5000/kong/kong-gateway@sha256:abc123", expected: true},
		{image: "", expected: false},
		{image: "kong", expected: false},
		{image: "kong:3.9.0", expected: false},
		{image: "kong/kong", expected: false},
		{image: "docker.io/library/kong", expected: false},
		{image: "docker.io/library/kong:3.9.0", expected: false},
		{image: "kong/kong-gateway2", expected: false},
		{image: "kong/kong-gateway2:3.9.0", expected: false},
		{image: "kong-gateway", expected: false},
	}

	for _, tc := range testCases {
		t.Run(tc.image, func(t *testing.T) {
			assert.Equal(t, tc.expected, isKongEnterpriseImage(tc.image))
		})
	}
}

func TestKongEnterpriseEnabled(t *testing.T) {
	testCases := []struct {
		name       string
		image      string
		enterprise string
		expected   bool
	}{
		{name: "EE dev image, no override", image: "kong/kong-gateway-dev", expected: true},
		{name: "OSS image, no override", image: "kong", expected: false},
		{name: "custom image forced to EE", image: "registry.corp/platform/kong-ee", enterprise: "true", expected: true},
		{name: "EE image forced to OSS", image: "kong/kong-gateway", enterprise: "false", expected: false},
		{name: "empty override falls back to image detection", image: "kong/kong-gateway", enterprise: "", expected: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_KONG_IMAGE", tc.image)
			t.Setenv("TEST_KONG_ENTERPRISE", tc.enterprise)
			assert.Equal(t, tc.expected, KongEnterpriseEnabled())
		})
	}
}

func TestKongEnterpriseOverride_Invalid(t *testing.T) {
	t.Setenv("TEST_KONG_ENTERPRISE", "maybe")
	assert.Panics(t, func() { KongEnterpriseOverride() })
}
