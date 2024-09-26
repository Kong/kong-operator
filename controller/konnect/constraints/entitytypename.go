package constraints

// EntityTypeName returns the name of the entity type.
func EntityTypeName[T SupportedKonnectEntityType]() string {
	var e T
	return e.GetTypeName()
}

// EntityTypeNameForObj returns the name of the provided entity.
func EntityTypeNameForObj[T interface {
	GetTypeName() string
}](obj T) string {
	return obj.GetTypeName()
}
