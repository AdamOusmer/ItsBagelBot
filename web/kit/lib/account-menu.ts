// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ProfileMenuLink } from '@bagel/ui/svelte/ProfileMenu.svelte';
import type { MessageKey } from './i18n/keys';
import type { DashboardLink } from './types';
import { SITE } from './site-links';
import { supportMenuLinks } from './support-lines';

export interface RailAccountValues {
  links: ProfileMenuLink[];
  linksLabel: string;
  exit?: ProfileMenuLink;
  help: ProfileMenuLink[];
  helpLabel: string;
  feedback: ProfileMenuLink;
}

// The rail renders the account menu itself; this returns the values it
// needs. No markup, no $app import, so it works for the SvelteKit apps and
// the router-less desktop app alike.
export function railAccountValues(
  t: (key: MessageKey) => string,
  options: {
    dashboards: DashboardLink[];
    isDelegate: boolean;
    delegateExitHref: string;
    delegateExitLabel: string;
  }
): RailAccountValues {
  const { dashboards, isDelegate, delegateExitHref, delegateExitLabel } = options;
  return {
    links: dashboards.map((d) => ({ href: d.href, label: d.name })),
    linksLabel: t('topbar.dashboards'),
    exit: isDelegate ? { href: delegateExitHref, label: delegateExitLabel } : undefined,
    help: supportMenuLinks(t),
    helpLabel: t('topbar.support'),
    feedback: { href: SITE.newIssue, label: t('topbar.feedback'), external: true }
  };
}
