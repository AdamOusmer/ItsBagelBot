<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { afterNavigate } from '$app/navigation';
  import ProfileMenu from '@bagel/ui/svelte/ProfileMenu.svelte';
  import Bolota from './Bolota.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';
  import { clearOnboarding } from '../lib/onboarding';
  import { SITE } from '../lib/site-links';
  import { supportMenuLinks } from '../lib/support-lines';

  const { t } = getI18n();

  let {
    name,
    role,
    dashboards = [],
    isDelegate = false,
    delegateExitHref = '',
    delegateExitLabel = ''
  }: {
    name: string;
    role: string;
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  let open = $state(false);
  let helpOpen = $state(false);

  const links = $derived(dashboards.map((d) => ({ href: d.href, label: d.name })));
  const exit = $derived(isDelegate ? { href: delegateExitHref, label: delegateExitLabel } : undefined);
  const help = $derived(supportMenuLinks(t));

  function closeMenus() {
    open = false;
    helpOpen = false;
  }

  afterNavigate(closeMenus);
</script>

<ProfileMenu
  variant="rail"
  bind:open
  bind:helpOpen
  {name}
  caption={role}
  {links}
  linksLabel={t('topbar.dashboards')}
  {exit}
  logoutLabel={t('topbar.logout')}
  onlogout={clearOnboarding}
  {help}
  helpLabel={t('topbar.support')}
  feedback={{ href: SITE.newIssue, label: t('topbar.feedback'), external: true }}
  menuLabel={name}
>
  {#snippet avatar(a)}<Bolota name={a.name} size={a.size} active={a.active} gate={a.item} />{/snippet}
</ProfileMenu>
