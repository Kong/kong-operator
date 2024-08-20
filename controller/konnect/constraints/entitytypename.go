package constraints

// EntityTypeName returns the name of the entity type.
func EntityTypeName[T SupportedKonnectEntityType]() string {
	var e T
	return e.GetTypeName()
}
