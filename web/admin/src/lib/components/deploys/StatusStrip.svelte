<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployPlanResult, DeployRunSummary, DeployRunsResult } from '#lib/deploys/types';
  import { KIND_KEY, RUN_STATE_KEY, STAGE_KEY, currentStageId, runName, shortSha } from './view';

  let { planned, runs }: { planned: Promise<DeployPlanResult>; runs: Promise<DeployRunsResult> } = $props();
  const { t, locale } = getI18n();
  const relative = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'short' });

  function relativeTime(iso: string): string {
    const time = Date.parse(iso);
    if (!Number.isFinite(time)) return '';
    const minutes = Math.max(0, Math.round((Date.now() - time) / 60_000));
    if (minutes < 60) return relative.format(-minutes, 'minute');
    const hours = Math.round(minutes / 60);
    if (hours < 48) return relative.format(-hours, 'hour');
    return relative.format(-Math.round(hours / 24), 'day');
  }

  function lastDelta(last: DeployRunSummary): string {
    return [t(KIND_KEY[last.kind]), t(RUN_STATE_KEY[last.state]), relativeTime(last.updated_at)].filter(Boolean).join(' · ');
  }
</script>

{#snippet unknownTile(label: string)}
  <StatTile static text fitCopy label={t(label)} value={t('admin.deploys.unknown')} />
{/snippet}

{#snippet planAlert(message: string)}
  <div class="tile-alert">
    <Scroller maxHeight="100%" role="region" aria-label={t('admin.deploys.statusLive')} tabindex={0}>
      <AlertBanner tone="danger" flush>{message}</AlertBanner>
    </Scroller>
  </div>
{/snippet}

{#snippet clusterPlan()}
  {#await planned}
    <Skeleton variant="block" height="100%" />
  {:then { plan }}
    {#if plan?.active_run_id}
      <StatTile static text fitCopy label={t('admin.deploys.statusCluster')} value={t('admin.deploys.rolling')} />
    {:else if plan}
      <StatTile static text fitCopy flat label={t('admin.deploys.statusCluster')}
        value={t(plan.in_sync ? 'admin.deploys.inSync' : 'admin.deploys.drifted')}
        delta={t(plan.in_sync ? 'admin.deploys.syncedDelta' : 'admin.deploys.behindDelta')} />
    {:else}
      {@render unknownTile('admin.deploys.statusCluster')}
    {/if}
  {:catch}
    {@render unknownTile('admin.deploys.statusCluster')}
  {/await}
{/snippet}

<Grid cols={3} gap={3} stackAt="md">
  <div class="tile">
    {#await planned}
      <Skeleton variant="block" height="100%" />
    {:then { plan, planError }}
      {#if planError}
        {@render planAlert(t('admin.deploys.planError', { error: planError }))}
      {:else}
        <StatTile static text fitCopy label={t('admin.deploys.statusLive')}
          value={plan?.live_version ?? t('admin.deploys.unknown')} delta={shortSha(plan?.live_sha)} />
      {/if}
    {:catch}
      {@render planAlert(t('admin.deploys.planUnavailable'))}
    {/await}
  </div>

  <div class="tile">
    {#await runs}
      <Skeleton variant="block" height="100%" />
    {:then { active }}
      {#if active}
        <StatTile static text fitCopy flat label={t('admin.deploys.statusCluster')}
          value={t('admin.deploys.rolling')} delta={t(STAGE_KEY[currentStageId(active)])} />
      {:else}
        {@render clusterPlan()}
      {/if}
    {:catch}
      {@render clusterPlan()}
    {/await}
  </div>

  <div class="tile">
    {#await runs}
      <Skeleton variant="block" height="100%" />
    {:then result}
      {@const last = result.runs.find((run) => run.id !== result.active?.id)}
      <StatTile static text fitCopy flat label={t('admin.deploys.statusLastRun')}
        value={last ? runName(last) || t(KIND_KEY[last.kind]) : t(result.runsError ? 'admin.deploys.unknown' : 'admin.deploys.none')}
        delta={last ? lastDelta(last) : ''}>
        {#snippet trailing()}
          {#if last}
            <TextLink href="/deploys/{encodeURIComponent(last.id)}" label={t('admin.deploys.openLast')} size="var(--bb-text-xs)" />
          {/if}
        {/snippet}
      </StatTile>
    {:catch}
      {@render unknownTile('admin.deploys.statusLastRun')}
    {/await}
  </div>
</Grid>

<style>
  .tile {
    display: grid;
    grid-template: minmax(0, 1fr) / minmax(0, 1fr);
    min-inline-size: 0;
    block-size: 8.25rem;
  }
  .tile-alert {
    min-block-size: 0;
    overflow-wrap: anywhere;
  }
</style>
