<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/tooltip.css';
  import type { Snippet } from 'svelte';
  import { wireTooltip } from '../lib/tooltip';

  type Own = {
    text: string;
    placement?: 'top' | 'bottom';
    id?: string;
    class?: string;
    children: Snippet;
  };

  let {
    text,
    placement = 'top',
    id,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['span'], keyof Own> = $props();

  const classes = $derived(
    ['bb-tooltip', placement === 'bottom' ? 'bb-tooltip--bottom' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );

  const uid = $props.id();
  const bubbleId = $derived(id ?? `bb-tooltip-${uid}`);
  let root = $state<HTMLSpanElement>();
  let bubble = $state<HTMLSpanElement>();

  $effect(() => {
    if (root && bubble) return wireTooltip(root, bubble);
  });
</script>

<span class={classes} bind:this={root} {...rest}>{@render children()}<span
    class="bb-tooltip__bubble"
    id={bubbleId}
    role="tooltip"
    bind:this={bubble}>{text}</span
  ></span>
