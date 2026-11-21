<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Shard } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import LoadMeter from './LoadMeter.svelte';
  import { rateLabel, shardBadge } from './shard-state';

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
  const headingId = $props.id();
  const badge = $derived(shardBadge(shard));
  const stateLabel = $derived(t(badge.label));
  const metadata = $derived(t('admin.shards.rowMeta', {
    host: shard.host || t('admin.shards.unknownHost'),
    pod: pod ? `pod${pod}` : '-',
    bound: t(shard.bound ? 'admin.shards.bound' : 'admin.shards.unbound'),
    attempts: String(shard.attempts ?? 0)
  }));
  const capacity = $derived(t('admin.shards.socketCapacity', { eps: rateLabel(eps), capacity: rateLabel(ratedEps) }));
  const target = $derived(t('admin.shards.targetLoad', { pct: String(targetUtilization) }));
</script>

<Card as="article" aria-labelledby={headingId} style="--bb-cursor: quiet; block-size: var(--socket-height)">
  <Stack gap={2}>
    <header class="socket-heading">
      <Heading level={6} as="h5" id={headingId}>
        <bb-fit><span data-fit>{t('admin.shards.rowId', { id: String(shard.shard_id) })}</span></bb-fit>
      </Heading>
      <div>
        <Cluster gap={2} justify="end" nowrap>
          <StatusDot tone={badge.tone} flat />
          <div class="state-copy">
            <Text as="div" size="xs" tone="muted" title={stateLabel}>
              <bb-fit><span data-fit>{stateLabel}</span></bb-fit>
            </Text>
          </div>
        </Cluster>
        <div class="handshake" class:visible={shard.handshake_in_flight} aria-hidden={!shard.handshake_in_flight}>
          <Text as="div" size="xs" tone="accent">
            <bb-fit><span data-fit>{t('admin.shards.handshaking')}</span></bb-fit>
          </Text>
        </div>
      </div>
    </header>

    <Text as="div" size="xs" tone="muted" title={shard.node ? `${metadata} · ${shard.node}` : metadata} style="block-size: 2lh; overflow-wrap: anywhere">
      <bb-fit mode="block"><span data-fit>{metadata}</span></bb-fit>
    </Text>

    <LoadMeter {eps} {burstEps} {utilization} {targetUtilization} showTarget />

    <div class="limits">
      <Text as="div" size="xs" tone="muted" title={capacity}>
        <bb-fit><span data-fit>{capacity}</span></bb-fit>
      </Text>
      <Text as="div" size="xs" tone="muted" title={target} style="text-align: end">
        <bb-fit><span data-fit>{target}</span></bb-fit>
      </Text>
    </div>
  </Stack>
</Card>

<style>
  .socket-heading,
  .limits {
    display: grid;
    gap: var(--bb-space-2);
  }

  .socket-heading {
    grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
  }

  .state-copy {
    min-width: 0;
  }

  .handshake {
    opacity: 0;
    text-align: end;
    transition: opacity var(--bb-dur-fast) ease;
  }

  .handshake.visible {
    opacity: 1;
  }

  .limits {
    grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
  }

  @media (prefers-reduced-motion: reduce) {
    .handshake { transition: none; }
  }
</style>
