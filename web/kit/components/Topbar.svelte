<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
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
