// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ProfileMenuLink } from '@bagel/ui/lib/nav-types';

// Mirrors the real console's support menu (web/kit/lib/support-lines.ts,
// locales/en/console/topbar.json) in plain values: desktop has no i18n or
// $app, so these are literal English strings, not translation keys.
export const SUPPORT_LINKS: ProfileMenuLink[] = [
  { label: 'Support', hint: 'support@itsbagelbot.com', href: 'mailto:support@itsbagelbot.com', icon: 'link', external: false },
  { label: 'Enterprise', hint: 'enterprise@itsbagelbot.com', href: 'mailto:enterprise@itsbagelbot.com', icon: 'server', external: false },
  { label: 'GitHub', hint: 'source available', href: 'https://github.com/AdamOusmer/ItsBagelBot', icon: 'github', external: true }
];

export const BUG_REPORT_LINK: ProfileMenuLink = {
  label: 'Bug or idea',
  href: 'https://github.com/AdamOusmer/ItsBagelBot/issues/new',
  external: true
};
