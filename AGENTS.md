<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Agent instructions

## Shared packages

Service code never calls Valkey, or any other backend a shared package owns, directly.

- Use the shared packages (`pkg/valkey`, `pkg/bus`, `pkg/db`, `pkg/ratelimit` and the rest). No raw client commands or Lua scripts in `app/`.
- If a shared package lacks the operation you need, add it to that package and call it from there. Extend the package; do not work around it.
- Declare things through the existing builders, so timeouts, metrics and identity checks stay in place.
