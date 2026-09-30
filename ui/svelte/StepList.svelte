<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import type { ProgressTone } from '../lib/tone';

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
    disabled?: boolean;
  };
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';
  import { prefersReducedMotion } from '../lib/motion-query';
  import Icon from './Icon.svelte';
  import ProgressBar from './ProgressBar.svelte';
  import { getUiI18n } from './i18n';
  import '../styles/tags.css';
  import '../styles/elements/step-list.css';

  type Own = {
    steps: StepItem[];
    detail?: Snippet<[StepItem]>;
    stateLabels?: Partial<Record<StepState, string>>;
    selected?: string;
    onselect?: (id: string) => void;
    label?: string;
    class?: string;
  };

  let {
    steps,
    detail,
    stateLabels = {},
    selected,
    onselect,
    label,
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const i18n = getUiI18n();
  const DEFAULT_LABELS: Record<StepState, string> = {
    pending: i18n.t('steps.pending'),
    running: i18n.t('steps.running'),
    waiting: i18n.t('steps.waiting'),
    succeeded: i18n.t('steps.succeeded'),
    failed: i18n.t('steps.failed'),
    skipped: i18n.t('steps.skipped'),
    cancelled: i18n.t('steps.cancelled'),
  };

  const TONE: Record<StepState, ProgressTone> = {
    pending: 'neutral',
    running: 'neutral',
    waiting: 'warning',
    succeeded: 'success',
    failed: 'danger',
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

  const ICON: Partial<Record<StepState, 'check' | 'x'>> = {
    succeeded: 'check',
    failed: 'x',
    cancelled: 'x',
  };

  const labels = $derived({ ...DEFAULT_LABELS, ...stateLabels });

  const classes = $derived(['bb-steps', className || null].filter(Boolean).join(' '));
  const navClasses = $derived(['bb-steps', 'bb-steps--nav', className || null].filter(Boolean).join(' '));
  const at = $derived(Math.max(steps.findIndex((s) => s.id === selected), 0));

  let nav = $state<HTMLElement | null>(null);
  let placed = false;

  function revealRow(list: HTMLElement, row: HTMLElement, behavior: ScrollBehavior) {
    if (list.scrollWidth > list.clientWidth) return list.scrollTo({ left: row.offsetLeft - 12, behavior });
    const box = list.parentElement;
    if (box && box.scrollHeight > box.clientHeight) {
      box.scrollTo({ top: list.offsetTop + row.offsetTop - box.clientHeight / 2, behavior });
    }
  }

  $effect(() => {
    void at;
    const row = nav?.querySelector<HTMLElement>('.bb-step__row[aria-current="step"]');
    if (!nav || !row) return;
    const behavior = placed && !prefersReducedMotion() ? 'smooth' : 'instant';
    placed = true;
    revealRow(nav, row, behavior);
  });
</script>

{#if onselect}
  <nav
    class={navClasses}
    aria-label={label}
    style="--steps-at: {at}; --steps-n: {steps.length};"
    bind:this={nav}
    {...rest}
  >
    <span class="bb-steps__glide" aria-hidden="true"></span>
    <ol class="bb-steps__list">
      {#each steps as step, i (step.id)}
        {@const icon = ICON[step.state]}
        <li class="bb-step bb-step--{step.state}">
          <button
            type="button"
            class="bb-step__row"
            aria-current={step.id === selected ? 'step' : undefined}
            disabled={step.disabled}
            onclick={() => onselect(step.id)}
          >
            <span class="bb-step__mark" aria-hidden="true">
              {#if icon}<Icon name={icon} size={12} />{:else}<span class="bb-step__num">{String(i + 1).padStart(2, '0')}</span>{/if}
            </span>
            <span class="bb-step__text">
              <span class="bb-step__label">{step.label}</span>
              <span class="bb-step__meta">{step.meta ?? ''}</span>
              <span class="bb-step__track" aria-hidden="true">
                <span class={step.value === null ? 'bb-step__fill bb-step__fill--indeterminate' : 'bb-step__fill'} style="--step-value: {step.value ?? 0};"></span>
              </span>
            </span>
          </button>
        </li>
      {/each}
    </ol>
  </nav>
{:else}
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
{/if}
