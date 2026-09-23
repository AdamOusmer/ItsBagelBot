<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Shard roll-call. The summary line answers the only question this panel is
  // asked at a glance ("are they all up"); the rows are there for the one time
  // a month the answer is no.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import { statusTone } from '@bagel/kit/status-tone';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { ShardSnapshot } from '@bagel/kit';
  import type { TrialSnapshot } from '$lib/server/services';
  import type { Panel } from '../../../routes/(admin)/+page.server';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';

  let { snapshot, ok, trialRead, showTrials }: { snapshot: ShardSnapshot; ok: boolean; trialRead: Promise<Panel<TrialSnapshot>>; showTrials: boolean } = $props();

  const { t } = getI18n();

  const connected = $derived(snapshot.shards.filter((s) => s.state === 'connected').length);
  const total = $derived(snapshot.shard_count || snapshot.shards.length);
  // `ok` is whether the READ landed, which is a different question from whether
  // the fleet is healthy: a failed read is neutral (we cannot tell), never green.
  const tone = $derived(
    !ok ? statusTone('unavailable') : statusTone(total > 0 && connected === total ? 'online' : 'degraded')
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
    <span>
      {t('admin.overview.fleetSummary', {
        connected: String(connected),
        total: String(total),
        nodes: String(snapshot.nodes.length)
      })}
    </span>
    {#if snapshot.conduit_manager}
      <span class="conduit">
        {t('admin.overview.fleetConduit', {
          state: snapshot.conduit_manager.state,
          node: snapshot.conduit_manager.node || '-'
        })}
      </span>
    {/if}
  </p>

  {#if snapshot.shards.length}
    <div class="node-list">
      {#each snapshot.shards as s (s.shard_id)}
        <div class="node-row">
          <StatusDot tone={statusTone(s.state === 'connected' ? 'online' : 'degraded')} />
          <span class="nm">{t('admin.overview.fleetShard', { id: String(s.shard_id) })}</span>
          <span class="sv">{s.state} · {s.host || '-'}</span>
          <span class="pg">{t('admin.overview.fleetAttempts', { n: String(s.attempts ?? 0) })}</span>
        </div>
      {/each}
    </div>
  {:else}
    <EmptyState title={t('admin.overview.fleetEmpty')} />
  {/if}

  {#if showTrials}
    {#await trialRead}
      <p class="trial-hint">{t('admin.shards.trialLoading')}</p>
    {:then result}
      {#if !result.ok}
        <p class="trial-hint">{t('admin.shards.trialUnavailable')}</p>
      {:else}
        {@const activeTrials = result.value.trials.filter((row) => row.state !== 'removed' && row.state !== 'promoted')}
        {#if activeTrials.length}
          <h3 class="trial-title">{t('admin.shards.trialConnections')}</h3>
          <div class="node-list">
            {#each [...activeTrials].sort((a, b) => (a.display_name || a.broadcaster_id).localeCompare(b.display_name || b.broadcaster_id)) as trial (trial.broadcaster_id)}
              <div class="node-row">
                <StatusDot tone={statusTone(trial.state === 'receiving' ? 'online' : 'degraded')} />
                <span class="nm">{trial.display_name?.trim() || trial.broadcaster_id}</span>
                <span class="sv">{trial.display_name?.trim() ? t('admin.shards.trialBroadcasterId', { id: trial.broadcaster_id }) : ''}</span>
                <span class="pg">{t(`admin.trials.state.${trial.state}`)}</span>
              </div>
            {/each}
          </div>
        {/if}
      {/if}
    {/await}
  {/if}
</Card>

<style>
  .summary {
    display: flex;
    align-items: center;
    gap: 9px;
    flex-wrap: wrap;
    margin: 0 0 12px;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
  }
  .summary .conduit {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
  }
  .trial-title { margin: 18px 0 8px; font-size: 13px; }
  .trial-hint { margin: 12px 0 0; color: var(--bb-muted); font-size: 12px; }
</style>
