<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Twitch ingress fleet health, on the shared deck.
  //
  // The page was a grid of `.shard-card`s with its own tone triple, its own
  // stat block, a hand-rolled `.btn-toggle` for autoscale and a per-shard SVG
  // sparkline. It is now three StatTiles, one toolbar and a DeckList of rows:
  // the sparkline went with the change, because a 30-sample ring buffer over an
  // 8-second poll is four minutes of history rendered as decoration, and the
  // per-shard load it drew is already the row's bar.
  //
  // Scale and autoscale are the only two verbs, both admin-only. The client
  // gate is `allows`, the same question ?/scale and ?/autoscale ask through
  // requireRole -- a hidden control is a courtesy, not the boundary.
  import { onMount } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/kit/components/PageHead.svelte';
  import PageToolbar from '@bagel/kit/components/PageToolbar.svelte';
  import DeckList from '@bagel/kit/components/DeckList.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import AlertBanner from '@bagel/kit/components/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/kit/components/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import { livePoll } from '@bagel/kit/live-poll';
  import { toast } from '@bagel/kit/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import type { ShardSnapshot } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { allows } from '$lib/access';
  import StatusDot from '$lib/components/StatusDot.svelte';
  import ShardRow from '$lib/components/shards/ShardRow.svelte';
  import { rateLabel } from '$lib/components/shards/shard-state';
  import { eventsPerSecond, resolveCapacity, utilizationPct } from '$lib/throughput';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const canScale = $derived(allows(data.role, 'shards.scale'));

  // ── Streamed snapshot -> local state ───────────────────────────────────────
  let snap = $state<ShardSnapshot | null>(null);
  let degraded = $state(false);
  let live = $state(false);
  $effect(() => {
    let alive = true;
    data.bundle.then((b) => {
      if (!alive) return;
      // The poll may already have delivered something fresher than SSR.
      if (snap === null) snap = b.snapshot;
      degraded = b.degraded;
    });
    return () => {
      alive = false;
    };
  });

  // ── Live poll ──────────────────────────────────────────────────────────────
  // Same schedule the hand-rolled loop had: 2s while a recent action settles,
  // else 8s, first tick at 1.5s, paused while the tab is hidden.
  //
  // livePoll, not another setTimeout chain: it owns the generation counter that
  // makes a teardown mid-fetch safe. The one thing it is NOT here is a poll that
  // settles -- fleet state never "arrives", it just keeps moving -- so the tick
  // always answers false and the deadline is Infinity.
  const FAST_MS = 2000;
  const SLOW_MS = 8000;
  const SETTLE_WINDOW_MS = 30_000;
  let fastUntil = 0;

  async function pollSnapshot(): Promise<boolean> {
    if (typeof document !== 'undefined' && document.hidden) return false;
    try {
      const res = await fetch('/shards/snapshot');
      if (!res.ok) return false;
      const body = (await res.json()) as { snapshot?: ShardSnapshot };
      if (!body.snapshot) {
        live = false; // the endpoint answered but had no live snapshot: say so
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
      delayMs: () => (Date.now() < fastUntil ? FAST_MS : SLOW_MS),
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

  // ── Derived fleet view ─────────────────────────────────────────────────────
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

  const aggregateEps = $derived(shards.reduce((sum, s) => sum + evRate(s.load), 0));
  const aggregateUtilization = $derived(
    capacity ? utilizationPct(aggregateEps, capacity.effective_rated_eps) : 0
  );
  const conduit = $derived(snap?.conduit_manager);

  // ── Scale stepper ──────────────────────────────────────────────────────────
  // Tracks the operator's edits as an OFFSET from the authoritative desired
  // count, so a poll landing mid-edit moves the base without stealing what was
  // typed; the offset resets only when the base itself moves.
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

  // ── Actions ────────────────────────────────────────────────────────────────
  type ActionPayload = {
    action?: { ok: boolean; notice: string };
    snapshot?: ShardSnapshot;
    error?: string;
  };

  let busy = $state(false);
  let scaleForm = $state<HTMLFormElement | null>(null);
  let autoscaleForm = $state<HTMLFormElement | null>(null);
  let confirmScaleDown = $state(false);

  // Scaling DOWN drops live websockets, so it confirms; scaling up is additive
  // and fires straight away.
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
    // Optimistic: the switch flips instantly and rolls back if the ingress
    // refuses, so the toggle never shows a mode the fleet is not in.
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
          pct: aggregateUtilization.toFixed(1)
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
          <!-- Switch renders no visible text of its own (its label is the
               accessible name), so the toolbar supplies one: an unlabelled
               toggle beside a number stepper is a coin flip. -->
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
          <!-- The stepper stays submittable while autoscale is on (visually
               muted) so an operator can pre-set the floor before turning it
               off. -->
          <span class="stepper" class:dim={autoscaleOn}>
            <button
              type="button"
              class="step"
              aria-label={t('admin.shards.decrease')}
              disabled={scaleCount <= minShards}
              onclick={() => stepScale(-1)}>&minus;</button
            >
            <input
              class="step-input"
              type="number"
              min={minShards}
              max={maxShards}
              value={scaleCount}
              aria-label={t('admin.shards.countLabel')}
              oninput={(e) => typeScale((e.target as HTMLInputElement).value)}
            />
            <button
              type="button"
              class="step"
              aria-label={t('admin.shards.increase')}
              disabled={scaleCount >= maxShards}
              onclick={() => stepScale(1)}>+</button
            >
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
    display: inline-flex;
    align-items: center;
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-sm);
    overflow: hidden;
    transition: opacity 0.15s;
  }
  .stepper.dim {
    opacity: 0.45;
  }
  .step {
    background: rgba(255, 255, 255, 0.04);
    border: none;
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: 16px;
    width: 34px;
    height: 34px;
    cursor: pointer;
    line-height: 1;
  }
  .step:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.09);
  }
  .step:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }
  .step-input {
    width: 50px;
    height: 34px;
    text-align: center;
    background: transparent;
    border: none;
    border-left: 1px solid var(--bb-border-strong);
    border-right: 1px solid var(--bb-border-strong);
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: 14px;
    font-weight: 600;
    -moz-appearance: textfield;
    appearance: textfield;
  }
  .step-input::-webkit-outer-spin-button,
  .step-input::-webkit-inner-spin-button {
    -webkit-appearance: none;
    margin: 0;
  }
</style>
