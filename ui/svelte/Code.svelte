<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-code` (../styles/elements/typography.css).
  // Astro twin: ../astro/Code.astro.
  //
  // `block` wraps the <code> in a <pre>, which is the only correct way to get
  // a code BLOCK: `white-space: pre` on a bare <code> preserves the newlines
  // but not the element semantics, and a <pre> without a <code> inside it is
  // preformatted text that happens to be code-shaped. The pair is what
  // assistive tech and every syntax highlighter expect.
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    block = false,
    class: className = '',
    children,
    ...rest
  }: {
    /** Renders inside a <pre>, for multi-line code. */
    block?: boolean;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-code', className || null].filter(Boolean).join(' '));
</script>

{#if block}<pre class="bb-code-block" {...rest}><code class={classes}>{@render children()}</code></pre>{:else}<code
    class={classes}
    {...rest}>{@render children()}</code
  >{/if}
