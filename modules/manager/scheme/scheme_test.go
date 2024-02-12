package scheme

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestGet(t *testing.T) {
	var s *runtime.Scheme
	require.NotPanics(t, func() {
		s = Get()
	})
	require.NotNil(t, s)
}
