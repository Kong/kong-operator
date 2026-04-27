package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterOutCRDsByNameKeepsRootXKonnectCRDs(t *testing.T) {
	t.Parallel()

	content := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: konnecteventgateways.x-konnect.konghq.com
spec:
  group: x-konnect.konghq.com
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: portals.x-konnect.konghq.com
spec:
  group: x-konnect.konghq.com
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: dcrproviders.x-konnect.konghq.com
spec:
  group: x-konnect.konghq.com
`

	filtered := filterOutCRDsByName(content, "dcrproviders.x-konnect.konghq.com")

	require.Contains(t, filtered, "konnecteventgateways.x-konnect.konghq.com")
	require.Contains(t, filtered, "portals.x-konnect.konghq.com")
	require.NotContains(t, filtered, "dcrproviders.x-konnect.konghq.com")
}
