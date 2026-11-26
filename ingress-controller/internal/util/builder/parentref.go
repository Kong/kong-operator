package builder

import (
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// ParentReferenceBuilder is a builder for constructing gwtypes.ParentReference objects.
// Primarily used for testing.
type ParentReferenceBuilder struct {
	pr gwtypes.ParentReference
}

// NewParentReference creates a new instance of ParentReferenceBuilder.
func NewParentReference() *ParentReferenceBuilder {
	return &ParentReferenceBuilder{
		pr: gwtypes.ParentReference{},
	}
}

// Group sets the Group field of the ParentReference.
func (b *ParentReferenceBuilder) Group(group gwtypes.Group) *ParentReferenceBuilder {
	b.pr.Group = &group
	return b
}

// Kind sets the Kind field of the ParentReference.
func (b *ParentReferenceBuilder) Kind(kind gwtypes.Kind) *ParentReferenceBuilder {
	b.pr.Kind = &kind
	return b
}

// Namespace sets the Namespace field of the ParentReference.
func (b *ParentReferenceBuilder) Namespace(namespace gwtypes.Namespace) *ParentReferenceBuilder {
	b.pr.Namespace = &namespace
	return b
}

// Name sets the Name field of the ParentReference.
func (b *ParentReferenceBuilder) Name(name gwtypes.ObjectName) *ParentReferenceBuilder {
	b.pr.Name = name
	return b
}

// SectionName sets the SectionName field of the ParentReference.
func (b *ParentReferenceBuilder) SectionName(sectionName gwtypes.SectionName) *ParentReferenceBuilder {
	b.pr.SectionName = &sectionName
	return b
}

// Build returns the configured ParentReference.
func (b *ParentReferenceBuilder) Build() gwtypes.ParentReference {
	return b.pr
}
