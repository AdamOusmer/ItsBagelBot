<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { LaneView } from '$lib/server/lanes';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import StatePill from '../StatePill.svelte';
  import { laneTone } from './lane-view';

  let {
    lane,
    selected,
    controls,
    onselect
  }: {
    lane: LaneView;
    selected: boolean;
    controls: string;
    onselect: () => void;
  } = $props();

  const { t } = getI18n();
</script>

<ManagementRow {selected} expanded={selected} {controls} {onselect}>
  {#snippet primary()}
    <span class="row">
      <StatusDot tone={laneTone(lane)} />
      <span class="who">
        <span class="name">{lane.display}</span>
        <span class="meta">
          {t('admin.lanes.rowMeta', {
            stream: lane.stream,
            consumer: lane.consumer,
            subject: lane.subject || '-'
          })}
        </span>
      </span>
      <span class="counts">
        <span class="metric">
          <span class="metric-label">{t('admin.lanes.factDelivered')}</span>
          <strong>{lane.delivered?.toLocaleString() ?? '—'}</strong>
        </span>
        <span class="metric traffic">
          <span class="metric-label">{t('admin.lanes.traffic')}</span>
          <strong>{lane.rate === '-' ? t('admin.lanes.sampling') : lane.rate}</strong>
        </span>
        <span class="metric" class:hot={lane.pending > 0}>
          <span class="metric-label">{t('admin.lanes.factPending')}</span>
          <strong>{lane.pending.toLocaleString()}</strong>
        </span>
        <span class="metric" title={t('admin.lanes.capacityHint')}>
          <span class="metric-label">{t('admin.lanes.awaitingAck')}</span>
          <strong>{lane.inFlight}</strong>
        </span>
        <span class="metric" class:hot={lane.redelivered > 0}>
          <span class="metric-label">{t('admin.lanes.factRedelivered')}</span>
          <strong>{lane.redelivered.toLocaleString()}</strong>
        </span>
      </span>
      <span class="marks">
        <StatePill tone={lane.ephemeral ? 'paid' : 'free'}>
          {lane.ephemeral ? t('admin.lanes.ephemeral') : t('admin.lanes.durable')}
        </StatePill>
        {#if lane.orphan}
          <StatePill tone="banned">{t('admin.lanes.orphan')}</StatePill>
        {/if}
      </span>
    </span>
  {/snippet}
</ManagementRow>

<style>
  .row {
    display: grid;
    grid-template-columns: 10px minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .who {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .name {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13.5px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .counts {
    display: grid;
    grid-column: 2 / -1;
    grid-row: 2;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 14px;
    padding-top: 6px;
    border-top: 1px solid color-mix(in srgb, var(--bb-muted) 14%, transparent);
  }
  .metric { display: flex; flex-direction: column; gap: 4px; text-align: left; }
  .metric-label { color: var(--bb-muted); font-size: 10px; }
  .metric strong {
    font-family: var(--bb-font-mono);
    font-weight: 500;
    font-size: 12px;
    color: var(--bb-white);
    white-space: nowrap;
  }
  .traffic strong { color: var(--bb-green-glow); }
  .metric.hot strong { color: var(--bb-tan-light); }
  .marks { display: flex; gap: 6px; flex: none; }
  .marks { grid-column: 3; grid-row: 1; }
  @media (max-width: 760px) {
    .counts { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
  }
  @media (max-width: 500px) {
    .row { grid-template-columns: 10px minmax(0, 1fr); }
    .counts { grid-column: 2; grid-row: 3; grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .marks { grid-column: 2; grid-row: 2; }
  }
</style>
