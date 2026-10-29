<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/setup-stage.css';
  import LightField from './LightField.svelte';
  import Stepper, { type StepperStep } from './Stepper.svelte';

  type Own = {
    label: string;
    steps?: readonly StepperStep[];
    current?: number;
    maxStep?: number;
    stepLabel?: string;
    onSelect?: (index: number) => void;
    wide?: boolean;
    full?: boolean;
    fill?: boolean;
    hero?: boolean;
    leaving?: boolean;
    class?: string;
    start?: Snippet;
    trail?: Snippet;
    art?: Snippet;
    aside?: Snippet;
    foot?: Snippet;
    end?: Snippet;
    children: Snippet;
  };

  let {
    label,
    steps = [],
    current = 0,
    maxStep,
    stepLabel = '',
    onSelect,
    wide = false,
    full = false,
    fill = false,
    hero = false,
    leaving = false,
    class: className = '',
    start,
    trail,
    art,
    aside,
    foot,
    end,
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLDivElement>, keyof Own> = $props();

  const classes = $derived(
    [
      'bb-setup-stage',
      wide && 'bb-setup-stage--wide',
      full && 'bb-setup-stage--full',
      fill && 'bb-setup-stage--fill',
      hero && 'bb-setup-stage--hero',
      leaving && 'bb-setup-stage--leaving',
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} role="region" aria-label={label} {...rest}>
  <div class="bb-setup-stage__backdrop" aria-hidden="true"><LightField /></div>
  <header class="bb-setup-stage__top">
    <div class="bb-setup-stage__start">{@render start?.()}</div>
    {#if steps.length > 0}
      <Stepper compact {steps} {current} {maxStep} label={stepLabel} {onSelect} />
    {/if}
    {#if trail}<div class="bb-setup-stage__trail">{@render trail()}</div>{/if}
  </header>
  <div class="bb-setup-stage__main">
    {#if art}
      <div class="bb-setup-stage__art" aria-hidden="true"><div class="bb-setup-stage__mascot">{@render art()}</div></div>
    {/if}
    <div class="bb-setup-stage__content">{@render children()}</div>
  </div>
  {#if foot || aside || end}
    <footer class="bb-setup-stage__foot">
      <div class="bb-setup-stage__foot-start">{@render foot?.()}</div>
      <div class="bb-setup-stage__aside">{@render aside?.()}</div>
      <div class="bb-setup-stage__foot-end">{@render end?.()}</div>
    </footer>
  {/if}
</div>
