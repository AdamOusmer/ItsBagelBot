<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/progress-bar.css';
  import type { ProgressTone } from '../lib/tone';

  type Own = {
    value: number | null;
    tone?: ProgressTone;
    label: string;
    size?: 'sm' | 'md';
    gradient?: boolean;
    ramp?: 1 | 2 | 3;
    target?: number;
    segments?: readonly (ProgressTone | null)[];
    current?: number;
    class?: string;
  };

  let {
    value,
    tone = 'neutral',
    label,
    size = 'md',
    gradient = false,
    ramp = 1,
    target,
    segments,
    current,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const clamp = (v: number) => (Number.isFinite(v) ? Math.min(1, Math.max(0, v)) : 0);

  const fraction = $derived(value === null ? null : clamp(value));
  const mark = $derived(target === undefined ? null : clamp(target));

  const classes = $derived(
    [
      'bb-progress',
      `bb-progress--${tone}`,
      size === 'sm' ? 'bb-progress--sm' : null,
      gradient ? 'bb-progress--gradient' : null,
      ramp > 1 ? `bb-progress--ramp-${ramp}` : null,
      fraction === null ? 'bb-progress--indeterminate' : null,
      mark === null ? null : 'bb-progress--target',
      segments ? 'bb-progress--segmented' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );

  const segmentClass = (segmentTone: string | null, index: number) =>
    [
      'bb-progress__seg',
      segmentTone ? `bb-progress__seg--${segmentTone}` : null,
      index === current ? 'is-current' : null,
    ]
      .filter(Boolean)
      .join(' ');
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
>{#if segments}{#each segments as segmentTone, index (index)}<span class={segmentClass(segmentTone, index)}></span>{/each}{:else}<span class="bb-progress__fill"></span>{#if mark !== null}<span class="bb-progress__target" style:--target={mark} aria-hidden="true"></span>{/if}{/if}</div>
