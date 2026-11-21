<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { livePoll } from '@bagel/kit/live-poll';
  import type { ShardSnapshot } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { RATE_NOW_SECONDS } from '@bagel/kit/rates';
  import type { TrialSnapshot } from '#lib/server/services';
  import { allows } from '#lib/access';
  import ShardControls from '#lib/components/shards/ShardControls.svelte';
  import ShardTopology from '#lib/components/shards/ShardTopology.svelte';
  import ThroughputChart from '#lib/components/shards/ThroughputChart.svelte';
  import { ThroughputHistory, type ThroughputPoint } from '#lib/components/shards/throughput-history';
  import { ReadOrder } from '#lib/components/shards/read-order';
  import { conduitStatus, rateLabel } from '#lib/components/shards/shard-state';
  import { activeTrial } from '#lib/components/shards/shard-topology';
  import { eventsPerSecond, pctLabel, resolveCapacity, utilizationPct } from '#lib/throughput';

  let { data } = $props();
  const { t, locale } = getI18n();
  const formatCount = new Intl.NumberFormat(locale);
  const canScale = $derived(allows(data.role, 'shards.scale'));

  const POLL_MS = 8000;
  const SETTLE_POLL_MS = 2000;
  const SETTLE_WINDOW_MS = 30_000;

  let snapshot = $state.raw<ShardSnapshot | null>(null);
  let trials = $state.raw<TrialSnapshot | null>(null);
  let fleetLoading = $state(true);
  let trialsLoading = $state(true);
  let stale = $state(false);
  let trialsStale = $state(false);
  let points = $state.raw<ThroughputPoint[]>([]);
  let changing = $state(false);
  let view = $state('all');
  let fastUntil = 0;
  const fleetOrder = new ReadOrder();
  const history = new ThroughputHistory();

  function receiveFleet(next: ShardSnapshot | null, ticket: number) {
    if (!fleetOrder.accept(ticket)) return;
    stale = next === null;
    if (next) snapshot = next;
  }

  function receiveTrials(next: TrialSnapshot | null) {
    trialsStale = next === null;
    if (next) trials = next;
  }

  async function readSnapshot<T>(route: string, signal: AbortSignal): Promise<T | null> {
    try {
      const response = await fetch(route, { signal, cache: 'no-store' });
      if (!response.ok) return null;
      const body: { snapshot?: T } = await response.json();
      return body.snapshot ?? null;
    } catch {
      return null;
    }
  }

  async function refreshFleet(ticket: number, signal: AbortSignal) {
    if (fleetLoading || changing) return undefined;
    const next = await readSnapshot<ShardSnapshot>('/shards/snapshot', signal);
    if (!signal.aborted) receiveFleet(next, ticket);
    return next;
  }

  async function refreshTrials(signal: AbortSignal) {
    if (!data.canViewTrials || trialsLoading) return null;
    const next = await readSnapshot<TrialSnapshot>('/trials/snapshot', signal);
    if (!signal.aborted) receiveTrials(next);
    return next;
  }

  async function refreshSnapshots(signal: AbortSignal): Promise<boolean> {
    if (document.hidden) return false;
    const ticket = fleetOrder.start();
    const [next, trial] = await Promise.all([refreshFleet(ticket, signal), refreshTrials(signal)]);
    if (next !== undefined && !signal.aborted) recordPoint(ticket, next, trial);
    return false;
  }

  function recordPoint(ticket: number, next: ShardSnapshot | null, trial: TrialSnapshot | null) {
    if (fleetOrder.current(ticket)) points = history.record(next, trial);
  }

  $effect(() => {
    const { fleet, trials: initialTrials, canViewTrials } = data;
    const controller = new AbortController();
    const { signal } = controller;
    const ticket = fleetOrder.start();
    fleetLoading = untrack(() => snapshot === null);
    trialsLoading = canViewTrials;
    if (!canViewTrials) trials = null;

    const firstFleet = fleet.then((read) => read.degraded ? null : read.snapshot, () => null);
    const firstTrials = initialTrials.catch(() => null);
    void firstFleet.then((next) => {
      if (signal.aborted) return;
      fleetLoading = false;
      receiveFleet(next, ticket);
    });
    void firstTrials.then((next) => {
      if (signal.aborted) return;
      trialsLoading = false;
      receiveTrials(next);
    });
    void Promise.all([firstFleet, firstTrials]).then(([next, trial]) => {
      if (!signal.aborted) recordPoint(ticket, next, trial);
    });

    const options = {
      firstDelayMs: POLL_MS,
      delayMs: () => Date.now() < fastUntil ? SETTLE_POLL_MS : POLL_MS,
      timeoutMs: Number.POSITIVE_INFINITY,
      hiddenDelayMs: POLL_MS,
      refreshOnVisible: true
    };
    const stop = livePoll(() => refreshSnapshots(signal), options);
    return () => {
      controller.abort();
      stop();
    };
  });

  function beginChange() {
    changing = true;
    fleetOrder.invalidate();
  }

  function finishChange(next?: ShardSnapshot) {
    fleetOrder.invalidate();
    if (next) {
      snapshot = next;
      stale = false;
    }
    fastUntil = Date.now() + SETTLE_WINDOW_MS;
    changing = false;
  }

  const capacity = $derived(snapshot ? resolveCapacity(snapshot) : null);
  const shards = $derived(snapshot?.shards ?? []);
  const inventoryUnavailable = $derived(snapshot?.inventory === 'unavailable');
  const showFleetMetrics = $derived(snapshot !== null && !inventoryUnavailable);
  const connected = $derived(shards.filter((shard) => shard.state === 'connected').length);
  const reported = $derived(snapshot?.shard_count || shards.length);
  const minShards = $derived(snapshot?.min_shards ?? 1);
  const maxShards = $derived(snapshot?.max_shards ?? capacity?.websocket_autoscale_max_shards ?? 11);
  const conduit = $derived(conduitStatus(snapshot?.conduit_manager?.state));
  const fleetStatus = $derived.by(() => {
    if (stale) return t('admin.shards.stale');
    if (inventoryUnavailable) return t('admin.shards.inventoryUnavailable');
    return t('admin.shards.live');
  });
  const productionCount = $derived(showFleetMetrics ? shards.length : null);
  const trialCount = $derived(trials?.trials.filter(activeTrial).length ?? null);
  const allCount = $derived(productionCount === null || trialCount === null ? null : productionCount + trialCount);
  const views = $derived([
    { value: 'all', label: t('admin.shards.allConnections'), count: countLabel(allCount) },
    { value: 'production', label: t('admin.shards.production'), count: countLabel(productionCount) },
    { value: 'trials', label: t('admin.shards.trialConnections'), count: countLabel(trialCount) }
  ]);

  function countLabel(count: number | null): string {
    return count === null ? '–' : formatCount.format(count);
  }

  function totalRate(loads: (number | undefined)[], window: number): number {
    return loads.reduce<number>((sum, load) => sum + eventsPerSecond(load, window), 0);
  }

  const fleetRate = $derived(totalRate(
    [...shards.map((shard) => shard.load), ...Object.values(snapshot?.trial_loads ?? {})],
    capacity?.load_window_seconds ?? 0
  ));
  const burstRate = $derived(totalRate(
    [...shards.map((shard) => shard.burst_load), ...Object.values(snapshot?.trial_burst_loads ?? {})],
    capacity?.burst_window_seconds ?? RATE_NOW_SECONDS
  ));
  const utilization = $derived(capacity ? utilizationPct(fleetRate, capacity.effective_rated_eps) : 0);
