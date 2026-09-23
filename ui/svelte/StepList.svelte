<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The seven states are the deploy run's StageState vocabulary
  // (internal/domain/rpc/deploy), and nothing in them is deploy-specific:
  // any ordered pipeline a page wants to show reads the same way.
  export type StepState =
    | 'pending'
    | 'running'
    | 'waiting'
    | 'succeeded'
    | 'failed'
    | 'skipped'
    | 'cancelled';

  export type StepItem = {
    id: string;
    label: string;
    state: StepState;
    /** 0..1 draws a bar, null an indeterminate one, undefined an empty slot. */
    value?: number | null;
    /** One line under the label: a count, a "queued behind" note. */
    meta?: string;
    href?: string;
  };
</script>

<script lang="ts">
  // Svelte adapter for the `.bb-steps` contract
  // (../styles/elements/step-list.css), composing ProgressBar and the tag
  // vocabulary's mark and sweep (../styles/tags.css). Svelte only: see
  // SINGLE_ADAPTER_REASON in ../scripts/gen-catalog.mjs.
  //
  // An <ol> because the order IS the information: stages run strictly in
  // sequence, and a screen reader announcing "3 of 11" is the text equivalent
  // of seeing where the run has got to. The running row is aria-current="step"
  // for the same reason.
  //
  // The state word is visible text in every row, not a screen-reader-only
  // span. Colour and mark are decoration on it. EVERY STATE WORD IS A PROP
  // (stateLabels): the console localises and this package holds no copy. The
  // defaults are English so an unlocalised surface still reads.
  import type { Snippet } from 'svelte';
  import ProgressBar from './ProgressBar.svelte';
  import '../styles/tags.css';
  import '../styles/elements/step-list.css';

  let {
    steps,
    detail,
    stateLabels = {},
    class: className = '',
    ...rest
  }: {
    steps: StepItem[];
    /** Rendered under each row, indented to the label: a log tail, a breakdown. */
    detail?: Snippet<[StepItem]>;
    /** State words, merged over the English defaults. */
    stateLabels?: Partial<Record<StepState, string>>;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const DEFAULT_LABELS: Record<StepState, string> = {
    pending: 'Pending',
    running: 'Running',
    waiting: 'Waiting',
    succeeded: 'Succeeded',
    failed: 'Failed',
    skipped: 'Skipped',
    cancelled: 'Cancelled',
  };

  // The bar's tone per state, on the status-tone.ts vocabulary. Running is
  // neutral: a stage in motion has no verdict yet.
  const TONE: Record<StepState, 'neutral' | 'success' | 'warning' | 'error'> = {
    pending: 'neutral',
    running: 'neutral',
    waiting: 'warning',
    succeeded: 'success',
    failed: 'error',
    skipped: 'neutral',
    cancelled: 'neutral',
  };

  // The rotated-square mark family. Solid for a state with a result or in
  // progress, hollow for not-started or stopped, the dash for a stage that
  // is holding (waiting) or was never needed (skipped).
  const MARK: Record<StepState, string> = {
    pending: 'bb-mark bb-mark--hollow',
    running: 'bb-mark',
    waiting: 'bb-mark bb-mark--dash',
    succeeded: 'bb-mark',
    failed: 'bb-mark',
    skipped: 'bb-mark bb-mark--dash',
    cancelled: 'bb-mark bb-mark--hollow',
  };

  const labels = $derived({ ...DEFAULT_LABELS, ...stateLabels });

  const classes = $derived(['bb-steps', className || null].filter(Boolean).join(' '));
</script>

<ol class={classes} {...rest}>
  {#each steps as step (step.id)}
    <li
      class="bb-step bb-step--{step.state}"
      aria-current={step.state === 'running' ? 'step' : undefined}
    >
      <div class="bb-step__row">
        <i class={MARK[step.state]} aria-hidden="true"></i>
        <span class="bb-step__text">
          {#if step.href}
            <a class="bb-step__label" href={step.href}>{step.label}</a>
          {:else}
            <span class="bb-step__label">{step.label}</span>
          {/if}
          {#if step.meta}<span class="bb-step__meta">{step.meta}</span>{/if}
        </span>
        <div class="bb-step__bar">
          {#if step.value !== undefined}
            <ProgressBar value={step.value} tone={TONE[step.state]} label={step.label} size="sm" />
          {/if}
        </div>
        <span class="bb-step__state">{labels[step.state]}</span>
        {#if step.state === 'running'}<i class="bb-sweep" aria-hidden="true"></i>{/if}
      </div>
      {#if detail}<div class="bb-step__detail">{@render detail(step)}</div>{/if}
    </li>
  {/each}
</ol>
