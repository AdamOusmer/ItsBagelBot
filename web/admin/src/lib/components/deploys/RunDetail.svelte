<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick, untrack } from 'svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import StepList from '@bagel/ui/svelte/StepList.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployRun, StageId } from '#lib/deploys/types';
  import RunActions from './RunActions.svelte';
  import RunStage from './RunStage.svelte';
  import RunStatus from './RunStatus.svelte';
  import { RunStream } from './run-stream.svelte';
  import { KIND_KEY, STAGE_KEY, STEP_STATE_KEY, failedStage, isTerminal, liveStageId, runFraction, runName, stageMeta, stageValue } from './view';

  let { initial }: { initial: DeployRun } = $props();
  const { t, locale } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const percent = new Intl.NumberFormat(locale, { style: 'percent' });
  const stream = new RunStream(untrack(() => initial));

  const run = $derived(stream.run);
  const runId = $derived(initial.id);
  const name = $derived(runName(run) || runId);
  const live = $derived(liveStageId(run));
  const failure = $derived(run.failure ?? failedStage(run)?.failure);
  const cancelling = $derived(run.cancel_requested && !isTerminal(run.state));
  const progress = $derived(runFraction(run));

  let pinned = $state<StageId | null>(null);
  let heading = $state<HTMLElement | null>(null);
  const selectedIndex = $derived(Math.max(0, run.stages.findIndex((stage) => stage.id === (pinned ?? live))));
  const selected = $derived(run.stages[selectedIndex]);
  const notice = $derived(failure?.message ?? (cancelling ? t('admin.deploys.cancelRequested') : ''));
  const stageCount = $derived(t('admin.deploys.run.stageOf', {
    n: number.format(selectedIndex + 1),
    total: number.format(run.stages.length)
  }));

  const steps = $derived(run.stages.map((stage) => {
    const stateLabel = t(STEP_STATE_KEY[stage.state]);
    const detail = stageMeta(stage, t);
    return {
      id: stage.id,
      state: stage.state,
      label: t(STAGE_KEY[stage.id]),
      meta: detail ? `${stateLabel} · ${detail}` : stateLabel,
      value: stageValue(stage)
    };
  }));
  const stateLabels = Object.fromEntries(
    Object.keys(STEP_STATE_KEY).map((state) => [state, ''])
  );
  const options = $derived(steps.map((step) => ({
    value: step.id,
    label: step.label,
    description: t(STEP_STATE_KEY[step.state])
  })));

  $effect(() => {
    const snapshot = initial;
    untrack(() => stream.accept(snapshot));
  });
  $effect(() => stream.connect(`/deploys/${encodeURIComponent(runId)}/stream`));

  async function select(id: string, moveFocus = false) {
    pinned = id === live ? null : (id as StageId);
    if (!moveFocus) return;
    await tick();
    heading?.focus({ preventScroll: true });
  }
</script>

<svelte:head>
  <title>{t(KIND_KEY[run.kind])} {name} · ItsBagelBot Admin</title>
</svelte:head>

