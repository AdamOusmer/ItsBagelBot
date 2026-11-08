<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# URL fetch: implementation status

This is an authoring note for the implemented custom-fetch path, not the earlier multi-phase proposal. Check the source and tests for exact limits and behavior before changing the contract.

## Current implementation

- Commands owns fetch definitions and keys. Its schemas are in `app/db/commands/ent/schema/`; `app/db/commands/rpc/fetchdashboard.go` exposes the dashboard operations. See `app/db/commands/context.md` for the repository and key-handling boundaries.
- Sesame resolves `{urlfetch:...}` tokens through the Gossip RPC client. `app/twitch/sesame/engine/urlfetch.go` handles concurrent requests, effect claims, failures and external-value sanitization; `urlfetch_test.go` covers fallback behavior. Its token limit is eight, not the three suggested by the old proposal.
- Gossip's custom provider implements fetch and post in `app/gossip/internal/providers/custom/`. It obtains definitions and keys through provider dependencies, applies admission and caching, and limits returned values. Its HTTP client and URL checks are in `app/gossip/internal/core/http.go`.
- The provider builder defaults to the WARP lane, and the custom provider does not declare itself trusted. The WARP transport fails closed when its proxy is unavailable; see `app/gossip/internal/provider/builder.go` and `app/gossip/internal/core/http.go`. Code establishes this behavior, not the availability or configuration of a particular deployed sidecar.
- The dashboard command route and shared kit components are the starting points for the authoring surface. Inspect `web/dashboard/src/routes/(app)/commands/` and `web/kit/lib/` rather than assuming the proposed separate `/commands/fetches` route exists.

## Future changes

Quota changes, new fetch editing or rehearsal flows, and deployment changes need their own source-backed design and tests. Do not reuse numeric limits, rollout claims or infrastructure assumptions from the retired proposal without checking the current implementation. Preserve the URL validation, egress selection, key custody and output-sanitization boundaries when changing this path.

From the repository root:

```sh
go test ./app/twitch/sesame/engine -run 'UrlFetch|URLFetch'
go test ./app/gossip/internal/providers/custom/...
go test ./app/gossip/internal/core/...
go test ./app/db/commands/...
```
