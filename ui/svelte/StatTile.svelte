<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/stat-tile.css';
  import type { Snippet } from 'svelte';
  import type { StatTone } from '../lib/tone';
  import { countUp } from '../lib/count-up';

  type Own = {
    label: string;
    value: string;
    unit?: string;
    delta?: string;
    flat?: boolean;
    static?: boolean;
    inline?: boolean;
    tone?: StatTone;
    class?: string;
    trailing?: Snippet;
  };

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
    trailing = undefined as Snippet | undefined,
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

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
    {#if trailing}{@render trailing()}{/if}
  </div>
  <div class="bb-stat__value">{#if isStatic}<span>{value}</span>{:else}<span data-count-up use:countUp={{ value }}>{value}</span>{/if}{#if unit}<small>{unit}</small>{/if}</div>
  {#if delta !== undefined}<div class="bb-stat__delta" data-flat={flat ? '' : undefined}>{delta}</div>{/if}
</div>
