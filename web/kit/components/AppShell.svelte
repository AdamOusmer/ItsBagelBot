<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // Data wrapper around @bagel/ui's `.bb-shell`. The layout -- strip on top,
  // one centred reading column, floating dock, optional desktop rail -- is the
  // library's. This file is what the library cannot be: the i18n strings for
  // the skip link and the two aria labels, this bot's logo path, and the two
  // session surfaces (the operator menu in the strip, the account foot in the
  // rail) that bind a session and a logout form.
  //
  // `rail` opts a board into the desktop sidebar. It stays a prop and not the
  // default because the dock alone cannot show a page's sub-pages, and the
  // grouped dock is what a board with several groups was built for.
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

  // The nav registry is structurally the library's `UiNavGroup`; the locked
  // hint is the one field added on the way through, because the wording is
  // this bot's and the element must not hold it.
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
