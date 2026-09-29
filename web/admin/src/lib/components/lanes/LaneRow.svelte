<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { LaneView } from '$lib/server/lanes';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
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
        <Text as="span" size="xs" mono tone="muted" truncate>
          {t('admin.lanes.rowMeta', {
            stream: lane.stream,
            consumer: lane.consumer,
            subject: lane.subject || '-'
          })}
        </Text>
      </span>
      <span class="counts">
        <span class="metric">
          <Text as="span" size="xs" tone="muted">{t('admin.lanes.factDelivered')}</Text>
          <strong class="value">{lane.delivered?.toLocaleString() ?? '-'}</strong>
        </span>
        <span class="metric traffic">
          <Text as="span" size="xs" tone="muted">{t('admin.lanes.traffic')}</Text>
          <strong class="value">{lane.rate === '-' ? t('admin.lanes.sampling') : lane.rate}</strong>
        </span>
        <span class="metric" class:hot={lane.pending > 0}>
          <Text as="span" size="xs" tone="muted">{t('admin.lanes.factPending')}</Text>
          <strong class="value">{lane.pending.toLocaleString()}</strong>
        </span>
        <span class="metric" title={t('admin.lanes.capacityHint')}>
          <Text as="span" size="xs" tone="muted">{t('admin.lanes.awaitingAck')}</Text>
          <strong class="value">{lane.inFlight}</strong>
        </span>
        <span class="metric" class:hot={lane.redelivered > 0}>
          <Text as="span" size="xs" tone="muted">{t('admin.lanes.factRedelivered')}</Text>
          <strong class="value">{lane.redelivered.toLocaleString()}</strong>
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
    gap: var(--bb-space-3);
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
    font-size: var(--bb-text-sm);
    color: var(--bb-white);
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
    border-top: 1px solid var(--bb-border);
  }
  .metric { display: flex; flex-direction: column; gap: var(--bb-space-1); text-align: left; }
  .value {
    font-family: var(--bb-font-mono);
    font-weight: 500;
    font-size: var(--bb-text-xs);
    color: var(--bb-white);
    white-space: nowrap;
  }
  .traffic .value { color: var(--bb-green-glow); }
  .metric.hot .value { color: var(--bb-tan-light); }
  .marks { display: flex; gap: 6px; flex: none; grid-column: 3; grid-row: 1; }
  @media (max-width: 760px) {
    .counts { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--bb-space-3); }
  }
  @media (max-width: 500px) {
    .row { grid-template-columns: 10px minmax(0, 1fr); }
    .counts { grid-column: 2; grid-row: 3; grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .marks { grid-column: 2; grid-row: 2; }
  }
</style>
