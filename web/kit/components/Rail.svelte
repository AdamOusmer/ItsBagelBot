<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // Data wrapper around @bagel/ui's `.bb-rail`. The rail itself -- the sticky
  // column, the single measured highlight that glides between rows, the one
  // collapsible group -- is the library's. What stays here is what the library
  // must not know: this bot's logo path, the i18n string a locked row needs,
  // and the account surface at the bottom, which is session data and a logout
  // form.
  import Rail from '@bagel/ui/svelte/Rail.svelte';
  import AccountFoot from './AccountFoot.svelte';
  import type { DashboardLink, NavGroupDef } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

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

  // The registry's shape is structurally the library's `UiNavGroup` already
  // (that compatibility is why nav.ts, nav-core.ts and nav-admin.ts did not
  // move); the only thing added on the way through is the locked hint.
  const withHints = $derived(
    groups.map((group) => ({
      ...group,
      items: group.items.map((item) =>
        item.locked ? { ...item, lockedHint: t('nav.lockedBroadcaster') } : item
      )
    }))
  );
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
