package ops

// matchStringField compares string-like values without reflection.
// Nil pointers are treated as empty strings to mirror the previous behavior.
func matchStringField[
	TWant ~string | ~*string,
	TGot ~string | ~*string,
](want TWant, got TGot) bool {
	return stringValueGeneric(want) == stringValueGeneric(got)
}

func stringValueGeneric[
	T ~string | ~*string,
](v T) string {
	switch value := any(v).(type) {
	case string:
		return value
	case *string:
		if value == nil {
			return ""
		}
		return *value
	default:
		return ""
	}
}
