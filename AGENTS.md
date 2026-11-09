<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Agent instructions

This repository is public. Keep operator runbooks, host scripts, benchmark results and competitor product names out of every tracked file, commit message and branch name.

## Workflow

- Start each task in its own worktree cut from a fresh `origin/main`: `git worktree add .claude/worktrees/<type>+<name> -b <type>/<name> origin/main`. Branch from an unmerged PR only when the work depends on it. A fresh worktree needs `bun install` in `web`, then in `ui`.
- Never edit in another session's worktree or PR branch, and never use `git stash`. The stash is shared across worktrees. For a baseline, add a throwaway worktree of `origin/main`.
- Back claims with the code on `origin/main` and with real runs. Cite `file:line`.
- Stage explicit paths. A single-package `go build` writes a binary into the current directory, so compile-check with `go build -o /dev/null ./app/<svc>/` or `go vet`.

## Branches and pull requests

- Never create or rename a branch with the `codex/` prefix. Use a task-appropriate conventional prefix such as `fix/`, `feature/`, `chore/`, `refactor/`, `docs/`, `test/`, or `perf/`, unless the user explicitly requests a branch name.
- Ship only the task's own changes. Before merging, check `git rev-list --count origin/main..<branch>` and `git diff --stat origin/main...<branch>`, and report anything not written for the task.
- When opening or updating a PR, resolve CodeScene findings introduced or exposed by the change. Before merging, check the latest CodeScene results, fix outstanding findings, and rerun the check until it passes. If the check is unavailable, report that limitation; do not claim it passed.
- CodeScene is the only required check, so a green CodeScene alone does not make a PR safe to merge. Wait for every check (`gh pr checks <n> --watch`). Before enabling auto-merge on a PR that touches shared UI or adds a `main.go`, run `bun run build` in `web/dashboard` and `web/admin`, plus `go test ./deploy/...`.
- Stacked PRs merge one at a time from the bottom. CodeScene runs only on PRs based on `main`, so an atomic stack merge fails. After each merge, confirm `origin/main` moved, retarget the next PR to `main`, wait for its checks, then merge it. Never chain ref moves after a rebase in one command.
- After confirming a PR has merged, delete its remote and local feature branches and clean up its task worktree. Preserve uncommitted or unrelated work; never force-delete it. Use the worktree management tools to archive tool-managed worktrees. Never delete the primary checkout or default branch.

## CodeScene

- Fix every finding, including advisory and test-file ones. Never use the suppress links.
- Reproduce locally before pushing: `git fetch`, then `zsh -lic 'CS_DISABLE_VERSION_CHECK=1 cs delta origin/main'` until it reports no issues. Local runs can miss findings, so also grep touched files for conditionals with two or more logical operators.
- Limits that bite: cyclomatic complexity 9 is flagged, so aim for 8 or less; at most 2 branches in a conditional inside `if`/`for` (extract a named predicate); no nested conditional blocks (Bumpy Road); at most 4 function parameters; at most 39% of a file's function arguments may be `string` (use named string types or methods); 4 or more consecutive assertions form a Large Assertion Block.
- Code Duplication flags sibling functions with the same shape. Merge them into one body or one table-driven test; a shared helper with parallel callers still fails.
- Findings surface one at a time per file. Fix the whole touched file in one push. Touching a file rescores all of it, so an old violation becomes your finding.

## Code

