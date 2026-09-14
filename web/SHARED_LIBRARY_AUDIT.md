# Shared-library audit — 2026-09-14

Three subagents reviewed dashboard, admin, and marketing/docs independently. The
integration pass reviewed `web/kit`, shared exports, global styles, and the
combined changes. The scan covered routes, components, scripts, helpers,
server adapters, configuration, and existing library contracts.

## Changes

- Dashboard: shared checkboxes, search inputs, icon actions, keyboard hints,
  visually hidden text, discard guards, clipboard operations, and motion queries.
- Admin: shared visible form controls and action links; status dots, pill badges,
  and CSV generation/downloads now draw from `ui/`.
- Marketing/docs: shared presentation blocks, site destinations, command limits,
  template lexer, fetch-path grammar, scroll access, and motion queries. The docs
  diagram viewer uses the shared overlay stack and focus trap.
- UI library: extracted picker panels, notification bell, profile-menu styling,
  navigation index/count styling, list reset, Lenis styling, CSV utilities,
  invalid-field focus, and scroll-position tracking. Shared controls gained the
  attributes and binding support needed by their callers.
- Kit: one validated return-path helper serves language and authentication
  redirects. Presentation exceptions dropped from six to one: the third-party
  Bolota adapter retains its engine-specific SVG layout rule.

## Boundaries retained

Application routes, permissions, sessions, service calls, mutations, domain
catalogs, translated content, and tutorial sample data remain with their owners.
Kit wrappers bind that data to UI contracts. Native hidden form fields,
authored Markdown, runtime-generated diagrams, and page artwork/composition are
not independent implementations of shared controls. Raw markup that already
uses a shared CSS contract need not become an adapter merely to change its name.

The audit found and addressed concrete reuse gaps; it is not a proof that every
future abstraction opportunity has been exhausted. Existing presentation guards
remain enabled without adding exceptions or counted debt.

## Verification and existing limitations

- UI library: typecheck, Svelte check, framework/layer/catalog guards, 268 tests,
  and bundle-size budgets pass.
- Web tests: 942 pass, one optional Valkey integration test skips; both
  presentation guards pass with zero counted debt.
- Both console typechecks pass; dashboard retains two existing warnings.
- Production builds pass for dashboard, admin, marketing (58 pages), and docs
  (46 pages). Console builds also verify demo gating and production output.
- Focused browser checks cover numeric input bindings and three marketing guide
  widgets, including shared template grammar.
- Marketing's standalone Astro check reports 18 errors verified against HEAD:
  sitemap link optionality, Encryption nullability, guide tone values, and a
  readonly SocialRail input. One guide browser test expects seven links although
  HEAD already contains fourteen; the other three selected tests pass.
- CodeScene follow-up: Luna agents simplified Twitch configuration validation
  and browser-test readiness checks; both reviewed files score 10.0. Hosted
  CodeScene passed. The scheduler tests now isolate their module instance and
  restore fake globals, fixing the CI file-order dependency. The UI suite also
  passes with randomized test order.
- Installed Bun is 1.3.10; the workspace specifies 1.4.2 and lockfile format 3.
  The docs workspace dependency was mirrored in the existing lockfile without
  re-resolving packages. CI verified frozen installation using the pinned
  Bun 1.4.2 runtime; local verification uses the installed dependencies.
