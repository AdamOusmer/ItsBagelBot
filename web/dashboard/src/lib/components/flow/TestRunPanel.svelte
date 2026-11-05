<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onDestroy, untrack } from 'svelte';
  import { page } from '$app/state';
  import { getI18n } from '@bagel/kit';
  import type { Seg } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Icon from './FlowIcon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import RehearsedSegments from '#lib/components/commands/RehearsedSegments.svelte';
  import type { Flow } from '#lib/flow/model';
  import { rehearseFlow, type RehearsalStep } from '#lib/flow/rehearse';
  import FlowRehearsal from './FlowRehearsal.svelte';
  import type { FlowTrace } from './FlowCanvas.svelte';

  const STEP_MS = 650;

  let { flow, onClose, onTrace }: { flow: Flow; onClose: () => void; onTrace: (trace: FlowTrace | null) => void } = $props();

  const { t } = getI18n();

  let decisions = $state<Record<string, string>>({});
  let shown = $state(0);
  let timer: ReturnType<typeof setTimeout> | undefined;

  const moduleFlags = $derived((page.data.moduleFlags as Record<string, boolean> | undefined) ?? {});
  const steps = $derived(rehearseFlow(flow, t, { decisions, modules: moduleFlags }));
  const unknownTitle = $derived(t('chatPreview.unknownVar'));
  const reached = $derived(steps.slice(0, shown));
  const finished = $derived(shown >= steps.length);
  const pathKey = $derived(steps.map((s) => `${s.id}:${s.kind === 'check' ? s.taken : ''}`).join());
  const status = $derived.by(() => {
    if (steps.length === 0) return t('flows.board.statusEmpty');
    if (!finished) return t('flows.board.statusRunning', { shown, total: steps.length });
    return steps.length === 1 ? t('flows.board.statusDoneOne') : t('flows.board.statusDoneMany', { count: steps.length });
  });

  function advance() {
    if (shown >= steps.length) return;
    shown += 1;
    timer = setTimeout(advance, STEP_MS);
  }

  function run() {
    clearTimeout(timer);
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      shown = steps.length;
      return;
    }
    shown = 0;
    advance();
  }

  $effect(() => {
    void pathKey;
    untrack(run);
  });

  $effect(() => {
    onTrace({
      nodes: new Set(reached.map((s) => s.id)),
      edges: new Set(reached.flatMap((s) => (s.via ? [s.via] : []))),
      current: finished ? null : (reached.at(-1)?.id ?? null)
    });
  });

  onDestroy(() => {
    clearTimeout(timer);
    onTrace(null);
  });

  function detail(step: RehearsalStep): Seg[] {
    if (step.kind === 'viewer') return [{ text: t('flows.board.viewerTyped', { name: step.name, text: step.text }), kind: 'plain' }];
    if (step.kind === 'bot') return step.lines[0]?.segments ?? [{ text: t('flows.board.nothingToSend'), kind: 'plain' }];
    return step.segments;
  }
</script>

<aside class="panel" aria-label={t('flows.board.testRun')}>
  <header class="head">
    <div class="titles">
      <h2 class="name">{t('flows.board.testRun')}</h2>
      <p class="sentence" aria-live="polite">{status}</p>
    </div>
    <IconButton label={t('flows.board.closeTestRun')} size="sm" onclick={onClose}><Icon name="x" size={14} /></IconButton>
  </header>

  <div class="scroll">
    <ol class="path">
      {#each steps as step, i (step.id)}
        <li class="step" data-state={i < shown - 1 || finished ? 'done' : i === shown - 1 ? 'running' : 'waiting'}>
          <IconTile size="sm" style="--tile: 32px"><Icon name={step.icon} size={16} /></IconTile>
          <div class="copy">
            <span class="step-name">{step.label}</span>
            <span class="bb-chat__msg step-detail"><RehearsedSegments segments={detail(step)} {unknownTitle} /></span>
            {#if step.kind === 'check'}
              <div class="branch">
                <span class="branch-label">{t('flows.board.goesTo')}</span>
                <div class="choices" role="radiogroup" aria-label={t('flows.board.branchFor', { label: step.label })}>
                  {#each step.choices as choice (choice.id)}
                    <button
                      class="choice"
                      type="button"
                      role="radio"
                      aria-checked={step.taken === choice.id}
                      onclick={() => (decisions = { ...decisions, [step.id]: choice.id })}
                    >
                      {choice.label}
                    </button>
                  {/each}
                </div>
              </div>
            {/if}
          </div>
        </li>
      {/each}
    </ol>

    {#if steps.length}
      <div class="actions">
        <Button variant="ghost" onclick={run}><Icon name="play" size={14} /> {t('flows.board.runAgain')}</Button>
      </div>
      <FlowRehearsal steps={reached} title={t('flows.board.testChat')} />
    {/if}
  </div>
</aside>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    border-radius: 12px;
    background: var(--bb-bg-1);
    box-shadow: 0 16px 48px rgba(var(--bb-shadow-rgb), 0.65);
  }
  .head {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    flex: none;
    padding: 16px 12px 8px 16px;
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  .name {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 17px;
    font-weight: 600;
    line-height: 1.3;
    color: var(--bb-white);
  }
  .sentence {
    margin: 2px 0 0;
    min-height: 2.8em;
    font-size: 14px;
    line-height: 1.4;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 8px 16px 20px;
  }
  .scroll > :global(*) {
    flex: none;
  }
  .path {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }
  .step {
    position: relative;
    display: flex;
    gap: 12px;
    padding-bottom: 16px;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .step:not(:last-child)::before {
    content: '';
    position: absolute;
    left: 15px;
    top: 36px;
    bottom: 4px;
    width: 2px;
    border-radius: var(--bb-radius-pill);
    background: rgba(var(--bb-white-rgb), 0.12);
  }
  .step[data-state='done']:not(:last-child)::before {
    background: var(--bb-green-glow);
  }
  .step[data-state='waiting'] {
    opacity: 0.35;
  }
  .step[data-state='running'] :global(.bb-icon-tile),
  .step[data-state='running'] .step-name {
    color: var(--bb-green-glow);
  }
  .copy {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .step-name {
    font-family: var(--bb-font-body);
    font-size: 15px;
    font-weight: 600;
    line-height: 20px;
    color: var(--bb-white);
  }
  .step-detail {
    font-size: 14px;
    line-height: 1.4;
    color: rgba(var(--bb-white-rgb), 0.92);
    overflow-wrap: anywhere;
  }
  .branch {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 8px;
  }
  .branch-label {
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .choices {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .choice {
    min-height: 36px;
    padding: 6px 12px;
    border: 0;
    border-radius: var(--bb-radius-sm);
    background: rgba(var(--bb-white-rgb), 0.07);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: background var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .choice:hover {
    background: rgba(var(--bb-white-rgb), 0.12);
  }
  .choice[aria-checked='true'] {
    background: var(--bb-green-glow);
    color: var(--bb-black);
    font-weight: 600;
  }
  .choice:focus-visible {
    outline: 2px solid var(--bb-white);
    outline-offset: 2px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
  }
  @media (pointer: coarse) {
    .choice {
      min-height: 44px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .step,
    .choice {
      transition: none;
    }
  }
</style>
