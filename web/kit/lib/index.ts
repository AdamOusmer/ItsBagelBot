// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export * from '@bagel/ui/svelte';

export { default as NavLink } from '@bagel/ui/svelte/NavLink.svelte';
export { default as FieldError } from '@bagel/ui/svelte/FieldError.svelte';

export { default as MasterToggle } from '../components/MasterToggle.svelte';
export { default as PermBadge } from '../components/PermBadge.svelte';
export { default as NavItem } from '../components/NavItem.svelte';
export { default as RootShell } from '../components/RootShell.svelte';
export { default as AppShell } from '../components/AppShell.svelte';
export { default as Rail } from '../components/Rail.svelte';
export { default as RailItem } from '../components/RailItem.svelte';
export { default as NavGroup } from '../components/NavGroup.svelte';
export { default as AccountFoot } from '../components/AccountFoot.svelte';
export { default as Topbar } from '../components/Topbar.svelte';
export { default as OperatorMenu } from '../components/OperatorMenu.svelte';
export { default as Bolota } from '../components/Bolota.svelte';
export { default as ImpersonationBanner } from '../components/ImpersonationBanner.svelte';
export { default as MiniButton } from '../components/MiniButton.svelte';
export { default as ErrorView } from '../components/ErrorView.svelte';
export { default as NotificationBell } from '@bagel/ui/svelte/NotificationBell.svelte';

export { initLenis, magnetic, countUp } from './actions';
export { copyFlash } from '@bagel/ui/lib/clipboard';
export { icons, type IconName } from '@bagel/ui/lib/icons';
export { customCursor } from './cursor';

export { setI18n, getI18n, type I18n } from './i18n/context';
export type { MessageKey } from './i18n/keys';
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
export * from './module-copy';
export * from './nav';
export * from './social';
export * from './spotify';
export * from './feature-presets';
export * from './connection-state';
export * from '@bagel/ui/lib/overlay-stack';
export * from '@bagel/ui/lib/inspector-machine';
export * from './command-active';
export * from './discord-config';
export * from './discord-overview';
export * from './engine/commands-validate';
export * from './engine/validation-messages';
export * from './uses';
export * from './engine/rehearsal';
export { intactSpan } from './engine/tmpl';
export * from './validation';
export * from './action-result';
export * from './format';
