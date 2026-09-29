<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AuditEntry } from '$lib/server/services';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let {
    entry,
    selected,
    controls,
    onselect
  }: {
    entry: AuditEntry;
    selected: boolean;
    controls: string;
    onselect: () => void;
  } = $props();

  const { t } = getI18n();
</script>

<ManagementRow {selected} expanded={selected} {controls} {onselect}>
  {#snippet primary()}
    <span class="row">
      <StatusDot tone={entry.ok ? 'success' : 'error'} />
      <Bolota name={entry.actor_login} size={26} active={selected} />
      <span class="who">
        <Cluster as="span" gap={2} align="baseline">
          <span class="actor">@{entry.actor_login}</span>
          <Text as="span" size="xs" mono tone="accent">{entry.action}</Text>
          {#if entry.target}<Text as="span" size="xs" mono tone="muted" truncate>{t('admin.audit.arrow')} {entry.target}</Text>{/if}
        </Cluster>
        {#if !entry.ok && entry.error}
          <Text as="span" size="xs" mono tone="danger" truncate>{entry.error}</Text>
        {:else if entry.detail}
          <Text as="span" size="xs" mono tone="muted" truncate>{entry.detail}</Text>
        {/if}
      </span>
      <span class="when"><Text as="span" size="xs" mono tone="muted">{ago(entry.created_at)}</Text></span>
    </span>
  {/snippet}
</ManagementRow>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .who {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }
  .actor {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: var(--bb-text-sm);
    color: var(--bb-white);
  }
  .when {
    white-space: nowrap;
    flex: none;
  }
</style>
