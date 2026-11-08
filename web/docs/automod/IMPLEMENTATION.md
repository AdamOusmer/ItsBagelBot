<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# AutoMod: implementation status

This is an authoring note, not a rollout plan. The merged implementation is Go code in Sesame. `app/twitch/automod/` is a placeholder for a possible standalone service: it contains no Rust executable or migration. A deployment manifest or a separate branch does not establish that such a service is running.

## Current implementation

- Twitch Ingress receives chat and folds duplicate messages into cohorts. Sesame assesses individual senders and cohorts in `app/twitch/sesame/engine/moderate.go` and `pipeline_automod.go`.
- `app/twitch/sesame/automod/` contains the gate, verdicts, configuration, lexicon and style checks, emote knowledge, adaptive observations and link checking. Shared normalization and content-floor logic live in `internal/moderation/`.
- Sesame produces typed moderation actions; Twitch Outgress executes them. Sesame also applies the outgoing content floor. Outgress does not provide an independent final text guard.
- `SESAME_AUTOMOD_ENFORCE`, `SESAME_AUTOMOD_ADAPTIVE` and `SESAME_AUTOMOD_SHIELD` default to false in `app/twitch/sesame/internal/config/config.go`. A configured module does not by itself mean enforcement is enabled. Trial traffic skips incoming moderation and its output is marked for Outgress refusal.
- Manual `Nuke` helpers and tests exist, but current production `buildDeps` does not assign `Deps.Nuke`; the registered command returns when that dependency is nil.

For ownership and current caveats, see `app/twitch/automod/context.md` and `app/twitch/sesame/context.md`. The module's settings are registered in `app/twitch/sesame/modules/automod.go` and `web/kit/lib/catalog/automod.ts`.

## Future work

A standalone implementation, new model or enforcement rollout would be a proposal, not a description of the current system. Establish its contracts, migration, safety evaluation and operational evidence in the change that implements it. Do not infer live precision or capacity from an old shadow experiment.

From the repository root, focused checks include:

```sh
go test ./app/twitch/sesame/automod/...
go test ./app/twitch/sesame/engine -run 'Automod|Moderation|Nuke|Recent|Raid'
go test ./app/twitch/sesame/modules -run 'Automod|Moderation'
```
