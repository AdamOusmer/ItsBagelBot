package projection

import "ItsBagelBot/pkg/codec"

// UnmarshalJSON accepts old numeric counters and new exact decimal strings.
func (v *Command) UnmarshalJSON(body []byte) error {
	type plain Command
	return codec.UnmarshalInt64StringField(body, "uses", (*plain)(v))
}
