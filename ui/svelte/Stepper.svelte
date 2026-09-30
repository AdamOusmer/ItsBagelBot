<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';

  export type StepperStep = { label: string; detail?: string };
</script>

<script lang="ts">
  import '../styles/elements/stepper.css';
  import Icon from './Icon.svelte';
  import VisuallyHidden from './VisuallyHidden.svelte';
  import { getUiI18n } from './i18n';

  type Own = {
    steps: readonly StepperStep[];
    current: number;
    label: string;
    orientation?: 'horizontal' | 'vertical';
    maxStep?: number;
    compact?: boolean;
    onSelect?: (index: number) => void;
    class?: string;
  };

  let {
    steps,
    current,
    label,
    orientation = 'horizontal',
    maxStep = Number.POSITIVE_INFINITY,
    compact = false,
    onSelect,
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    [
      'bb-stepper',
      orientation === 'vertical' ? 'bb-stepper--vertical' : null,
      compact ? 'bb-stepper--compact' : null,
      className || null,
    ].filter(Boolean).join(' '),
  );

  function itemClass(index: number): string {
    if (index === current) return 'bb-stepper__item bb-stepper__item--current';
    return index < current ? 'bb-stepper__item bb-stepper__item--done' : 'bb-stepper__item';
  }

  const i18n = getUiI18n();
  function stateText(index: number): string {
    if (index === current) return i18n.t('steps.current');
    return i18n.t(index < current ? 'steps.completed' : 'steps.upcoming');
  }

  const ordinal = (index: number) => String(index + 1).padStart(2, '0');
  const currentOf = (index: number) => (index === current ? 'step' : undefined);
</script>

{#snippet marker(index: number)}
  <span class="bb-stepper__num" aria-hidden="true">{ordinal(index)}</span>
  <span class="bb-stepper__pip" aria-hidden="true"></span>
{/snippet}

{#if orientation === 'vertical'}
  <ol class={classes} aria-label={label} {...rest}>
    {#each steps as step, i (i)}
      <li class={itemClass(i)} aria-current={currentOf(i)}>
        <span class="bb-stepper__gutter" aria-hidden="true">
          <span class="bb-stepper__dot">{#if i < current}<Icon name="check" size={11} />{:else}{i + 1}{/if}</span>
          {#if i < steps.length - 1}<span class="bb-stepper__bar"></span>{/if}
        </span>
        <span class="bb-stepper__text">
          <span class="bb-stepper__title">{step.label}</span>
          <VisuallyHidden>{stateText(i)}</VisuallyHidden>
          {#if step.detail}<span class="bb-stepper__detail">{step.detail}</span>{/if}
        </span>
      </li>
    {/each}
  </ol>
{:else}
  <nav
    class={classes}
    aria-label={label}
    style="--stepper-i: {Math.max(current, 0)}; --stepper-n: {Math.max(steps.length, 1)};"
    {...rest}
  >
    <span class={current < 0 ? 'bb-stepper__track bb-stepper__track--idle' : 'bb-stepper__track'} aria-hidden="true"><span class="bb-stepper__trail"></span></span>
    <ol class="bb-stepper__list">
      {#each steps as step, i (i)}
        <li class={itemClass(i)}>
          {#if onSelect}
            <button
              type="button"
              class="bb-stepper__step"
              aria-current={currentOf(i)}
              disabled={i > maxStep}
              onclick={() => onSelect(i)}
            >{@render marker(i)}<VisuallyHidden>{step.label}, {stateText(i)}</VisuallyHidden></button>
          {:else}
            <span class="bb-stepper__step" aria-current={currentOf(i)}>{@render marker(i)}<VisuallyHidden>{step.label}, {stateText(i)}</VisuallyHidden></span>
          {/if}
        </li>
      {/each}
    </ol>
    <span class={current < 0 ? 'bb-stepper__glide bb-stepper__glide--hidden' : 'bb-stepper__glide'} aria-hidden="true"></span>
    {#if compact}<span class="bb-stepper__compact" aria-hidden="true">{steps[current]?.label ?? ''}</span>{/if}
  </nav>
{/if}
