<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { afterNavigate } from '$app/navigation';
  import ProfileMenu from '@bagel/ui/svelte/ProfileMenu.svelte';
  import Bolota from './Bolota.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

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

  const links = $derived(dashboards.map((d) => ({ href: d.href, label: d.name })));
  const exit = $derived(isDelegate ? { href: delegateExitHref, label: delegateExitLabel } : undefined);

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
>
  {#snippet avatar(a)}<Bolota name={a.name} size={a.size} active={a.active} gate={a.item} />{/snippet}
</ProfileMenu>
