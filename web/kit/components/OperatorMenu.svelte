<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { afterNavigate } from '$app/navigation';
  import ProfileMenu, { type ProfileMenuLink } from '@bagel/ui/svelte/ProfileMenu.svelte';
  import Bolota from './Bolota.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';
  import { clearOnboarding } from '../lib/onboarding';
  import { SITE } from '../lib/site-links';
  import { supportMenuLinks } from '../lib/support-lines';

  const { t } = getI18n();

  let {
    accountName,
    accountRole,
    dashboards = [],
    isDelegate = false,
    delegateExitHref = '',
    delegateExitLabel = ''
  }: {
    accountName: string;
    accountRole: string;
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  let open = $state(false);

  const label = $derived(`${accountName} · ${accountRole}`);
  const links = $derived(dashboards.map((d) => ({ href: d.href, label: d.name })));
  const exit = $derived(isDelegate ? { href: delegateExitHref, label: delegateExitLabel } : undefined);
  const help = $derived(supportMenuLinks(t));
  const more = $derived<ProfileMenuLink[]>([
    { label: t('topbar.feedback'), href: SITE.newIssue, icon: 'edit', external: true },
    { label: t('nav.status'), href: SITE.status, icon: 'pulse', external: true }
  ]);

  afterNavigate(() => (open = false));
</script>

<ProfileMenu
  bind:open
  name={accountName}
  caption={accountRole}
  {links}
  linksLabel={t('topbar.dashboards')}
  {exit}
  logoutLabel={t('topbar.logout')}
  onLogout={clearOnboarding}
  {help}
  helpTitle={t('topbar.supportTitle')}
  {more}
  newTabLabel={t('common.opensInNewTab')}
  menuLabel={label}
>
  {#snippet avatar(a)}<Bolota name={a.name} size={a.size} active={a.active} gate={a.item} />{/snippet}
</ProfileMenu>
