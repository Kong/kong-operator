package ops

import (
	"reflect"
	"slices"
)

// matchStringField compares string-like values without reflection.
// Nil pointers are treated as empty strings to mirror the previous behavior.
func matchStringField[
	TWant ~string | ~*string,
	TGot ~string | ~*string,
](want TWant, got TGot) bool {
	return stringValueGeneric(want) == stringValueGeneric(got)
}

// matchOptionalStringField compares string-like values like matchStringField,
// but treats an empty want as "not specified" and returns true without
// comparing. It is used for optional spec fields that the Konnect API may
// populate server-side (for example a SAML identity provider's metadata XML
// resolved from its metadata URL), where requiring exact equality would make a
// spec that legitimately leaves the field unset fail to match its own entity.
func matchOptionalStringField[
	TWant ~string | ~*string,
	TGot ~string | ~*string,
](want TWant, got TGot) bool {
	wantValue := stringValueGeneric(want)
	if wantValue == "" {
		return true
	}
	return wantValue == stringValueGeneric(got)
}

// matchSliceField compares two string slices for equality.
func matchSliceField(want, got []string) bool {
	return slices.Equal(want, got)
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
	}

	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return ""
	}
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.String {
		return ""
	}
	return rv.String()
}
