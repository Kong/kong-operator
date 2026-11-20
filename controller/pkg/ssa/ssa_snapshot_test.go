package ssa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"sigs.k8s.io/structured-merge-diff/v6/typed"
)

// swapAfterFirstUseConverter wraps a TypeConverterProvider and swaps the
// provider's converter right after the first ObjectToTyped call, the way a
// CRD-triggered Rebuild can land between the two conversions of one operation.
type swapAfterFirstUseConverter struct {
	p    *TypeConverterProvider
	next managedfields.TypeConverter
	used bool
}

func (s *swapAfterFirstUseConverter) ObjectToTyped(obj runtime.Object, opts ...typed.ValidationOptions) (*typed.TypedValue, error) {
	tv, err := s.p.ObjectToTyped(obj, opts...)
	if !s.used {
		s.used = true
		s.p.current.Store(&s.next)
	}
	return tv, err
}

func (s *swapAfterFirstUseConverter) TypedToObject(v *typed.TypedValue) (runtime.Object, error) {
	return s.p.TypedToObject(v)
}

func (s *swapAfterFirstUseConverter) Snapshot() managedfields.TypeConverter {
	return s.p.Snapshot()
}

func newSwappingConverter() *swapAfterFirstUseConverter {
	// Two converters built from the same CRDs still have distinct schemas.
	first, second := buildRealSchemaTypeConverter(), buildRealSchemaTypeConverter()
	p := &TypeConverterProvider{}
	p.current.Store(&first)
	return &swapAfterFirstUseConverter{p: p, next: second}
}

func Test_TypeConverterSwapMidOperation(t *testing.T) {
	base := kongServiceWithHost("old.example.com")
	overlay := kongServiceWithHost("new.example.com")

	t.Run("converting twice across a swap breaks Merge", func(t *testing.T) {
		tc := newSwappingConverter()
		baseTyped, err := tc.ObjectToTyped(base)
		require.NoError(t, err)
		overlayTyped, err := tc.ObjectToTyped(overlay)
		require.NoError(t, err)
		_, err = baseTyped.Merge(overlayTyped)
		require.ErrorContains(t, err, "expected objects with types from the same schema")
	})

	t.Run("MergeObjects uses one snapshot for both conversions", func(t *testing.T) {
		merged, err := MergeObjects(newSwappingConverter(), base, overlay)
		require.NoError(t, err)
		host := merged.Object["spec"].(map[string]any)["host"]
		assert.Equal(t, "new.example.com", host)
	})
}
