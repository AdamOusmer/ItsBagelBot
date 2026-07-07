package modules

import (
	"context"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
)

// Automod surfaces the inline chat guard in the module system like any other
// feature: a named KindDefault module (ships enabled; a broadcaster can disable
// it or tune it from the dashboard module page, MODULE_CATALOG id "automod").
//
// The gate itself never runs here — it is inline in the pipeline BEFORE command
// dispatch (see engine/pipeline.go), because a verdict must be able to suppress
// the dispatch of the very line it judged. This module's only jobs are:
//
//   - owning the broadcaster's row: the standard enable toggle and the config
//     blob (profile / block_terms / allow_terms) the pipeline reads via its
//     ModuleView (automodConfigFrom);
//   - registering a chat handler so the registry marks channel.chat.message as
//     needing ModuleViews, which is what makes that row reach the pipeline.
//
// The handler is a no-op: all behavior stays in the inline gate.
func Automod(_ engine.Deps) module.Module {
	m := module.NewModule("automod", module.KindDefault)
	m.On("channel.chat.message", func(context.Context, *module.Context, module.Emit) error {
		return nil
	})
	return m.Build()
}
