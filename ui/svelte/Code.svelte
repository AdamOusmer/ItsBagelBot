<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    block = false,
    tone = undefined,
    class: className = '',
    children,
    ...rest
  }: {
    block?: boolean;
    tone?: 'danger';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-code', tone ? `bb-code--${tone}` : null, className || null].filter(Boolean).join(' '),
  );
</script>

{#if block}<pre class="bb-code-block" {...rest}><code class={classes}>{@render children()}</code></pre>{:else}<code
    class={classes}
    {...rest}>{@render children()}</code
  >{/if}
