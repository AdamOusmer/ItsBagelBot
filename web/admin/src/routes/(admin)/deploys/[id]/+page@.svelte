<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick, untrack } from 'svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StepList, { type StepItem } from '@bagel/ui/svelte/StepList.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { RunState, StageId } from '$lib/deploys/types';
  import StatePill from '$lib/components/StatePill.svelte';
  import { RunStream, type StreamConn } from '$lib/components/deploys/run-stream.svelte';
  import DeployScreen from '$lib/components/deploys/DeployScreen.svelte';
  import StageScene from '$lib/components/deploys/StageScene.svelte';
  import Mascot from '$lib/components/deploys/Mascot.svelte';
  import NowPanel from '$lib/components/deploys/NowPanel.svelte';
  import RunActions from '$lib/components/deploys/RunActions.svelte';
  import StageDetail from '$lib/components/deploys/StageDetail.svelte';
  import {
    KIND_KEY,
    RUN_STATE_KEY,
    STAGE_ABOUT_KEY,
    STAGE_KEY,
    STAGE_STATE_BODY_KEY,
    STEP_STATE_KEY,
    formatElapsed,
    isTerminal,
    liveStageId,
    runElapsed,
    runFraction,
    runName,
    runPill,
    stageMeta,
    stageTone,
    stageValue,
    type Tone
  } from '$lib/components/deploys/view';

  let { data } = $props();

  const { t } = getI18n();

  const stream = new RunStream(untrack(() => data.run));
  const run = $derived(stream.run);

  $effect(() => {
    const next = data.run;
    untrack(() => stream.accept(next));
  });

  const runId = $derived(run.id);
  $effect(() => stream.connect(`/deploys/${encodeURIComponent(runId)}/stream`));

  let now = $state(Date.now());
  $effect(() => {
    if (isTerminal(run.state)) return;
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  const CONN: Record<StreamConn, { key: string; tone: Tone }> = {
    live: { key: 'admin.deploys.streamLive', tone: 'success' },
    connecting: { key: 'admin.deploys.streamConnecting', tone: 'warning' },
    reconnecting: { key: 'admin.deploys.streamReconnecting', tone: 'warning' }
  };

  const FACE: Record<RunState, string> = {
    running: 'attentive',
    waiting: 'curious',
    succeeded: 'proud',
    failed: 'surprised',
    verify_failed: 'surprised',
    cancelled: 'sleepy'
  };

  const LIVE_PANEL: ReadonlySet<StageId> = new Set(['build', 'rollout']);

  const live = $derived(liveStageId(run));
  let pinned = $state<StageId | null>(null);
  const focusId = $derived(pinned ?? live);
  const focusIndex = $derived(Math.max(run.stages.findIndex((s) => s.id === focusId), 0));
  const stage = $derived(run.stages[focusIndex]);
  const fraction = $derived(runFraction(run));
  const showNow = $derived(Boolean(stage && LIVE_PANEL.has(stage.id) && (stage.items?.length ?? 0) > 0));
  const hasDetail = $derived(
    (stage?.items?.length ?? 0) > 0 || (stage?.links?.length ?? 0) > 0 || (stage?.failure?.log_tail?.length ?? 0) > 0
  );
  const stageOf = $derived(
    t('admin.deploys.run.stageOf', { n: String(focusIndex + 1), total: String(run.stages.length) })
  );

  const checklist = $derived<StepItem[]>(
    run.stages.map((s) => {
      const meta = stageMeta(s, t);
      return {
        id: s.id,
        label: t(STAGE_KEY[s.id]),
        meta: meta ? `${t(STEP_STATE_KEY[s.state])} · ${meta}` : t(STEP_STATE_KEY[s.state]),
        state: s.state,
        value: stageValue(s)
      };
    })
  );

  let dir = $state(1);
  let lastIndex = untrack(() => focusIndex);
  $effect.pre(() => {
    const i = focusIndex;
    untrack(() => {
      if (i !== lastIndex) dir = i > lastIndex ? 1 : -1;
      lastIndex = i;
    });
  });

  type Sequence = 'entrance' | 'burst' | 'orbit' | 'comet';
  let sequence = $state<Sequence>('entrance');
  let sequenceKey = $state(0);
  let lastState = untrack(() => run.state);
  $effect(() => {
    const state = run.state;
    void live;
    untrack(() => {
      sequence = state === 'succeeded' && lastState !== 'succeeded' ? 'burst' : 'entrance';
      sequenceKey += 1;
      lastState = state;
    });
  });

  let heading = $state<HTMLHeadingElement | null>(null);
  async function select(id: string) {
    pinned = id === live ? null : (id as StageId);
    await tick();
    heading?.focus({ preventScroll: true });
  }
</script>

<svelte:head>
  <title>{t(KIND_KEY[run.kind])} {runName(run) || run.id} · ItsBagelBot Admin</title>
</svelte:head>

<DeployScreen
  eyebrow={t('admin.deploys.runEyebrow')}
  name={t(KIND_KEY[run.kind])}
  em={runName(run) || run.id}
  closeHref="/deploys"
  closeLabel={t('admin.deploys.back')}
  progress={fraction}
  progressLabel={t('admin.deploys.overall')}
  count={stageOf}
  turn={focusIndex}
  leaving={run.state === 'succeeded'}
>
  {#snippet status()}
    <StatePill tone={runPill(run.state)}>{t(RUN_STATE_KEY[run.state])}</StatePill>
    <Text as="span" size="xs" mono>{t('admin.deploys.elapsed', { time: formatElapsed(runElapsed(run, now)) })}</Text>
    <Cluster as="span" gap={2} nowrap>
      <StatusDot tone={CONN[stream.conn].tone} />
      <Text as="span" size="xs" mono>{t(CONN[stream.conn].key)}</Text>
    </Cluster>
  {/snippet}

  {#snippet side()}
    <StepList steps={checklist} selected={focusId} label={t('admin.deploys.stages')} onselect={select} />
    <div class="by"><Text size="xs" tone="muted" mono>{t('admin.deploys.startedBy', { login: run.actor.login })}</Text></div>
  {/snippet}

  <div class="stage-row">
    <div class="scenes">
      {#if stage}
        {#key focusId}
          <StageScene
            {dir}
            title={t(STAGE_KEY[stage.id])}
            body={t(STAGE_ABOUT_KEY[stage.id])}
            headingId="run-stage-title"
            bind:heading
          >
            {#snippet kicker()}
              <span>{stageOf}</span>
              <span class="dot">·</span>
              <span class="state {stage.state}">{t(STEP_STATE_KEY[stage.state])}</span>
            {/snippet}
            {#snippet note()}
              <Stack gap={3}>
                <Cluster align="baseline" gap={3}>
                  <Text as="span" size="sm">{t(STAGE_STATE_BODY_KEY[stage.state])}</Text>
                  {#if stageMeta(stage, t)}<Text as="span" size="xs" tone="pale" mono>{stageMeta(stage, t)}</Text>{/if}
                </Cluster>
                {#if stage.progress.total > 0}
                  <ProgressBar
                    value={stageValue(stage) ?? 0}
                    tone={stageTone(stage.state)}
                    label={t(STAGE_KEY[stage.id])}
                  />
                {/if}
              </Stack>
            {/snippet}
            {#snippet children()}
              {#if hasDetail}
                <div class="detail"><Card glass><StageDetail {stage} /></Card></div>
              {/if}
            {/snippet}
          </StageScene>
        {/key}
      {/if}
    </div>

    <div class="aside">
      <div class="mascot-slot"><Mascot expression={FACE[run.state]} {sequence} {sequenceKey} /></div>
      {#if showNow && stage}<NowPanel {stage} />{/if}
    </div>
  </div>

  <div class="acts">
    <span class="follow" class:gone={pinned === null}>
      <Button
        variant="secondary"
        size="sm"
        tabindex={pinned === null ? -1 : undefined}
        aria-hidden={pinned === null}
        onclick={() => select(live)}
        tone="success"
      >
        {t('admin.deploys.run.follow')}
      </Button>
    </span>
    <div class="verbs"><RunActions {run} onrun={(next) => stream.accept(next)} /></div>
  </div>
</DeployScreen>

<style>
  .by {
    margin: var(--bb-space-2) var(--bb-space-3) var(--bb-space-1);
  }

  .stage-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 300px;
    gap: var(--gap);
    align-items: start;
  }
  .scenes {
    display: grid;
    min-width: 0;
  }
  .aside {
    display: grid;
    gap: var(--bb-space-4);
    min-width: 0;
  }

  .dot {
    color: rgba(var(--bb-white-pure-rgb), 0.5);
  }
  .state.running,
  .state.waiting {
    color: var(--bb-green-glow);
  }
  .state.failed {
    color: var(--bb-status-danger);
  }
  .state.pending,
  .state.skipped,
  .state.cancelled {
    color: rgba(var(--bb-white-pure-rgb), 0.6);
  }
  .detail {
    --card-pad: var(--bb-space-3) var(--bb-space-4);
  }

  .acts {
    position: sticky;
    bottom: 0;
    z-index: 2;
    display: flex;
    align-items: flex-start;
    gap: var(--bb-space-4);
    flex-wrap: wrap;
    margin-top: auto;
    padding: var(--bb-space-2) 0 var(--bb-space-1);
    background: linear-gradient(180deg, transparent, rgba(var(--bb-black-rgb), 0.78) 40%);
    backdrop-filter: blur(6px);
  }
  .verbs {
    flex: 1;
    min-width: 0;
  }
  .follow {
    display: inline-flex;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .follow.gone {
    visibility: hidden;
    opacity: 0;
  }

  @media (max-width: 1180px) {
    .stage-row {
      grid-template-columns: minmax(0, 1fr);
    }
    .mascot-slot {
      display: none;
    }
  }
</style>
