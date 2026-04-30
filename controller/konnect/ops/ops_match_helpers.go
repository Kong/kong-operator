package ops

import "reflect"

// matchStringField compares string-like values from generated getForUID code.
// Nil pointers are treated as empty strings to mirror existing manual matching.
func matchStringField(want, got any) bool {
	return stringFieldValue(want) == stringFieldValue(got)
}

func stringFieldValue(v any) string {
	if v == nil {
		return ""
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
