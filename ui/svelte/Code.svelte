<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';
  import type { CodeTone } from '../lib/tone';

  type Own = {
    block?: boolean;
    wrap?: boolean;
    maxHeight?: string;
    tone?: CodeTone;
    class?: string;
    children: Snippet;
  };

  let {
    block = false,
    wrap = false,
    maxHeight,
    tone = undefined,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    ['bb-code', tone ? `bb-code--${tone}` : null, className || null].filter(Boolean).join(' '),
  );
  const blockClasses = $derived(wrap ? 'bb-code-block bb-code-block--wrap' : 'bb-code-block');
  const blockStyle = $derived(maxHeight ? `max-height:${maxHeight}` : undefined);
</script>

{#if block}<pre class={blockClasses} style={blockStyle} {...rest}><code class={classes}>{@render children()}</code></pre>{:else}<code
    class={classes}
    {...rest}>{@render children()}</code
  >{/if}
