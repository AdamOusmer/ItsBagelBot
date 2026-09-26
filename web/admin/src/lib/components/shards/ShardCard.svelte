<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Shard } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import StatePill from '../StatePill.svelte';
  import { pctLabel } from '$lib/throughput';
  import { loadTone, rateLabel, shardBadge } from './shard-state';

  let { shard, pod, eps, burstEps, utilization, ratedEps, targetUtilization }: {
    shard: Shard;
    pod: string;
    eps: number;
    burstEps: number;
    utilization: number;
    ratedEps: number;
    targetUtilization: number;
  } = $props();
  const { t } = getI18n();
  const badge = $derived(shardBadge(shard));
  const tone = $derived(loadTone(utilization, targetUtilization));
  const width = $derived(Math.min(100, Math.max(0, utilization)));
</script>

<article class="socket" data-health={badge.tone} data-cursor="quiet">
  <header>
    <strong>{t('admin.shards.rowId', { id: String(shard.shard_id) })}</strong>
    <span class="state"><StatusDot tone={badge.tone} />{t(badge.label)}</span>
  </header>
  <p class="meta" title={shard.node}>
    {t('admin.shards.rowMeta', {
      host: shard.host || t('admin.shards.unknownHost'),
      pod: pod ? `pod${pod}` : '-',
      bound: shard.bound ? t('admin.shards.bound') : t('admin.shards.unbound'),
      attempts: String(shard.attempts ?? 0)
    })}
  </p>
  <div class="load" data-tone={tone}>
    <span class="rate"><strong>{rateLabel(eps)}</strong> {t('admin.shards.eps')}</span>
    <span class="percent">{pctLabel(utilization)}%</span>
  </div>
  <p class="instant">{t('admin.shards.rowLoadBurst', { now: rateLabel(burstEps), eps: rateLabel(eps), pct: pctLabel(utilization) })}</p>
  <div class="bar" aria-hidden="true" data-tone={tone}>
    <span class="fill" style:width={`${width}%`}></span>
    <span class="target" style:left={`${Math.min(100, Math.max(0, targetUtilization))}%`}></span>
  </div>
  <div class="capacity">
    <span>{t('admin.shards.socketCapacity', { eps: rateLabel(eps), capacity: rateLabel(ratedEps) })}</span>
    <span>{t('admin.shards.targetLoad', { pct: String(targetUtilization) })}</span>
  </div>
  {#if shard.handshake_in_flight}
    <div class="marks"><StatePill tone="paid">{t('admin.shards.handshaking')}</StatePill></div>
  {/if}
</article>

<style>
  .socket { position: relative; min-width: 0; padding: 15px; border: 1px solid var(--bb-border); border-radius: 10px; background: var(--bb-card-bg); }
  header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
  header > strong { font-family: var(--bb-font-mono); font-size: 13px; color: var(--bb-white); }
  .state { display: inline-flex; align-items: center; gap: 6px; font-size: 10px; color: var(--bb-muted); }
  .meta { min-height: 30px; margin: 10px 0 13px; font-family: var(--bb-font-mono); font-size: 10px; line-height: 1.5; color: var(--bb-muted); overflow-wrap: anywhere; }
  .load { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; font-family: var(--bb-font-mono); }
  .rate { color: var(--bb-muted); font-size: 10px; }
  .rate strong { font-size: 22px; font-weight: 500; color: var(--bb-white); }
  .percent { font-size: 11px; color: var(--bb-muted); }
  .instant { margin: 7px 0 0; font-family: var(--bb-font-mono); font-size: 10px; line-height: 1.5; color: var(--bb-muted); }
  .bar { position: relative; margin: 10px 0 8px; height: 5px; border-radius: 3px; background: var(--bb-border); }
  .fill { display: block; height: 100%; border-radius: inherit; background: var(--bb-green-glow); transition: width .3s ease; }
  .target { position: absolute; top: -2px; width: 1px; height: 9px; background: var(--bb-muted); }
  [data-tone='warning'] .fill { background: var(--bb-tan); }
  [data-tone='error'] .fill { background: var(--bb-status-error); }
  [data-tone='warning'] .percent { color: var(--bb-tan-light); }
  [data-tone='error'] .percent { color: var(--bb-status-error); }
  .capacity { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 4px; font-family: var(--bb-font-mono); font-size: 9px; color: var(--bb-muted); }
  .marks { margin-top: 10px; }
</style>
