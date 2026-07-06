package modules

import (
	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
)

// All builds every module wired for the service, in registration order. Adding a
// feature is writing its file and adding one line here. Core modules come first
// so their reserved commands win the registry's first-wins de-dup over any named
// module that might declare a clashing trigger.
func All(d engine.Deps) []module.Module {
	return []module.Module{
		Core(d),
		Live(d),
		Cmd(d),
		Shoutout(d),
		Alerts(d),
		Clip(d),
		Urchin(d),
		Mcsr(d),
	}
}
