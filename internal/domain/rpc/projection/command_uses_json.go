package projection

import "ItsBagelBot/pkg/codec"

// UnmarshalJSON accepts old numeric counters and new exact decimal strings.
func (v *CommandView) UnmarshalJSON(body []byte) error {
	type plain CommandView
	return codec.UnmarshalInt64StringField(body, "uses", (*plain)(v))
}
