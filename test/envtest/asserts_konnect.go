package envtest

func objectMatchesKonnectID[
	T interface {
		GetKonnectID() string
	},
](id string) func(T) bool {
	return func(obj T) bool {
		return obj.GetKonnectID() == id
	}
}
