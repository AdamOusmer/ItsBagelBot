// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from '@bagel/ui/lib/icons';
import type { ProfileMenuLink } from '@bagel/ui/svelte/ProfileMenu.svelte';
import type { MessageKey } from './i18n/keys';
import { SITE } from './site-links';

export interface SupportLine {
  title: string;
  hint: string;
  href: string;
  icon: IconName;
  external: boolean;
}

export function supportMenuLinks(t: (key: MessageKey) => string): ProfileMenuLink[] {
  return supportLines(t).map(({ title, ...line }) => ({ ...line, label: title }));
}

export function supportLines(t: (key: MessageKey) => string): readonly SupportLine[] {
  return [
    { title: 'Discord', hint: t('topbar.supportDiscordHint'), href: SITE.discord, icon: 'discord', external: true },
    { title: t('topbar.support'), hint: SITE.supportEmail, href: `mailto:${SITE.supportEmail}`, icon: 'link', external: false },
    { title: t('topbar.enterprise'), hint: SITE.enterpriseEmail, href: `mailto:${SITE.enterpriseEmail}`, icon: 'server', external: false },
    { title: 'GitHub', hint: t('topbar.supportGithubHint'), href: SITE.github, icon: 'github', external: true }
  ];
}
