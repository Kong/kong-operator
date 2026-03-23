package index

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKongPluginsOnHTTPRoute(t *testing.T) {
	testCases := []struct {
		name string
		obj  client.Object
		want []string
	}{}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

		})
	}
}
