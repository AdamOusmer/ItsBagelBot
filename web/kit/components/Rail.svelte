<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Rail from '@bagel/ui/svelte/Rail.svelte';
  import AccountFoot from './AccountFoot.svelte';
  import type { DashboardLink, NavGroupDef } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';
  import { groupsWithLockedHints } from '../lib/nav-core';

  const { t } = getI18n();

  let {
    brandTitle = 'ItsBagelBot', brandSub, groups, accountName, accountRole,
    dashboards = [], isDelegate = false, delegateExitHref = '', delegateExitLabel = ''
  }: {
    brandTitle?: string;
    brandSub: string;
    groups: NavGroupDef[];
    accountName: string;
    accountRole: string;
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  const withHints = $derived(groupsWithLockedHints(groups, t));
</script>

<Rail
  brand={{ title: brandTitle, sub: brandSub, logoSrc: '/logo.png', logoAlt: brandTitle }}
  groups={withHints}
  ariaLabel={t('nav.ariaMain')}
>
  {#snippet foot()}
    <AccountFoot
      name={accountName}
      role={accountRole}
      {dashboards}
      {isDelegate}
      {delegateExitHref}
      {delegateExitLabel}
    />
  {/snippet}
</Rail>
