package managedfields

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Custom types for error branches
// These are used in extract_test.go for coverage of ExtractAsUnstructured

type (
	brokenObject       struct{}
	brokenUnstructured struct{}
	noMetaObject       struct{}
)

type brokenKind struct{}

// GetObjectKind returns a dummy ObjectKind for brokenObject, used for error simulation in tests.
func (b *brokenObject) GetObjectKind() schema.ObjectKind { return &brokenKind{} }

// DeepCopyObject returns itself for brokenObject, simulating a broken deep copy.
func (b *brokenObject) DeepCopyObject() runtime.Object { return b }

// GetObjectKind returns a dummy ObjectKind for brokenUnstructured, used for error simulation in tests.
func (b *brokenUnstructured) GetObjectKind() schema.ObjectKind { return &brokenKind{} }

// DeepCopyObject returns itself for brokenUnstructured, simulating a broken deep copy.
func (b *brokenUnstructured) DeepCopyObject() runtime.Object { return b }

// GetObjectKind returns a dummy ObjectKind for noMetaObject, used for error simulation in tests.
func (n *noMetaObject) GetObjectKind() schema.ObjectKind { return &brokenKind{} }

// DeepCopyObject returns itself for noMetaObject, simulating a broken deep copy.
func (n *noMetaObject) DeepCopyObject() runtime.Object { return n }

// SetGroupVersionKind is a no-op for brokenKind, simulating a broken kind.
func (b *brokenKind) SetGroupVersionKind(_ schema.GroupVersionKind) {}

// GroupVersionKind returns an empty GroupVersionKind for brokenKind, simulating a broken kind.
func (b *brokenKind) GroupVersionKind() schema.GroupVersionKind { return schema.GroupVersionKind{} }
