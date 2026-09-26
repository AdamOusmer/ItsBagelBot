<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import '../styles/elements/counter-card.css';

  let {
    label, value, unit = '', detail = '', rate = null, rateUnit = '/s',
    rateLabel = 'Right now', period = 'All time', tone = 'green',
    appearance = 'soft', tilt = 'none', artwork, class: cls = '', ...rest
  }: {
    label: string;
    value: string;
    unit?: string;
    detail?: string;
    rate?: string | null;
    rateUnit?: string;
    rateLabel?: string;
    period?: string;
    tone?: 'green' | 'tan';
    appearance?: 'solid' | 'soft';
    tilt?: 'left' | 'right' | 'none';
    /** Decorative artwork can peek above the edge without clipping. */
    artwork?: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();
  const classes = $derived([
    'bb-counter-card', `bb-counter-card--${tone}`, `bb-counter-card--${appearance}`,
    tilt !== 'none' && `bb-counter-card--tilt-${tilt}`, cls,
  ].filter(Boolean).join(' '));
</script>

<article class={classes} aria-label={label} {...rest}>
  {#if artwork}<div class="bb-counter-card__artwork" aria-hidden="true">{@render artwork()}</div>{/if}
  <div class="bb-counter-card__body">
    <div class="bb-counter-card__head"><p class="bb-counter-card__label">{label}</p>{#if period}<p class="bb-counter-card__period">{period}</p>{/if}</div>
    <div class="bb-counter-card__measure"><strong class="bb-counter-card__value">{value}</strong>{#if unit}<span class="bb-counter-card__unit">{unit}</span>{/if}</div>
    {#if detail}<p class="bb-counter-card__detail">{detail}</p>{/if}
  </div>
  {#if rate !== null}<div class="bb-counter-card__foot"><span class="bb-counter-card__rate">{rate}{#if rateUnit}<small>{rateUnit}</small>{/if}</span>{#if rateLabel}<p class="bb-counter-card__rate-label">{rateLabel}</p>{/if}</div>{/if}
</article>
