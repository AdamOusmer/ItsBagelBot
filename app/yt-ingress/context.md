# YouTube ingress directory context

`app/yt-ingress` currently has no tracked application source, manifest, entrypoint, or service container definition. This documentation placeholder is not an implemented YouTube ingestion service; local build/dependency remnants in other checkouts do not establish an application.

See the [shared vocabulary](../../CONTEXT.md) and [context map](../../CONTEXT-MAP.md).

- **Ingress** names the receive/normalize/publish boundary elsewhere in this repository, but no YouTube event contract or pipeline is established here.
- Do not infer application behavior from local `_build/` or `deps/` artifacts or copy their generated contents into source documentation.
- For implemented platform ingestion, start with [Twitch Ingress](../twitch/ingress/context.md) or [Discord Ingress](../discord/ingress/context.md).
- There is no app-specific build/test command to run here. Implementing this app requires defining its source, platform contracts, bus routing, ownership, and deployment first.
