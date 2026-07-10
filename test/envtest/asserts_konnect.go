package envtest

// ObjectMatchesKonnectID returns a function that checks if an object's
// Konnect ID matches the given ID.
func ObjectMatchesKonnectID[
	T interface {
		GetKonnectID() string
	},
](id string) func(T) bool {
	return func(obj T) bool {
		return obj.GetKonnectID() == id
	}
}
