<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import { onMount } from 'svelte';
  import { mountSlider, sliderFraction, sliderLabelIds } from '../lib/slider';
  import '../styles/elements/slider.css';

  type Own = {
    value?: number;
    min?: number;
    max?: number;
    step?: number;
    disabled?: boolean;
    name?: string;
    form?: string;
    id?: string;
    class?: string;
    oninput?: (value: number) => void;
    onchange?: (value: number) => void;
  };

  let {
    value = $bindable(0),
    min = 0,
    max = 100,
    step = 1,
    disabled = false,
    name,
    form,
    id,
    class: className = '',
    oninput,
    onchange,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLSpanElement>, keyof Own> = $props();

  let rail = $state<HTMLDivElement>();
  let labelledBy = $state<string>();

  const classes = $derived(['bb-slider', disabled ? 'bb-slider--disabled' : null, className || null].filter(Boolean).join(' '));
  const progress = $derived(sliderFraction(value, { min, max }));

  onMount(() => {
    const thumb = rail!.querySelector<HTMLElement>('[role="slider"]')!;
    if (!rest['aria-label'] && !rest['aria-labelledby']) labelledBy = sliderLabelIds(thumb);
    return mountSlider(rail!, {
      range: () => ({ min, max, step, disabled }),
      value: () => value,
      input(next) {
        value = next;
        oninput?.(next);
      },
      change: (next) => onchange?.(next),
    });
  });
</script>

<div bind:this={rail} class={classes} style:--slider-progress={progress} style:--slider-step={step}>
  {#if name}<input type="hidden" {name} {form} {value} />{/if}
  <span class="bb-slider__fill" aria-hidden="true"></span>
  <span class="bb-slider__thumb" role="slider" tabindex={disabled ? -1 : 0} {id}
    aria-valuemin={min} aria-valuemax={max} aria-valuenow={value} aria-disabled={disabled ? 'true' : undefined}
    aria-labelledby={labelledBy} {...rest}></span>
</div>
