<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One JetStream lane on the shared ManagementRow. No `actions` snippet: the
  // rename pencil, the make-permanent lock and the delete bin used to sit here,
  // which put three <button>s beside a row that was itself clickable (and a
  // fourth, an inline rename <input>, inside it). All three moved into the
  // inspector, so the row is a selector and nothing else.
  import ManagementRow from '@bagel/shared/components/ManagementRow.svelte';
  import { getI18n } from '@bagel/shared/i18n/context';
  import type { LaneView } from '$lib/server/lanes';
  import StatusDot from '../StatusDot.svelte';
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
        <span class="num" class:hot={lane.pending > 0}>
          {t('admin.lanes.pending', { n: lane.pending.toLocaleString() })}
        </span>
        <span class="num">{t('admin.lanes.inFlight', { n: lane.inFlight })}</span>
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
    display: flex;
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
    display: flex;
    flex-direction: column;
    gap: 2px;
    align-items: flex-end;
    flex: none;
  }
  .num {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    white-space: nowrap;
  }
  .num.hot {
    color: var(--bb-tan-light);
  }

  .marks {
    display: flex;
    gap: 6px;
    flex: none;
  }
  @media (max-width: 760px) {
    .counts {
      display: none;
    }
  }
</style>
