<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/elements/stat-tile.css';
  import type { Snippet } from 'svelte';
  import { countUp } from '../lib/count-up';
  import '../styles/elements/stat-tile.css';

  let {
    label,
    value,
    unit = '',
    delta,
    flat = false,
    static: isStatic = false,
    inline = false,
    tone,
    class: className = '',
    trail = undefined as Snippet | undefined,
    ...rest
  }: {
    label: string;
    value: string;
    unit?: string;
    delta?: string;
    flat?: boolean;
    static?: boolean;
    inline?: boolean;
    tone?: 'positive' | 'accent';
    class?: string;
    trail?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-stat',
      isStatic ? 'bb-stat--static' : null,
      inline ? 'bb-stat--inline' : null,
      tone ? `bb-stat--${tone}` : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {...rest}>
  <div class="bb-stat__head">
    <span class="bb-stat__label">{label}</span>
    {#if trail}{@render trail()}{/if}
  </div>
  <div class="bb-stat__value">{#if isStatic}<span>{value}</span>{:else}<span data-count-up use:countUp={{ value }}>{value}</span>{/if}{#if unit}<small>{unit}</small>{/if}</div>
  {#if delta !== undefined}<div class="bb-stat__delta" data-flat={flat ? '' : undefined}>{delta}</div>{/if}
</div>
