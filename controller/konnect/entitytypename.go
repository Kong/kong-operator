package konnect

func entityTypeName[T SupportedKonnectEntityType]() string {
	var e T
	return e.GetTypeName()
}