</script>

<section class="screen active">
  <PageHead compact description={t('admin.shards.description')}>
    {t('admin.shards.titlePre')}<em>{t('admin.shards.titleEm')}</em>
  </PageHead>

  <div class="fleet-content">
    <PageToolbar class="shards-toolbar">
      {#snippet leading()}
        <Stack gap={2}>
          <div class="fleet-status">
            {#if fleetLoading}
              <div aria-hidden="true"><Skeleton variant="pill" width="min(16rem, 100%)" /></div>
            {:else if !snapshot}
              <AlertBanner flush role="status">{t('admin.shards.degraded')}</AlertBanner>
            {:else}
              <div class="conduit-status">
                <StatusDot tone={conduit.tone} flat />
                <Text as="span" size="xs" tone="muted" mono>
                  <bb-fit><span data-fit>{t('admin.shards.conduit', { state: t(conduit.label) })}</span></bb-fit>
                </Text>
                <span class="live-state" role="status">
                  <Tag tone={stale || inventoryUnavailable ? 'warning' : 'success'} contain title={fleetStatus}>
                    <bb-fit><span data-fit>{fleetStatus}</span></bb-fit>
                  </Tag>
                </span>
              </div>
            {/if}
          </div>
          {#if data.canViewTrials}
            <SegmentedControl options={views} bind:value={view} label={t('admin.shards.topologyTitle')} even />
          {/if}
        </Stack>
      {/snippet}
      {#snippet trailing()}
        {#if canScale}
          <ShardControls {snapshot} {minShards} {maxShards} {changing} {stale} {beginChange} {finishChange} />
        {/if}
      {/snippet}
    </PageToolbar>

    <Grid cols={3} gap={3} aria-busy={fleetLoading} class="shard-metrics">
      {#if fleetLoading}
        {#each [0, 1, 2] as tile (tile)}
          <div class="metric-slot" aria-hidden="true"><Skeleton variant="block" height="100%" /></div>
        {/each}
      {:else}
        <div class="metric-slot">
          <StatTile static flat fitCopy label={t('admin.shards.tileShards')}
            value={showFleetMetrics ? `${countLabel(connected)}/${countLabel(reported)}` : '–'}
            delta={showFleetMetrics ? t('admin.shards.tileShardsDelta', { nodes: countLabel(snapshot?.nodes.length ?? 0) }) : undefined} />
        </div>
        <div class="metric-slot">
          <StatTile static flat fitCopy label={t('admin.shards.tileThroughput')}
            value={showFleetMetrics ? rateLabel(fleetRate) : '–'} unit={showFleetMetrics ? t('admin.shards.eps') : undefined}
            delta={showFleetMetrics ? t('admin.shards.tileThroughputDeltaBurst', { now: rateLabel(burstRate), pct: pctLabel(utilization) }) : undefined} />
        </div>
        <div class="metric-slot">
          <StatTile static flat fitCopy text label={t('admin.shards.tileAutoscale')}
            value={snapshot ? t(snapshot.autoscale ? 'admin.shards.on' : 'admin.shards.off') : '–'}
            delta={snapshot ? t('admin.shards.tileAutoscaleDelta', { min: countLabel(minShards), max: countLabel(maxShards) }) : undefined} />
        </div>
      {/if}
    </Grid>

    <ThroughputChart {points} showTrials={data.canViewTrials} />
    <ShardTopology {snapshot} {capacity} {trials} {fleetLoading} {trialsStale}
      showProduction={!data.canViewTrials || view !== 'trials'} showTrials={data.canViewTrials && view !== 'production'}
      trialsLoading={trialsLoading && trials === null} />
  </div>
</section>

<style>
  .fleet-content {
    container-type: inline-size;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-4);
  }

  .fleet-content > :global(.shards-toolbar) {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 0 auto;
    align-items: start;
    margin-bottom: 0;
  }

  .fleet-status {
    display: grid;
    align-items: center;
    min-block-size: 44px;
    min-inline-size: 0;
  }

  .conduit-status {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) 8rem;
    align-items: center;
    gap: var(--bb-space-2);
  }

  .live-state {
    display: grid;
  }

  .metric-slot {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    block-size: 9rem;
    min-inline-size: 0;
  }

  @container (max-width: 740px) {
    .fleet-content > :global(.shards-toolbar) {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: auto 0 auto;
      row-gap: var(--bb-space-2);
    }
  }

  @container (max-width: 39rem) {
    .fleet-content > :global(.shard-metrics) {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .metric-slot:last-child {
      grid-column: 1 / -1;
    }
  }
</style>
