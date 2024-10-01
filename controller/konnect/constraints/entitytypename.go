package constraints

type typeWithName interface {
	GetTypeName() string
}

// EntityTypeName returns the name of the entity type.
func EntityTypeName[T typeWithName]() string {
	var e T
	return e.GetTypeName()
}

// EntityTypeNameForObj returns the name of the provided entity.
func EntityTypeNameForObj[T typeWithName](obj T) string {
	return obj.GetTypeName()
}
