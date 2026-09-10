// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export { default as Icon } from '../components/Icon.svelte';
export { default as Card } from '../components/Card.svelte';
export { default as Button } from '../components/Button.svelte';
export { default as ButtonLink } from '../components/ButtonLink.svelte';
export { default as Toggle } from '../components/Toggle.svelte';
export { default as Switch } from '../components/Switch.svelte';
export { default as MasterToggle } from '../components/MasterToggle.svelte';
export { default as Badge } from '../components/Badge.svelte';
export { default as StatTile } from '../components/StatTile.svelte';
export { default as NavItem } from '../components/NavItem.svelte';
// Cursor and LightField are the design library's, re-exported here so the
// console's 40-odd call sites keep importing them from `@bagel/kit` like every
// other primitive. Both were kit components until the cursor/reveal PR moved
// the physics and the markup into @bagel/ui; the barrel entry is what made
// that a zero-diff change for the apps.
export { default as Cursor } from '@bagel/ui/svelte/Cursor.svelte';
export { default as RootShell } from '../components/RootShell.svelte';
export { default as AuroraBg } from '../components/AuroraBg.svelte';
export { default as Modal } from '../components/Modal.svelte';
export { default as AppShell } from '../components/AppShell.svelte';
export { default as Rail } from '../components/Rail.svelte';
export { default as RailItem } from '../components/RailItem.svelte';
export { default as Brand } from '../components/Brand.svelte';
export { default as NavGroup } from '../components/NavGroup.svelte';
export { default as AccountFoot } from '../components/AccountFoot.svelte';
export { default as Topbar } from '../components/Topbar.svelte';
export { default as Bolota } from '../components/Bolota.svelte';
export { default as ImpersonationBanner } from '../components/ImpersonationBanner.svelte';
export { default as PageHead } from '../components/PageHead.svelte';
export { default as PageToolbar } from '../components/PageToolbar.svelte';
export { default as SectionNav } from '../components/SectionNav.svelte';
export { default as CardHead } from '../components/CardHead.svelte';
export { default as AlertBanner } from '../components/AlertBanner.svelte';
export { default as Chip } from '../components/Chip.svelte';
export { default as MiniButton } from '../components/MiniButton.svelte';
export { default as ErrorView } from '../components/ErrorView.svelte';
export { default as LightField } from '@bagel/ui/svelte/LightField.svelte';
export { default as Field } from '../components/Field.svelte';
export { default as ToastHost } from '../components/ToastHost.svelte';
export { default as SaveStatus } from '../components/SaveStatus.svelte';
export { default as EditorFooter } from '../components/EditorFooter.svelte';
export { default as ManagementRow } from '../components/ManagementRow.svelte';
export { default as InspectorSurface } from '../components/InspectorSurface.svelte';
export { default as FieldError } from '../components/FieldError.svelte';
export { default as ConfirmDialog } from '../components/ConfirmDialog.svelte';
export { default as Skeleton } from '../components/Skeleton.svelte';
export { default as EmptyState } from '../components/EmptyState.svelte';
export { default as SegmentedControl } from '../components/SegmentedControl.svelte';
export { default as RadioGroup } from '../components/RadioGroup.svelte';
export { default as NotificationBell } from '../components/NotificationBell.svelte';
export { default as SearchInput } from '../components/SearchInput.svelte';
export { default as DeckList } from '../components/DeckList.svelte';
export { default as Scroller } from '../components/Scroller.svelte';

export { initLenis, magnetic, countUp } from './actions';
export { copyFlash } from '@bagel/ui/lib/clipboard';
export { icons, type IconName } from './icons';
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
export * from './spotify';
export * from './toast';
export * from './connection-state';
export * from './overlay-stack';
export * from './inspector-machine';
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
