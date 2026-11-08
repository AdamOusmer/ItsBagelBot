<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Agent instructions

## Git branches and pull requests

- Never create or rename a branch with the `codex/` prefix. Use a task-appropriate conventional prefix such as `fix/`, `feature/`, `chore/`, `refactor/`, `docs/`, `test/`, or `perf/`, unless the user explicitly requests a branch name.
- When opening or updating a PR, resolve CodeScene findings introduced or exposed by the change. Before merging, check the latest CodeScene results, fix outstanding findings, and rerun the check until it passes. If the check is unavailable, report that limitation; do not claim it passed.
- After confirming a PR has merged, delete its remote and local feature branches and clean up its task worktree. Preserve uncommitted or unrelated work; never force-delete it. Use the worktree management tools to archive tool-managed worktrees. Never delete the primary checkout or default branch.

## Shared packages

Service code never calls Valkey, or any other backend a shared package owns, directly.

- Use the shared packages (`pkg/valkey`, `pkg/bus`, `pkg/db`, `pkg/ratelimit` and the rest). No raw client commands or Lua scripts in `app/`.
- If a shared package lacks the operation you need, add it to that package and call it from there. Extend the package; do not work around it.
- Declare things through the existing builders, so timeouts, metrics and identity checks stay in place.
