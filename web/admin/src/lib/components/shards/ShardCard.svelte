<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Shard } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
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
  const PERCENT_TONE = { success: 'muted', warning: 'accent', danger: 'danger', neutral: 'muted' } as const;
  const tone = $derived(loadTone(utilization, targetUtilization));
  const instant = $derived(t('admin.shards.rowLoadBurst', { now: rateLabel(burstEps), eps: rateLabel(eps), pct: pctLabel(utilization) }));
</script>

<Card as="article" data-health={badge.tone} data-cursor="quiet">
  <div class="socket">
    <header class="head">
      <strong class="id">{t('admin.shards.rowId', { id: String(shard.shard_id) })}</strong>
      <span class="state"><StatusDot tone={badge.tone} /><Text as="span" size="xs" tone="muted">{t(badge.label)}</Text></span>
    </header>
    <div class="meta">
      <Text size="xs" mono tone="muted" title={shard.node}>
        {t('admin.shards.rowMeta', {
          host: shard.host || t('admin.shards.unknownHost'),
          pod: pod ? `pod${pod}` : '-',
          bound: shard.bound ? t('admin.shards.bound') : t('admin.shards.unbound'),
          attempts: String(shard.attempts ?? 0)
        })}
      </Text>
    </div>
    <div class="load">
      <Text as="span" size="xs" mono tone="muted"><strong class="figure">{rateLabel(eps)}</strong> {t('admin.shards.eps')}</Text>
      <Text as="span" size="xs" mono tone={PERCENT_TONE[tone]}>{pctLabel(utilization)}%</Text>
    </div>
    <Text size="xs" mono tone="muted">{instant}</Text>
    <ProgressBar value={utilization / 100} {tone} target={targetUtilization / 100} label={instant} aria-hidden="true" />
    <div class="capacity">
      <Text as="span" size="xs" mono tone="muted">{t('admin.shards.socketCapacity', { eps: rateLabel(eps), capacity: rateLabel(ratedEps) })}</Text>
      <Text as="span" size="xs" mono tone="muted">{t('admin.shards.targetLoad', { pct: String(targetUtilization) })}</Text>
    </div>
    {#if shard.handshake_in_flight}
      <div><StatePill tone="paid">{t('admin.shards.handshaking')}</StatePill></div>
    {/if}
  </div>
</Card>

<style>
  .socket { display: grid; gap: var(--bb-space-2); min-width: 0; }
  .head { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--bb-space-2); }
  .id { font-family: var(--bb-font-mono); font-size: var(--bb-text-sm); color: var(--bb-white); }
  .state { display: inline-flex; align-items: center; gap: var(--bb-space-2); }
  .meta { min-height: calc(var(--bb-text-xs) * 2.9); overflow-wrap: anywhere; }
  .load { display: flex; align-items: baseline; justify-content: space-between; gap: var(--bb-space-2); }
  .figure { font-size: var(--bb-text-xl); font-weight: 500; color: var(--bb-white); }
  .capacity { display: flex; justify-content: space-between; flex-wrap: wrap; gap: var(--bb-space-1); }
</style>