- Service code never calls Valkey, or any other backend a shared package owns, directly. Use the shared packages (`pkg/valkey`, `pkg/bus`, `pkg/db`, `pkg/ratelimit` and the rest). No raw client commands or Lua scripts in `app/`.
- If a shared package lacks the operation you need, add it to that package and call it from there. Extend the package; do not work around it.
- Declare things through the existing builders, so timeouts, metrics and identity checks stay in place.
- A service owns exactly one MySQL schema. The Elixir ingress (`app/ingress`) never opens a database connection; it fetches data over NATS request-reply from the owning Go service.
- The firehose JetStream streams (`TWITCH_*`, `DISCORD_*`) and their consumers stay on memory storage, including in recovery fixes.
- No defensive scaffolding: no rollback-on-partial-failure loops, guard wrappers or cleanup-on-error unwinds. Let errors propagate to the coarse failure path. Remove such code when touching it.
- No fixed global or per-channel internal capacity caps. Rate limits toward Twitch and outside APIs stay, and outgress and gossip own their buckets. Never change their rates without a stated number.
- Viewer identity is the Twitch user id. Chat commands use ids from event data only. Dashboard search may match names and reconcile a stored login on a miss. Add no other login resolution.
- Recipe tokens use a colon (`{cooldown:left}`, `{var:points}`). Variable names are one lowercase word, with no `_`, `:` or `-`.
- Comments are limited to license headers, tool directives, type-bearing JSDoc, and one-line warnings where a wrong edit causes an outage, a security hole, data loss or a cross-service break. Removing comments can expose CodeScene duplication.

## Tests

- Test behavior through exported seams: package API, RPC or HTTP handlers, pipeline `Process`, repositories on SQLite, embedded NATS. No tests whose subject is an unexported helper, except regression tests added with a fix.
- Product policy pins (permission levels, default-enabled state, shipped commands, mirrored Go/TS limits) are contracts. Keep them as one table.
- Valkey-backed tests run only locally (`VALKEY_TEST_ADDR` or a local `valkey-server`).
- Load generators, probes and benchmarks never enter the tree or the image publish matrix.

## Builds and deploys

- Use podman, never Docker. Say ARM and Intel, never amd64, and never cross-build under emulation.
- Go service Containerfiles copy `internal/` directories one by one. A new internal package needs a `COPY` line in every importing service's Containerfile.
- `web/dashboard/Containerfile.ignore` and `web/admin/Containerfile.ignore` are allowlists. A new root-level directory the web build reads needs its own `!dir` line in both.
- First-party images in `deploy/k8s` are pinned as `tag@sha256:<index digest>` (`deploy/k8s/image_pins_test.go`).
- Every live change is merged on git before it is applied, and images come only from git-built artifacts. Deploy only the requested service; never apply a whole directory or bump an unrelated pin from `main`.

## UI

- Never rebuild what exists. Find the shared `ui` block, wire it the way the real dashboard does, and add an opt-in prop only when it truly lacks something.
- App stylesheets in `web/*/src` never target `.bb-*` classes or bare elements; `:root` token overrides are allowed. Element looks belong in `ui/styles/elements/*.css`. The library has one style, with no surface scopes.
- Radius scale (`--bb-radius-*` in `ui/styles/brand.css`): xs 4, sm 8, md 12, lg 16, pill. Controls use sm, containers md, hero surfaces lg. Nested corners are concentric.
- State colors: free green, paid gold, VIP silver (never purple), banned red, inactive steel blue.
- No emoji, grid or checker patterns, or colored accent borders. Reuse the site background (motes, ring, orbs). Texture belongs on chosen sections only.
- No layout shift. Conditional controls stay in the tree and fade, and a language switch never reloads a stepped flow. Keep no step state in the URL. No magnetic buttons.
- Onboarding and setup stages fit the viewport without scrolling on desktop and phone.
- Frame limiters skip whole refreshes, never a time accumulator. The cursor keeps its difference blend.

## Locales and copy

- Every translated key falls back to English; a raw key on screen is a bug. Never split catalogs with hand-kept route tables.
- Layout is identical in every locale. Boxes keep a fixed size and text scales down inside `data-fit` spans (`ui/lib/text-fit.ts`). Never grow a container per language. Check every locale at desktop, tablet and phone sizes before merging.
- Every English change is mirrored in the other locales and passes `web/kit/scripts/check-i18n.mjs`.
- Brand lines such as "Baked late, served fresh." are never rewritten.
- No em dashes anywhere in `web/` or `ui/`. Avoid "no X, no Y, no Z" lists, "Not X. Y." pivots and numeric parallel headlines. Each card carries one concrete fact and stays short.
