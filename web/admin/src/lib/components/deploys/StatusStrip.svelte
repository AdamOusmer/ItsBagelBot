<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployPlan, DeployRun, DeployRunSummary } from '$lib/deploys/types';
  import { KIND_KEY, RUN_STATE_KEY, STAGE_KEY, currentStageId, runName, shortSha } from './view';

  let {
    plan,
    active,
    last
  }: {
    plan: DeployPlan | null;
    active: DeployRun | null;
    last: DeployRunSummary | null;
  } = $props();

  const { t } = getI18n();

  const dash = '-';

  const drifted = $derived([...new Set((plan?.drift ?? []).map((d) => d.workload))]);

  function clusterValue(): string {
    if (active) return t('admin.deploys.rolling');
    if (!plan) return t('admin.deploys.unknown');
    return plan.in_sync ? t('admin.deploys.inSync') : t('admin.deploys.drifted');
  }

  function clusterDelta(): string {
    if (active) return t(STAGE_KEY[currentStageId(active)]);
    if (drifted.length > 0) return drifted.join(', ');
    return plan ? t('admin.deploys.syncedDelta') : dash;
  }

  function lastDelta(): string {
    if (!last) return dash;
    return `${t(KIND_KEY[last.kind])} · ${t(RUN_STATE_KEY[last.state])} · ${ago(last.updated_at)}`;
  }

  const lastValue = $derived(last ? runName(last) || t(KIND_KEY[last.kind]) : t('admin.deploys.none'));
</script>

<div class="strip">
  <StatTile
    data-text
    label={t('admin.deploys.statusLive')}
    value={plan?.live_version ?? dash}
    delta={plan?.live_sha ? shortSha(plan.live_sha) : dash}
  />
  <StatTile
    data-text
    label={t('admin.deploys.statusCluster')}
    value={clusterValue()}
    delta={clusterDelta()}
  >
    {#snippet trail()}
      <span class="trail">
        {#if active}<TextLink href="/deploys/{active.id}" label={t('admin.deploys.openRun')} />{/if}
      </span>
    {/snippet}
  </StatTile>
  <StatTile data-text label={t('admin.deploys.statusLastRun')} value={lastValue} delta={lastDelta()}>
    {#snippet trail()}
      <span class="trail">
        {#if last}<TextLink href="/deploys/{last.id}" label={t('admin.deploys.openLast')} />{/if}
      </span>
    {/snippet}
  </StatTile>
</div>

<style>
  .strip {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }
  .trail {
    display: inline-block;
    min-height: 1.4em;
    font-size: 12px;
  }
  @media (max-width: 760px) {
    .strip {
      grid-template-columns: 1fr;
    }
  }
</style>
