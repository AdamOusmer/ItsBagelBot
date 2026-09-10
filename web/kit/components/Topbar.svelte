<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // Data wrapper around @bagel/ui's `.bb-topbar`. The strip -- the glass pill,
  // the breadcrumb, the wall clock -- is the library's. Two things stay here
  // and neither could go: the crumb labels and aria strings come from this
  // app's i18n catalog, and the operator chip is a session, an avatar engine
  // and a POST /auth/logout form (./OperatorMenu.svelte).
  //
  // The notification-bell fallback that used to render when no `actions`
  // snippet was given is gone with the move. It was a dead button that opened
  // nothing, on every page of the admin board.
  import type { Snippet } from 'svelte';
  import Topbar from '@bagel/ui/svelte/Topbar.svelte';
  import OperatorMenu from './OperatorMenu.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let {
    root,
    crumb,
    actions,
    brandTitle = 'ItsBagelBot',
    brandSub = '',
    accountName = '',
    accountRole = '',
    dashboards = [],
    logoSrc = '/logo.png',
    isPremium = false,
    isDelegate = false,
    delegateExitHref = '',
    delegateExitLabel = '',
    railed = false
  }: {
    root: string;
    crumb: string;
    actions?: Snippet;
    brandTitle?: string;
    brandSub?: string;
    accountName?: string;
    accountRole?: string;
    dashboards?: DashboardLink[];
    logoSrc?: string;
    isPremium?: boolean;
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
    // The rail owns the account surface at desktop widths, so a railed app
    // keeps the chip here only on phones, where the rail is off screen.
    railed?: boolean;
  } = $props();
</script>

<Topbar
  brand={{ title: brandTitle, sub: brandSub, href: '/', logoSrc, logoAlt: '', premium: isPremium }}
  crumbs={[{ label: root, href: '/' }, { label: crumb }]}
  crumbAriaLabel={t('common.breadcrumb')}
  {railed}
  {actions}
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
</Topbar>
