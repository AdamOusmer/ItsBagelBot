<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { IngressCapacity, ShardSnapshot } from '@bagel/kit';
  import type { TrialChannel } from '#lib/server/services';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { RATE_AVG_SECONDS, RATE_NOW_SECONDS } from '@bagel/kit/rates';
  import type { StatusTone } from '@bagel/kit/status-tone';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { durationLabel } from '#lib/duration';
  import { eventsPerSecond, utilizationPct } from '#lib/throughput';
  import LoadMeter from './LoadMeter.svelte';

  let { trial, snapshot, capacity }: {
    trial: TrialChannel;
    snapshot: ShardSnapshot | null;
    capacity: IngressCapacity | null;
  } = $props();

  const { t, locale } = getI18n();
  const numberFormat = new Intl.NumberFormat(locale);
  const channelTones: Partial<Record<TrialChannel['state'], StatusTone>> = { receiving: 'success', failed: 'danger' };
  const name = $derived(trial.display_name?.trim() || trial.broadcaster_id);
  const tone = $derived(trial.enabled ? channelTones[trial.state] ?? 'warning' : 'neutral');
  const eps = $derived(eventsPerSecond(snapshot?.trial_loads?.[trial.broadcaster_id], capacity?.load_window_seconds ?? RATE_AVG_SECONDS));
  const burstEps = $derived(eventsPerSecond(
    snapshot?.trial_burst_loads?.[trial.broadcaster_id],
    capacity?.burst_window_seconds ?? RATE_NOW_SECONDS
  ));
  const metrics = $derived([
    { key: 'admin.trials.decoded', count: trial.decoded },
    { key: 'admin.trials.processed', count: trial.processed },
    { key: 'admin.trials.failed', count: trial.failed },
    { key: 'admin.trials.retried', count: trial.retried },
    { key: 'admin.trials.blocked', count: trial.blocked_actions }
  ] as const);

  function formattedCount(count?: number): string {
    return numberFormat.format(count ?? 0);
  }
</script>

<Card as="article" dashed aria-label={name} style="--card-pad: var(--bb-space-3); --card-border: var(--bb-rule); --card-shadow: none; block-size: var(--trial-channel-height)">
  <Stack gap={1}>
    <div class="channel-heading">
      <Heading level={6} as="h5" title={name}><bb-fit><span data-fit>{name}</span></bb-fit></Heading>
      <div class="channel-status">
        <StatusDot {tone} flat />
        <Text size="xs" tone="muted">
          <bb-fit><span data-fit>{trial.enabled ? t(`admin.trials.state.${trial.state}`) : t('admin.trials.off')}</span></bb-fit>
        </Text>
      </div>
    </div>
    <Text size="xs" mono tone="muted">
      <bb-fit><span data-fit>{t('admin.shards.trialBroadcasterId', { id: trial.broadcaster_id })}</span></bb-fit>
    </Text>

    <div class="channel-load">
      {#if capacity}
        <LoadMeter {eps} {burstEps} utilization={utilizationPct(eps, capacity.websocket_rated_eps)} targetUtilization={capacity.target_utilization_pct} />
      {:else}
        <Text size="xs" tone="muted"><bb-fit><span data-fit>{t('admin.shards.throughputUnavailable')}</span></bb-fit></Text>
      {/if}
    </div>

    <Text size="sm">
      <bb-fit><span data-fit>{t('admin.trials.received', { count: formattedCount(trial.received) })}</span></bb-fit>
    </Text>
    <div class="channel-metrics">
      {#each metrics as metric (metric.key)}
        <Text size="xs" mono tone="muted">
          <bb-fit><span data-fit>{t(metric.key, { count: formattedCount(metric.count) })}</span></bb-fit>
        </Text>
      {/each}
      <Text size="xs" mono tone="muted">
        <bb-fit><span data-fit>{t('admin.trials.latency', { duration: durationLabel(trial.average_processing_latency_ns ?? 0) })}</span></bb-fit>
      </Text>
    </div>
    <div class="channel-error">
      {#if trial.error}
        <Text size="xs" tone="danger" title={trial.error}>
          <bb-fit mode="block"><span data-fit>{trial.error}</span></bb-fit>
        </Text>
      {/if}
    </div>
  </Stack>
</Card>

<style>
  .channel-heading {
    display: grid;
    grid-template-columns: minmax(0, 1fr) min(43%, 10rem);
    align-items: baseline;
    gap: var(--bb-space-2);
  }

  .channel-status {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: var(--bb-space-2);
  }

  .channel-load {
    min-block-size: 2.75rem;
  }

  .channel-metrics {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--bb-space-1) var(--bb-space-2);
  }

  .channel-error {
    block-size: 1.5rem;
    overflow-wrap: anywhere;
    overflow: auto;
  }

  @container (max-width: 21rem) {
    .channel-metrics {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
</style>
