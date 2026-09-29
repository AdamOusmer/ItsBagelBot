<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { statusTone } from '@bagel/kit/status-tone';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { ShardSnapshot } from '@bagel/kit';
  import type { TrialSnapshot } from '$lib/server/services';
  import type { Panel } from '../../../routes/(admin)/+page.server';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import { onMount } from 'svelte';
  import { livePoll } from '@bagel/kit/live-poll';

  let { snapshot, ok, trialRead, showTrials }: { snapshot: ShardSnapshot; ok: boolean; trialRead: Promise<Panel<TrialSnapshot>>; showTrials: boolean } = $props();

  const { t } = getI18n();

  let polled = $state<ShardSnapshot | null>(null);
  let trials = $state<Panel<TrialSnapshot> | null>(null);
  let trialsPolled = false;
  const view = $derived(polled ?? snapshot);
  const live = $derived(polled !== null || ok);

  $effect(() => {
    let alive = true;
    trialRead.then((read) => {
      if (alive && !trialsPolled) trials = read;
    });
    return () => {
      alive = false;
    };
  });

  onMount(() => {
    let alive = true;
    const options = {
      firstDelayMs: 8000,
      delayMs: () => (document.hidden ? 15000 : 8000),
      timeoutMs: Number.POSITIVE_INFINITY
    };

    async function pollFleet(): Promise<boolean> {
      try {
        const res = await fetch('/shards/snapshot');
        if (!res.ok) return false;
        const body = (await res.json()) as { snapshot?: ShardSnapshot | null };
        if (alive && body.snapshot) polled = body.snapshot;
      } catch {
        return false;
      }
      return false;
    }

    async function pollTrials(): Promise<boolean> {
      try {
        const res = await fetch('/trials/snapshot');
        if (!res.ok) return false;
        const body = (await res.json()) as { snapshot?: TrialSnapshot | null };
        if (alive && body.snapshot) {
          trialsPolled = true;
          trials = { ok: true, value: body.snapshot };
        }
      } catch {
        return false;
      }
      return false;
    }

    const stopFleet = livePoll(pollFleet, options);
    const stopTrials = showTrials ? livePoll(pollTrials, options) : () => {};
    return () => {
      alive = false;
      stopFleet();
      stopTrials();
    };
  });

  const connected = $derived(view.shards.filter((s) => s.state === 'connected').length);
  const total = $derived(view.shard_count || view.shards.length);
  const tone = $derived(
    !live ? statusTone('unavailable') : statusTone(total > 0 && connected === total ? 'online' : 'degraded')
  );
</script>

<Card as="section">
  <CardHead title={t('admin.overview.fleetTitle')}>
    {#snippet action()}
      <a class="bb-card-head__more" href="/shards">{t('admin.overview.fleetAll')}</a>
    {/snippet}
  </CardHead>

  <p class="summary">
    <StatusDot {tone} />
    <Text as="span" size="sm">
      {t('admin.overview.fleetSummary', {
        connected: String(connected),
        total: String(total),
        nodes: String(view.nodes.length)
      })}
    </Text>
    {#if view.conduit_manager}
      <Text as="span" size="xs" mono tone="muted">
        {t('admin.overview.fleetConduit', {
          state: view.conduit_manager.state,
          node: view.conduit_manager.node || '-'
        })}
      </Text>
    {/if}
  </p>

  {#if view.shards.length}
    <div class="bb-node-list bb-stagger">
      {#each view.shards as s (s.shard_id)}
        <div class="bb-node-list__row">
          <StatusDot tone={statusTone(s.state === 'connected' ? 'online' : 'degraded')} />
          <span class="bb-node-list__name">{t('admin.overview.fleetShard', { id: String(s.shard_id) })}</span>
          <span class="bb-node-list__meta">{s.state} · {s.host || '-'}</span>
          <span class="bb-node-list__trail">{t('admin.overview.fleetAttempts', { n: String(s.attempts ?? 0) })}</span>
        </div>
      {/each}
    </div>
  {:else}
    <EmptyState title={t('admin.overview.fleetEmpty')} />
  {/if}

  {#if showTrials}
    {#if trials === null}
      <div class="trial-hint"><Text size="xs" tone="muted">{t('admin.shards.trialLoading')}</Text></div>
    {:else if !trials.ok}
      <div class="trial-hint"><Text size="xs" tone="muted">{t('admin.shards.trialUnavailable')}</Text></div>
    {:else}
      {@const activeTrials = trials.value.trials.filter((row) => row.state !== 'removed' && row.state !== 'promoted')}
      {#if activeTrials.length}
        <div class="trials">
          <Heading level={6} as="h3">{t('admin.shards.trialConnections')}</Heading>
          <div class="bb-node-list bb-stagger">
            {#each [...activeTrials].sort((a, b) => (a.display_name || a.broadcaster_id).localeCompare(b.display_name || b.broadcaster_id)) as trial (trial.broadcaster_id)}
              <div class="bb-node-list__row">
                <StatusDot tone={statusTone(!trial.enabled ? 'unavailable' : trial.state === 'receiving' ? 'online' : 'degraded')} />
                <span class="bb-node-list__name">{trial.display_name?.trim() || trial.broadcaster_id}</span>
                <span class="bb-node-list__meta">{trial.display_name?.trim() ? t('admin.shards.trialBroadcasterId', { id: trial.broadcaster_id }) : ''}</span>
                <span class="bb-node-list__trail">{trial.enabled ? t(`admin.trials.state.${trial.state}`) : t('admin.trials.off')}</span>
              </div>
            {/each}
          </div>
        </div>
      {/if}
    {/if}
  {/if}
</Card>

<style>
  .summary {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    flex-wrap: wrap;
    margin: 0 0 12px;
  }
  .trials {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    margin-top: 18px;
  }
  .trial-hint {
    margin-top: 12px;
  }
</style>
