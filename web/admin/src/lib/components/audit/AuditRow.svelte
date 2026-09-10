<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One audit entry on the shared ManagementRow. The row shows the four fields
  // an operator scans for -- who, what, to whom, when -- and nothing else; the
  // detail and the error text belong to the inspector, because they are the two
  // fields that are arbitrarily long and were what made the old rows ragged.
  import ManagementRow from '@bagel/kit/components/ManagementRow.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AuditEntry } from '$lib/server/services';
  import StatusDot from '../StatusDot.svelte';

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
        <span class="line">
          <span class="actor">@{entry.actor_login}</span>
          <span class="action">{entry.action}</span>
          {#if entry.target}<span class="target">{t('admin.audit.arrow')} {entry.target}</span>{/if}
        </span>
        {#if !entry.ok && entry.error}
          <span class="err">{entry.error}</span>
        {:else if entry.detail}
          <span class="detail">{entry.detail}</span>
        {/if}
      </span>
      <span class="when">{ago(entry.created_at)}</span>
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
  .line {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .actor {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13px;
    color: var(--bb-white);
  }
  .action {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-tan-light);
  }
  .target {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .detail,
  .err {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .err {
    color: var(--bb-status-error);
  }
  .when {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    white-space: nowrap;
    flex: none;
  }
</style>
