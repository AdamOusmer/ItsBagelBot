# Web workspace context

`web/` is the Bun workspace for the product's web apps and domain-oriented `@bagel/kit` package. The reusable `@bagel/ui` design system lives outside this workspace at `ui/` and is linked by the workspace install script.

Use the [shared vocabulary](../CONTEXT.md) and [context map](../CONTEXT-MAP.md), then load only the guide for the app or package you are changing.

| Area | Responsibility | Guide |
| --- | --- | --- |
| Dashboard | Broadcaster-facing account onboarding, configuration, and public channel command pages. | [context](dashboard/context.md) |
| Admin | Staff-only fleet, account, operations, and promotion management. | [context](admin/context.md) |
| Marketing | Public product/marketing site. | [context](marketing/context.md) |
| Docs | Public guides, reference material, and architecture decisions. | [context](docs/context.md) |
| Kit | Product contracts, catalogs, validation, server integration helpers, and console composition. | [context](kit/context.md) |
| UI | Shared visual components, styles, tokens, and interaction primitives. | [context](../ui/context.md) |

## Shared conventions

- Dashboard and Admin are SvelteKit apps. Marketing and Docs use Astro. Read each app's scripts instead of assuming all apps build or test the same way.
- Product vocabulary is shared across UI copy, validation, Go contracts, and bot behavior. Premium access, Tebex subscriptions, account VIP, Twitch VIP, and trial channels are different concepts.
- Kit carries ItsBagelBot-specific semantics; UI carries reusable presentation and interaction. Decide ownership before adding a component or catalog entry.
- Web server handlers reach domain services through the established server/RPC helpers. Trace DTO changes to the Go owner rather than introducing another service's direct database reads.
- `package.json` is the command authority. The workspace `build` script builds Dashboard and Admin only; the workspace `test` script runs isolated Bun suites across Kit and all web apps, excludes browser specs, and checks bundle/motion budgets.
- Avoid exploring `node_modules`, `.svelte-kit`, `.astro`, `build`, or `dist` to learn source behavior. Generated catalogs have source generators documented in the package guides.

## Workspace commands

Run these from `web/`:

```sh
bun install --frozen-lockfile
bun run check
bun run build
bun run test
bun run dev:dashboard
bun run dev:admin
```

Installation runs the UI link/production-install postinstall step. Use the app-local guide for scoped validation and Marketing/Docs builds. The console CI checks this workspace; separate UI CI validates the external UI package.
