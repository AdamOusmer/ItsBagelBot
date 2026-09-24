package data

import "ItsBagelBot/pkg/codec"

// UnmarshalJSON accepts old numeric counters and new exact decimal strings.
func (v *CommandChangedDTO) UnmarshalJSON(body []byte) error {
	type plain CommandChangedDTO
	return codec.UnmarshalInt64StringField(body, "uses", (*plain)(v))
}
