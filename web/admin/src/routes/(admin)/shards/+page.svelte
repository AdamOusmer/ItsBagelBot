<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import { livePoll } from '@bagel/kit/live-poll';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import type { ShardSnapshot, TrialSocket } from '@bagel/kit';
  import type { StatusTone } from '@bagel/kit/status-tone';
  import type { TrialChannel, TrialSnapshot } from '$lib/server/services';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { allows } from '$lib/access';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import ShardRow from '$lib/components/shards/ShardRow.svelte';
  import LoadMeter from '$lib/components/shards/LoadMeter.svelte';
  import FleetRow from '$lib/components/shards/FleetRow.svelte';
  import { podIndex, rateLabel } from '$lib/components/shards/shard-state';
  import { eventsPerSecond, pctLabel, resolveCapacity, utilizationPct } from '$lib/throughput';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const canScale = $derived(allows(data.role, 'shards.scale'));

  let snap = $state<ShardSnapshot | null>(null);
  let trialSnapshot = $state<TrialSnapshot | null>(null);
  let trialPolled = false;
  let degraded = $state(false);
  let live = $state(false);
  $effect(() => {
    let alive = true;
    data.bundle.then((b) => {
      if (!alive) return;
      if (snap === null) snap = b.snapshot;
      if (!trialPolled) trialSnapshot = b.trials;
      degraded = b.degraded;
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

  async function pollSnapshot(): Promise<boolean> {
    try {
      const res = await fetch('/shards/snapshot');
      if (!res.ok) return false;
      const body = (await res.json()) as { snapshot?: ShardSnapshot | null; trials?: TrialSnapshot | null };
      trialPolled = true;
      trialSnapshot = body.trials ?? null;
      if (!body.snapshot) {
        live = false;
        return false;
      }
      snap = body.snapshot;
      degraded = false;
      live = true;
    } catch {
      live = false;
    }
    return false;
  }

  onMount(() => {
    const stop = livePoll(pollSnapshot, {
      firstDelayMs: 1500,
      delayMs: () => (document.hidden ? HIDDEN_MS : Date.now() < fastUntil ? FAST_MS : SLOW_MS),
      timeoutMs: Number.POSITIVE_INFINITY
    });
    const onVis = () => {
      if (!document.hidden) pollSnapshot();
    };
    document.addEventListener('visibilitychange', onVis);
    return () => {
      stop();
      document.removeEventListener('visibilitychange', onVis);
    };
  });

  const capacity = $derived(snap ? resolveCapacity(snap) : null);
  const shards = $derived(snap?.shards ?? []);
  const trialConnections = $derived(
    trialSnapshot?.trials.filter((row) => row.state !== 'removed' && row.state !== 'promoted') ?? []
  );
  const connected = $derived(shards.filter((s) => s.state === 'connected').length);
  const minShards = $derived(snap?.min_shards ?? 1);
  const maxShards = $derived(
    snap?.max_shards ?? capacity?.websocket_autoscale_max_shards ?? 11
  );
  const autoscaleOn = $derived(snap?.autoscale ?? false);

  function evRate(load?: number): number {
    return capacity ? eventsPerSecond(load, capacity.load_window_seconds) : 0;
  }

  const trialLoads = $derived(snap?.trial_loads ?? {});
  const trialSockets = $derived(snap?.trial_sockets ?? []);

  type TrialGroup = { slot: number; socket?: TrialSocket; channels: TrialChannel[] };
  const trialGroups = $derived.by((): TrialGroup[] => {
    const slots = new Set([
      ...trialSockets.filter((socket) => socket.channels > 0).map((socket) => socket.slot),
      ...trialConnections.map((trial) => trial.slot ?? 0)
    ]);
    return [...slots]
      .sort((a, b) => a - b)
      .map((slot) => ({
        slot,
        socket: trialSockets.find((socket) => socket.slot === slot),
        channels: trialConnections
          .filter((trial) => (trial.slot ?? 0) === slot)
          .sort((a, b) => (a.display_name || a.broadcaster_id).localeCompare(b.display_name || b.broadcaster_id))
      }));
  });

  const SOCKET_TONE: Record<TrialSocket['state'], StatusTone> = {
    connected: 'success',
    connecting: 'warning',
    idle: 'neutral'
  };
  const SOCKET_LABEL: Record<TrialSocket['state'], string> = {
    connected: 'admin.shards.trialSocketConnected',
    connecting: 'admin.shards.trialSocketConnecting',
    idle: 'admin.shards.trialSocketIdle'
  };
  const aggregateEps = $derived(
    shards.reduce((sum, s) => sum + evRate(s.load), 0) +
      Object.values(trialLoads).reduce((sum, load) => sum + evRate(load), 0)
  );
  const aggregateUtilization = $derived(
    capacity ? utilizationPct(aggregateEps, capacity.effective_rated_eps) : 0
  );
  const conduit = $derived(snap?.conduit_manager);

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
    busy = true;
    confirmScaleDown = false;
    return async ({ result }) => {
      busy = false;
      fastUntil = Date.now() + SETTLE_WINDOW_MS;
      const p = actionPayload<ActionPayload>(result);
      if (result.type === 'success' && p?.action?.ok) {
        if (p.snapshot) snap = p.snapshot;
        toast('ok', p.action.notice);
        return;
      }
      failed(p, t('admin.shards.scaleFailed'));
    };
  };

  const autoscaleSubmit: SubmitFunction = () => {
    busy = true;
    const before = snap ? { ...snap } : null;
    if (snap) snap = { ...snap, autoscale: !snap.autoscale };
    return async ({ result }) => {
      busy = false;
      fastUntil = Date.now() + SETTLE_WINDOW_MS;
      const p = actionPayload<ActionPayload>(result);
      if (result.type === 'success' && p?.action?.ok) {
        if (p.snapshot) snap = p.snapshot;
        toast('ok', p.action.notice);
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
      <StatTile
        label={t('admin.shards.tileShards')}
        value={`${connected}/${snap.shard_count || shards.length}`}
        delta={t('admin.shards.tileShardsDelta', { nodes: String(snap.nodes.length) })}
      />
      <StatTile
        label={t('admin.shards.tileThroughput')}
        value={rateLabel(aggregateEps)}
        unit={t('admin.shards.eps')}
        delta={t('admin.shards.tileThroughputDelta', {
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
    </div>

    <PageToolbar>
      {#snippet lead()}
        <span class="conduit">
          <StatusDot tone={conduit?.state === 'ready' ? 'success' : 'warning'} />
          {t('admin.shards.conduit', {
            state: conduit?.state ?? t('admin.shards.unknownState'),
            node: conduit?.node ?? '-'
          })}
          {#if live}<span class="live">{t('admin.shards.live')}</span>{/if}
        </span>
      {/snippet}
      {#snippet trail()}
        {#if canScale}
          <span class="switch-field">
            <span class="switch-label">{t('admin.shards.autoscaleLabel')}</span>
            <Switch
              checked={autoscaleOn}
              label={t('admin.shards.autoscaleLabel')}
              describedby="shards-autoscale-hint"
              pending={busy}
              onchange={() => autoscaleForm?.requestSubmit()}
            />
          </span>
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
      <p class="hint" id="shards-autoscale-hint">
        {autoscaleOn ? t('admin.shards.autoscaleOnHint') : t('admin.shards.autoscaleOffHint')}
      </p>
    {/if}

    <DeckList>
      {#if shards.length && capacity}
        <ul class="bb-list" aria-label={t('admin.shards.listLabel')}>
          {#each shards as shard (shard.shard_id)}
            <li>
              <ShardRow
                {shard}
                nodes={snap.nodes}
                eps={evRate(shard.load)}
                utilization={utilizationPct(evRate(shard.load), capacity.websocket_rated_eps)}
                targetUtilization={capacity.target_utilization_pct}
              />
            </li>
          {/each}
        </ul>
      {:else}
        <EmptyState title={t('admin.shards.empty')} body={t('admin.shards.emptyBody')} />
      {/if}
    </DeckList>

    {#if data.canViewTrials}
    <section class="trial-connections" aria-label={t('admin.shards.trialConnections')}>
      <h2>{t('admin.shards.trialConnections')}</h2>
      {#if trialSnapshot?.socket_target}
        <p class="trial-hint">{t('admin.shards.trialConduit', { target: String(trialSnapshot.socket_target), max: '3' })}</p>
      {/if}
      {#if trialSnapshot === null}
        <p class="trial-hint">{t('admin.shards.trialUnavailable')}</p>
      {:else if trialGroups.length === 0}
        <p class="trial-hint">{t('admin.shards.trialEmpty')}</p>
      {:else}
        <DeckList>
          <ul class="bb-list" aria-label={t('admin.shards.trialConnections')}>
            {#each trialGroups as group (group.slot)}
              <li>
                <FleetRow
                  tone={group.socket ? SOCKET_TONE[group.socket.state] : 'warning'}
                  name={t('admin.shards.trialSocket', { id: String(group.slot) })}
                  state={t(group.socket ? SOCKET_LABEL[group.socket.state] : 'admin.shards.trialSocketUnowned')}
                  meta={t(group.channels.length === 1 ? 'admin.shards.trialSocketMetaOne' : 'admin.shards.trialSocketMeta', {
                    pod: (group.socket && podIndex(snap.nodes, group.socket.node)) || '-',
                    count: String(group.channels.length)
                  })}
                  eps={evRate(group.socket?.load)}
                  utilization={capacity ? utilizationPct(evRate(group.socket?.load), capacity.websocket_rated_eps) : 0}
                  targetUtilization={capacity?.target_utilization_pct ?? 75}
                />
              </li>
              {#each group.channels as trial (trial.broadcaster_id)}
                <li class="trial-row">
                  <StatusDot tone={!trial.enabled ? 'neutral' : trial.state === 'receiving' ? 'success' : 'warning'} />
                  <span class="trial-who">
                    <strong>{trial.display_name?.trim() || trial.broadcaster_id}</strong>
                    {#if trial.display_name?.trim()}<small>{t('admin.shards.trialBroadcasterId', { id: trial.broadcaster_id })}</small>{/if}
                  </span>
                  <span class="trial-detail">
                    {trial.enabled ? t(`admin.trials.state.${trial.state}`) : t('admin.trials.off')} ·
                    {t('admin.trials.received', { count: String(trial.received ?? 0) })}
                  </span>
                  {#if capacity}
                    <LoadMeter
                      eps={evRate(trialLoads[trial.broadcaster_id])}
                      utilization={utilizationPct(evRate(trialLoads[trial.broadcaster_id]), capacity.websocket_rated_eps)}
                      targetUtilization={capacity.target_utilization_pct}
                    />
                  {/if}
                </li>
              {/each}
            {/each}
          </ul>
        </DeckList>
      {/if}
    </section>
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
  danger
  {busy}
  onCancel={() => (confirmScaleDown = false)}
  onConfirm={() => scaleForm?.requestSubmit()}
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
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 14px;
    margin-bottom: 18px;
  }

  .conduit {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
  }
  .live {
    color: var(--bb-green-glow);
    letter-spacing: 0.08em;
    text-transform: uppercase;
    font-size: 10px;
  }

  .hint {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    margin: 0 0 14px;
  }

  .switch-field {
    display: inline-flex;
    align-items: center;
    gap: 9px;
  }
  .switch-label {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .stepper {
    --input-w: 76px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    transition: opacity 0.15s;
  }
  .stepper.dim {
    opacity: 0.45;
  }
  .trial-connections { margin-top: 24px; }
  .trial-connections h2 { font-size: 16px; margin: 0 0 10px; }
  .trial-hint { color: var(--bb-muted); font-size: 12.5px; }
  .trial-row { display: flex; align-items: center; gap: 12px; min-width: 0; padding: 12px 14px 12px 34px; border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08)); }
  .trial-who { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
  .trial-who strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .trial-who small, .trial-detail { color: var(--bb-muted); font-size: 11px; }
  .trial-detail { text-align: right; }
</style>
