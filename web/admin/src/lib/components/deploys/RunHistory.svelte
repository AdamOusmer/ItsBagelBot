<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
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
            <td><Text as="span" mono><TextLink variant="inline" href="/deploys/{run.id}">{runName(run) || run.id}</TextLink></Text></td>
            <td>{t(KIND_KEY[run.kind])}</td>
            <td><StatePill tone={runPill(run.state)}>{t(RUN_STATE_KEY[run.state])}</StatePill></td>
            <td>{run.current_stage ? t(STAGE_KEY[run.current_stage]) : '-'}</td>
            <td><Text as="span" mono tone="muted">{run.actor.login}</Text></td>
            <td title={run.created_at}><Text as="span" mono tone="muted">{ago(run.created_at)}</Text></td>
          </tr>
        {/each}
      </tbody>
    </Table>
  {/if}
</Card>

