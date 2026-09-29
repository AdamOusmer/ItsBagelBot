<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import '../styles/tags.css';
  import '../styles/elements/chip.css';

  let {
    on = false,
    onclick,
    type = 'button',
    tone = undefined,
    as: tag = 'button',
    class: cls = '',
    children,
    ...rest
  }: {
    on?: boolean;
    onclick?: () => void;
    type?: 'button' | 'submit' | 'reset';
    tone?: 'muted' | 'danger' | 'eyebrow' | 'free' | 'paid' | 'vip' | 'banned' | 'inactive';
    as?: 'button' | 'span';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();
</script>

<svelte:element
  this={tag}
  type={tag === 'button' ? type : undefined}
  class="bb-chip{tone ? ` bb-chip--${tone}` : ''}{cls ? ` ${cls}` : ''}"
  data-on={on ? '' : undefined}
  {onclick}
  {...rest}
>{@render children()}</svelte:element>
