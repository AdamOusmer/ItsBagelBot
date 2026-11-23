<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployRunSummary, DeployRunsResult } from '#lib/deploys/types';
  import FilterTabs from '#lib/components/FilterTabs.svelte';
  import StatePill from '#lib/components/StatePill.svelte';
  import { KIND_KEY, RUN_STATE_KEY, STAGE_KEY, isTerminal, runName, runPill } from './view';

  let { result }: { result: Promise<DeployRunsResult> } = $props();
  const { locale, t } = getI18n();
  const dates = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' });
  const numbers = new Intl.NumberFormat(locale);
  const empty: DeployRunSummary[] = [];
  let filter = $state('all');
  let loaded = $state.raw<DeployRunsResult | null>(null);
  let pending = $state(true);

  $effect(() => {
    let current = true;

    function settle(value: DeployRunsResult | null) {
      if (!current) return;
      loaded = value;
      pending = false;
    }

    pending = true;
    void result.then(settle, () => settle(null));
    return () => {
      current = false;
    };
  });

  const active = $derived(loaded?.active);
  const filters = $derived(options(loaded?.runs ?? empty, !loaded || Boolean(loaded.runsError)));

  function options(runs: DeployRunSummary[], pending: boolean) {
    const finished = runs.filter((run) => isTerminal(run.state)).length;
    const labels = [
      { value: 'all', label: t('admin.deploys.filterAll'), count: runs.length },
      { value: 'active', label: t('admin.deploys.filterActive'), count: runs.length - finished },
      { value: 'finished', label: t('admin.deploys.filterFinished'), count: finished }
    ];
    return labels.map((option) => ({ ...option, count: pending ? '–' : numbers.format(option.count) }));
  }

  function matches(run: DeployRunSummary): boolean {
    return filter === 'all' || isTerminal(run.state) === (filter === 'finished');
  }

  function dateLabel(iso: string): string {
    const time = Date.parse(iso);
    return Number.isFinite(time) ? dates.format(time) : '–';
  }
</script>

{#snippet history(runs: DeployRunSummary[])}
  {@const rows = runs.filter(matches)}
  {#if rows.length}
    <Table label={t('admin.deploys.history')} zebra class="deploy-history-table">
      <thead>
        <tr>
          <th class="run-name" scope="col">{t('admin.deploys.col.run')}</th>
          <th class="wide" scope="col">{t('admin.deploys.col.kind')}</th>
          <th class="run-state" scope="col">{t('admin.deploys.col.state')}</th>
          <th class="wide" scope="col">{t('admin.deploys.col.stage')}</th>
          <th class="wide" scope="col">{t('admin.deploys.col.by')}</th>
          <th class="wide" scope="col">{t('admin.deploys.col.started')}</th>
        </tr>
      </thead>
      <tbody>
        {#each rows as run (run.id)}
          {@const name = runName(run) || run.id}
          {@const href = `/deploys/${encodeURIComponent(run.id)}`}
          {@const stage = run.current_stage ? t(STAGE_KEY[run.current_stage]) : ''}
          {@const started = dateLabel(run.created_at)}
          <tr>
            <th scope="row" class="run-name">
              <Text as="span" mono truncate title={name}>
                <TextLink variant="inline" {href}>{name}</TextLink>
              </Text>
              <div class="narrow">
                <Text as="span" size="xs" tone="muted" truncate title={stage}>{t(KIND_KEY[run.kind])}{stage ? ` · ${stage}` : ''}</Text>
                <Text as="span" size="xs" tone="muted" truncate title={run.actor.login}>{run.actor.login} · <time datetime={run.created_at}>{started}</time></Text>
              </div>
            </th>
            <td class="wide">{t(KIND_KEY[run.kind])}</td>
            <td class="run-state">
              <StatePill tone={runPill(run.state)}>
                <span class="state-copy"><bb-fit><span data-fit>{t(RUN_STATE_KEY[run.state])}</span></bb-fit></span>
              </StatePill>
            </td>
            <td class="wide">{stage || '–'}</td>
            <td class="wide"><Text as="span" size="sm" mono tone="muted">{run.actor.login}</Text></td>
            <td class="wide" title={run.created_at}><time datetime={run.created_at}>{started}</time></td>
          </tr>
        {/each}
      </tbody>
    </Table>
  {:else if runs.length}
    <EmptyState title={t('admin.deploys.filterEmpty')} />
  {:else}
    <EmptyState title={t('admin.deploys.historyEmpty')} body={t('admin.deploys.historyEmptyBody')} />
  {/if}
{/snippet}

<Stack gap={3}>
  <FilterTabs options={filters} bind:value={filter} label={t('admin.deploys.filterLabel')} countWidth="5ch" />
  <Cluster justify="end">
    <div class="run-action">
      <ButtonLink block tone="success" disabled={pending} href={active ? `/deploys/${encodeURIComponent(active.id)}` : '/deploys/new'}>
        <span class="control-copy"><bb-fit><span data-fit>{t(active ? 'admin.deploys.openRun' : 'admin.deploys.start.cta')}</span></bb-fit></span>
      </ButtonLink>
    </div>
  </Cluster>
</Stack>

<Card>
  <CardHead title={t('admin.deploys.history')} headingLevel={2} />
  <div class="history">
    <Scroller fill aria-busy={pending} role="region" aria-label={t('admin.deploys.history')} tabindex={0}>
      {#if loaded?.runsError}
        <AlertBanner tone="danger">{t('admin.deploys.historyError', { error: loaded.runsError })}</AlertBanner>
      {:else if loaded}
        {@render history(loaded.runs)}
      {:else if pending}
        <SkeletonStack rows={4} height="2.25rem" />
      {:else}
        <AlertBanner tone="danger">{t('admin.deploys.historyUnavailable')}</AlertBanner>
      {/if}
    </Scroller>
  </div>
</Card>

<style>
  .run-action {
    inline-size: min(100%, 10rem);
  }

  .control-copy {
    display: block;
    max-inline-size: 7.5rem;
    overflow: hidden;
  }

  .history {
    display: flex;
    block-size: 14rem;
    container-type: inline-size;
    overflow-wrap: anywhere;
  }

  .run-name {
    max-inline-size: 12rem;
    overflow: hidden;
  }

  .narrow {
    display: none;
  }

  .state-copy {
    display: block;
    inline-size: 6.5rem;
  }

  @container (max-width: 41rem) {
    .history :global(.deploy-history-table) {
      table-layout: fixed;
    }

    .wide {
      display: none;
    }

    .narrow {
      display: grid;
      font-weight: 400;
    }

    .run-name {
      inline-size: auto;
    }

    .run-state {
      inline-size: 8.5rem;
    }
  }
</style>
