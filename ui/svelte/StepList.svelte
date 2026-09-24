<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

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
    value?: number | null;
    meta?: string;
    href?: string;
  };
</script>

<script lang="ts">
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
    detail?: Snippet<[StepItem]>;
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

  const TONE: Record<StepState, 'neutral' | 'success' | 'warning' | 'error'> = {
    pending: 'neutral',
    running: 'neutral',
    waiting: 'warning',
    succeeded: 'success',
    failed: 'error',
    skipped: 'neutral',
    cancelled: 'neutral',
  };

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
