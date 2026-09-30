<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { livePoll } from '@bagel/ui/lib/live-poll';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import type { ShardSnapshot } from '@bagel/kit';
  import type { TrialSnapshot } from '$lib/server/services';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { allows } from '$lib/access';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import ShardTopology from '$lib/components/shards/ShardTopology.svelte';
  import ThroughputChart from '$lib/components/shards/ThroughputChart.svelte';
  import { ThroughputHistory, type ThroughputPoint } from '$lib/components/shards/throughput-history';
  import { ReadOrder } from '$lib/components/shards/read-order';
  import { rateLabel } from '$lib/components/shards/shard-state';
  import { eventsPerSecond, pctLabel, resolveCapacity, utilizationPct } from '$lib/throughput';
  import { RATE_NOW_SECONDS } from '@bagel/kit/rates';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const canScale = $derived(allows(data.role, 'shards.scale'));

  let snap = $state<ShardSnapshot | null>(null);
  let trialSnapshot = $state<TrialSnapshot | null>(null);
  const fleetOrder = new ReadOrder();
  const trialOrder = new ReadOrder();
  const throughputHistory = new ThroughputHistory();
  let throughputPoints = $state<ThroughputPoint[]>([]);
  let degraded = $state(false);
  let live = $state(false);
  let pollingActive = true;
  let initialTrialsPending = true;
  $effect(() => {
    let alive = true;
    initialTrialsPending = true;
    data.fleet.then((fleet) => {
      if (!alive || !fleetOrder.accept(0)) return;
      snap = fleet.snapshot;
      degraded = fleet.degraded;
      throughputPoints = throughputHistory.record(fleet.degraded ? null : fleet.snapshot, trialSnapshot);
    });
    data.trials.then((trials) => {
      if (!alive) return;
      initialTrialsPending = false;
      if (trialOrder.accept(0)) trialSnapshot = trials;
    }).catch(() => {
      if (!alive) return;
      initialTrialsPending = false;
      if (trialOrder.accept(0)) trialSnapshot = null;
    });
    return () => {
      alive = false;
    };
  });

  const FAST_MS = 2000;
  const SLOW_MS = 8000;
  const SETTLE_WINDOW_MS = 30_000;
  let fastUntil = 0;

  const HIDDEN_MS = 15_000;

  function fleetUnavailable(ticket: number) {
    if (!pollingActive || !fleetOrder.current(ticket)) return;
    live = false;
    throughputPoints = throughputHistory.record(null, trialSnapshot);
  }

  async function pollSnapshot(): Promise<boolean> {
    if (busy) return false;
    const ticket = fleetOrder.start();
    try {
      const res = await fetch('/shards/snapshot');
      if (!res.ok) {
        fleetUnavailable(ticket);
        return false;
      }
      const body = (await res.json()) as { snapshot?: ShardSnapshot | null };
      if (!body.snapshot) {
        fleetUnavailable(ticket);
        return false;
      }
      if (!pollingActive || !fleetOrder.accept(ticket)) return false;
      snap = body.snapshot;
      throughputPoints = throughputHistory.record(body.snapshot, trialSnapshot);
      degraded = false;
      live = true;
    } catch {
      fleetUnavailable(ticket);
    }
    return false;
  }

  async function pollTrials(): Promise<boolean> {
    if (initialTrialsPending) return false;
    const ticket = trialOrder.start();
    try {
      const res = await fetch('/trials/snapshot');
      if (!res.ok) throw new Error('Trial snapshot unavailable');
      const body = (await res.json()) as { snapshot?: TrialSnapshot | null };
      if (!pollingActive || !trialOrder.current(ticket)) return false;
      if (body.snapshot) trialOrder.accept(ticket);
      trialSnapshot = body.snapshot ?? null;
    } catch {
      if (pollingActive && trialOrder.current(ticket)) trialSnapshot = null;
    }
    return false;
  }

  onMount(() => {
    const options = {
      firstDelayMs: 1500,
      delayMs: () => (Date.now() < fastUntil ? FAST_MS : SLOW_MS),
      timeoutMs: Number.POSITIVE_INFINITY,
      hiddenDelayMs: HIDDEN_MS,
      refreshOnVisible: true
    };
    const stopFleet = livePoll(pollSnapshot, options);
    const stopTrials = data.canViewTrials ? livePoll(pollTrials, options) : () => {};
    return () => {
      pollingActive = false;
      stopFleet();
      stopTrials();
    };
  });

  const capacity = $derived(snap ? resolveCapacity(snap) : null);
  const shards = $derived(snap?.shards ?? []);
  const connected = $derived(shards.filter((s) => s.state === 'connected').length);
  const minShards = $derived(snap?.min_shards ?? 1);
  const maxShards = $derived(
    snap?.max_shards ?? capacity?.websocket_autoscale_max_shards ?? 11
  );
  const autoscaleOn = $derived(snap?.autoscale ?? false);

  function evRate(load?: number): number {
    return capacity ? eventsPerSecond(load, capacity.load_window_seconds) : 0;
  }

  function burstRate(load?: number): number {
    return capacity ? eventsPerSecond(load, capacity.burst_window_seconds ?? RATE_NOW_SECONDS) : 0;
  }

  const trialLoads = $derived(snap?.trial_loads ?? {});
  const trialBursts = $derived(snap?.trial_burst_loads ?? {});
  const aggregateBurst = $derived(
    shards.reduce((sum, s) => sum + burstRate(s.burst_load), 0) +
      Object.values(trialBursts).reduce((sum, load) => sum + burstRate(load), 0)
  );
  const aggregateEps = $derived(
    shards.reduce((sum, s) => sum + evRate(s.load), 0) +
      Object.values(trialLoads).reduce((sum, load) => sum + evRate(load), 0)
  );
  const aggregateUtilization = $derived(
    capacity ? utilizationPct(aggregateEps, capacity.effective_rated_eps) : 0
  );
  const conduit = $derived(snap?.conduit_manager);
  const conduitReady = $derived(conduit?.state === 'ready' || conduit?.state === 'leader');

  const scaleBase = $derived(snap?.desired_count ?? snap?.shard_count ?? 1);
  let scaleOffset = $state(0);
  const scaleCount = $derived(scaleBase + scaleOffset);
  let seedBase = -1;
  $effect(() => {
    if (seedBase === scaleBase) return;
    seedBase = scaleBase;
    scaleOffset = 0;
  });

  function stepScale(by: number) {
    const next = Math.min(maxShards, Math.max(minShards, scaleCount + by));
    scaleOffset = next - scaleBase;
  }

  function typeScale(raw: string) {
    const v = parseInt(raw, 10);
    if (Number.isNaN(v)) return;
    scaleOffset = v - scaleBase;
  }

  type ActionPayload = {
    action?: { ok: boolean; notice: string };
    snapshot?: ShardSnapshot;
    error?: string;
  };

  let busy = $state(false);
  let scaleForm = $state<HTMLFormElement | null>(null);
  let autoscaleForm = $state<HTMLFormElement | null>(null);
  let confirmScaleDown = $state(false);

  function requestScale() {
    if (scaleCount < scaleBase) {
      confirmScaleDown = true;
      return;
    }
    scaleForm?.requestSubmit();
  }

  const scaleSubmit: SubmitFunction = ({ formData }) => {
    formData.set('count', String(scaleCount));
    fleetOrder.invalidate();
    busy = true;
    confirmScaleDown = false;
    return async ({ result }) => {
      fleetOrder.invalidate();
      busy = false;
      fastUntil = Date.now() + SETTLE_WINDOW_MS;
      const p = actionPayload<ActionPayload>(result);
      if (result.type === 'success' && p?.action?.ok) {
        if (p.snapshot) snap = p.snapshot;
        toast('success', p.action.notice);
        return;
      }
      failed(p, t('admin.shards.scaleFailed'));
    };
  };

  const autoscaleSubmit: SubmitFunction = () => {
    fleetOrder.invalidate();
    busy = true;
    const before = snap ? { ...snap } : null;
    if (snap) snap = { ...snap, autoscale: !snap.autoscale };
    return async ({ result }) => {
      fleetOrder.invalidate();
      busy = false;
      fastUntil = Date.now() + SETTLE_WINDOW_MS;
      const p = actionPayload<ActionPayload>(result);
      if (result.type === 'success' && p?.action?.ok) {
        if (p.snapshot) snap = p.snapshot;
        toast('success', p.action.notice);
        return;
      }
      if (before) snap = before;
      failed(p, t('admin.shards.autoscaleFailed'));
    };
  };
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.shards.eyebrow')} description={t('admin.shards.description')}>
    {t('admin.shards.titlePre')}<em>{t('admin.shards.titleEm')}</em>
  </PageHead>

  {#if degraded}
    <AlertBanner>{t('admin.shards.degraded')}</AlertBanner>
  {/if}

  {#if snap === null}
    <SkeletonStack rows={3} height="96px" />
  {:else}
    <div class="tiles">
      <Grid min="200px" gap={3}>
        <StatTile
          label={t('admin.shards.tileShards')}
          value={`${connected}/${snap.shard_count || shards.length}`}
          delta={t('admin.shards.tileShardsDelta', { nodes: String(snap.nodes.length) })}
        />
        <StatTile
          label={t('admin.shards.tileThroughput')}
          value={rateLabel(aggregateEps)}
          unit={t('admin.shards.eps')}
          delta={t('admin.shards.tileThroughputDeltaBurst', {
            now: rateLabel(aggregateBurst),
            pct: pctLabel(aggregateUtilization)
          })}
        />
        <StatTile
          label={t('admin.shards.tileAutoscale')}
          value={autoscaleOn ? t('admin.shards.on') : t('admin.shards.off')}
          delta={t('admin.shards.tileAutoscaleDelta', {
            min: String(minShards),
            max: String(maxShards)
          })}
        />
      </Grid>
    </div>

    <PageToolbar>
      {#snippet lead()}
        <Cluster gap={2}>
          <StatusDot tone={conduitReady ? 'success' : 'warning'} />
          <Text as="span" size="xs" tone="muted" mono>
            {t('admin.shards.conduit', {
              state: conduit?.state ?? t('admin.shards.unknownState'),
              node: conduit?.node ?? '-'
            })}
          </Text>
          {#if live}<Tag tone="live">{t('admin.shards.live')}</Tag>{/if}
        </Cluster>
      {/snippet}
      {#snippet trail()}
        {#if canScale}
          <Cluster gap={2} nowrap>
            <Label mono as="span">{t('admin.shards.autoscaleLabel')}</Label>
            <Switch
              checked={autoscaleOn}
              label={t('admin.shards.autoscaleLabel')}
              describedby="shards-autoscale-hint"
              busy={busy}
              onCheckedChange={() => autoscaleForm?.requestSubmit()}
            />
          </Cluster>
          <span class="stepper" class:dim={autoscaleOn}>
            <IconButton
              size="sm"
              label={t('admin.shards.decrease')}
              disabled={scaleCount <= minShards}
              onclick={() => stepScale(-1)}>&minus;</IconButton>
            <Input
              mono
              type="number"
              min={minShards}
              max={maxShards}
              value={scaleCount}
              aria-label={t('admin.shards.countLabel')}
              oninput={(e: Event) => typeScale((e.target as HTMLInputElement).value)}
            />
            <IconButton
              size="sm"
              label={t('admin.shards.increase')}
              disabled={scaleCount >= maxShards}
              onclick={() => stepScale(1)}>+</IconButton>
          </span>
          <Button
            variant="secondary"
            disabled={autoscaleOn || busy || scaleCount === scaleBase}
            onclick={requestScale}
          >
            {t('admin.shards.apply')}
          </Button>
        {/if}
      {/snippet}
    </PageToolbar>

    {#if canScale}
      <Text size="sm" tone="muted" id="shards-autoscale-hint">
        {autoscaleOn ? t('admin.shards.autoscaleOnHint') : t('admin.shards.autoscaleOffHint')}
      </Text>
    {/if}

    <ThroughputChart points={throughputPoints} showTrials={data.canViewTrials} />
    {#if capacity}
      <ShardTopology snapshot={snap} {capacity} trials={trialSnapshot} showTrials={data.canViewTrials} />
    {/if}
  {/if}
</section>

<ConfirmDialog
  open={confirmScaleDown}
  title={t('admin.shards.confirmScaleDownTitle')}
  body={t('admin.shards.confirmScaleDownBody', {
    from: String(scaleBase),
    to: String(scaleCount)
  })}
  confirmLabel={t('admin.shards.apply')}
  cancelLabel={t('common.cancel')}
  {busy}
  onCancel={() => (confirmScaleDown = false)}
  onConfirm={() => scaleForm?.requestSubmit()}
  tone="danger"
/>

<form method="POST" action="?/scale" use:enhance={scaleSubmit} bind:this={scaleForm} hidden>
  <input type="hidden" name="count" value={scaleCount} />
</form>
<form
  method="POST"
  action="?/autoscale"
  use:enhance={autoscaleSubmit}
  bind:this={autoscaleForm}
  hidden
>
  <input type="hidden" name="enabled" value={autoscaleOn ? 'false' : 'true'} />
</form>

<style>
  .tiles {
    margin-bottom: var(--bb-space-4);
  }

  .stepper {
    --input-w: 76px;
    display: inline-flex;
    align-items: center;
    gap: var(--bb-space-1);
    transition: opacity var(--bb-dur-fast) ease;
  }
  .stepper.dim {
    opacity: 0.45;
  }
</style>
