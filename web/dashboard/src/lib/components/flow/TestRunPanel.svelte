<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import type { RehearsalOverrides, RehearsalScenario, RehearsalView } from '#lib/flow/rehearse';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import '@bagel/ui/styles/elements/collapse.css';
  import Icon from './FlowIcon.svelte';
  import ScenarioInputs from './ScenarioInputs.svelte';
  import RehearsalTimeline from './RehearsalTimeline.svelte';

  let { view, overrides, onScenario, onOverride, onSelect, onClose }: {
    view: RehearsalView;
    overrides: RehearsalOverrides;
    onScenario: (patch: Partial<RehearsalScenario>) => void;
    onOverride: (key: string, value: string) => void;
    onSelect: (id: string, fieldKey?: string) => void;
    onClose: () => void;
  } = $props();
  const { t } = getI18n();
  const uid = $props.id();
  let rejectedStart = $state(false);
  function makeStart() {
    if (!view.startPatch) return;
    rejectedStart = !view.verifyRepair?.(view.startPatch);
    if (!rejectedStart) onScenario(view.startPatch);
  }
  const stoppedStep = $derived(view.steps.find((step) => step.id === view.stoppedAt)?.label ?? '');
  const headline = $derived(view.issue?.text ?? {
    not_started: t('flows.test.wouldNotStart', { reason: view.blocked ?? '' }),
    started: t('flows.test.started'),
    stopped: t('flows.test.stoppedAt', { step: stoppedStep })
  }[view.state]);
  const canStart = $derived(!view.issue && view.state === 'not_started' && Boolean(view.startPatch));
  const issueNode = $derived(view.issue?.node ?? null);
  const stoppedNode = $derived(!view.issue && view.state === 'stopped' ? view.stoppedAt ?? null : null);
  const hasTrigger = $derived(view.steps.some((step) => step.kind === 'trigger'));

  const actionOpen = $derived(Boolean(issueNode || stoppedNode));
  const noticeOpen = $derived(Boolean(view.definitionNotice));
  let shownFix = $state(false);
  let shownNotice = $state('');
  $effect(() => {
    if (issueNode) shownFix = true;
    else if (stoppedNode) shownFix = false;
  });
  $effect(() => {
    if (view.definitionNotice) shownNotice = view.definitionNotice;
  });

  function showStep() {
    if (issueNode) onSelect(issueNode, view.issue?.fieldKey);
    else if (stoppedNode) onSelect(stoppedNode);
  }
</script>

{#snippet event()}
  <div class="event-body">
    <ScenarioInputs fields={view.fields} scenario={view.scenario} onChange={onScenario} />
    <div class="bb-collapse" class:is-open={canStart} aria-hidden={!canStart} inert={!canStart}>
      <div>
        <div class="start">
          <Button variant="secondary" size="sm" disabled={!canStart} onclick={makeStart}><bb-fit>{t('flows.test.makeStart')}</bb-fit></Button>
          <Text as="span" size="xs" tone="warning" class="start-rejected" role="status">{rejectedStart ? t('flows.test.repairBlocked') : ''}</Text>
        </div>
      </div>
    </div>
  </div>
{/snippet}

<aside class="panel" aria-labelledby="{uid}-title">
  <header class="head">
    <IconTile style="--tile: var(--bb-space-6)"><Icon name="start" size={16} /></IconTile>
    <Heading level={2} variant="card" class="run-heading bb-fit-box" id="{uid}-title"><bb-fit>{t('flows.board.testRun')}</bb-fit></Heading>
    <IconButton label={t('flows.board.closeTestRun')} size="sm" onclick={onClose}><Icon name="x" size={15} /></IconButton>
  </header>
  <div class="scroll">
    <div class="intro">
      <Text size="sm" class="run-headline bb-fit-box" role="status" aria-live="polite"><bb-fit mode="block">{headline}</bb-fit></Text>
      <div class="bb-collapse intro-collapse" class:is-open={actionOpen} aria-hidden={!actionOpen} inert={!actionOpen}>
        <div>
          <Button variant="ghost" size="sm" onclick={showStep}><bb-fit>{t(shownFix ? 'flows.test.fixStep' : 'flows.test.showStep')}</bb-fit></Button>
        </div>
      </div>
      <Text size="xs" tone="muted" class="run-pretend bb-fit-box"><bb-fit mode="block">{t('flows.test.pretend')}</bb-fit></Text>
      <div class="bb-collapse intro-collapse" class:is-open={noticeOpen} aria-hidden={!noticeOpen} inert={!noticeOpen}>
        <div>
          <Text size="xs" tone="warning" class="run-definition bb-fit-box" role="status"><bb-fit mode="block">{shownNotice}</bb-fit></Text>
        </div>
      </div>
    </div>
    {#if !hasTrigger}
      <Card class="event-card">
        {#if view.fields.length}
          {@render event()}
        {:else}
          <Text size="sm" tone="muted-light" class="event-empty bb-fit-box"><bb-fit mode="block">{t('flows.test.noTrigger')}</bb-fit></Text>
        {/if}
      </Card>
    {/if}
    <RehearsalTimeline steps={view.steps} scenario={view.scenario} summary={view.summary} verifyRepair={view.verifyRepair} {overrides} {onScenario} {onOverride} {onSelect} {event} />
  </div>
</aside>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    border-radius: var(--bb-radius-md);
    background: var(--bb-bg-1);
    box-shadow: var(--bb-depth-1), 0 16px 48px rgba(var(--bb-shadow-rgb), 0.55);
    --panel-pad: var(--bb-space-5);
  }
  .head {
    display: flex;
    flex: none;
    align-items: center;
    gap: var(--bb-space-3);
    padding: var(--bb-space-4) var(--bb-space-3) var(--bb-space-4) var(--panel-pad);
    border-block-end: 1px solid var(--bb-rule);
  }
  .head :global(.run-heading) {
    flex: 1;
    min-width: 0;
    height: var(--bb-space-5);
    margin: 0;
    font: 600 var(--bb-text-md)/1.25 var(--bb-font-body);
    letter-spacing: normal;
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    scrollbar-width: thin;
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-4);
    padding: var(--bb-space-4) var(--panel-pad) var(--bb-space-5);
  }
  .intro {
    display: flex;
    flex: none;
    flex-direction: column;
    gap: var(--bb-space-1);
  }
  .intro-collapse { --bb-collapse-gap: var(--bb-space-1); }
  .intro :global(.run-headline) {
    height: var(--bb-space-6);
    margin: 0;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .intro :global(.run-pretend) { height: var(--bb-space-5); margin: 0; }
  .intro :global(.run-definition) { height: var(--bb-space-5); margin: 0; }
  .scroll :global(.event-card) {
    --card-pad: var(--bb-space-4);
    --card-bg: var(--bb-elev-2);
    display: flex;
    flex: none;
    flex-direction: column;
    gap: var(--bb-space-3);
  }
  .scroll :global(.event-empty) {
    height: calc(var(--bb-space-5) + var(--bb-space-4));
    margin: 0;
  }
  .event-body {
    --bb-collapse-gap: var(--bb-space-3);
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-3);
  }
  .start {
    display: flex;
    align-items: center;
    gap: var(--bb-space-1);
  }
  .start :global(.start-rejected) { flex: 1; min-width: 0; }
  @media (max-width: 760px) {
    .panel { --panel-pad: var(--bb-space-4); }
  }
</style>
