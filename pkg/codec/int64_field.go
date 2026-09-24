package codec

import (
	"fmt"
	"strconv"
)

// UnmarshalInt64StringField accepts historical integer JSON and decimal strings
// for a field declared with the JSON string option. dst must not call this
// function recursively (use a local alias of a type with UnmarshalJSON).
func UnmarshalInt64StringField(body []byte, field string, dst any) error {
	var fields map[string]RawMessage
	if err := Unmarshal(body, &fields); err != nil {
		return err
	}
	if err := normalizeInt64StringField(fields, field); err != nil {
		return err
	}
	normalized, err := FastMarshal(fields)
	if err != nil {
		return err
	}
	return Unmarshal(normalized, dst)
}

func normalizeInt64StringField(fields map[string]RawMessage, field string) error {
	raw, ok := fields[field]
	if !ok || string(raw) == "null" {
		return nil
	}
	value, err := int64FieldText(raw)
	if err != nil {
		return err
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return fmt.Errorf("%s: invalid signed int64: %w", field, err)
	}
	quoted, err := FastMarshal(value)
	if err != nil {
		return err
	}
	fields[field] = quoted
	return nil
}

func int64FieldText(raw RawMessage) (string, error) {
	if len(raw) == 0 || raw[0] != '"' {
		return string(raw), nil
	}
	var value string
	if err := Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}
