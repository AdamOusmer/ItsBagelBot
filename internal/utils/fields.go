package utils

// BoolField encodes a bool as a compact Valkey/Redis hash field value: "1" for
// true, "0" for false. This is used in hash fields where "true"/"false" would
// waste space and "1"/"0" is the idiomatic Redis convention.
func BoolField(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
