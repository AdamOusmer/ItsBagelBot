# Standalone AutoMod context (unimplemented)

This directory is a documentation placeholder for a potential standalone Twitch
AutoMod app. Read [shared language](../../../CONTEXT.md) and the
[app map](../../../CONTEXT-MAP.md) for the implemented owners.

## Current status

There is no executable, Cargo workspace, Containerfile, runtime configuration,
model inventory, journal transport or deployment here in the merged source.
This guide does not designate a running service or an implemented migration.
The actual incoming moderation code is Go inside [Sesame](../sesame/context.md).
A branch or local working copy can contain a different implementation; verify
its source and deployment before carrying its architecture back into this guide.

## Implemented moderation vocabulary and ownership

- **AutoMod:** incoming chat assessment in Sesame's `automod` gate and engine.
- **Gate:** normalization/lexicon/config/style/link checks returning a verdict.
- **Verdict:** proposed action/rule; recording one is separate from enforcement.
- **Shadow:** default automatic mode, where verdicts are recorded without effects.
- **Floor:** shared immutable content rules, also checked against outgoing replies.
- **Cohort:** ingress-folded equal-text messages retaining individual senders.
- **Reputation/campaign:** Valkey-backed escalation and cross-channel signals.
- **Manual sweep:** moderator-requested recent-chat match, implemented by `Nuke`.

Sesame owns assessments, channel configuration interpretation and typed output
production. Twitch ingress owns EventSub reception and duplicate-chat folding.
Twitch outgress executes moderation through its ordinary typed action registry.
Modules/Projector own authoritative settings and their read projections.

## Navigate the existing implementation

| Concern | Actual source |
| --- | --- |
| Gate and proposed actions | [gate.go](../sesame/automod/gate.go), [verdict.go](../sesame/automod/verdict.go), [config.go](../sesame/automod/config.go) |
| Pipeline moderation and cohort escalation | [moderate.go](../sesame/engine/moderate.go), [reputation_valkey.go](../sesame/engine/reputation_valkey.go), [campaign.go](../sesame/engine/campaign.go) |
| Shared normalization/floor/lexicon | [internal/moderation](../../../internal/moderation) |
| Adaptive/emote/link observations | [automod package](../sesame/automod), [linkcheck runner](../sesame/linkcheck_runner.go) |
| Settings registration | [modules/automod.go](../sesame/modules/automod.go), [web catalog](../../../web/kit/lib/catalog/automod.ts) |
| Manual sweep and retained chat | [nuke.go](../sesame/engine/nuke.go), [recent.go](../sesame/engine/recent.go), [recent_valkey.go](../sesame/engine/recent_valkey.go) |
| Runtime wiring/defaults | [main.go](../sesame/main.go), [wiring.go](../sesame/wiring.go), [config.go](../sesame/internal/config/config.go) |
| Twitch moderation executor | [outgress context](../outgress/context.md), [worker actions](../outgress/internal/worker/actions.go) |

## Existing limits to preserve

`SESAME_AUTOMOD_ENFORCE` defaults false; adaptive and shield behavior also default
false. Configuration presence alone does not imply enabled enforcement.
`Nuke` helper and tests exist, but current production `buildDeps` does not assign
`Deps.Nuke`; the registered command returns when that dependency is nil.
Outgoing floor checks are in Sesame; Outgress has no independent final text guard.
Trials skip incoming moderation and mark generated output for Outgress refusal.
No Rust v2 chat envelopes, fenced action journal or promotion workflow is implemented
by this directory. Do not add commands or links to nonexistent Rust files.

## Focused verification

From the repository root, inspect/test the actual Go owner:

```sh
go test ./app/twitch/sesame/automod/...
go test ./app/twitch/sesame/engine -run 'Automod|Moderation|Nuke|Recent|Raid'
go test ./app/twitch/sesame/modules -run 'Automod|Moderation'
```

There is no app-local build or run command for this placeholder. When a standalone
implementation is introduced, replace this guide with source-verified contracts,
ownership, navigation and checks in the same change.
