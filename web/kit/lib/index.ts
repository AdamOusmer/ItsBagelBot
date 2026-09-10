// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The buttons, cards, fields, nav, footer and shell elements are the design
// library's now. They are re-exported here under the names the console already
// imports, so moving them was a zero-diff change for 200-odd call sites -- the
// same trick the Cursor/LightField pair set below.
export { default as Icon } from '@bagel/ui/svelte/Icon.svelte';
export { default as Card } from '@bagel/ui/svelte/Card.svelte';
export { default as Button } from '@bagel/ui/svelte/Button.svelte';
export { default as ButtonLink } from '@bagel/ui/svelte/ButtonLink.svelte';
export { default as Toggle } from '@bagel/ui/svelte/Toggle.svelte';
export { default as Switch } from '@bagel/ui/svelte/Switch.svelte';
export { default as MasterToggle } from '../components/MasterToggle.svelte';
export { default as Badge } from '@bagel/ui/svelte/Badge.svelte';
export { default as PermBadge } from '../components/PermBadge.svelte';
export { default as StatTile } from '@bagel/ui/svelte/StatTile.svelte';
export { default as NavItem } from '../components/NavItem.svelte';
// Cursor and LightField are the design library's, re-exported here so the
// console's 40-odd call sites keep importing them from `@bagel/kit` like every
// other primitive. Both were kit components until the cursor/reveal PR moved
// the physics and the markup into @bagel/ui; the barrel entry is what made
// that a zero-diff change for the apps.
export { default as Cursor } from '@bagel/ui/svelte/Cursor.svelte';
export { default as RootShell } from '../components/RootShell.svelte';
export { default as AuroraBg } from '@bagel/ui/svelte/AuroraBg.svelte';
export { default as Modal } from '@bagel/ui/svelte/Modal.svelte';
export { default as AppShell } from '../components/AppShell.svelte';
export { default as Rail } from '../components/Rail.svelte';
export { default as RailItem } from '../components/RailItem.svelte';
export { default as Brand } from '@bagel/ui/svelte/Brand.svelte';
export { default as NavGroup } from '../components/NavGroup.svelte';
export { default as AccountFoot } from '../components/AccountFoot.svelte';
export { default as Topbar } from '../components/Topbar.svelte';
// The operator chip and its menu: the half of the old Topbar that binds a
// session, an avatar engine and a POST /auth/logout form, and therefore the
// half that could not move into the library.
export { default as OperatorMenu } from '../components/OperatorMenu.svelte';
export { default as Bolota } from '../components/Bolota.svelte';
export { default as ImpersonationBanner } from '../components/ImpersonationBanner.svelte';
export { default as PageHead } from '@bagel/ui/svelte/PageHead.svelte';
export { default as PageToolbar } from '@bagel/ui/svelte/PageToolbar.svelte';
export { default as SectionNav } from '@bagel/ui/svelte/SectionNav.svelte';
export { default as CardHead } from '@bagel/ui/svelte/CardHead.svelte';
export { default as AlertBanner } from '@bagel/ui/svelte/AlertBanner.svelte';
export { default as Chip } from '@bagel/ui/svelte/Chip.svelte';
export { default as MiniButton } from '../components/MiniButton.svelte';
export { default as ErrorView } from '../components/ErrorView.svelte';
export { default as ErrorScene } from '@bagel/ui/svelte/ErrorScene.svelte';
export { default as LightField } from '@bagel/ui/svelte/LightField.svelte';
export { default as Field } from '@bagel/ui/svelte/Field.svelte';
export { default as ToastHost } from '@bagel/ui/svelte/ToastHost.svelte';
export { default as SaveStatus } from '@bagel/ui/svelte/SaveStatus.svelte';
export { default as EditorFooter } from '@bagel/ui/svelte/EditorFooter.svelte';
export { default as ManagementRow } from '@bagel/ui/svelte/ManagementRow.svelte';
export { default as InspectorSurface } from '@bagel/ui/svelte/InspectorSurface.svelte';
export { default as FieldError } from '@bagel/ui/svelte/FieldError.svelte';
export { default as ConfirmDialog } from '@bagel/ui/svelte/ConfirmDialog.svelte';
export { default as Skeleton } from '@bagel/ui/svelte/Skeleton.svelte';
export { default as SkeletonStack } from '@bagel/ui/svelte/SkeletonStack.svelte';
export { default as EmptyState } from '@bagel/ui/svelte/EmptyState.svelte';
export { default as SegmentedControl } from '@bagel/ui/svelte/SegmentedControl.svelte';
export { default as RadioGroup } from '@bagel/ui/svelte/RadioGroup.svelte';
export { default as NotificationBell } from '../components/NotificationBell.svelte';
export { default as SearchInput } from '@bagel/ui/svelte/SearchInput.svelte';
export { default as DeckList } from '@bagel/ui/svelte/DeckList.svelte';
export { default as Scroller } from '@bagel/ui/svelte/Scroller.svelte';
export { default as OverviewGrid } from '@bagel/ui/svelte/OverviewGrid.svelte';
export { default as AreaSeries } from '@bagel/ui/svelte/AreaSeries.svelte';
export { default as BackgroundOrbs } from '@bagel/ui/svelte/BackgroundOrbs.svelte';

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
export * from '@bagel/ui/svelte/toast';
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
export { default as NavLink } from '@bagel/ui/svelte/NavLink.svelte';
