<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/progress-bar.css';

  let {
    value,
    tone = 'neutral',
    label,
    size = 'md',
    class: className = '',
    ...rest
  }: {
    value: number | null;
    tone?: 'neutral' | 'success' | 'warning' | 'error';
    label: string;
    size?: 'sm' | 'md';
    class?: string;
    [key: string]: unknown;
  } = $props();

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
