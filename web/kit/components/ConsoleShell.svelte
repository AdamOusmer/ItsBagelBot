<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { afterNavigate } from '$app/navigation';
  import AppShell from '@bagel/ui/svelte/AppShell.svelte';
  import OperatorMenu from './OperatorMenu.svelte';
  import { getI18n } from '../lib/i18n/context';
  import { groupsWithLockedHints } from '../lib/nav-core';
  import { railAccountValues } from '../lib/account-menu';
  import { clearOnboarding } from '../lib/onboarding';
  import type { NavGroupDef, NavLink, DashboardLink } from '../lib/types';

  const { t } = getI18n();
  let {
    brandTitle = 'ItsBagelBot', brandSub, crumbRoot, crumb,
    accountName, accountRole, dashboards = [], groups, mobileItems, rail = false,
    offset = false, stacked = false, logoSrc = '/logo.png', isPremium = false, banner, topActions, children,
    isDelegate = false, delegateExitHref = '', delegateExitLabel = ''
  }: {
    brandTitle?: string; brandSub: string; crumbRoot: string; crumb: string;
    accountName: string; accountRole: string; dashboards?: DashboardLink[];
    groups: NavGroupDef[]; mobileItems: NavLink[]; rail?: boolean;
    offset?: boolean; stacked?: boolean; logoSrc?: string; isPremium?: boolean; banner?: Snippet; topActions?: Snippet; children: Snippet;
    isDelegate?: boolean; delegateExitHref?: string; delegateExitLabel?: string;
  } = $props();

  const withHints = $derived(groupsWithLockedHints(groups, t));

  const accountValues = $derived(
    railAccountValues(t, { dashboards, isDelegate, delegateExitHref, delegateExitLabel })
  );
  const railAccount = $derived(
    accountName
      ? {
          name: accountName,
          caption: accountRole,
          ...accountValues,
          logoutLabel: t('topbar.logout'),
          onLogout: clearOnboarding
        }
      : undefined
  );

  let railAccountOpen = $state(false);
  afterNavigate(() => (railAccountOpen = false));
</script>

<AppShell
  brand={{ title: brandTitle, sub: brandSub, href: '/', logoSrc, logoAlt: '', logoShape: isPremium ? 'circle' : 'square' }}
  crumbs={[{ label: crumbRoot, href: '/' }, { label: crumb }]}
  groups={withHints}
  dockItems={mobileItems}
  dockQuick={4}
  {rail}
  {offset}
  {stacked}
  skipLabel={t('common.skipToContent')}
  crumbLabel={t('common.breadcrumb')}
  dockLabel={t('nav.ariaMain')}
  railLabel={t('nav.ariaMain')}
  {banner}
  {topActions}
  {railAccount}
  bind:railAccountOpen
>
  {#snippet account()}
    {#if accountName}
      <OperatorMenu
        {accountName}
        {accountRole}
        {dashboards}
        {isDelegate}
        {delegateExitHref}
        {delegateExitLabel}
      />
    {/if}
  {/snippet}
  {@render children()}
</AppShell>
