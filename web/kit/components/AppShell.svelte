<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import AppShell from '@bagel/ui/svelte/AppShell.svelte';
  import AccountFoot from './AccountFoot.svelte';
  import OperatorMenu from './OperatorMenu.svelte';
  import { getI18n } from '../lib/i18n/context';
  import type { NavGroupDef, NavLink, DashboardLink } from '../lib/types';

  const { t } = getI18n();
  let {
    brandTitle = 'ItsBagelBot', brandSub, crumbRoot, crumb,
    accountName, accountRole, dashboards = [], groups, mobileItems, rail = false,
    offset = false, logoSrc = '/logo.png', isPremium = false, banner, topActions, children,
    isDelegate = false, delegateExitHref = '', delegateExitLabel = ''
  }: {
    brandTitle?: string; brandSub: string; crumbRoot: string; crumb: string;
    accountName: string; accountRole: string; dashboards?: DashboardLink[];
    groups: NavGroupDef[]; mobileItems: NavLink[]; rail?: boolean;
    offset?: boolean; logoSrc?: string; isPremium?: boolean; banner?: Snippet; topActions?: Snippet; children: Snippet;
    isDelegate?: boolean; delegateExitHref?: string; delegateExitLabel?: string;
  } = $props();

  const withHints = $derived(
    groups.map((group) => ({
      ...group,
      items: group.items.map((item) =>
        item.locked ? { ...item, lockedHint: t('nav.lockedBroadcaster') } : item
      )
    }))
  );
</script>

<AppShell
  brand={{ title: brandTitle, sub: brandSub, href: '/', logoSrc, logoAlt: '', premium: isPremium }}
  crumbs={[{ label: crumbRoot, href: '/' }, { label: crumb }]}
  groups={withHints}
  dockItems={mobileItems}
  {rail}
  {offset}
  skipLabel={t('common.skipToContent')}
  crumbAriaLabel={t('common.breadcrumb')}
  dockAriaLabel={t('nav.ariaMain')}
  railAriaLabel={t('nav.ariaMain')}
  {banner}
  {topActions}
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
  {#snippet railFoot()}
    <AccountFoot
      name={accountName}
      role={accountRole}
      {dashboards}
      {isDelegate}
      {delegateExitHref}
      {delegateExitLabel}
    />
  {/snippet}
  {@render children()}
</AppShell>
