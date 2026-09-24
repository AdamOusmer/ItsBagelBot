<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/shell.css';
  import type { Snippet } from 'svelte';

  let {
    eyebrow,
    title,
    description,
    children,
    trail,
    compact = false,
    class: className = '',
    ...rest
  }: {
    eyebrow?: string;
    title?: string;
    description?: string;
    children?: Snippet;
    trail?: Snippet;
    compact?: boolean;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-page-head',
      trail ? 'bb-page-head--trailed' : null,
      compact ? 'bb-page-head--compact' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {...rest}
  ><div class="bb-page-head__main"
    >{#if eyebrow}<span class="bb-page-head__eyebrow">{eyebrow}</span>{/if}<h1 tabindex="-1">{#if children}{@render children()}{:else}{title}{/if}</h1
    >{#if description}<p class="bb-page-head__description">{description}</p>{/if}</div
  >{#if trail}<div class="bb-page-head__trail">{@render trail()}</div>{/if}</div
>
