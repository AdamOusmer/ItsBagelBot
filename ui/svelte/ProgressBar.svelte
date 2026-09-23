<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-progress` contract
  // (../styles/elements/progress-bar.css). Svelte only: see
  // SINGLE_ADAPTER_REASON in ../scripts/gen-catalog.mjs.
  //
  // aria-valuenow is a whole percent against a 0..100 range rather than the
  // raw fraction against 0..1: screen readers announce the number as given,
  // and "0.4666" is not something a person says. Indeterminate drops
  // aria-valuenow entirely, which is how ARIA spells "no value yet", and adds
  // aria-busy so the region reads as working rather than stalled at nothing.
  import '../styles/elements/progress-bar.css';

  let {
    value,
    tone = 'neutral',
    label,
    size = 'md',
    class: className = '',
    ...rest
  }: {
    /** Share done, 0..1. null = indeterminate (moving, no total yet). */
    value: number | null;
    tone?: 'neutral' | 'success' | 'warning' | 'error';
    /** Accessible name. The bar draws no text of its own. */
    label: string;
    size?: 'sm' | 'md';
    class?: string;
    [key: string]: unknown;
  } = $props();

  // A caller computing done/total hands in NaN for a stage whose total is
  // still 0, which is a normal state (a build with no jobs listed yet), not a
  // bug. It renders as an empty bar instead of `aria-valuenow="NaN"`.
  const clamp = (v: number) => (Number.isFinite(v) ? Math.min(1, Math.max(0, v)) : 0);

  const fraction = $derived(value === null ? null : clamp(value));

  const classes = $derived(
    [
      'bb-progress',
      `bb-progress--${tone}`,
      size === 'sm' ? 'bb-progress--sm' : null,
      fraction === null ? 'bb-progress--indeterminate' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div
  class={classes}
  role="progressbar"
  aria-label={label}
  aria-valuemin={0}
  aria-valuemax={100}
  aria-valuenow={fraction === null ? undefined : Math.round(fraction * 100)}
  aria-busy={fraction === null ? 'true' : undefined}
  style:--progress={fraction ?? undefined}
  {...rest}
><span class="bb-progress__fill"></span></div>
