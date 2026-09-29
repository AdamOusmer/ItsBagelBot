<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/fact-list.css';
  import type { Snippet } from 'svelte';

  let {
    term,
    tone,
    mono = false,
    wide = false,
    truncate = false,
    class: className = '',
    children,
    ...rest
  }: {
    term: string;
    tone?: 'danger';
    mono?: boolean;
    wide?: boolean;
    truncate?: boolean;
    class?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-fact',
      tone ? `bb-fact--${tone}` : null,
      mono ? 'bb-fact--mono' : null,
      wide ? 'bb-fact--wide' : null,
      truncate ? 'bb-fact--truncate' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {...rest}><dt class="bb-fact__term">{term}</dt><dd class="bb-fact__value">{#if children}{@render children()}{/if}</dd></div>
