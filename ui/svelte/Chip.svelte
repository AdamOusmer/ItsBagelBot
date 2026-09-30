<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/chip.css';
  import type { Snippet } from 'svelte';

  type Own = {
    pressed?: boolean;
    type?: 'button' | 'submit' | 'reset';
    tone?: 'muted' | 'danger' | 'eyebrow' | 'free' | 'paid' | 'vip' | 'banned' | 'inactive';
    as?: 'button' | 'span';
    class?: string;
    children: Snippet;
  };

  let {
    pressed,
    type = 'button',
    tone = undefined,
    as: tag = 'button',
    class: cls = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['button'], keyof Own> = $props();
</script>

<svelte:element
  this={tag}
  type={tag === 'button' ? type : undefined}
  class="bb-chip{tone ? ` bb-chip--${tone}` : ''}{cls ? ` ${cls}` : ''}"
  aria-pressed={tag === 'button' && pressed !== undefined ? pressed : undefined}
  data-pressed={pressed ? '' : undefined}
  {...rest}
>{@render children()}</svelte:element>
