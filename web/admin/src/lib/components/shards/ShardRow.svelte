<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Shard } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import StatePill from '../StatePill.svelte';
  import FleetRow from './FleetRow.svelte';
  import { podIndex, shardBadge } from './shard-state';

  let {
    shard,
    nodes,
    eps,
    burstEps,
    utilization,
    targetUtilization
  }: {
    shard: Shard;
    nodes: readonly string[];
    eps: number;
    burstEps?: number;
    utilization: number;
    targetUtilization: number;
  } = $props();

  const { t } = getI18n();

  const badge = $derived(shardBadge(shard));
  const pod = $derived(podIndex(nodes, shard.node));
</script>

{#snippet handshake()}
  <StatePill tone="paid">{t('admin.shards.handshaking')}</StatePill>
{/snippet}

<FleetRow
  tone={badge.tone}
  name={t('admin.shards.rowId', { id: String(shard.shard_id) })}
  state={t(badge.label)}
  meta={t('admin.shards.rowMeta', {
    host: shard.host || t('admin.shards.unknownHost'),
    pod: pod || '-',
    bound: shard.bound ? t('admin.shards.bound') : t('admin.shards.unbound'),
    attempts: String(shard.attempts ?? 0)
  })}
  {eps}
  {burstEps}
  {utilization}
  {targetUtilization}
  marks={shard.handshake_in_flight ? handshake : undefined}
/>
