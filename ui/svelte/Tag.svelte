<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/tags.css';
  import type { Snippet } from 'svelte';

  let {
    tone = undefined,
    mark = undefined,
    sweep = false,
    status = false,
    as: tag = 'span',
    class: className = '',
    children,
    ...rest
  }: {
    tone?: 'quiet' | 'live' | 'alpha' | 'pre' | 'incoming' | 'bare' | 'error';
    mark?: 'solid' | 'hollow' | 'dash' | 'up' | 'plus';
    sweep?: boolean;
    status?: boolean;
    as?: 'span' | 'small' | 'div';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-tag', tone ? `bb-tag--${tone}` : null, className || null].filter(Boolean).join(' '),
  );
</script>

<svelte:element this={tag} class={classes} role={status ? 'status' : undefined} {...rest}
  >{#if mark}<i
      class="bb-mark{mark === 'solid' ? '' : ` bb-mark--${mark}`}"
      aria-hidden="true"
    ></i>{/if}{@render children()}{#if sweep}<i class="bb-sweep" aria-hidden="true"></i>{/if}</svelte:element
>
