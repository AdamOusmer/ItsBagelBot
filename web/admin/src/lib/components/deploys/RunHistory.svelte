<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // The last runs the deployer keeps (DEPLOY_KEEP_RUNS), newest first. Each
  // row opens the run page, where the stages, logs and verbs are.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployRunSummary } from '$lib/deploys/types';
  import StatePill from '$lib/components/StatePill.svelte';
  import { KIND_KEY, RUN_STATE_KEY, STAGE_KEY, runName, runPill } from './view';

  let { runs }: { runs: DeployRunSummary[] } = $props();

  const { t } = getI18n();
</script>

<Card>
  <CardHead title={t('admin.deploys.history')} />
  {#if runs.length === 0}
    <EmptyState title={t('admin.deploys.historyEmpty')} body={t('admin.deploys.historyEmptyBody')} />
  {:else}
    <Table label={t('admin.deploys.history')} compact>
      <thead>
        <tr>
          <th scope="col">{t('admin.deploys.col.run')}</th>
          <th scope="col">{t('admin.deploys.col.kind')}</th>
          <th scope="col">{t('admin.deploys.col.state')}</th>
          <th scope="col">{t('admin.deploys.col.stage')}</th>
          <th scope="col">{t('admin.deploys.col.by')}</th>
          <th scope="col">{t('admin.deploys.col.started')}</th>
        </tr>
      </thead>
      <tbody>
        {#each runs as run (run.id)}
          <tr>
            <td><a class="run" href="/deploys/{run.id}">{runName(run) || run.id}</a></td>
            <td>{t(KIND_KEY[run.kind])}</td>
            <td><StatePill tone={runPill(run.state)}>{t(RUN_STATE_KEY[run.state])}</StatePill></td>
            <td>{run.current_stage ? t(STAGE_KEY[run.current_stage]) : '-'}</td>
            <td class="mono">{run.actor.login}</td>
            <td class="mono" title={run.created_at}>{ago(run.created_at)}</td>
          </tr>
        {/each}
      </tbody>
    </Table>
  {/if}
</Card>

<style>
  .run {
    font-family: var(--bb-font-mono);
    font-size: 12.5px;
    color: var(--bb-white);
    text-decoration: underline;
    text-decoration-color: var(--rule);
    text-underline-offset: 3px;
  }
  .mono {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    white-space: nowrap;
  }
</style>
