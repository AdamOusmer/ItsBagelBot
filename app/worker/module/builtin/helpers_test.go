package builtin

import (
	"ItsBagelBot/app/worker/module"
)

// collector accumulates the Outputs a module emits. Since modules must not
// retain the *Output past the Emit call, each is copied into the slice.
type collector struct {
	out []module.Output
}

func (c *collector) emit(o *module.Output) { c.out = append(c.out, *o) }
