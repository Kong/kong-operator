package v1beta1_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// dummyHub implements conversion.Hub but is not the expected type for conversion.
type dummyHub struct{}

// Hub implements conversion.Hub for dummyHub
func (d *dummyHub) Hub() {}

// GetObjectKind implements runtime.Object methods for dummyHub
func (d *dummyHub) GetObjectKind() schema.ObjectKind { return schema.EmptyObjectKind }

// DeepCopyObject implements runtime.Object for dummyHub
func (d *dummyHub) DeepCopyObject() runtime.Object { return &dummyHub{} }

func testConversionError(t *testing.T, testFunc func() error, expectedMsgFormat string) {
	t.Helper()
	err := testFunc()
	require.Error(t, err)
	expectedMsg := fmt.Sprintf(expectedMsgFormat, &dummyHub{})
	require.EqualError(t, err, expectedMsg)
}
