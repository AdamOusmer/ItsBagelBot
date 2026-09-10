<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One shard in the fleet deck.
  //
  // NOT a ManagementRow: there is no per-shard verb (scale is a fleet-wide
  // control, and a single shard has nothing to inspect that the row does not
  // already show), so this row is not selectable. ManagementRow renders its
  // primary content inside a <button>, and a button that does nothing when
  // pressed is worse than no button -- it announces itself as actionable to
  // assistive tech and morphs the custom cursor for a dead target. The row is a
  // plain <div> that borrows the deck's rule and padding instead.
  import type { Shard } from '@bagel/shared';
  import { getI18n } from '@bagel/shared/i18n/context';
  import StatusDot from '../StatusDot.svelte';
  import StatePill from '../StatePill.svelte';
  import { loadTone, podIndex, rateLabel, shardBadge } from './shard-state';

  let {
    shard,
    nodes,
    eps,
    utilization,
    targetUtilization
  }: {
    shard: Shard;
    nodes: readonly string[];
    /** Events per second, already resolved against the load window. */
    eps: number;
    /** Percent of one socket's rated throughput. */
    utilization: number;
    targetUtilization: number;
  } = $props();

  const { t } = getI18n();

  const badge = $derived(shardBadge(shard));
  const tone = $derived(loadTone(utilization, targetUtilization));
  const pod = $derived(podIndex(nodes, shard.node));
  const width = $derived(Math.min(100, Math.max(0, Math.round(utilization))));
</script>

<div class="row" data-cursor="off">
  <StatusDot tone={badge.tone} />
  <span class="who">
    <span class="name">
      {t('admin.shards.rowId', { id: String(shard.shard_id) })}
      <span class="state">{t(badge.label)}</span>
    </span>
    <span class="meta">
      {t('admin.shards.rowMeta', {
        host: shard.host || t('admin.shards.unknownHost'),
        pod: pod || '-',
        bound: shard.bound ? t('admin.shards.bound') : t('admin.shards.unbound'),
        attempts: String(shard.attempts ?? 0)
      })}
    </span>
  </span>
  <span class="load">
    <span class="bar" aria-hidden="true">
      <span class="fill {tone}" style="width:{width}%"></span>
    </span>
    <span class="rate {tone}">
      {t('admin.shards.rowLoad', { eps: rateLabel(eps), pct: utilization.toFixed(1) })}
    </span>
  </span>
  {#if shard.handshake_in_flight}
    <span class="marks">
      <StatePill tone="paid">{t('admin.shards.handshaking')}</StatePill>
    </span>
  {/if}
</div>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
    padding: 12px 14px;
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
  }
  .who {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .name {
    display: flex;
    align-items: baseline;
    gap: 8px;
    font-family: var(--bb-font-mono);
    font-weight: 600;
    font-size: 13.5px;
    color: var(--bb-white);
  }
  .state {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .meta {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .load {
    display: flex;
    align-items: center;
    gap: 9px;
    flex: none;
    width: 200px;
  }
  .bar {
    flex: 1;
    height: 6px;
    border-radius: var(--bb-radius-pill);
    background: rgba(255, 255, 255, 0.08);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    border-radius: var(--bb-radius-pill);
    transition: width 0.3s ease;
  }
  .fill.success {
    background: var(--bb-green-glow);
  }
  .fill.warning {
    background: var(--bb-tan);
  }
  .fill.error {
    background: var(--bb-status-error);
  }
  .fill.neutral {
    background: rgba(255, 255, 255, 0.18);
  }
  .rate {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    white-space: nowrap;
  }
  .rate.success {
    color: var(--bb-green-glow);
  }
  .rate.warning {
    color: var(--bb-tan-light);
  }
  .rate.error {
    color: var(--bb-status-error);
  }
  .rate.neutral {
    color: var(--bb-muted);
  }

  .marks {
    display: flex;
    gap: 6px;
    flex: none;
  }

  @media (max-width: 760px) {
    .load {
      width: 110px;
    }
    .rate {
      display: none;
    }
  }
</style>