<section class="screen active">
  <PageHead compact eyebrow={t('admin.deploys.runEyebrow')} description={t('admin.deploys.startedBy', { login: run.actor.login })}>
    <span class="run-title">{t(KIND_KEY[run.kind])} <em>{name}</em></span>
  </PageHead>

  <PageToolbar class="detail-toolbar">
    {#snippet leading()}
      <RunStatus {run} conn={stream.conn} />
    {/snippet}
    {#snippet trailing()}
      <div class="toolbar-actions">
        <RunActions {run} onrun={(next) => stream.accept(next)} />
        <ButtonLink href="/deploys" variant="secondary" size="sm" block>
          <bb-fit><span data-fit>{t('admin.deploys.back')}</span></bb-fit>
        </ButtonLink>
      </div>
    {/snippet}
  </PageToolbar>

  <div class="run-notice" class:unavailable={!notice} inert={!notice}>
    <Scroller maxHeight="3.375rem" role="region" aria-label={t('admin.deploys.runEyebrow')} tabindex={notice ? 0 : undefined}>
      <AlertBanner tone={failure ? 'danger' : 'warning'} role={failure ? 'alert' : 'status'} flush>{notice}</AlertBanner>
    </Scroller>
  </div>

  {#if selected}
    <div class="run-workspace">
      <Card as="aside" aria-label={t('admin.deploys.stages')}>
        <div class="stage-heading">
          <CardHead title={t('admin.deploys.stages')} headingLevel={2} />
        </div>
        <div class="run-progress">
          <Cluster justify="between" nowrap>
            <Text as="span" size="xs" tone="muted">
              <bb-fit><span data-fit>{t('admin.deploys.overall')}</span></bb-fit>
            </Text>
            <Text as="span" size="xs" mono>{percent.format(progress)}</Text>
          </Cluster>
          <ProgressBar value={progress} tone="success" label={t('admin.deploys.overall')} size="sm" />
        </div>
        <div class="stage-list">
          <StepList {steps} {stateLabels} selected={selected.id} label={t('admin.deploys.stages')} onSelect={(id) => select(id, true)} />
        </div>
        <div class="stage-picker">
          <Label htmlFor="deploy-stage-picker"><bb-fit><span data-fit>{t('admin.deploys.stages')}</span></bb-fit></Label>
          <Select id="deploy-stage-picker" label={t('admin.deploys.stages')} value={selected.id} {options} fill onchange={(event) => select(event.currentTarget.value)} />
        </div>
        <div class="follow" class:unavailable={pinned === null} inert={pinned === null}>
          <Button variant="secondary" size="sm" block disabled={pinned === null} onclick={() => select(live, true)}>
            <bb-fit><span data-fit>{t('admin.deploys.run.follow')}</span></bb-fit>
          </Button>
        </div>
      </Card>
      <RunStage stage={selected} count={stageCount} bind:heading />
    </div>
  {:else}
    <Card><EmptyState title={t('admin.deploys.run.noStages')} body={t('admin.deploys.run.noStagesBody')} /></Card>
  {/if}
</section>

<style>
  .run-title {
    overflow-wrap: anywhere;
  }
  .run-notice {
    min-block-size: 3.375rem;
    margin-block-end: var(--bb-space-2);
    overflow-wrap: anywhere;
  }
  .toolbar-actions {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 6.75rem;
    align-items: center;
    gap: var(--bb-space-2);
    inline-size: min(100%, 29rem);
    min-inline-size: 0;
  }
  .run-workspace {
    display: grid;
    grid-template-columns: minmax(0, 15rem) minmax(0, 1fr);
    gap: var(--bb-space-4);
    align-items: start;
  }
  .stage-picker {
    display: none;
  }
  .stage-list {
    max-height: max(11rem, calc(100svh - 29rem));
    overflow-y: auto;
    scrollbar-width: thin;
  }
  .run-progress {
    display: grid;
    gap: var(--bb-space-2);
    margin-block-end: var(--bb-space-3);
  }
  .follow {
    margin-block-start: var(--bb-space-3);
  }
  .follow,
  .run-notice {
    transition: opacity var(--bb-dur-base) ease;
  }
  .unavailable {
    opacity: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .follow,
    .run-notice {
      transition: none;
    }
  }

  @media (max-width: 960px) {
    .run-workspace {
      grid-template-columns: minmax(0, 1fr);
    }
    .stage-heading,
    .stage-list {
      display: none;
    }
    .stage-picker {
      display: grid;
      gap: var(--bb-space-2);
      min-width: 0;
    }
    .follow {
      max-width: 10rem;
    }
  }

  @media (max-width: 880px) {
    .screen :global(.detail-toolbar) {
      display: block;
    }
    .toolbar-actions {
      margin-block-start: var(--bb-space-2);
    }
  }

  @media (max-width: 520px) {
    .toolbar-actions {
      grid-template-columns: minmax(0, 1fr) 5.75rem;
    }
  }
</style>
