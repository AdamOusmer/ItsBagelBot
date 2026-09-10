// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// THE WHOLE DESIGN LIBRARY, re-exported under the names the console already
// imports. `import { Button, Card, Stack } from '@bagel/kit'` resolves to
// @bagel/ui's adapters; moving an element into the library has therefore been
// a zero-diff change for ~200 call sites, three times now.
//
// One star export rather than the 40-odd hand-written lines that used to be
// here. Those had to be edited every time the library grew, and the library
// growing is the point -- twice already a new element landed in ui/ and stayed
// invisible to the console for a week because nobody added the line. The
// catalog gate (ui/scripts/gen-catalog.mjs) keeps ui/svelte/index.ts complete,
// so this one line now inherits that guarantee.
//
// IT DOES NOT COST BUNDLE SIZE. Each adapter JS-imports its own contract
// stylesheet, so the fear is that a star export drags every stylesheet in.
// It does not: @bagel/ui declares `sideEffects: ["**/*.css"]`, which tells
// Rollup the adapter MODULES are side-effect free, so an unused one is dropped
// with its CSS import. Verified against web/kit/scripts/size.ts, whose per-
// entry budgets are unchanged by this line.
//
// Kit's own components are exported BELOW and win: an explicit local export
// takes precedence over a star export of the same name, which is what lets
// AppShell, Rail, RailItem, Topbar, NavGroup and NavItem stay the console's
// bot-aware versions while everything else comes from the library.
export * from '@bagel/ui/svelte';

// TWO NAMES HAVE TO BE PULLED OUT OF THE STAR, because kit exports a TYPE with
// each name from its own modules (`NavLink` from ./types, `FieldError` from
// ./validation) and two star exports of one name resolve to nothing rather
// than to one of them — silently, at the import site, as "has no exported
// member". An explicit re-export wins over both stars and restores exactly the
// binding the console had before the star landed: the COMPONENT.
export { default as NavLink } from '@bagel/ui/svelte/NavLink.svelte';
export { default as FieldError } from '@bagel/ui/svelte/FieldError.svelte';

export { default as MasterToggle } from '../components/MasterToggle.svelte';
export { default as PermBadge } from '../components/PermBadge.svelte';
export { default as NavItem } from '../components/NavItem.svelte';
// The bot-aware halves of the shell. Each one binds a session, a board list or
// a permission ladder, which is exactly the knowledge the library must not
// have; each renders the corresponding @bagel/ui element inside itself. Named
// the same as the library's on purpose -- an explicit export wins over the
// star above, so a console call site gets the bot-aware one and a static
// surface importing from @bagel/ui/svelte gets the pure one.
export { default as RootShell } from '../components/RootShell.svelte';
export { default as AppShell } from '../components/AppShell.svelte';
export { default as Rail } from '../components/Rail.svelte';
export { default as RailItem } from '../components/RailItem.svelte';
export { default as NavGroup } from '../components/NavGroup.svelte';
export { default as AccountFoot } from '../components/AccountFoot.svelte';
export { default as Topbar } from '../components/Topbar.svelte';
// The operator chip and its menu: the half of the old Topbar that binds a
// session, an avatar engine and a POST /auth/logout form, and therefore the
// half that could not move into the library.
export { default as OperatorMenu } from '../components/OperatorMenu.svelte';
export { default as Bolota } from '../components/Bolota.svelte';
export { default as ImpersonationBanner } from '../components/ImpersonationBanner.svelte';
export { default as MiniButton } from '../components/MiniButton.svelte';
export { default as ErrorView } from '../components/ErrorView.svelte';
export { default as NotificationBell } from '../components/NotificationBell.svelte';

export { initLenis, magnetic, countUp } from './actions';
export { copyFlash } from '@bagel/ui/lib/clipboard';
export { icons, type IconName } from '@bagel/ui/lib/icons';
export { customCursor } from './cursor';

// i18n: context helpers for components + the pure runtime/detection surface.
export { setI18n, getI18n, type I18n } from './i18n/context';
export {
  translate,
  translateList,
  detectLocale,
  isLocale,
  localeName,
  ensureCatalog,
  LOCALES,
  DEFAULT_LOCALE,
  LOCALE_COOKIE,
  type Locale
} from './i18n/messages';
export * from './types';
export * from './module-index';
export * from './nav';
export * from './social';
export * from './spotify';
export * from './connection-state';
export * from '@bagel/ui/lib/overlay-stack';
export * from '@bagel/ui/lib/inspector-machine';
export * from './command-active';
export * from './discord-config';
export * from './discord-overview';
export * from './engine/commands-validate';
export * from './uses';
export * from './engine/rehearsal';
// Only the span builder is re-exported from the lexer, not the lexer itself:
// a component that needs to READ a template rehearses it (./rehearsal), while
// one that needs to WRITE a span out of a catalog string must not hand-build
// it. `export *` would also collide with rehearsal's own Token alias.
export { intactSpan } from './engine/tmpl';
export * from './validation';
export * from './action-result';
export * from './format';
