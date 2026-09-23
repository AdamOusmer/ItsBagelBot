<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // One deploy run, live. The load's snapshot renders first; ./stream then
  // sends every state change as a full Run, and RunStream keeps the newest
  // by seq. A verb's reply (resume, cancel, approve) is also a full Run and
  // goes through the same seq check, so whichever of the two lands last
  // cannot roll the page back.
  import { untrack } from 'svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import StepList from '@bagel/ui/svelte/StepList.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { StageState } from '$lib/deploys/types';
  import { RunStream } from '$lib/components/deploys/run-stream.svelte';
  import RunHeader from '$lib/components/deploys/RunHeader.svelte';
  import RunActions from '$lib/components/deploys/RunActions.svelte';
  import StageDetail from '$lib/components/deploys/StageDetail.svelte';
  import {
    KIND_KEY,
    STAGE_KEY,
    STEP_STATE_KEY,
    isTerminal,
    runName,
    stageMeta,
    stageValue
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

  // The clock only ticks while the run moves; a finished run's elapsed time
  // is fixed at its last update.
  let now = $state(Date.now());
  $effect(() => {
    if (isTerminal(run.state)) return;
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  const steps = $derived(
    run.stages.map((s) => ({
      id: s.id,
      label: t(STAGE_KEY[s.id]),
      state: s.state,
      value: stageValue(s),
      meta: stageMeta(s, t)
    }))
  );

  const stateLabels = Object.fromEntries(
    Object.entries(STEP_STATE_KEY).map(([state, key]) => [state, t(key)])
  ) as Record<StageState, string>;
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.deploys.runEyebrow')}>
    {#snippet trail()}
      <TextLink href="/deploys" label={t('admin.deploys.back')} />
    {/snippet}
    {t(KIND_KEY[run.kind])} <em>{runName(run) || run.id}</em>
  </PageHead>

  <RunHeader {run} conn={stream.conn} {now} />
  <RunActions {run} onrun={(next) => stream.accept(next)} />

  <Card>
    <CardHead title={t('admin.deploys.stages')} />
    <StepList {steps} {stateLabels}>
      {#snippet detail(step)}
        {@const stage = run.stages.find((s) => s.id === step.id)}
        {#if stage}<StageDetail {stage} />{/if}
      {/snippet}
    </StepList>
  </Card>
</section>
