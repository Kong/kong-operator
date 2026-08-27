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

// matchSliceField compares two string slices for equality.
func matchSliceField(want, got []string) bool {
	return slices.Equal(want, got)
}

// matchSensitiveDataSourceField compares a SensitiveDataSource against a
// string-like SDK response field. When the source is inline, the Value is
// compared; when it is a secretRef (Value is nil), the comparison is skipped
// and the function returns true so the field does not block a UID match.
func matchSensitiveDataSourceField[TGot ~string | ~*string](
	want interface {
		GetValue() string
	},
	got TGot,
) bool {
	if want.GetValue() == "" {
		// secretRef: resolved value is not available here — skip match.
		return true
	}
	return want.GetValue() == stringValueGeneric(got)
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
